//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionhttp

// ─── POST /reactivacion/tienda/mensaje-entrante ────────────────────────────

// TiendaMensajeEntranteInput carries the VPS's forwarded entrante. There is
// no cliente_id path parameter here — unlike
// /reactivacion/conversaciones/{cliente_id}/mensaje-entrante, the VPS knows
// only the sender's phone number, never a cliente_id: per
// docs/adr/0010-whatsapp-cloud-api-and-the-always-on-edge.md §3 it is never
// a source of truth for business data, so resolving the phone to a
// cliente_id happens inside the handler, on-premise.
type TiendaMensajeEntranteInput struct {
	Body TiendaMensajeEntranteBody
}

// TiendaMensajeEntranteBody is the wire shape
// canalhttp.ForwarderClient.Reenviar posts for one entrante. Field names
// and json tags MUST stay in lockstep with forwarderPayload in
// internal/canal/infra/canalhttp/forwarder_client.go — that struct is the
// other half of this contract and this task does not own it.
type TiendaMensajeEntranteBody struct {
	Wamid         string `json:"wamid"           doc:"ID del mensaje de Meta (wamid); usado sólo para trazabilidad en los logs"`
	Remitente     string `json:"remitente"       doc:"Teléfono del remitente tal como lo envió Meta (dígitos, con lada país)"`
	PhoneNumberID string `json:"phone_number_id" doc:"ID del número de WhatsApp de destino en Meta"`
	Tipo          string `json:"tipo"            doc:"Tipo de mensaje de Meta (text, image, ...); sólo 'text' se procesa en fase 3a"`
	Contenido     string `json:"contenido"       doc:"Cuerpo del mensaje de texto; vacío o irrelevante para otros tipos"`
	TimestampMeta string `json:"timestamp_meta"  doc:"Marca de tiempo de Meta, RFC3339Nano UTC"`
	RecibidoEn    string `json:"recibido_en"     doc:"Momento en que la VPS recibió el webhook, RFC3339Nano UTC"`
}

// TiendaMensajeEntranteOutput wraps the response body for POST
// /reactivacion/tienda/mensaje-entrante. Reuses DecisionResultDTO — the
// exact shape the Firebase-gated simulate endpoint
// (POST /reactivacion/conversaciones/{cliente_id}/mensaje-entrante)
// already returns, since both call the same
// reactivacionapp.Service.ProcesarMensajeEntrante.
type TiendaMensajeEntranteOutput struct {
	Body DecisionResultDTO
}
