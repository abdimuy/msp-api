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
//
// Lat and Lng are POINTERS so their absence is distinguishable from a zero.
// Before this route read its own body, Huma required every field without an
// `omitempty` json tag, so omitting `lat` was a 422. Reading the body by hand
// lost that for free, and losing it here is not free: a missing `lat` would
// decode to 0.0 and the visit would be recorded at Null Island as data rather
// than rejected as an error.
//
// What is required is PRESENCE, not a range. A cobrador genuinely may have no
// GPS fix, and a deliberate 0 stays valid — see requireCoordenadas.
//
// Note on key matching: encoding/json matches object keys
// case-INSENSITIVELY, so `"Lat"` now decodes into this field where Huma's
// schema required the exact lower-case `lat`. That is a widening, examined
// and deliberately left alone: it accepts strictly more of what a client
// might send and rejects nothing that used to work.
type CrearVisitaBody struct {
	ID             string   `json:"id"               doc:"UUID generado por el cliente; clave de idempotencia"`
	ClienteID      int      `json:"cliente_id"       doc:"ID del cliente Microsip visitado"`
	CobradorID     int      `json:"cobrador_id"      doc:"ID del cobrador Microsip"`
	Cobrador       string   `json:"cobrador"         doc:"Nombre del cobrador"`
	FormaCobroID   int      `json:"forma_cobro_id"   doc:"ID de la forma de cobro Microsip; 0 si la visita no generó pago"`
	Lat            *float64 `json:"lat"             doc:"Latitud donde se capturó la visita. Obligatorio: debe venir presente y no nulo. El VALOR no se restringe — 0 es una coordenada legítima."`
	Lng            *float64 `json:"lng"             doc:"Longitud donde se capturó la visita. Obligatorio: debe venir presente y no nulo. El VALOR no se restringe — 0 es una coordenada legítima."`
	Nota           string   `json:"nota,omitempty"   doc:"Nota libre del cobrador"`
	TipoVisita     string   `json:"tipo_visita"      doc:"Tipo de visita (p.ej. cobro, no_encontrado)"`
	ZonaClienteID  int      `json:"zona_cliente_id"  doc:"Zona del cliente al momento de la visita"`
	ImpteDoctoCCID *int     `json:"impte_docto_cc_id,omitempty" doc:"IMPORTES_DOCTOS_CC.IMPTE_DOCTO_CC_ID vinculado al pago de esta visita; nulo si no aplica"`
	Fecha          string   `json:"fecha"            doc:"Fecha-hora de la visita, RFC3339 UTC"`
}

// CrearVisitaInput is the HTTP input for POST /visitas.
//
// # Why RawBody []byte and not a typed Body
//
// This route accepts TWO wire formats on the same path and method:
//
//   - application/json — the body IS a CrearVisitaBody. This is the format
//     the phones in the field already send and it must keep working
//     unchanged; a build that predates the photo feature has no idea
//     multipart exists.
//   - multipart/form-data — a `datos` field carrying that same JSON document
//     verbatim, plus 0..N repeated `imagen` file parts and their optional
//     positional `id_<n>` / `descripcion_<n>` companions.
//
// Huma cannot express that with a typed body. `Body CrearVisitaBody` parses
// JSON unconditionally and `huma.MultipartFormFiles[T]` parses multipart
// unconditionally — its decoder calls readForm without ever consulting
// Content-Type, so a JSON body against it is a 422 with no way to intervene.
// Declaring RawBody as []byte puts Huma in pass-through mode (rawBodyType
// "other"): it reads the bytes, bounded by the global BodyLimit, and hands
// them over undecoded. [Handlers.CrearVisita] then branches on ContentType.
//
// What that costs is Huma's automatic body validation, which this route never
// relied on: CrearVisitaBody declares no `required` and no `format`, and every
// field is parsed and validated by hand precisely so each failure gets its
// own apperror code. What it buys is that both formats reach the same
// handler, the same service call, and the same idempotency guarantee.
//
// The OpenAPI request body is rewritten after registration (see
// describeCrearVisitaBody in routes.go) so the published contract shows both
// media types instead of the opaque octet-stream Huma would infer.
type CrearVisitaInput struct {
	ContentType    string `header:"Content-Type"    doc:"application/json (contrato legacy) o multipart/form-data (con fotos)"`
	IdempotencyKey string `header:"Idempotency-Key" doc:"Opcional. Si presente, debe coincidir con el id del cuerpo"`
	RawBody        []byte
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

// VisitasExistentesPorIDsInput is the query parameter for
// GET /visitas/by-ids. ids is a required, comma-separated list of UUIDs; see
// parseIDsParam (handlers_visitas_existentes_por_ids.go) for the exact
// parsing/validation rules and maxIDsPorRequest for the cap.
//
// Declared as a plain string, not `query:"ids" required:"true"`: an absent
// or empty ids must answer the module's own ids_required apperror (422,
// distinguishable code), not Huma's generic required-field 422 — so
// required-ness is enforced by the handler, not by the Huma schema.
type VisitasExistentesPorIDsInput struct {
	IDs string `query:"ids" doc:"UUIDs separados por coma; ids que el servidor no conoce simplemente no aparecen en la respuesta"`
}

// VisitasExistentesPorIDsOutput wraps the flat JSON array of existing UUIDs
// (as strings) for GET /visitas/by-ids — no envelope. The reconciler this
// endpoint serves never downloads visits, only confirms which of the ids it
// uploaded already exist, so the response carries ids, not VisitaDTOs.
//
// A bare array as a Huma response body is a supported, native shape — Huma
// treats a GET operation whose Body is a slice as a "list" operation (see
// huma.GenerateOperationID) — so this endpoint stays inside Huma rather than
// dropping to a raw chi handler the way cobranza's by-ids channel does (see
// this handler's package doc comment for the full reasoning).
type VisitasExistentesPorIDsOutput struct {
	Body []string
}
