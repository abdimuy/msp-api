// Package failedintenthttp provides the admin HTTP transport for the
// failedintent platform package: handlers, DTOs, and the chi router mount.
package failedintenthttp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// errCursorFormat is the sentinel returned by decodeCursor when the decoded
// payload does not split into the expected two parts.
var errCursorFormat = errors.New("decodeCursor: unexpected format")

// IntentDTO is the JSON projection of a failedintent.Intent.
type IntentDTO struct {
	ID         string `json:"id"`
	ReceivedAt string `json:"received_at"`
	// LastSeenAt es el último intento con la misma clave. Se omite cuando la
	// fila se ha visto una sola vez — el consumidor lee esa ausencia como
	// "el último intento ES received_at". Rellenarlo con received_at
	// afirmaría un segundo intento que no ocurrió.
	LastSeenAt      *string `json:"last_seen_at,omitempty"`
	Method          string  `json:"method"`
	Path            string  `json:"path"`
	FirebaseUID     string  `json:"firebase_uid,omitempty"`
	UsuarioID       *string `json:"usuario_id,omitempty"`
	IdempotencyKey  string  `json:"idempotency_key,omitempty"`
	RequestID       string  `json:"request_id"`
	Body            any     `json:"body"`
	BodyTruncated   bool    `json:"body_truncated"`
	HasBlob         bool    `json:"has_blob"`
	BodyContentType string  `json:"body_content_type,omitempty"`
	HTTPStatus      int     `json:"http_status"`
	ErrorCode       string  `json:"error_code,omitempty"`
	ErrorMessage    string  `json:"error_message,omitempty"`
	RetryCount      int     `json:"retry_count"`
	Status          string  `json:"status"`
	ResolvedAt      *string `json:"resolved_at,omitempty"`
	ResolvedBy      *string `json:"resolved_by,omitempty"`
	Notes           string  `json:"notes,omitempty"`
	// Modulo es el módulo dueño de la ruta ('ventas', 'pagos'). Se omite
	// cuando el servidor no lo extrajo — el escritorio lo degrada entonces a
	// deducirlo de la ruta, como hacía antes.
	//
	// Que venga del servidor es lo que permite añadir un módulo sin tocar el
	// escritorio: basta registrar su extractor en cmd/api.
	Modulo string `json:"modulo,omitempty"`
	// Resumen es quién y cuánto. Se omite cuando no se pudo extraer, y esa
	// ausencia es un dato: la tarjeta se degrada a mostrar la referencia en
	// vez de inventar un nombre.
	Resumen *ResumenDTO `json:"resumen,omitempty"`
}

// ResumenDTO es la proyección JSON del failedintent.Resumen: el dato de
// negocio que hace legible un renglón de la pantalla.
//
// Viaja aquí, en el LISTADO, y no se pide por renglón. Existe
// GET /{id}/blob-parts y sería tentador usarlo para sacar el nombre de cada
// tarjeta, pero eso convierte la ruta más caliente de la pantalla en un N+1
// sobre el disco. El resumen se extrae una vez, al capturar, y viaja en la
// fila.
type ResumenDTO struct {
	// Titulo es el nombre del cliente en ventas; en pagos, el del cobrador —
	// el cuerpo de un pago no trae el nombre del cliente.
	Titulo string `json:"titulo,omitempty"`
	// Monto es una CADENA decimal, igual que en el resto del contrato. Un
	// número JSON pasaría el importe por un float64 de ida y de vuelta.
	Monto string `json:"monto,omitempty"`
	// Referencia es el ancla para encontrar el trabajo: el id de la venta, el
	// del cliente en un pago.
	Referencia string `json:"referencia,omitempty"`
}

// ListResponse is the cursor-paginated envelope returned by the list endpoint.
// NextCursor is the empty string when there are no more pages.
type ListResponse struct {
	Items      []IntentDTO `json:"items"`
	NextCursor string      `json:"next_cursor"`
	HasMore    bool        `json:"has_more"`
}

// ResolveRequest is the body accepted by PATCH /{id}/resolve.
type ResolveRequest struct {
	Status string `json:"status"`
	Notes  string `json:"notes"`
}

// ReplayResponse is the body returned by POST /{id}/replay and POST /{id}/replay-with.
type ReplayResponse struct {
	Outcome           string `json:"outcome"`
	ReplayHTTPStatus  int    `json:"replay_http_status"`
	ReplayBodyPreview string `json:"replay_body_preview"`
}

