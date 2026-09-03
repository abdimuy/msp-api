//nolint:misspell // visitas vocabulary is Spanish per project convention.
package visitashttp

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/eventbus"
)

// TopicVisitasConfirmadas is the eventbus topic the visitas stream serves.
//
// It is deliberately NOT one of cobranza's topics, and the stream it feeds is
// deliberately NOT one of cobranza's two streams. The reason is measured, not
// stylistic: the shipped Android client
// (CobranzaSseSubscriber.kt:324-333 and :381-390 in msp-app-kt) reads the SSE
// event NAME into a log string and then discards it — it decides what an event
// means from WHICH SOCKET the bytes arrived on, never from what the server
// named the event. A second event type multiplexed onto the pagos stream would
// therefore not break old phones; it would make every one of them run a full
// cobranza cursor sync on every visit confirmed anywhere in the fleet.
//
// Serving visitas on its own route makes old-client impact zero by
// construction rather than by parser tolerance: those phones open exactly two
// hardcoded URLs and will never request this third one.
const TopicVisitasConfirmadas = "visitas_confirmadas"

// sseEventName is the SSE `event:` field written on this stream.
//
// Same string as the topic, but they are separate constants on purpose: the
// topic is an in-process routing key and the event name is a wire contract
// with the app. They are free to diverge later without one silently dragging
// the other along.
const sseEventName = "visitas_confirmadas"

// defaultPingEvery is the keep-alive interval used when config leaves
// SSEPingEvery unset or non-positive.
const defaultPingEvery = 25 * time.Second

// sseHandler serves the visitas SSE stream.
//
// Raw chi rather than Huma, for the same reason cobranza's is: Huma has no
// streaming response model. Every other visitas route stays on Huma.
type sseHandler struct {
	bus    *eventbus.Bus
	cfg    config.Visitas
	logger *slog.Logger
}

// newSSEHandler constructs an sseHandler.
func newSSEHandler(bus *eventbus.Bus, cfg config.Visitas, logger *slog.Logger) *sseHandler {
	return &sseHandler{bus: bus, cfg: cfg, logger: logger}
}

// streamVisitas handles GET /visitas/sync/visitas/zona/{zona_id}/stream.
//
// The event is a WAKEUP, not an authority. It carries no visit ids and the
// client is required to mark nothing on receipt: it wakes the reconciler,
// which then confirms through GET /visitas/by-ids exactly as it does for the
// other three triggers (app open, the 15-minute periodic worker, connectivity
// restored). Task 10 established exactly one write path to "synced" — positive
// confirmation — and a push event that marked rows by itself would be a second
// path with weaker evidence.
//
// That is also why the payload is empty: there is nothing for it to carry.
// eventbus treats an empty []int as a valid "something changed, re-sync"
// signal, which is precisely the semantics wanted here, and it means the bus
// keeps its []int payload type even though visit ids are UUIDs.
//
// Fan-out is fleet-wide and that is accepted: the bus topic is process-global,
// so every connected phone wakes. It self-limits, because a phone with no
// pending visits issues no by-ids request at all — only phones that actually
// have something to reconcile generate traffic.
func (h *sseHandler) streamVisitas(w http.ResponseWriter, r *http.Request) {
	flusher, zonaID, ok := h.authorizeSSE(w, r)
	if !ok {
		return
	}

	// Disable the *http.Server WriteTimeout for this connection. Without it
	// the server closes the TCP socket on schedule and the app sees a drop
	// every WriteTimeout seconds. The middleware.Timeout that wraps this
	// handler is already skipped by path suffix (isStreamingPath in
	// internal/platform/middleware) — this is the other half of the same
	// problem, on the server rather than the router.
	if rc := http.NewResponseController(w); rc != nil {
		if err := rc.SetWriteDeadline(time.Time{}); err != nil {
			h.logger.WarnContext(r.Context(), "visitas.sse_set_deadline_failed",
				slog.String("error", err.Error()))
		}
	}

	ch, unsubscribe := h.bus.Subscribe(TopicVisitasConfirmadas)
	defer unsubscribe()

	h.logger.InfoContext(r.Context(), "visitas.sse_connected", slog.Int("zona_id", zonaID))
	defer h.logger.InfoContext(r.Context(), "visitas.sse_disconnected", slog.Int("zona_id", zonaID))

	h.streamLoop(w, r, flusher, ch, zonaID)
}

