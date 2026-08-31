package canalhttp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	canalapp "github.com/abdimuy/msp-api/internal/canal/app"
	canaldomain "github.com/abdimuy/msp-api/internal/canal/domain"
	canaloutbound "github.com/abdimuy/msp-api/internal/canal/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// webhookPath is Meta's WhatsApp webhook endpoint, mounted on raw chi (not
// Huma) so nothing between net/http and this handler decodes and re-encodes
// the request body — see verifySignature's doc comment.
const webhookPath = "/canal/v1/webhook/whatsapp"

// signatureHeader carries Meta's HMAC-SHA256 signature over the raw POST
// body, formatted "sha256=<hex>".
const signatureHeader = "X-Hub-Signature-256"

// signaturePrefix precedes the hex digest in signatureHeader.
const signaturePrefix = "sha256="

// hubModeSubscribe is the only hub.mode value the GET challenge answers.
const hubModeSubscribe = "subscribe"

// tipoTexto is the WhatsApp message type carrying a free-form text body.
const tipoTexto = "text"

// webhookHandler holds the raw chi handlers for Meta's webhook. Kept
// separate from the Huma Handlers struct (registerSalud/registerSalientes)
// because this one deliberately never touches Huma's request pipeline.
type webhookHandler struct {
	svc    *canalapp.Service
	clock  canaloutbound.Clock
	cfg    Config
	logger *slog.Logger
}

// mountWebhook registers Meta's webhook GET/POST directly on r via chi,
// bypassing Huma entirely. Call this on the same chi.Router passed to
// humachi.New so both surfaces share one mux — chi dispatches by method,
// so GET and POST on the same path coexist without conflict.
//
// The caller (Task 7's composition root, when it wires cmd/api/server.go)
// must not attach any middleware upstream of this path that reads the
// request body — a body-size limiter that only wraps the reader
// (http.MaxBytesReader) is fine; anything that decodes and re-encodes JSON,
// or otherwise consumes and replaces r.Body, invalidates the HMAC check
// below before it ever runs.
func mountWebhook(r chi.Router, h *webhookHandler) {
	r.Get(webhookPath, h.handleChallenge)
	r.Post(webhookPath, h.handleReceive)
}

// handleChallenge answers Meta's GET subscription challenge: when
// hub.mode=subscribe and hub.verify_token matches (compared with
// hmac.Equal, not ==), it echoes hub.challenge verbatim as text/plain.
// Anything else is a 403 with no body — an attacker gets no signal about
// which half (mode or token) was wrong.
func (h *webhookHandler) handleChallenge(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	mode := q.Get("hub.mode")
	token := q.Get("hub.verify_token")
	challenge := q.Get("hub.challenge")

	if mode != hubModeSubscribe || !secureCompare(token, h.cfg.VerifyToken) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	// Echoing hub.challenge verbatim is Meta's own subscription-verification
	// protocol, not user-controlled content rendered as markup — this
	// response is text/plain, never interpreted as HTML/JS by anything.
	_, _ = io.WriteString(w, challenge) //nolint:gosec // G705: intentional verbatim echo of Meta's own challenge value, text/plain.
}

