//nolint:misspell // domain vocabulary is Spanish (envío, tipo, comprobante, etc.) per project convention.
package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// Envio es una entidad Type B (pipeline) según la taxonomía de CLAUDE.md.
// Almacena el estado y datos de un envío en el sistema, desde su creación
// hasta su entrega o cancelación. Es la entidad que el worker de envío reclama,
// y sobre la que operan las transiciones (Reclamar, MarcarEnviado, MarcarFallido,
// Reenviar, Detener, MarcarSinTelefono).
//
// Type B: state-machine entity que embeds audit.Timestamped (sin usuario de
// creación; el que detiene va en detenidoPor).
type Envio struct {
	id               uuid.UUID
	tipo             TipoComprobante
	referencia       string
	clienteID        int
	telefono         *string
	estado           EstadoEnvio
	programadoPara   time.Time
	documentoRuta    *string
	canal            Canal
	mensajeExternoID *string
	intentos         int
	ultimoError      *string
	detenidoPor      *string
	enviadoEn        *time.Time
	audit.Timestamped
}

// NewEnvioParams agrupa los campos para crear un Envio nuevo.
type NewEnvioParams struct {
	Tipo             TipoComprobante
	Referencia       string
	ClienteID        int
	Telefono         *string
	Estado           EstadoEnvio
	ProgramadoPara   time.Time
	DocumentoRuta    *string
	Canal            Canal
	MensajeExternoID *string
}

// NewEnvio construye un nuevo Envio validando cada campo.
// Retorna (*Envio, error) — el error es un apperror con el sentinel correspondiente.
func NewEnvio(p NewEnvioParams) (*Envio, error) {
	if !p.Tipo.IsValid() {
		return nil, ErrTipoComprobanteInvalido
	}
	if strings.TrimSpace(p.Referencia) == "" {
		return nil, ErrEnvioReferenciaRequerido
	}
	if !p.Estado.IsValid() {
		return nil, ErrEstadoEnvioInvalido
	}
	if !p.Canal.IsValid() {
		return nil, ErrCanalInvalido
	}
	return &Envio{
		id:               uuid.New(),
		tipo:             p.Tipo,
		referencia:       strings.TrimSpace(p.Referencia),
		clienteID:        p.ClienteID,
		telefono:         p.Telefono,
		estado:           p.Estado,
		programadoPara:   p.ProgramadoPara,
		documentoRuta:    p.DocumentoRuta,
		canal:            p.Canal,
		mensajeExternoID: p.MensajeExternoID,
		Timestamped:      audit.NewTimestamped(time.Now()),
	}, nil
}