// ReplayWithRequest is the body accepted by POST /{id}/replay-with.
// Body is the corrected request payload to use instead of the captured one.
type ReplayWithRequest struct {
	Body json.RawMessage `json:"body"`
}

// BlobPartDTO is the JSON projection of a failedintent.BlobPart, returned
// by GET /{id}/blob-parts. The `value` field is base64-encoded so any byte
// sequence survives the JSON round-trip; the UI decodes it when rendering
// inline (text fields).
type BlobPartDTO struct {
	Index       int    `json:"index"`
	Name        string `json:"name,omitempty"`
	Kind        string `json:"kind"`
	ContentType string `json:"content_type"`
	Filename    string `json:"filename,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
	// Value is base64(stdlib base64.StdEncoding). Present iff Kind=="field".
	Value string `json:"value,omitempty"`
}

// BlobPartsResponse is the envelope returned by GET /{id}/blob-parts.
type BlobPartsResponse struct {
	ContentType string        `json:"content_type"`
	Parts       []BlobPartDTO `json:"parts"`
}

// intentToDTO maps a domain Intent to its JSON projection.
func intentToDTO(i failedintent.Intent) IntentDTO {
	dto := IntentDTO{
		ID:              i.ID.String(),
		ReceivedAt:      i.ReceivedAt.UTC().Format(time.RFC3339Nano),
		Method:          i.Method,
		Path:            i.Path,
		FirebaseUID:     i.FirebaseUID,
		RequestID:       i.RequestID.String(),
		BodyTruncated:   i.BodyTruncated,
		HasBlob:         i.BodyBlobPath != "",
		BodyContentType: i.BodyContentType,
		HTTPStatus:      i.HTTPStatus,
		ErrorCode:       i.ErrorCode,
		ErrorMessage:    i.ErrorMessage,
		RetryCount:      i.RetryCount,
		Status:          string(i.Status),
		Notes:           i.Notes,
		IdempotencyKey:  i.IdempotencyKey,
	}

	// Body is already json.RawMessage; embed as-is so it stays a JSON object
	// rather than a base64 string. If null/invalid, keep it as null.
	if len(i.Body) > 0 {
		dto.Body = i.Body
	}

	if i.UsuarioID != nil {
		s := i.UsuarioID.String()
		dto.UsuarioID = &s
	}
	if i.LastSeenAt != nil {
		s := i.LastSeenAt.UTC().Format(time.RFC3339Nano)
		dto.LastSeenAt = &s
	}
	if i.ResolvedAt != nil {
		s := i.ResolvedAt.UTC().Format(time.RFC3339Nano)
		dto.ResolvedAt = &s
	}
	if i.ResolvedBy != nil {
		s := i.ResolvedBy.String()
		dto.ResolvedBy = &s
	}
	dto.Modulo = i.Modulo
	dto.Resumen = resumenToDTO(i.Resumen)
	return dto
}

// resumenToDTO proyecta el resumen. Devuelve nil cuando no hay nada que
// mostrar, para que el campo se omita del JSON en vez de viajar como un objeto
// vacío que el escritorio tendría que distinguir de "sí hay resumen".
func resumenToDTO(r *failedintent.Resumen) *ResumenDTO {
	if r.Vacio() {
		return nil
	}
	dto := &ResumenDTO{Titulo: r.Titulo, Referencia: r.Referencia}
	if r.Monto != nil {
		dto.Monto = r.Monto.String()
	}
	return dto
}

// encodeCursor produces a base64url (no padding) cursor from the two pagination
// fields. The format is "<received_at_RFC3339Nano>|<id_uuid>".
func encodeCursor(receivedAt time.Time, id uuid.UUID) string {
	raw := fmt.Sprintf("%s|%s", receivedAt.UTC().Format(time.RFC3339Nano), id.String())
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor parses a base64url cursor. An empty string is a valid sentinel
// meaning "start from the newest row" and returns zero values without an error.
func decodeCursor(s string) (time.Time, uuid.UUID, error) {
	if s == "" {
		return time.Time{}, uuid.UUID{}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.UUID{}, fmt.Errorf("decodeCursor: base64: %w", err)
	}
	parts := strings.SplitN(string(b), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.UUID{}, errCursorFormat
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.UUID{}, fmt.Errorf("decodeCursor: time: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.UUID{}, fmt.Errorf("decodeCursor: uuid: %w", err)
	}
	return t, id, nil
}
