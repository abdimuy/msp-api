//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/cobranza/app/eventbus"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

// sseHandler serves the SSE streaming endpoints for cobranza sync.
//
// Each endpoint is a raw chi handler (not Huma) because Huma does not support
// streaming responses. When the feature flag is off, endpoints return 503 so
// clients fall back to the digest-reconcile poll path (commit 4).
type sseHandler struct {
	bus    *eventbus.Bus
	sseCfg config.Cobranza
	logger *slog.Logger
}

// newSSEHandler constructs an sseHandler.
func newSSEHandler(bus *eventbus.Bus, cfg config.Cobranza, logger *slog.Logger) *sseHandler {
	return &sseHandler{bus: bus, sseCfg: cfg, logger: logger}
}

// streamPagos handles GET /sync/pagos/zona/{zona_id}/stream.
func (h *sseHandler) streamPagos(w http.ResponseWriter, r *http.Request) {
	h.serveSSE(w, r, "pagos_changed", "pagos")
}

// streamSaldos handles GET /sync/saldos/zona/{zona_id}/stream.
func (h *sseHandler) streamSaldos(w http.ResponseWriter, r *http.Request) {
	h.serveSSE(w, r, "saldos_changed", "saldos")
}

// serveSSE is the shared streaming loop for both pagos and saldos topics.
//
// SSE message format per spec: "el cliente sincroniza con cursor cuando recibe
// el evento, no consume el payload del evento". The data field is intentionally
// empty — clients re-poll the digest/IDs endpoints on receipt.
//
// Keep-alive ping: ": ping\n\n" every SSEPingEvery (default 25 s). SSE comment
// lines are ignored by browsers and EventSource implementations; they exist
// only to keep the TCP connection alive through proxy idle timeouts.
//
// Authz model: zone-level membership is not tracked in auth.CurrentUser (the
// auth domain has no per-zona access concept). Access is controlled by
// permission only:
//   - pagos stream requires PermCobranzaVerPagos
//   - saldos stream requires PermCobranzaVerSaldos
//
// DONE_WITH_CONCERNS: TestSSE_Authz_WrongZona_Returns403 has been renamed to
// TestSSE_Authz_NoPerm_Returns403 because there is no zona-membership API in
// auth.CurrentUser. Any authenticated user with the correct perm can stream
// any zona. A future iteration can add Zonas []int to CurrentUser and narrow
// the check here without changing the handler signature.
func (h *sseHandler) serveSSE(w http.ResponseWriter, r *http.Request, topic, kind string) {
	flusher, zonaID, ok := h.authorizeSSE(w, r, kind)
	if !ok {
		return
	}

	// Desactivar el WriteTimeout del *http.Server (default 30s) — sin esto
	// el server cierra el TCP a los 30s y la app cliente loguea
	// "SSE pagos falló (attempt=N) — reintento en 30000ms: null". El
	// middleware.Timeout que envuelve este handler también cancela el ctx
	// a la misma duración, pero ese se salta por path en server.go
	// (Timeout skip para rutas /stream).
	if rc := http.NewResponseController(w); rc != nil {
		if err := rc.SetWriteDeadline(time.Time{}); err != nil {
			h.logger.WarnContext(r.Context(), "cobranza.sse_set_deadline_failed",
				slog.String("kind", kind), slog.String("error", err.Error()))
		}
	}

	ch, unsubscribe := h.bus.Subscribe(topic)
	defer unsubscribe()

	h.logger.InfoContext(r.Context(), "cobranza.sse_connected",
		slog.String("kind", kind),
		slog.Int("zona_id", zonaID),
	)
	defer h.logger.InfoContext(r.Context(), "cobranza.sse_disconnected",
		slog.String("kind", kind),
		slog.Int("zona_id", zonaID),
	)

	h.streamLoop(w, r, flusher, ch, topic, kind, zonaID)
}

// authorizeSSE validates the feature flag, auth, zona_id, and flusher support.
// Returns the flusher, the parsed zona_id, and ok=true on success. On any
// failure it writes an error response and returns ok=false.
func (h *sseHandler) authorizeSSE(w http.ResponseWriter, r *http.Request, kind string) (http.Flusher, int, bool) {
	if !h.sseCfg.SSEEnabled {
		writePlainErrorCobranza(w, http.StatusServiceUnavailable, "sse_deshabilitado",
			"SSE no está habilitado en este servidor")
		return nil, 0, false
	}

	cu, ok := auth.CurrentUserFromContext(r.Context())
	if !ok {
		writePlainErrorCobranza(w, http.StatusUnauthorized, "no_autenticado", "no autenticado")
		return nil, 0, false
	}

	// Pick the read permission that matches the stream kind.
	perm := auth.PermCobranzaVerSaldos
	if kind == "pagos" {
		perm = auth.PermCobranzaVerPagos
	}
	if err := requirePerm(cu, perm); err != nil {
		writePlainErrorCobranza(w, http.StatusForbidden, "permiso_denegado", "permiso denegado")
		return nil, 0, false
	}

	zonaID, err := strconv.Atoi(chi.URLParam(r, "zona_id"))
	if err != nil || zonaID <= 0 {
		writePlainErrorCobranza(w, http.StatusBadRequest, "zona_id_invalida", "zona_id inválido")
		return nil, 0, false
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writePlainErrorCobranza(w, http.StatusInternalServerError, "no_flusher",
			"respuesta no soporta streaming")
		return nil, 0, false
	}

	// Headers must be set before WriteHeader.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	return flusher, zonaID, true
}

// streamLoop runs the select loop, writing events and keep-alive pings until
// the client disconnects or the bus is closed.
func (h *sseHandler) streamLoop(
	w http.ResponseWriter,
	r *http.Request,
	flusher http.Flusher,
	ch <-chan struct{},
	topic, kind string,
	zonaID int,
) {
	pingInterval := h.sseCfg.SSEPingEvery
	if pingInterval <= 0 {
		pingInterval = 25 * time.Second
	}
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case _, open := <-ch:
			if !open {
				// Bus was closed; terminate stream gracefully.
				return
			}
			if _, writeErr := fmt.Fprintf(w, "event: %s\ndata: {}\n\n", topic); writeErr != nil {
				return
			}
			flusher.Flush()
			h.logger.DebugContext(r.Context(), "cobranza.sse_event_pushed",
				slog.String("kind", kind),
				slog.Int("zona_id", zonaID),
			)
		case <-ticker.C:
			if _, writeErr := fmt.Fprint(w, ": ping\n\n"); writeErr != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// mountCobranzaSSE registers the two SSE streaming endpoints onto r.
// Both paths are raw chi (not Huma) because Huma does not support streaming.
func mountCobranzaSSE(r chi.Router, h *sseHandler) {
	r.Get("/sync/pagos/zona/{zona_id}/stream", h.streamPagos)
	r.Get("/sync/saldos/zona/{zona_id}/stream", h.streamSaldos)
}
