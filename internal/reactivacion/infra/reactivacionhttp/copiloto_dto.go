// Package reactivacionhttp — Fase 3a copiloto DTOs (conversaciones, decisiones,
// operator actions). Kept in a sibling file to dto.go per the module's Fase
// 1/2 file-per-slice convention.
//
//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionhttp

// ─── POST /reactivacion/conversaciones/{cliente_id}/mensaje-entrante ───────────

// MensajeEntranteInput carries the cliente_id path parameter and the
// simulated inbound message body. In Fase 3a there is no real WhatsApp
// channel wired to inbound — this endpoint lets an operator simulate the
// cliente's reply for the copiloto to analyze.
type MensajeEntranteInput struct {
	ClienteID int `path:"cliente_id" doc:"ID del cliente en Microsip"`
	Body      MensajeEntranteBody
}

// MensajeEntranteBody is the request body for the simulated inbound message.
type MensajeEntranteBody struct {
	Mensaje string `json:"mensaje" doc:"Texto del mensaje entrante simulado del cliente"`
}

// MensajeEntranteOutput wraps the response for POST .../mensaje-entrante.
type MensajeEntranteOutput struct {
	Body DecisionResultDTO
}

// DecisionResultDTO is the wire representation of ProcesarMensajeEntrante's
// outcome: the copiloto's analysis (intención/confianza/señales/evidencia),
// the FINAL triaged acción/resultado, the drafted reply (empty when
// escalada), and whether the turn ended up escalated.
type DecisionResultDTO struct {
	Intencion         string   `json:"intencion"          doc:"Lectura del LLM sobre la intención del cliente"`
	Confianza         int      `json:"confianza"          doc:"Confianza del LLM en su lectura, 0-100"`
	Senales           []string `json:"senales"            doc:"Señales detectadas por el LLM (deuda, senal_compra, pide_humano, ...)"`
	Accion            string   `json:"accion"             doc:"Acción final tras la política determinista: responder o escalar"`
	Borrador          string   `json:"borrador"           doc:"Borrador de respuesta guardado como pendiente; vacío si se escaló"`
	Evidencia         []string `json:"evidencia"          doc:"Fragmentos del mensaje que respaldan la lectura del LLM"`
	RazonEscalamiento string   `json:"razon_escalamiento" doc:"Motivo del escalamiento; vacío si la acción es responder"`
	Resultado         string   `json:"resultado"          doc:"Resultado de la decisión: propuesto, aprobado, editado o escalado"`
	Escalada          bool     `json:"escalada"           doc:"true si el turno terminó escalado a un operador humano"`
}

// ─── GET /reactivacion/conversaciones ───────────────────────────────────────

// ListConversacionesInput carries the optional query filters for the bandeja
// queue. SoloEscaladas is a shortcut that takes precedence over Estado: the
// repo's filter ANDs both, so sending estado alongside solo_escaladas=true
// would silently yield zero rows whenever estado isn't "escalado" — Estado is
// ignored once SoloEscaladas is true.
type ListConversacionesInput struct {
	Estado        string `query:"estado"         doc:"Filtra por estado: contactado, respondio, conversando, escalado, interesado, enganche o descartado. Vacío = todos. Ignorado cuando solo_escaladas=true."`
	SoloEscaladas bool   `query:"solo_escaladas" doc:"Cuando es true, devuelve solo conversaciones escaladas (ignora estado)."`
}

// ListConversacionesOutput wraps the response body for GET /reactivacion/conversaciones.
type ListConversacionesOutput struct {
	Body struct {
		Items []ConversacionResumenDTO `json:"items"`
	}
}

