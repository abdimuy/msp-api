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
// Reenviar, Detener).
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

// CrearEnvioParams agrupa los campos para crear un Envio nuevo.
type CrearEnvioParams struct {
	Tipo           TipoComprobante
	Referencia     string
	ClienteID      int
	Telefono       *string
	ProgramadoPara time.Time
	DocumentoRuta  *string
	Canal          Canal
}

// CrearEnvio construye un nuevo Envio validando cada campo.
// Nace siempre en en_espera con intentos = 0, salvo cuando el cliente no tiene
// teléfono utilizable — en ese caso nace en sin_telefono (estado de nacimiento,
// no una transición).
//
// El parámetro now se recibe para que el dominio nunca dependa del reloj
// interno, lo que permite pruebas deterministas.
func CrearEnvio(p CrearEnvioParams, now time.Time) (*Envio, error) {
	if !p.Tipo.IsValid() {
		return nil, ErrTipoComprobanteInvalido
	}
	if strings.TrimSpace(p.Referencia) == "" {
		return nil, ErrEnvioReferenciaRequerido
	}
	if p.ClienteID <= 0 {
		return nil, ErrEnvioClienteIDInvalido
	}
	if !p.Canal.IsValid() {
		return nil, ErrCanalInvalido
	}
	// telefono es obligatorio salvo que nazca en sin_telefono.
	// La distinción se hace aquí: si telefono es nil, nace en sin_telefono.
	estado := EstadoEnvioEnEspera
	if p.Telefono == nil || strings.TrimSpace(*p.Telefono) == "" {
		estado = EstadoEnvioSinTelefono
	}
	return &Envio{
		id:             uuid.New(),
		tipo:           p.Tipo,
		referencia:     strings.TrimSpace(p.Referencia),
		clienteID:      p.ClienteID,
		telefono:       p.Telefono,
		estado:         estado,
		programadoPara: p.ProgramadoPara,
		documentoRuta:  p.DocumentoRuta,
		canal:          p.Canal,
		Timestamped:    audit.NewTimestamped(now),
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
func (e *Envio) Reclamar(now time.Time) error {
	if !e.estado.CanTransitionTo(EstadoEnvioEnviando) {
		return ErrEnvioTransicionInvalido
	}
	e.transitionTo(EstadoEnvioEnviando, now)
	return nil
}

// MarcarEnviado marca el envío como enviado, fijando el mensajeExternoID
// devuelto por WhatsApp y el timestamp de envío.
// Solo puede marcarse desde enviando. Si la transición no es válida,
// retorna ErrEnvioTransicionInvalido y el estado NO cambia.
func (e *Envio) MarcarEnviado(mensajeExternoID string, now time.Time) error {
	if !e.estado.CanTransitionTo(EstadoEnvioEnviado) {
		return ErrEnvioTransicionInvalido
	}
	e.transitionTo(EstadoEnvioEnviado, now)
	e.mensajeExternoID = &mensajeExternoID
	e.enviadoEn = &now
	return nil
}

// MarcarFallido marca el envío como fallido, registrando el motivo.
// Solo puede marcarse desde enviando. Si la transición no es válida,
// retorna ErrEnvioTransicionInvalido y el estado NO cambia.
func (e *Envio) MarcarFallido(motivo string, now time.Time) error {
	if !e.estado.CanTransitionTo(EstadoEnvioFallido) {
		return ErrEnvioTransicionInvalido
	}
	e.transitionTo(EstadoEnvioFallido, now)
	e.ultimoError = &motivo
	return nil
}

// Reenviar pasa el estado de fallido a en_espera e incrementa el contador
// de intentos. Solo puede aplicarse desde fallido. Es la única transición
// que muta intentos. Si el estado no es fallido, retorna
// ErrEnvioTransicionInvalido.
func (e *Envio) Reenviar(now time.Time) error {
	if e.estado != EstadoEnvioFallido {
		return ErrEnvioTransicionInvalido
	}
	e.transitionTo(EstadoEnvioEnEspera, now)
	e.intentos++
	e.ultimoError = nil
	return nil
}

// Detener marca el envío como detenido, pasando el estado a detenido.
// Solo puede aplicarse desde en_espera. El parámetro por identifica
// quién o qué detuvo el envío. Si la transición no es válida, retorna
// ErrEnvioTransicionInvalido.
func (e *Envio) Detener(por string, now time.Time) error {
	if !e.estado.CanTransitionTo(EstadoEnvioDetenido) {
		return ErrEnvioTransicionInvalido
	}
	e.transitionTo(EstadoEnvioDetenido, now)
	e.detenidoPor = &por
	return nil
}

// transitionTo actualiza el estado y fija UpdatedAt en el instante de la
// operación. Solo debe llamarse después de validar la transición.
//
// Recibe now porque las transiciones ya lo traen: usar el reloj de pared aquí
// lo tiraría, y dos entidades mutadas en la misma operación deben cargar el
// mismo UpdatedAt para que quien lea las filas sepa que cambiaron juntas.
func (e *Envio) transitionTo(nuevo EstadoEnvio, now time.Time) {
	e.estado = nuevo
	e.MarkUpdatedAt(now)
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
