// Package visitashttp is the visitas module's HTTP transport: DTOs,
// handlers, and the Huma-over-chi router mount point.
//
//nolint:misspell // visitas vocabulary is Spanish per project convention.
package visitashttp

import (
	"time"

	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// ─── VisitaDTO ────────────────────────────────────────────────────────────────

// VisitaDTO is the JSON projection of a domain.Visita.
type VisitaDTO struct {
	ID             string  `json:"id"`
	ClienteID      int     `json:"cliente_id"`
	CobradorID     int     `json:"cobrador_id"`
	Cobrador       string  `json:"cobrador"`
	FormaCobroID   int     `json:"forma_cobro_id"`
	Lat            float64 `json:"lat"`
	Lng            float64 `json:"lng"`
	Nota           string  `json:"nota,omitempty"`
	TipoVisita     string  `json:"tipo_visita"`
	ZonaClienteID  int     `json:"zona_cliente_id"`
	ImpteDoctoCCID *int    `json:"impte_docto_cc_id,omitempty"`
	Fecha          string  `json:"fecha"`      // RFC3339 UTC
	CreatedAt      string  `json:"created_at"` // RFC3339 UTC; in-memory only, see domain.Visita doc comment
}

// toVisitaDTO projects a domain.Visita into a VisitaDTO.
func toVisitaDTO(v *domain.Visita) VisitaDTO {
	aud := v.Audit()
	return VisitaDTO{
		ID:             v.ID().String(),
		ClienteID:      v.ClienteID(),
		CobradorID:     v.CobradorID(),
		Cobrador:       v.Cobrador(),
		FormaCobroID:   v.FormaCobroID(),
		Lat:            v.Lat(),
		Lng:            v.Lng(),
		Nota:           v.Nota(),
		TipoVisita:     v.TipoVisita().String(),
		ZonaClienteID:  v.ZonaClienteID(),
		ImpteDoctoCCID: v.ImpteDoctoCCID(),
		Fecha:          v.Fecha().UTC().Format(time.RFC3339),
		CreatedAt:      aud.CreatedAt().UTC().Format(time.RFC3339),
	}
}

// ─── Input / Output DTOs ──────────────────────────────────────────────────────

// CrearVisitaBody is the JSON document for POST /visitas. The mobile app
// generates ID offline; it doubles as the idempotency key end-to-end (see
// app.Service.RegistrarVisita).
type CrearVisitaBody struct {
	ID             string  `json:"id"               doc:"UUID generado por el cliente; clave de idempotencia"`
	ClienteID      int     `json:"cliente_id"       doc:"ID del cliente Microsip visitado"`
	CobradorID     int     `json:"cobrador_id"      doc:"ID del cobrador Microsip"`
	Cobrador       string  `json:"cobrador"         doc:"Nombre del cobrador"`
	FormaCobroID   int     `json:"forma_cobro_id"   doc:"ID de la forma de cobro Microsip; 0 si la visita no generó pago"`
	Lat            float64 `json:"lat"              doc:"Latitud donde se capturó la visita"`
	Lng            float64 `json:"lng"              doc:"Longitud donde se capturó la visita"`
	Nota           string  `json:"nota,omitempty"   doc:"Nota libre del cobrador"`
	TipoVisita     string  `json:"tipo_visita"      doc:"Tipo de visita (p.ej. cobro, no_encontrado)"`
	ZonaClienteID  int     `json:"zona_cliente_id"  doc:"Zona del cliente al momento de la visita"`
	ImpteDoctoCCID *int    `json:"impte_docto_cc_id,omitempty" doc:"IMPORTES_DOCTOS_CC.IMPTE_DOCTO_CC_ID vinculado al pago de esta visita; nulo si no aplica"`
	Fecha          string  `json:"fecha"            doc:"Fecha-hora de la visita, RFC3339 UTC"`
}

// CrearVisitaInput is the HTTP input for POST /visitas.
type CrearVisitaInput struct {
	IdempotencyKey string `header:"Idempotency-Key" doc:"Opcional. Si presente, debe coincidir con body.id"`
	Body           CrearVisitaBody
}

// CrearVisitaOutput wraps a 201 Created response.
type CrearVisitaOutput struct {
	Body VisitaDTO
}

// ObtenerVisitaInput is the path parameter for GET /visitas/{id}. Huma
// enforces format:"uuid" from the schema, so a non-UUID path segment is
// rejected with a 422 before the handler runs; the handler parses again
// anyway rather than trusting the transport (mirrors cobranza's
// ObtenerPagoInput).
type ObtenerVisitaInput struct {
	ID string `path:"id" format:"uuid"`
}

// ObtenerVisitaOutput wraps a single VisitaDTO for GET /visitas/{id}.
//
// The tipo_visita it carries is whatever the row holds — including a retired
// literal or the empty string on the oldest rows. The closed catalogue is a
// write-path rule; see domain.RehydrateVisita.
type ObtenerVisitaOutput struct {
	Body VisitaDTO
}
