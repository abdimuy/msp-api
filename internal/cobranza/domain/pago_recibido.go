//nolint:misspell // domain vocabulary is Spanish (concepto, importe, cobrador, etc.) per project convention.
package domain

import (
	"iter"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// Column-width limits matching MSP_PAGOS_RECIBIDOS.
const (
	maxCobradorLength    = 100
	maxLatLonLength      = 20
	maxUltimoErrorLength = 500
	maxFolioLength       = 20
)

// Microsip concepto_cc_id mapping driven by forma_cobro_id.
//
//	formaCobroIDAbonoMostrador (137026) → conceptoAbonoMostrador  (27969)
//	any other                          → conceptoCobranzaRuta     (87327)
//
// Pulled from the legacy Node implementation (sys_msp_backend
// ventas/stores.ts ~ línea 432). Treated here as constant business rules; if
// either id ever changes in Microsip we update these constants.
const (
	formaCobroIDAbonoMostrador = 137026
	conceptoAbonoMostrador     = 27969
	conceptoCobranzaRuta       = 87327
)

// DerivarConceptoCC maps a forma_cobro_id to its corresponding concepto_cc_id
// per the cobranza ruta rules. Exposed for tests and the app layer; the
// constructor calls it internally.
func DerivarConceptoCC(formaCobroID int) int {
	if formaCobroID == formaCobroIDAbonoMostrador {
		return conceptoAbonoMostrador
	}
	return conceptoCobranzaRuta
}

// PagoRecibido is the writable aggregate root for a payment captured by the
// mobile app and persisted into MSP_PAGOS_RECIBIDOS as part of the cobranza
// outbox. It is *separate* from the read-only Pago value object, which
// projects MSP_PAGOS_VENTAS (the Microsip-driven cache).
//
// State machine (Sincronizacion):
//
//	pendiente  — fila guardada por CrearPago, aún no aplicada a Microsip.
//	aplicada   — los 5 INSERTs a Microsip corrieron OK; DoctoCCID,
//	             ImpteDoctoCCID y Folio están populados.
//
// La única transición legal es pendiente → aplicada (vía MarcarAplicada). Si
// el writer falla se llama RegistrarFallo: incrementa el contador de intentos
// y guarda el último error, pero NO cambia el estado — el pago queda
// pendiente para el PagoRetryWorker.
//
// El receivedAt es la marca temporal *del servidor* (cuándo aterrizó la
// request), separada del FechaHoraPago que es la captura *del cliente* (cuándo
// el cobrador registró el pago en la app). El cliente es autoritativo sobre
// la fecha; el servidor solo registra cuándo recibió la petición para audit.
type PagoRecibido struct {
	id             uuid.UUID
	cargoDoctoCCID int
	clienteID      int
	cobradorID     int
	cobrador       string
	importe        decimal.Decimal
	formaCobroID   int
	conceptoCCID   int
	fechaHoraPago  time.Time
	lat            *string
	lon            *string

	sincronizacion Sincronizacion
	intentos       int
	ultimoError    *string

	doctoCCID      *int
	impteDoctoCCID *int
	folio          *string

	receivedAt time.Time
	aplicadoAt *time.Time

	audit audit.Auditable

	imagenes []*Imagen
}

// CrearPagoRecibidoParams carries the inputs to NewPagoRecibido. ID is the
// client-generated UUID and acts as the idempotency key end-to-end.
type CrearPagoRecibidoParams struct {
	ID             uuid.UUID
	CargoDoctoCCID int
	ClienteID      int
	CobradorID     int
	Cobrador       string
	Importe        decimal.Decimal
	FormaCobroID   int
	FechaHoraPago  time.Time
	Lat            *string
	Lon            *string
	CreatedBy      uuid.UUID
	Now            time.Time
}

// NewPagoRecibido validates the inputs, derives the Microsip concepto from
// the forma_cobro_id, and constructs a fresh PagoRecibido in
// SincronizacionPendiente. The caller (app layer) is responsible for
// timestamp-bounds validation against business rules (futuro, muy antigua,
// late upload) and for resolving + locking the cargo balance.
func NewPagoRecibido(p CrearPagoRecibidoParams) (*PagoRecibido, error) {
	if p.ID == uuid.Nil {
		return nil, ErrPagoIDRequerido
	}
	if p.CargoDoctoCCID <= 0 {
		return nil, ErrPagoCargoIDInvalido
	}
	if p.ClienteID <= 0 {
		return nil, ErrPagoClienteIDInvalido
	}
	if p.CobradorID <= 0 {
		return nil, ErrPagoCobradorIDInvalido
	}
	if p.FormaCobroID <= 0 {
		return nil, ErrPagoFormaCobroInvalida
	}
	if !p.Importe.IsPositive() {
		return nil, ErrPagoImporteInvalido
	}
	cobrador, err := requireBounded(p.Cobrador, maxCobradorLength, ErrPagoCobradorRequerido, ErrPagoCobradorDemasiadoLargo)
	if err != nil {
		return nil, err
	}
	lat, err := trimOptionalBounded(p.Lat, maxLatLonLength, ErrPagoLatLonInvalida)
	if err != nil {
		return nil, err
	}
	lon, err := trimOptionalBounded(p.Lon, maxLatLonLength, ErrPagoLatLonInvalida)
	if err != nil {
		return nil, err
	}
	return &PagoRecibido{
		id:             p.ID,
		cargoDoctoCCID: p.CargoDoctoCCID,
		clienteID:      p.ClienteID,
		cobradorID:     p.CobradorID,
		cobrador:       cobrador,
		importe:        p.Importe,
		formaCobroID:   p.FormaCobroID,
		conceptoCCID:   DerivarConceptoCC(p.FormaCobroID),
		fechaHoraPago:  p.FechaHoraPago,
		lat:            lat,
		lon:            lon,
		sincronizacion: SincronizacionPendiente,
		intentos:       0,
		receivedAt:     p.Now,
		audit:          audit.NewAuditable(p.Now, p.CreatedBy),
	}, nil
}

// HydratePagoRecibidoParams carries the persisted shape of a PagoRecibido for
// repository reconstruction. The repo passes pointers for fields that may be
// NULL in the DB (backfilled rows from before this migration, or pending rows
// before they're applied).
type HydratePagoRecibidoParams struct {
	ID             uuid.UUID
	CargoDoctoCCID int
	ClienteID      int
	CobradorID     int
	Cobrador       string
	Importe        decimal.Decimal
	FormaCobroID   int
	ConceptoCCID   int
	FechaHoraPago  time.Time
	Lat            *string
	Lon            *string

	Sincronizacion Sincronizacion
	Intentos       int
	UltimoError    *string

	DoctoCCID      *int
	ImpteDoctoCCID *int
	Folio          *string

	ReceivedAt time.Time
	AplicadoAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
	CreatedBy uuid.UUID
	UpdatedBy uuid.UUID

	Imagenes []*Imagen
}

// HydratePagoRecibido rebuilds a PagoRecibido from persistence without
// validation. The repo MUST trust the persisted state — old/backfilled rows
// may carry zero values for fields the writable flow always sets.
func HydratePagoRecibido(p HydratePagoRecibidoParams) *PagoRecibido {
	return &PagoRecibido{
		id:             p.ID,
		cargoDoctoCCID: p.CargoDoctoCCID,
		clienteID:      p.ClienteID,
		cobradorID:     p.CobradorID,
		cobrador:       p.Cobrador,
		importe:        p.Importe,
		formaCobroID:   p.FormaCobroID,
		conceptoCCID:   p.ConceptoCCID,
		fechaHoraPago:  p.FechaHoraPago,
		lat:            p.Lat,
		lon:            p.Lon,
		sincronizacion: p.Sincronizacion,
		intentos:       p.Intentos,
		ultimoError:    p.UltimoError,
		doctoCCID:      p.DoctoCCID,
		impteDoctoCCID: p.ImpteDoctoCCID,
		folio:          p.Folio,
		receivedAt:     p.ReceivedAt,
		aplicadoAt:     p.AplicadoAt,
		audit:          audit.HydrateAuditable(p.CreatedAt, p.UpdatedAt, p.CreatedBy, p.UpdatedBy),
		imagenes:       p.Imagenes,
	}
}

// ─── Getters ────────────────────────────────────────────────────────────────

// ID returns the payment's UUID (idempotency key end-to-end).
func (p *PagoRecibido) ID() uuid.UUID { return p.id }

// CargoDoctoCCID returns the cargo being paid (DOCTOS_CC.DOCTO_CC_ID with
// TIPO_DOCTO='C' in Microsip).
func (p *PagoRecibido) CargoDoctoCCID() int { return p.cargoDoctoCCID }

// ClienteID returns the Microsip cliente_id.
func (p *PagoRecibido) ClienteID() int { return p.clienteID }

// CobradorID returns the Microsip cobrador_id.
func (p *PagoRecibido) CobradorID() int { return p.cobradorID }

// Cobrador returns the cobrador's free-text name (DESCRIPCION column).
func (p *PagoRecibido) Cobrador() string { return p.cobrador }

// Importe returns the payment amount.
func (p *PagoRecibido) Importe() decimal.Decimal { return p.importe }

// FormaCobroID returns the Microsip forma_cobro_id (efectivo/cheque/transfer).
func (p *PagoRecibido) FormaCobroID() int { return p.formaCobroID }

// ConceptoCCID returns the derived Microsip concepto_cc_id (cobranza ruta or
// abono mostrador).
func (p *PagoRecibido) ConceptoCCID() int { return p.conceptoCCID }

// FechaHoraPago returns the client-captured payment timestamp (FECHA in DB).
func (p *PagoRecibido) FechaHoraPago() time.Time { return p.fechaHoraPago }

// Lat returns the latitude where the payment was captured, when available.
func (p *PagoRecibido) Lat() *string { return p.lat }

// Lon returns the longitude where the payment was captured, when available.
func (p *PagoRecibido) Lon() *string { return p.lon }

// Sincronizacion returns the current state of the payment (pendiente or
// aplicada).
func (p *PagoRecibido) Sincronizacion() Sincronizacion { return p.sincronizacion }

// IsPendiente reports whether the payment is awaiting application to Microsip.
func (p *PagoRecibido) IsPendiente() bool { return p.sincronizacion == SincronizacionPendiente }

// IsAplicada reports whether the payment has been applied to Microsip.
func (p *PagoRecibido) IsAplicada() bool { return p.sincronizacion == SincronizacionAplicada }

// Intentos returns the number of apply attempts.
func (p *PagoRecibido) Intentos() int { return p.intentos }

// UltimoError returns the last writer error message, if any.
func (p *PagoRecibido) UltimoError() *string { return p.ultimoError }

// DoctoCCID returns the Microsip DOCTOS_CC.DOCTO_CC_ID of the applied abono,
// or nil if not yet applied.
func (p *PagoRecibido) DoctoCCID() *int { return p.doctoCCID }

// ImpteDoctoCCID returns the Microsip IMPORTES_DOCTOS_CC.IMPTE_DOCTO_CC_ID of
// the applied importe, or nil if not yet applied.
func (p *PagoRecibido) ImpteDoctoCCID() *int { return p.impteDoctoCCID }

// Folio returns the Microsip folio assigned to the abono, or nil if not yet
// applied.
func (p *PagoRecibido) Folio() *string { return p.folio }

// ReceivedAt returns the server-side timestamp of when the payment was
// received (audit, separate from FechaHoraPago).
func (p *PagoRecibido) ReceivedAt() time.Time { return p.receivedAt }

// AplicadoAt returns the server-side timestamp of when the payment was
// successfully applied, or nil if pendiente.
func (p *PagoRecibido) AplicadoAt() *time.Time { return p.aplicadoAt }

// Audit returns a copy of the audit subrecord.
func (p *PagoRecibido) Audit() audit.Auditable { return p.audit }

// Imagenes returns an iterator over the comprobantes attached to this pago.
func (p *PagoRecibido) Imagenes() iter.Seq[*Imagen] {
	return func(yield func(*Imagen) bool) {
		for _, img := range p.imagenes {
			if !yield(img) {
				return
			}
		}
	}
}

// ImagenesCount returns the number of comprobantes attached.
func (p *PagoRecibido) ImagenesCount() int { return len(p.imagenes) }

// ImagenesForRepo returns the live imagenes slice. Repo-only, do not mutate.
func (p *PagoRecibido) ImagenesForRepo() []*Imagen { return p.imagenes }

// ─── State transitions ─────────────────────────────────────────────────────

// PreconditionForAplicar checks whether the payment is in a state that allows
// Aplicar() to proceed. Returns nil if pendiente; otherwise an error that
// the app layer maps to a user-visible response.
func (p *PagoRecibido) PreconditionForAplicar() error {
	if p.IsAplicada() {
		return ErrPagoYaAplicado
	}
	if !p.sincronizacion.IsValid() {
		return ErrSincronizacionInvalida
	}
	return nil
}

// MarcarAplicada transitions the payment from pendiente to aplicada,
// recording the Microsip artifacts (DoctoCCID, ImpteDoctoCCID, Folio) and
// the application timestamp. Returns ErrPagoYaAplicado on double-apply.
func (p *PagoRecibido) MarcarAplicada(doctoCCID, impteDoctoCCID int, folio string, now time.Time, by uuid.UUID) error {
	if err := p.PreconditionForAplicar(); err != nil {
		return err
	}
	if doctoCCID <= 0 {
		return ErrPagoDoctoCCIDInvalido
	}
	if impteDoctoCCID <= 0 {
		return ErrPagoImpteDoctoCCIDInvalido
	}
	folioTrimmed := strings.TrimSpace(folio)
	if folioTrimmed == "" {
		return ErrPagoFolioRequerido
	}
	if utf8RuneLen(folioTrimmed) > maxFolioLength {
		return ErrPagoFolioDemasiadoLargo
	}
	p.doctoCCID = &doctoCCID
	p.impteDoctoCCID = &impteDoctoCCID
	p.folio = &folioTrimmed
	p.sincronizacion = SincronizacionAplicada
	p.aplicadoAt = &now
	p.ultimoError = nil
	p.audit.MarkUpdated(by)
	return nil
}

// RegistrarFallo increments the intentos counter and stores the writer error
// message. Does NOT transition state — the payment stays pendiente so the
// retry worker can pick it up later. Truncates excessively long errors at
// the column boundary.
func (p *PagoRecibido) RegistrarFallo(msg string, now time.Time, by uuid.UUID) {
	trimmed := normalizeNFC(strings.TrimSpace(msg))
	if trimmed == "" {
		trimmed = "error desconocido del writer"
	}
	if utf8RuneLen(trimmed) > maxUltimoErrorLength {
		// Truncate by runes, not bytes, to stay within the column width while
		// preserving valid UTF-8.
		runes := []rune(trimmed)
		trimmed = string(runes[:maxUltimoErrorLength])
	}
	p.intentos++
	p.ultimoError = &trimmed
	// receivedAt unchanged; updatedAt reflects the retry attempt for audit.
	_ = now
	p.audit.MarkUpdated(by)
}

// ─── Imagen mutators ────────────────────────────────────────────────────────

// AdjuntarImagenParams aggregates the inputs to AdjuntarImagen.
type AdjuntarImagenParams struct {
	ID          uuid.UUID
	Storage     ImagenStorage
	Mime        string
	SizeBytes   int64
	Descripcion *string
	By          uuid.UUID
	Now         time.Time
}

// AdjuntarImagen attaches a fresh comprobante to the pago. The pago may be
// pendiente or aplicada — comprobantes are out-of-band and may be uploaded
// after the apply succeeds.
func (p *PagoRecibido) AdjuntarImagen(params AdjuntarImagenParams) (*Imagen, error) {
	img, err := newImagen(NewImagenParams{
		ID:          params.ID,
		Storage:     params.Storage,
		Mime:        params.Mime,
		SizeBytes:   params.SizeBytes,
		Descripcion: params.Descripcion,
		CreatedBy:   params.By,
		Now:         params.Now,
	})
	if err != nil {
		return nil, err
	}
	p.imagenes = append(p.imagenes, img)
	p.audit.MarkUpdated(params.By)
	return img, nil
}

// EliminarImagen removes the comprobante with the given ID.
func (p *PagoRecibido) EliminarImagen(id, by uuid.UUID, now time.Time) error {
	idx := -1
	for i, img := range p.imagenes {
		if img.ID() == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrImagenNoEncontrada
	}
	p.imagenes = append(p.imagenes[:idx], p.imagenes[idx+1:]...)
	p.audit.MarkUpdated(by)
	_ = now
	return nil
}
