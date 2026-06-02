//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp

import (
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// ─── PagoRecibidoDTO ──────────────────────────────────────────────────────────

// PagoRecibidoDTO is the JSON projection of a domain.PagoRecibido.
// Importe is serialized as a string to preserve decimal precision across
// JSON parsers that lose precision on float64 for large amounts.
type PagoRecibidoDTO struct {
	ID             string  `json:"id"`
	CargoDoctoCCID int     `json:"cargo_docto_cc_id"`
	ClienteID      int     `json:"cliente_id"`
	CobradorID     int     `json:"cobrador_id"`
	Cobrador       string  `json:"cobrador"`
	Importe        string  `json:"importe"` // decimal as string for JSON precision
	FormaCobroID   int     `json:"forma_cobro_id"`
	ConceptoCCID   int     `json:"concepto_cc_id"`
	FechaHoraPago  string  `json:"fecha_hora_pago"` // RFC3339 UTC
	Lat            *string `json:"lat,omitempty"`
	Lon            *string `json:"lon,omitempty"`
	Sincronizacion string  `json:"sincronizacion"` // "pendiente" | "aplicada"
	Intentos       int     `json:"intentos"`
	UltimoError    *string `json:"ultimo_error,omitempty"`
	DoctoCCID      *int    `json:"docto_cc_id,omitempty"`
	ImpteDoctoCCID *int    `json:"impte_docto_cc_id,omitempty"`
	Folio          *string `json:"folio,omitempty"`
	ReceivedAt     string  `json:"received_at"` // RFC3339 UTC
	AplicadoAt     *string `json:"aplicado_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

// toPagoRecibidoDTO projects a domain.PagoRecibido into a PagoRecibidoDTO.
func toPagoRecibidoDTO(p *domain.PagoRecibido) PagoRecibidoDTO {
	aud := p.Audit()
	dto := PagoRecibidoDTO{
		ID:             p.ID().String(),
		CargoDoctoCCID: p.CargoDoctoCCID(),
		ClienteID:      p.ClienteID(),
		CobradorID:     p.CobradorID(),
		Cobrador:       p.Cobrador(),
		Importe:        p.Importe().StringFixed(2),
		FormaCobroID:   p.FormaCobroID(),
		ConceptoCCID:   p.ConceptoCCID(),
		FechaHoraPago:  p.FechaHoraPago().UTC().Format(time.RFC3339),
		Lat:            p.Lat(),
		Lon:            p.Lon(),
		Sincronizacion: p.Sincronizacion().String(),
		Intentos:       p.Intentos(),
		UltimoError:    p.UltimoError(),
		DoctoCCID:      p.DoctoCCID(),
		ImpteDoctoCCID: p.ImpteDoctoCCID(),
		Folio:          p.Folio(),
		ReceivedAt:     p.ReceivedAt().UTC().Format(time.RFC3339),
		CreatedAt:      aud.CreatedAt().UTC().Format(time.RFC3339),
		UpdatedAt:      aud.UpdatedAt().UTC().Format(time.RFC3339),
	}
	if a := p.AplicadoAt(); a != nil {
		s := a.UTC().Format(time.RFC3339)
		dto.AplicadoAt = &s
	}
	return dto
}

// ─── Input / Output DTOs ──────────────────────────────────────────────────────

// CrearPagoBody is the JSON document carried inside the multipart `datos`
// field. Decoded server-side from a raw string — Huma does not validate the
// inner JSON schema; the handler does ID / fecha / importe parsing itself
// and surfaces apperror codes per field.
//
// The wire format is unchanged from the legacy JSON-only endpoint, so a
// client that already builds CrearPagoBody as a JSON body just needs to
// switch transport: send the same JSON as the `datos` part of a multipart
// request, plus 0..N `imagen` files.
type CrearPagoBody struct {
	ID             string `json:"id"`
	CargoDoctoCCID int    `json:"cargo_docto_cc_id"`
	ClienteID      int    `json:"cliente_id"`
	CobradorID     int    `json:"cobrador_id"`
	Cobrador       string `json:"cobrador"`
	Importe        string `json:"importe"`
	FormaCobroID   int    `json:"forma_cobro_id"`
	FechaHoraPago  string `json:"fecha_hora_pago"`
	Lat            string `json:"lat,omitempty"`
	Lon            string `json:"lon,omitempty"`
}

// CrearPagoMultipartFields is the typed projection of the multipart body Huma
// parses for [Handlers.CrearPago]. The per-image `id_N` / `descripcion_N`
// metadata fields are read separately from the raw form via
// [parseImagenesFromMultipart] because Huma's struct decoder does not support
// dynamic field names.
//
// Imagen contentType whitelist matches the legacy POST /pagos/{id}/imagenes
// endpoint: JPEG, PNG, GIF, WebP, PDF. Cobranza accepts PDFs (recibos SAT).
type CrearPagoMultipartFields struct {
	Datos  string          `form:"datos"  required:"true"  doc:"JSON con el pago (mismos campos que el legacy CrearPagoBody)"`
	Imagen []huma.FormFile `form:"imagen" contentType:"image/jpeg,image/png,image/gif,image/webp,application/pdf" required:"false" doc:"0..N comprobantes; cada uno opcionalmente pareado con id_<n> y descripcion_<n>"`
}

// CrearPagoInput is the multipart body for POST /cobranza/pagos.
//
// The new atomic contract: a single multipart request carries the pago JSON
// (`datos`) plus zero or more comprobantes (`imagen` repeated). Server
// persists pago + imagenes en una sola tx Firebird; cualquier fallo deja el
// sistema sin estado parcial.
//
// Per-image metadata (`id_<n>`, `descripcion_<n>`) is read positionally from
// the raw form — `id_0` pairs with the first `imagen`, `id_1` with the
// second, and so on. Client SHOULD send a stable UUID per image for replay
// safety; absent IDs are server-generated and reintents will duplicate.
type CrearPagoInput struct {
	IdempotencyKey string                                            `header:"Idempotency-Key" doc:"Opcional. Si presente, debe coincidir con datos.id"`
	RawBody        huma.MultipartFormFiles[CrearPagoMultipartFields] `doc:"multipart/form-data: datos (JSON) + N imagen (archivos) + opcionales id_<n>/descripcion_<n>"`
}

// CrearPagoOutput wraps a 201 Created response.
type CrearPagoOutput struct {
	Body PagoRecibidoDTO
}

// ObtenerPagoInput is the path parameter for GET /cobranza/pagos/{id}.
type ObtenerPagoInput struct {
	ID string `path:"id" format:"uuid"`
}

// ObtenerPagoOutput wraps a single PagoRecibidoDTO.
type ObtenerPagoOutput struct {
	Body PagoRecibidoDTO
}

// ListarPendientesInput carries the optional query params for the pending-queue
// admin endpoint.
type ListarPendientesInput struct {
	MaxIntentos int `query:"max_intentos" doc:"Máximo de intentos al filtrar (default 10)"`
	Limit       int `query:"limit"        doc:"Tamaño de página (default 100, máximo 500)"`
}

// ListarPendientesOutput wraps the pagos slice.
type ListarPendientesOutput struct {
	Body []PagoRecibidoDTO
}

// AplicarPagoInput is the path parameter for POST /cobranza/pagos/{id}/aplicar.
type AplicarPagoInput struct {
	ID string `path:"id" format:"uuid"`
}

// AplicarPagoOutput wraps the updated PagoRecibidoDTO.
type AplicarPagoOutput struct {
	Body PagoRecibidoDTO
}