// handleReceive is Meta's inbound-message delivery.
//
// 🔴 The body is read once, as raw bytes, and the signature is verified
// against exactly those bytes before anything parses them as JSON — Meta
// escapes Unicode on its side, so a body that went through any
// decode/re-encode step would no longer match the signature it computed.
// A missing or invalid signature is 403 with no body.
//
// Past the signature check, the status answered depends on WHY a message
// failed, not just whether one did: a body that parses but whose content
// domain.NewMensajeEntrante rejects (a VALIDATION failure) still answers
// 200 — retrying an unparseable/invalid payload cannot help, so a non-2xx
// here would only teach Meta to keep redelivering something that will
// never succeed. But a message that validates and still fails to persist
// (a STORAGE failure — SQLITE_BUSY from an external reader, a disk-full
// window, a transient I/O error) answers a non-2xx instead: that failure
// is overwhelmingly transient, Meta's own retry would very plausibly
// succeed, and redelivery is provably safe either way — Guardar is
// `ON CONFLICT (wamid) DO NOTHING` and RecibirMensaje reports
// inserted=false on a redelivered wamid already in the mailbox. Answering
// 200 on a storage failure is the worse mistake: no mailbox row exists, so
// GET /canal/v1/salud reports pendientes: 0 and looks healthy while the
// reply was silently destroyed — the only trace was an ERROR log line, on
// a box whose entire purpose is not losing these messages. See
// procesarEntrantes/procesarMensaje for how each message in a delivery
// batch is classified; if a batch has multiple messages, one storage
// failure among them makes the whole batch answer non-2xx (redelivering
// the ones already persisted is a harmless no-op, per Guardar's
// idempotency above).
func (h *webhookHandler) handleReceive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if !verifySignature(h.cfg.AppSecret, body, r.Header.Get(signatureHeader)) {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	if !h.procesarEntrantes(r.Context(), body) {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// verifySignature reports whether header is a valid "sha256=<hex>"
// HMAC-SHA256 signature of body under secret. An empty secret always fails
// closed — see secureCompare's doc comment for why that matters.
func verifySignature(secret string, body []byte, header string) bool {
	if secret == "" {
		return false
	}
	hexDigest, ok := strings.CutPrefix(header, signaturePrefix)
	if !ok {
		return false
	}
	got, err := hex.DecodeString(hexDigest)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body) // hash.Hash.Write never returns a non-nil error.
	want := mac.Sum(nil)
	return hmac.Equal(got, want)
}

// wireEntrada is the top-level shape of Meta's WhatsApp webhook payload.
// Only the parts canal's mailbox needs are modeled; anything else (e.g. a
// "statuses" delivery-receipt array instead of "messages") is silently
// ignored by virtue of not being read.
type wireEntrada struct {
	Entry []wireEntry `json:"entry"`
}

// wireEntry is one WABA's worth of changes in a webhook delivery.
type wireEntry struct {
	Changes []wireChange `json:"changes"`
}

// wireChange is one field change; canal only cares about ones that carry
// inbound messages.
type wireChange struct {
	Value wireValue `json:"value"`
}

// wireValue carries the destination phone_number_id and the inbound
// messages themselves.
type wireValue struct {
	Metadata wireMetadata  `json:"metadata"`
	Messages []wireMessage `json:"messages"`
}

// wireMetadata carries which of the store's WhatsApp numbers received the
// message.
type wireMetadata struct {
	PhoneNumberID string `json:"phone_number_id"`
}

// wireMessage is one inbound message, decoded loosely as a field map: Meta
// nests the type-specific payload (text.body, image.id, document.id, ...)
// under a key matching the message's own "type" field, so a fixed struct
// would need one field per message type canal does not otherwise care
// about. contenido extracts whichever of those this mailbox understands.
type wireMessage map[string]json.RawMessage

// str returns the string field key, or "" if absent or not a JSON string.
func (m wireMessage) str(key string) string {
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

// contenido extracts the message body for a "text" message, or the media
// id for any other type that nests one under its own type name (image,
// document, audio, video, sticker). Types that carry neither (e.g.
// location, button, interactive) yield "" — MensajeEntrante's Contenido is
// the one field domain allows to be empty, precisely so a message type the
// mailbox does not deeply understand is still relayed rather than
// rejected.
func (m wireMessage) contenido(tipo string) string {
	if tipo == tipoTexto {
		raw, ok := m[tipoTexto]
		if !ok {
			return ""
		}
		var body struct {
			Body string `json:"body"`
		}
		_ = json.Unmarshal(raw, &body)
		return body.Body
	}
	raw, ok := m[tipo]
	if !ok {
		return ""
	}
	var ref struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &ref)
	return ref.ID
}

// procesarEntrantes parses body as Meta's webhook payload and hands every
// inbound message it finds to the service, returning false the moment ANY
// of them hit a STORAGE-class failure (see handleReceive's doc comment for
// why that must not answer 200). A malformed body is a permanent failure
// on its own — logged, not persisted, and does not affect the return
// value — and every message found is still attempted even after an earlier
// one in the same batch failed to persist, so a single storage hiccup
// never silently drops the rest of the batch too.
func (h *webhookHandler) procesarEntrantes(ctx context.Context, body []byte) bool {
	var payload wireEntrada
	if err := json.Unmarshal(body, &payload); err != nil {
		h.logger.WarnContext(ctx, "canalhttp.webhook_payload_invalido", slog.String("error", err.Error()))
		return true
	}
	ok := true
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				if !h.procesarMensaje(ctx, change.Value.Metadata.PhoneNumberID, msg) {
					ok = false
				}
			}
		}
	}
	return ok
}