// ConversacionResumenDTO is the wire representation of one bandeja queue row:
// the conversation header plus its newest decision (nil when the conversation
// has no decision yet, e.g. only opened by the Fase 2 channel).
type ConversacionResumenDTO struct {
	ClienteID      int                `json:"cliente_id"      doc:"ID del cliente en Microsip"`
	Estado         string             `json:"estado"          doc:"Estado actual de la conversación"`
	AsignadoA      string             `json:"asignado_a"      doc:"Operador asignado; vacío si no está escalada"`
	UpdatedAt      string             `json:"updated_at"      doc:"Fecha de la última transición o actualización (RFC3339 UTC)"`
	UltimaDecision *UltimaDecisionDTO `json:"ultima_decision" doc:"Decisión más reciente del cliente; nulo si aún no hay ninguna"`
	Nombre         string             `json:"nombre"          doc:"Nombre del cliente; vacío si no está en la cohorte"`
	Segmento       string             `json:"segmento"        doc:"Segmento del cliente en la cohorte; vacío si no está en la cohorte"`
	UltimoMensaje  string             `json:"ultimo_mensaje"  doc:"Vista previa del último mensaje entrante del cliente (máx. 120 caracteres); vacío si aún no hay ninguno"`
}

// UltimaDecisionDTO is the condensed decision shown in the bandeja queue —
// enough for the operator to triage without opening the full ficha.
type UltimaDecisionDTO struct {
	Intencion         string `json:"intencion"          doc:"Lectura del LLM sobre la intención del cliente"`
	Confianza         int    `json:"confianza"          doc:"Confianza del LLM en su lectura, 0-100"`
	Accion            string `json:"accion"             doc:"Acción propuesta: responder o escalar"`
	Resultado         string `json:"resultado"          doc:"Resultado de la decisión"`
	RazonEscalamiento string `json:"razon_escalamiento" doc:"Motivo del escalamiento; vacío si la acción es responder"`
}

// ─── GET /reactivacion/conversaciones/{cliente_id} ──────────────────────────

// ObtenerConversacionInput carries the cliente_id path parameter.
type ObtenerConversacionInput struct {
	ClienteID int `path:"cliente_id" doc:"ID del cliente en Microsip"`
}

// ObtenerConversacionOutput wraps the response body for
// GET /reactivacion/conversaciones/{cliente_id}.
type ObtenerConversacionOutput struct {
	Body ConversacionDetalleDTO
}

// ConversacionDetalleDTO is the full ficha view for one cliente's
// conversation: the state-machine header, the complete turno thread, and the
// decision audit trail.
type ConversacionDetalleDTO struct {
	Conversacion ConversacionDTO `json:"conversacion"`
	Turnos       []TurnoDTO      `json:"turnos"`
	Decisiones   []DecisionDTO   `json:"decisiones"`
}

// ConversacionDTO is the wire representation of the Conversacion header.
// ContextoNota/Banderas are the DISTILLED, governed summary of the cobrador's
// note — safe to show the operator — never the raw note text.
type ConversacionDTO struct {
	ClienteID      int      `json:"cliente_id"      doc:"ID del cliente en Microsip"`
	Estado         string   `json:"estado"          doc:"Estado actual en la máquina de escalamiento"`
	AsignadoA      string   `json:"asignado_a"      doc:"Operador asignado; vacío si no está escalada"`
	ContextoNota   string   `json:"contexto_nota"   doc:"Resumen destilado (gobernado) de la nota del cobrador, para el operador"`
	Banderas       []string `json:"banderas"        doc:"Banderas operativas derivadas de la nota del cobrador"`
	ResumenMemoria string   `json:"resumen_memoria" doc:"Resumen de la conversación acumulado hasta ahora"`
	CreatedAt      string   `json:"created_at"      doc:"Fecha de creación de la conversación (RFC3339 UTC)"`
	UpdatedAt      string   `json:"updated_at"      doc:"Fecha de la última transición o actualización (RFC3339 UTC)"`
	Nombre         string   `json:"nombre"          doc:"Nombre del cliente; vacío si no está en la cohorte"`
	Segmento       string   `json:"segmento"        doc:"Segmento del cliente en la cohorte; vacío si no está en la cohorte"`
	Telefono       string   `json:"telefono"        doc:"Teléfono del cliente; vacío si no está en la cohorte"`
}

