//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp

import (
	"time"

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

// CrearPagoInput is the JSON body for POST /cobranza/pagos.
type CrearPagoInput struct {
	// IdempotencyKey is the optional Idempotency-Key header. When present it
	// MUST equal Body.ID (the client-generated UUID is the canonical key).
	IdempotencyKey string `header:"Idempotency-Key" doc:"Opcional. Si presente, debe coincidir con body.id"`
	Body           struct {
		ID             string `json:"id"               format:"uuid"      doc:"UUID generado por el cliente (idempotency key end-to-end)"`
		CargoDoctoCCID int    `json:"cargo_docto_cc_id"                  doc:"DOCTOS_CC.DOCTO_CC_ID del cargo a abonar" minimum:"1"`
		ClienteID      int    `json:"cliente_id"       minimum:"1"`
		CobradorID     int    `json:"cobrador_id"      minimum:"1"`
		Cobrador       string `json:"cobrador"         minLength:"1"      maxLength:"100"`
		Importe        string `json:"importe"                            doc:"Importe en MXN como string decimal, p.ej. \"100.00\""`
		FormaCobroID   int    `json:"forma_cobro_id"   minimum:"1"`
		FechaHoraPago  string `json:"fecha_hora_pago"  format:"date-time" doc:"RFC3339 — captura del cliente"`
		Lat            string `json:"lat,omitempty"    maxLength:"20"`
		Lon            string `json:"lon,omitempty"    maxLength:"20"`
	}
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