// procesarMensaje builds a NewMensajeEntranteParams from one wireMessage and
// hands it to the service, reporting whether handleReceive may still
// answer 200 for the batch this message belongs to.
//
// A malformed timestamp never reaches the service at all — it is the same
// kind of permanent, un-retryable failure as a domain validation
// rejection, so it is logged and reported true (200-safe). Once
// RecibirMensaje is called, its error is classified via apperror.As: a
// KindValidation failure (domain.NewMensajeEntrante rejected the payload)
// is genuinely permanent and reported true; anything else — a storage
// failure or an error apperror.As cannot even classify — is reported
// false, so the caller answers Meta a non-2xx and gets a redelivery.
func (h *webhookHandler) procesarMensaje(ctx context.Context, phoneNumberID string, msg wireMessage) bool {
	tipo := msg.str("type")
	wamid := msg.str("id")

	ts, err := parseMetaTimestamp(msg.str("timestamp"))
	if err != nil {
		h.logger.WarnContext(ctx, "canalhttp.webhook_timestamp_invalido",
			slog.String("wamid", wamid), slog.String("error", err.Error()))
		return true
	}

	params := canaldomain.NewMensajeEntranteParams{
		Wamid:         wamid,
		Remitente:     msg.str("from"),
		PhoneNumberID: phoneNumberID,
		Tipo:          tipo,
		Contenido:     msg.contenido(tipo),
		TimestampMeta: ts,
		RecibidoEn:    h.clock.Now(),
	}
	_, err = h.svc.RecibirMensaje(ctx, params)
	if err == nil {
		return true
	}
	if esFalloDeValidacion(err) {
		h.logger.WarnContext(ctx, "canalhttp.webhook_validacion_fallida",
			slog.String("wamid", wamid), slog.String("error", err.Error()))
		return true
	}
	h.logger.ErrorContext(ctx, "canalhttp.webhook_persistencia_fallida",
		slog.String("wamid", wamid), slog.String("error", err.Error()))
	return false
}

// esFalloDeValidacion reports whether err is a KindValidation apperror —
// domain.NewMensajeEntrante rejecting the payload, the one RecibirMensaje
// failure that redelivery cannot fix. An error apperror.As cannot classify
// at all is treated the same as a storage failure (false): "unclassified"
// must fail safe toward a redelivery, never toward silently swallowing a
// message.
func esFalloDeValidacion(err error) bool {
	ae, ok := apperror.As(err)
	return ok && ae.Kind == apperror.KindValidation
}

// parseMetaTimestamp parses Meta's message timestamp — a decimal string of
// Unix seconds — into UTC.
func parseMetaTimestamp(s string) (time.Time, error) {
	secs, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("canalhttp: parse meta timestamp %q: %w", s, err)
	}
	return time.Unix(secs, 0).UTC(), nil
}