// TurnoDTO is the wire representation of one message in the conversation
// thread — the real messages exchanged with the cliente.
type TurnoDTO struct {
	Direccion  string `json:"direccion"   doc:"entrante (del cliente) o saliente (hacia el cliente)"`
	Autor      string `json:"autor"       doc:"cliente, ia o humano"`
	Cuerpo     string `json:"cuerpo"      doc:"Texto del turno"`
	MensajeRef string `json:"mensaje_ref" doc:"ID del envío del canal (Fase 2) vinculado; vacío si no aplica"`
	CreatedAt  string `json:"created_at"  doc:"Fecha del turno (RFC3339 UTC)"`
}

// DecisionDTO is the wire representation of one Decision in the audit trail.
type DecisionDTO struct {
	Intencion         string   `json:"intencion"          doc:"Lectura del LLM sobre la intención del cliente"`
	Confianza         int      `json:"confianza"          doc:"Confianza del LLM en su lectura, 0-100"`
	Senales           []string `json:"senales"            doc:"Señales detectadas por el LLM"`
	Accion            string   `json:"accion"             doc:"Acción propuesta: responder o escalar"`
	Borrador          string   `json:"borrador"           doc:"Borrador de respuesta"`
	Evidencia         []string `json:"evidencia"          doc:"Fragmentos que respaldan la lectura del LLM"`
	RazonEscalamiento string   `json:"razon_escalamiento" doc:"Motivo del escalamiento; vacío si la acción es responder"`
	Resultado         string   `json:"resultado"          doc:"Resultado: propuesto, aprobado, editado o escalado"`
	CreatedAt         string   `json:"created_at"         doc:"Fecha de la decisión (RFC3339 UTC)"`
}

// ─── POST /reactivacion/conversaciones/{cliente_id}/aprobar ─────────────────

// AprobarBorradorInput carries the cliente_id path parameter. No body — the
// newest pending draft is approved as-is.
type AprobarBorradorInput struct {
	ClienteID int `path:"cliente_id" doc:"ID del cliente en Microsip"`
}

// AprobarBorradorOutput wraps the response body for POST .../aprobar.
type AprobarBorradorOutput struct {
	Body OkDTO
}

// OkDTO is a minimal acknowledgement body for operator actions that have no
// richer response payload.
type OkDTO struct {
	Ok bool `json:"ok" doc:"true si la acción se aplicó correctamente"`
}

// ─── POST /reactivacion/conversaciones/{cliente_id}/editar ──────────────────

// EditarInput carries the cliente_id path parameter and the operator's
// edited draft text.
type EditarInput struct {
	ClienteID int `path:"cliente_id" doc:"ID del cliente en Microsip"`
	Body      EditarBody
}

// EditarBody is the request body for POST .../editar.
type EditarBody struct {
	Texto string `json:"texto" doc:"Texto editado del borrador, listo para enviar"`
}

// EditarOutput wraps the response body for POST .../editar.
type EditarOutput struct {
	Body OkDTO
}

// ─── POST /reactivacion/conversaciones/{cliente_id}/dictar ──────────────────

// DictarInput carries the cliente_id path parameter and the operator's
// stated intent for a fresh AI draft.
type DictarInput struct {
	ClienteID int `path:"cliente_id" doc:"ID del cliente en Microsip"`
	Body      DictarBody
}

// DictarBody is the request body for POST .../dictar.
type DictarBody struct {
	Intencion string `json:"intencion" doc:"Intención dictada por el operador, en sus propias palabras"`
}

// DictarOutput wraps the response body for POST .../dictar.
type DictarOutput struct {
	Body struct {
		Borrador string `json:"borrador" doc:"Borrador redactado por el copiloto a partir de la intención dictada"`
	}
}

// ─── POST /reactivacion/conversaciones/{cliente_id}/escalar ─────────────────

// EscalarInput carries the cliente_id path parameter and the optional
// assignee for the hand-off.
type EscalarInput struct {
	ClienteID int `path:"cliente_id" doc:"ID del cliente en Microsip"`
	Body      EscalarBody
}

// EscalarBody is the request body for POST .../escalar. AsignadoA is
// optional — an empty value escalates without assigning a specific operator.
type EscalarBody struct {
	AsignadoA string `json:"asignado_a,omitempty" doc:"Operador al que se asigna la conversación; opcional"`
}

// EscalarOutput wraps the response body for POST .../escalar.
type EscalarOutput struct {
	Body OkDTO
}
