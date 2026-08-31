//nolint:misspell // wire vocabulary is Spanish per project convention (Parametros, etc.).
package canalhttp

// ─── POST /canal/v1/salientes ───────────────────────────────────────────────

// CrearSalienteInput carries the shared token (checked by
// sharedTokenMiddleware before this handler ever runs) and the outbound
// message to send.
type CrearSalienteInput struct {
	Token string `header:"X-Canal-Token"  doc:"Token compartido interno."`
	Body  CrearSalienteBody
}

// CrearSalienteBody is the outbound message the store wants sent through
// canal's WhatsApp edge. Exactly one of Texto/Plantilla is read, selected by
// Tipo ("texto" or "plantilla" — see canalapp.TipoSalienteTexto/
// TipoSalientePlantilla, the app layer's own constants for these wire
// values, which toEnviarSalienteParams passes Tipo straight through to).
type CrearSalienteBody struct {
	Destinatario string                `json:"destinatario"        doc:"Teléfono E.164 del destinatario."`
	Tipo         string                `json:"tipo"                doc:"texto o plantilla."`
	Texto        string                `json:"texto,omitempty"     doc:"Cuerpo del mensaje libre. Requerido cuando tipo=texto."`
	Plantilla    *PlantillaSalienteDTO `json:"plantilla,omitempty" doc:"Datos de la plantilla. Requerido cuando tipo=plantilla."`
}

// PlantillaSalienteDTO is the wire shape of a template-message invocation,
// mirroring canalapp.SalientePlantilla (itself a mirror of
// internal/platform/whatsapp.Template — see that type's own doc comment
// for why canalhttp does not import whatsapp directly to build one).
type PlantillaSalienteDTO struct {
	Nombre     string   `json:"nombre"               doc:"Nombre de la plantilla registrada en WhatsApp Manager."`
	Idioma     string   `json:"idioma"               doc:"Código de idioma aprobado, p. ej. es_MX."`
	Parametros []string `json:"parametros,omitempty" doc:"Parámetros posicionales del cuerpo de la plantilla."`
}

// CrearSalienteOutput wraps the response body for POST /canal/v1/salientes.
type CrearSalienteOutput struct {
	Body struct {
		Wamid string `json:"wamid" doc:"ID del mensaje asignado por WhatsApp."`
	}
}

// ─── GET /canal/v1/salud ────────────────────────────────────────────────────

// SaludInput has no parameters — see registerSalud for why this route is
// deliberately unauthenticated.
type SaludInput struct{}

// SaludOutput wraps the response body for GET /canal/v1/salud.
type SaludOutput struct {
	Body SaludDTO
}

// SaludDTO reports liveness and the mailbox backlog. It must never carry a
// token, a phone number, or a message body — this endpoint is intentionally
// reachable without the shared token, so nothing sensitive can leak through
// it. See registerSalud.
type SaludDTO struct {
	Estado     string `json:"estado"     doc:"ok si el proceso está vivo y pudo leer el buzón."`
	Pendientes int    `json:"pendientes" doc:"Mensajes en el buzón esperando ser reenviados a la tienda."`
}