// authorizeSSE validates the feature flag, auth, permission, zona_id and
// flusher support, then writes the SSE response headers. On any failure it
// writes an error response and returns ok=false.
//
// Permission is PermCobranzaVerPagos — the same one the rest of the visitas
// module requires (see VisitasExistentesPorIDs). A cobrador who may record a
// visita and confirm which ones landed may also be told when to go ask.
//
// Like cobranza's stream, zona_id is validated and logged but does not narrow
// what is delivered: auth.CurrentUser has no per-zona membership concept, and
// the bus topic is global. It is a client-scoping and observability parameter,
// not an authorization boundary — and it carries no leak, because the event
// carries no data.
func (h *sseHandler) authorizeSSE(w http.ResponseWriter, r *http.Request) (http.Flusher, int, bool) {
	if !h.cfg.SSEEnabled {
		writePlainErrorVisitas(w, http.StatusServiceUnavailable, "sse_deshabilitado",
			"SSE no está habilitado en este servidor")
		return nil, 0, false
	}

	cu, ok := auth.CurrentUserFromContext(r.Context())
	if !ok {
		writePlainErrorVisitas(w, http.StatusUnauthorized, "no_autenticado", "no autenticado")
		return nil, 0, false
	}
	if err := requirePerm(cu, auth.PermCobranzaVerPagos); err != nil {
		writePlainErrorVisitas(w, http.StatusForbidden, "permiso_denegado", "permiso denegado")
		return nil, 0, false
	}

	zonaID, err := strconv.Atoi(chi.URLParam(r, "zona_id"))
	if err != nil || zonaID <= 0 {
		writePlainErrorVisitas(w, http.StatusBadRequest, "zona_id_invalida", "zona_id inválido")
		return nil, 0, false
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writePlainErrorVisitas(w, http.StatusInternalServerError, "no_flusher",
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

// streamLoop writes events and keep-alive pings until the client disconnects
// or the bus closes.
//
// Wire shape: `event: visitas_confirmadas` + `data: {"ts":N}` where ts is
// millis since epoch UTC, used by the client only to measure transit time.
// There is no `ids` field — see streamVisitas' doc for why the event carries
// no payload.
func (h *sseHandler) streamLoop(
	w http.ResponseWriter,
	r *http.Request,
	flusher http.Flusher,
	ch <-chan []int,
	zonaID int,
) {
	pingInterval := h.cfg.SSEPingEvery
	if pingInterval <= 0 {
		pingInterval = defaultPingEvery
	}
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case _, open := <-ch:
			if !open {
				// Bus closed; terminate the stream gracefully.
				return
			}
			tsMs := time.Now().UnixMilli()
			if _, err := fmt.Fprintf(w, "event: %s\ndata: {\"ts\":%d}\n\n", sseEventName, tsMs); err != nil {
				return
			}
			flusher.Flush()
			h.logger.DebugContext(
				r.Context(), "visitas.sse_event_pushed",
				slog.Int("zona_id", zonaID),
				slog.Int64("ts_ms", tsMs),
			)
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// mountVisitasSSE registers the streaming endpoint on r. Raw chi, not Huma.
//
// The path ends in "/stream", which is not cosmetic: middleware.isStreamingPath
// exempts exactly that suffix from the request Timeout middleware. A route
// named otherwise would be cancelled on schedule mid-stream.
func mountVisitasSSE(r chi.Router, h *sseHandler) {
	r.Get("/visitas/sync/visitas/zona/{zona_id}/stream", h.streamVisitas)
}

// writePlainErrorVisitas writes the same flat JSON error envelope Huma
// produces, for the raw-chi routes that never reach Huma's error mapper.
func writePlainErrorVisitas(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `{"status":`+strconv.Itoa(status)+
		`,"title":"`+http.StatusText(status)+
		`","detail":"`+message+
		`","errors":[{"message":"code=`+code+`"}]}`)
}