// HydrateEnvioParams agrupa los campos para reconstruir un Envio desde la
// base de datos, sin validación. Solo uso de repositorio.
type HydrateEnvioParams struct {
	ID               uuid.UUID
	Tipo             TipoComprobante
	Referencia       string
	ClienteID        int
	Telefono         *string
	Estado           EstadoEnvio
	ProgramadoPara   time.Time
	DocumentoRuta    *string
	Canal            Canal
	MensajeExternoID *string
	Intentos         int
	UltimoError      *string
	DetenidoPor      *string
	EnviadoEn        *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// HydrateEnvio reconstruye un Envio desde la base de datos sin validación.
// Solo uso de repositorio.
func HydrateEnvio(p HydrateEnvioParams) *Envio {
	return &Envio{
		id:               p.ID,
		tipo:             p.Tipo,
		referencia:       p.Referencia,
		clienteID:        p.ClienteID,
		telefono:         p.Telefono,
		estado:           p.Estado,
		programadoPara:   p.ProgramadoPara,
		documentoRuta:    p.DocumentoRuta,
		canal:            p.Canal,
		mensajeExternoID: p.MensajeExternoID,
		intentos:         p.Intentos,
		ultimoError:      p.UltimoError,
		detenidoPor:      p.DetenidoPor,
		enviadoEn:        p.EnviadoEn,
		Timestamped:      audit.HydrateTimestamped(p.CreatedAt, p.UpdatedAt),
	}
}

// --- Transiciones de estado ---

// Reclamar intenta reclamar el envío pasando el estado a enviando.
// Solo puede reclamarse desde en_espera. Si la transición no es válida,
// retorna ErrEnvioTransicionInvalido y el estado NO cambia.
func (e *Envio) Reclamar() error {
	if !e.estado.CanTransitionTo(EstadoEnvioEnviando) {
		return ErrEnvioTransicionInvalido
	}
	e.transitionTo(EstadoEnvioEnviando)
	return nil
}

// MarcarEnviado marca el envío como enviado, pasando el estado a enviado.
// Solo puede marcarse desde enviando. Si la transición no es válida,
// retorna ErrEnvioTransicionInvalido y el estado NO cambia.
func (e *Envio) MarcarEnviado() error {
	if !e.estado.CanTransitionTo(EstadoEnvioEnviado) {
		return ErrEnvioTransicionInvalido
	}
	e.transitionTo(EstadoEnvioEnviado)
	now := time.Now()
	e.enviadoEn = &now
	return nil
}

// MarcarFallido marca el envío como fallido, pasando el estado a fallido.
// Solo puede marcarse desde enviando. Si la transición no es válida,
// retorna ErrEnvioTransicionInvalido y el estado NO cambia.
func (e *Envio) MarcarFallido(razon string) error {
	if !e.estado.CanTransitionTo(EstadoEnvioFallido) {
		return ErrEnvioTransicionInvalido
	}
	e.transitionTo(EstadoEnvioFallido)
	e.ultimoError = &razon
	return nil
}

// Reenviar pasa el estado de fallido a en_espera e incrementa el contador
// de intentos. Solo puede aplicarse desde fallido. Es la única transición
// que muta intentos. Si el estado no es fallido, retorna
// ErrEnvioTransicionInvalido.
func (e *Envio) Reenviar() error {
	if e.estado != EstadoEnvioFallido {
		return ErrEnvioTransicionInvalido
	}
	e.transitionTo(EstadoEnvioEnEspera)
	e.intentos++
	e.ultimoError = nil
	return nil
}

// Detener marca el envío como detenido, pasando el estado a detenido.
// Solo puede aplicarse desde en_espera. El parámetro detenidoPor identifica
// quién o qué detuvo el envío. Si la transición no es válida, retorna
// ErrEnvioTransicionInvalido.
func (e *Envio) Detener(quien string) error {
	if !e.estado.CanTransitionTo(EstadoEnvioDetenido) {
		return ErrEnvioTransicionInvalido
	}
	e.transitionTo(EstadoEnvioDetenido)
	e.detenidoPor = &quien
	return nil
}

// MarcarSinTelefono pasa el estado a sin_telefono, identificando que el envío
// no tiene teléfono utilizable. Es un estado terminal: no tiene salida.
// Solo puede aplicarse desde en_espera. Si la transición no es válida,
// retorna ErrEnvioTransicionInvalido.
func (e *Envio) MarcarSinTelefono() error {
	if !e.estado.CanTransitionTo(EstadoEnvioSinTelefono) {
		return ErrEnvioTransicionInvalido
	}
	e.transitionTo(EstadoEnvioSinTelefono)
	return nil
}

// transitionTo actualiza el estado y registra la marca de tiempo.
// Solo debe llamarse después de validar la transición.
func (e *Envio) transitionTo(nuevo EstadoEnvio) {
	e.estado = nuevo
	e.MarkUpdated()
}

// --- Getters ---

// ID returns the unique identifier of the envío.
func (e *Envio) ID() uuid.UUID { return e.id }

// Tipo returns whether the envío originates from a venta or a pago.
func (e *Envio) Tipo() TipoComprobante { return e.tipo }

// Referencia returns the source reference (venta_id or serie_folio).
func (e *Envio) Referencia() string { return e.referencia }

// ClienteID returns the snapshot of CLIENTE_ID.
func (e *Envio) ClienteID() int { return e.clienteID }

// Telefono returns the contact phone number, or nil if unavailable.
func (e *Envio) Telefono() *string { return e.telefono }

// Estado returns the current delivery state.
func (e *Envio) Estado() EstadoEnvio { return e.estado }

// ProgramadoPara returns the UTC time the envío was scheduled for.
func (e *Envio) ProgramadoPara() time.Time { return e.programadoPara }

// DocumentoRuta returns the local filesystem path to the generated PDF, or nil.
func (e *Envio) DocumentoRuta() *string { return e.documentoRuta }

// Canal returns the delivery channel (local or whatsapp_business).
func (e *Envio) Canal() Canal { return e.canal }

// MensajeExternoID returns the external message identifier, or nil.
func (e *Envio) MensajeExternoID() *string { return e.mensajeExternoID }

// Intentos returns the retry count (incremented only by Reenviar).
func (e *Envio) Intentos() int { return e.intentos }

// UltimoError returns the last recorded error message, or nil.
func (e *Envio) UltimoError() *string { return e.ultimoError }

// DetenidoPor returns who or what stopped the envío, or nil.
func (e *Envio) DetenidoPor() *string { return e.detenidoPor }

// EnviadoEn returns the time the envío was sent, or nil if not yet sent.
func (e *Envio) EnviadoEn() *time.Time { return e.enviadoEn }

// String devuelve la representación en string del estado del envío.
func (e *Envio) String() string { return string(e.estado) }
