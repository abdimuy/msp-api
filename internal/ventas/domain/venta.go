//nolint:misspell // domain vocabulary is Spanish (productos, etc.) per project convention.
package domain

import (
	"iter"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// maxNotaLength is the byte width of the nota column on MSP_VENTAS.
const maxNotaLength = 500

// Venta is the aggregate root of the ventas module. It snapshots the cliente,
// dirección, vendedores and pricing at the moment of sale, and owns the
// child Combos / Productos / Vendedores / Imagenes collections.
type Venta struct {
	id             uuid.UUID
	cliente        ClienteSnapshot
	direccion      Direccion
	gps            GPSCoords
	almacenOrigen  int
	almacenDestino int
	fechaVenta     time.Time
	tipoVenta      TipoVenta
	montos         MontoSnapshot
	planCredito    *PlanCredito
	diaCobranza    *DiaCobranza
	nota           *string
	combos         []*Combo
	productos      []*Producto
	vendedores     []*Vendedor
	imagenes       []*Imagen
	audit          audit.Auditable
	cancelacion    *Cancelacion
	pendingEvents  []Event
}

// CrearVentaProductoInput is one producto line submitted to CrearVenta.
type CrearVentaProductoInput struct {
	ID         uuid.UUID
	ArticuloID int
	Articulo   string
	Cantidad   decimal.Decimal
	Precios    MontoSnapshot
	ComboID    *uuid.UUID
}

// CrearVentaComboInput is one combo submitted to CrearVenta.
type CrearVentaComboInput struct {
	ID      uuid.UUID
	Nombre  string
	Precios MontoSnapshot
}

// CrearVentaVendedorInput is one vendedor submitted to CrearVenta.
type CrearVentaVendedorInput struct {
	ID        uuid.UUID
	UsuarioID uuid.UUID
	Email     string
	Nombre    string
}

// CrearVentaParams aggregates every field needed to build a fresh Venta.
type CrearVentaParams struct {
	ID             uuid.UUID
	Cliente        ClienteSnapshot
	Direccion      Direccion
	GPS            GPSCoords
	AlmacenOrigen  int
	AlmacenDestino int
	FechaVenta     time.Time
	TipoVenta      TipoVenta
	Montos         MontoSnapshot
	PlanCredito    *PlanCredito
	DiaCobranza    *DiaCobranza
	Nota           *string
	Combos         []CrearVentaComboInput
	Productos      []CrearVentaProductoInput
	Vendedores     []CrearVentaVendedorInput
	CreatedBy      uuid.UUID
	Now            time.Time
}

// CrearVenta validates the inputs, builds the aggregate, and emits a
// VentaCreadaEvent. Every CHECK constraint in MSP_VENTAS is replicated here.
func CrearVenta(p CrearVentaParams) (*Venta, error) {
	if err := validateHeader(p); err != nil {
		return nil, err
	}
	if err := validateCreditoCoherencia(p); err != nil {
		return nil, err
	}
	nota, err := validateNota(p.Nota)
	if err != nil {
		return nil, err
	}
	combos, productos, vendedores, err := buildChildren(p)
	if err != nil {
		return nil, err
	}
	v := &Venta{
		id:             p.ID,
		cliente:        p.Cliente,
		direccion:      p.Direccion,
		gps:            p.GPS,
		almacenOrigen:  p.AlmacenOrigen,
		almacenDestino: p.AlmacenDestino,
		fechaVenta:     p.FechaVenta,
		tipoVenta:      p.TipoVenta,
		montos:         p.Montos,
		planCredito:    p.PlanCredito,
		diaCobranza:    p.DiaCobranza,
		nota:           nota,
		combos:         combos,
		productos:      productos,
		vendedores:     vendedores,
		imagenes:       nil,
		audit:          audit.NewAuditable(p.Now, p.CreatedBy),
	}
	v.pendingEvents = []Event{NewVentaCreadaEvent(v.id, v.tipoVenta, p.CreatedBy, p.Now)}
	return v, nil
}

// validateHeader checks the venta header invariants that do not depend on
// the CONTADO/CREDITO coherence rules.
func validateHeader(p CrearVentaParams) error {
	if !p.TipoVenta.IsValid() {
		return ErrTipoVentaInvalido
	}
	if p.FechaVenta.IsZero() {
		return ErrFechaVentaZero
	}
	if p.AlmacenOrigen == p.AlmacenDestino {
		return ErrVentaAlmacenesIguales
	}
	if p.Cliente.Nombre().IsZero() {
		return ErrNombreClienteRequerido
	}
	if len(p.Productos) == 0 {
		return ErrVentaProductosVacios
	}
	if len(p.Vendedores) == 0 {
		return ErrVentaVendedoresVacios
	}
	return nil
}

// validateCreditoCoherencia replicates CK_MSP_VENTAS_TIPO_CREDITO_COHERENTE
// and CK_MSP_VENTAS_DIA_COBRANZA_COHERENTE.
func validateCreditoCoherencia(p CrearVentaParams) error {
	switch p.TipoVenta {
	case TipoVentaContado:
		if p.PlanCredito != nil || p.DiaCobranza != nil {
			return ErrPlanCreditoNoPermitidoEnContado
		}
		return nil
	case TipoVentaCredito:
		if p.PlanCredito == nil {
			return ErrPlanCreditoRequiredEnCredito
		}
		if p.DiaCobranza == nil {
			return ErrDiaCobranzaRequeridoEnCredito
		}
		return validateDiaCobranzaForFrec(p.PlanCredito.FrecPago(), p.DiaCobranza)
	}
	return ErrTipoVentaInvalido
}

// validateDiaCobranzaForFrec enforces the (frec_pago, dia_cobranza) coherence
// matrix mirrored from CK_MSP_VENTAS_DIA_COBRANZA_COHERENTE.
func validateDiaCobranzaForFrec(frec FrecPago, dc *DiaCobranza) error {
	switch frec {
	case FrecPagoSemanal:
		if !dc.IsSemana() || dc.IsMes() {
			return ErrDiaCobranzaIncoherenteSemanal
		}
		return nil
	case FrecPagoQuincenal, FrecPagoMensual:
		// Exactly one of semana/mes must be set.
		if dc.IsSemana() == dc.IsMes() {
			return ErrDiaCobranzaIncoherenteQuincenalMensual
		}
		return nil
	}
	return ErrFrecPagoInvalida
}

// validateNota normalizes the optional nota field. Returns the trimmed
// pointer (nil when blank).
func validateNota(p *string) (*string, error) {
	return trimOptionalBounded(p, maxNotaLength, ErrNotaDemasiadoLarga)
}

// buildChildren constructs the child collections in one shot.
func buildChildren(p CrearVentaParams) ([]*Combo, []*Producto, []*Vendedor, error) {
	combos, err := buildCombos(p.Combos, p.CreatedBy, p.Now)
	if err != nil {
		return nil, nil, nil, err
	}
	productos, err := buildProductos(p.Productos, p.CreatedBy, p.Now)
	if err != nil {
		return nil, nil, nil, err
	}
	vendedores, err := buildVendedores(p.Vendedores, p.CreatedBy, p.Now)
	if err != nil {
		return nil, nil, nil, err
	}
	return combos, productos, vendedores, nil
}

// buildCombos materializes the combo entities from the input rows.
func buildCombos(in []CrearVentaComboInput, createdBy uuid.UUID, now time.Time) ([]*Combo, error) {
	out := make([]*Combo, 0, len(in))
	for _, c := range in {
		combo, err := newCombo(NewComboParams{
			ID: c.ID, Nombre: c.Nombre, Precios: c.Precios,
			CreatedBy: createdBy, Now: now,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, combo)
	}
	return out, nil
}

// buildProductos materializes the producto entities from the input rows.
func buildProductos(in []CrearVentaProductoInput, createdBy uuid.UUID, now time.Time) ([]*Producto, error) {
	out := make([]*Producto, 0, len(in))
	for _, pr := range in {
		producto, err := newProducto(NewProductoParams{
			ID:         pr.ID,
			ArticuloID: pr.ArticuloID,
			Articulo:   pr.Articulo,
			Cantidad:   pr.Cantidad,
			Precios:    pr.Precios,
			ComboID:    pr.ComboID,
			CreatedBy:  createdBy,
			Now:        now,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, producto)
	}
	return out, nil
}

// buildVendedores materializes the vendedor entities from the input rows.
func buildVendedores(in []CrearVentaVendedorInput, createdBy uuid.UUID, now time.Time) ([]*Vendedor, error) {
	out := make([]*Vendedor, 0, len(in))
	for _, v := range in {
		snapshot, err := NewVendedorSnapshot(NewVendedorSnapshotParams{
			UsuarioID: v.UsuarioID,
			Email:     v.Email,
			Nombre:    v.Nombre,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, newVendedor(NewVendedorParams{
			ID: v.ID, Snapshot: snapshot, CreatedBy: createdBy, Now: now,
		}))
	}
	return out, nil
}

// HydrateVentaParams carries the persisted shape of a Venta for repository
// reconstruction.
type HydrateVentaParams struct {
	ID             uuid.UUID
	Cliente        ClienteSnapshot
	Direccion      Direccion
	GPS            GPSCoords
	AlmacenOrigen  int
	AlmacenDestino int
	FechaVenta     time.Time
	TipoVenta      TipoVenta
	Montos         MontoSnapshot
	PlanCredito    *PlanCredito
	DiaCobranza    *DiaCobranza
	Nota           *string
	Combos         []*Combo
	Productos      []*Producto
	Vendedores     []*Vendedor
	Imagenes       []*Imagen
	Cancelacion    *Cancelacion
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CreatedBy      uuid.UUID
	UpdatedBy      uuid.UUID
}

// HydrateVenta rebuilds a Venta from persistence without validation.
func HydrateVenta(p HydrateVentaParams) *Venta {
	return &Venta{
		id:             p.ID,
		cliente:        p.Cliente,
		direccion:      p.Direccion,
		gps:            p.GPS,
		almacenOrigen:  p.AlmacenOrigen,
		almacenDestino: p.AlmacenDestino,
		fechaVenta:     p.FechaVenta,
		tipoVenta:      p.TipoVenta,
		montos:         p.Montos,
		planCredito:    p.PlanCredito,
		diaCobranza:    p.DiaCobranza,
		nota:           p.Nota,
		combos:         p.Combos,
		productos:      p.Productos,
		vendedores:     p.Vendedores,
		imagenes:       p.Imagenes,
		audit:          audit.HydrateAuditable(p.CreatedAt, p.UpdatedAt, p.CreatedBy, p.UpdatedBy),
		cancelacion:    p.Cancelacion,
	}
}

// ─── Accessors ─────────────────────────────────────────────────────────────

// ID returns the venta's primary key.
func (v *Venta) ID() uuid.UUID { return v.id }

// Cliente returns the cliente snapshot.
func (v *Venta) Cliente() ClienteSnapshot { return v.cliente }

// Direccion returns the dirección snapshot.
func (v *Venta) Direccion() Direccion { return v.direccion }

// GPS returns the gps coordinates.
func (v *Venta) GPS() GPSCoords { return v.gps }

// AlmacenOrigen returns the origin warehouse ID.
func (v *Venta) AlmacenOrigen() int { return v.almacenOrigen }

// AlmacenDestino returns the destination warehouse ID.
func (v *Venta) AlmacenDestino() int { return v.almacenDestino }

// FechaVenta returns the sale timestamp.
func (v *Venta) FechaVenta() time.Time { return v.fechaVenta }

// TipoVenta returns the sale type.
func (v *Venta) TipoVenta() TipoVenta { return v.tipoVenta }

// Montos returns the three-price monto snapshot.
func (v *Venta) Montos() MontoSnapshot { return v.montos }

// PlanCredito returns the credit plan or nil for CONTADO ventas.
func (v *Venta) PlanCredito() *PlanCredito { return v.planCredito }

// DiaCobranza returns the cobranza day VO or nil for CONTADO ventas.
func (v *Venta) DiaCobranza() *DiaCobranza { return v.diaCobranza }

// Nota returns the optional free-form note.
func (v *Venta) Nota() *string { return v.nota }

// Audit returns a copy of the audit subrecord.
func (v *Venta) Audit() audit.Auditable { return v.audit }

// Cancelacion returns the cancellation record or nil when not canceled.
func (v *Venta) Cancelacion() *Cancelacion { return v.cancelacion }

// IsCanceled reports whether the venta has been canceled.
func (v *Venta) IsCanceled() bool { return v.cancelacion != nil }

// ─── Read-only child iterators ─────────────────────────────────────────────

// Combos returns an iterator over the combos of this venta.
func (v *Venta) Combos() iter.Seq[*Combo] {
	return func(yield func(*Combo) bool) {
		for _, c := range v.combos {
			if !yield(c) {
				return
			}
		}
	}
}

// Productos returns an iterator over the productos of this venta.
func (v *Venta) Productos() iter.Seq[*Producto] {
	return func(yield func(*Producto) bool) {
		for _, p := range v.productos {
			if !yield(p) {
				return
			}
		}
	}
}

// Vendedores returns an iterator over the vendedores of this venta.
func (v *Venta) Vendedores() iter.Seq[*Vendedor] {
	return func(yield func(*Vendedor) bool) {
		for _, vd := range v.vendedores {
			if !yield(vd) {
				return
			}
		}
	}
}

// Imagenes returns an iterator over the imágenes of this venta.
func (v *Venta) Imagenes() iter.Seq[*Imagen] {
	return func(yield func(*Imagen) bool) {
		for _, i := range v.imagenes {
			if !yield(i) {
				return
			}
		}
	}
}

// CombosCount returns the number of combos.
func (v *Venta) CombosCount() int { return len(v.combos) }

// ProductosCount returns the number of productos.
func (v *Venta) ProductosCount() int { return len(v.productos) }

// VendedoresCount returns the number of vendedores.
func (v *Venta) VendedoresCount() int { return len(v.vendedores) }

// ImagenesCount returns the number of imágenes.
func (v *Venta) ImagenesCount() int { return len(v.imagenes) }

// CombosForRepo returns the live combos slice. Intended for the repository
// layer only — callers must not mutate the returned slice.
func (v *Venta) CombosForRepo() []*Combo { return v.combos }

// ProductosForRepo returns the live productos slice. Repo-only, do not
// mutate.
func (v *Venta) ProductosForRepo() []*Producto { return v.productos }

// VendedoresForRepo returns the live vendedores slice. Repo-only, do not
// mutate.
func (v *Venta) VendedoresForRepo() []*Vendedor { return v.vendedores }

// ImagenesForRepo returns the live imagenes slice. Repo-only, do not mutate.
func (v *Venta) ImagenesForRepo() []*Imagen { return v.imagenes }

// ─── Mutators ──────────────────────────────────────────────────────────────

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

// AdjuntarImagen attaches a fresh imagen to the venta and emits an
// ImagenAdjuntadaEvent. Refuses to mutate a canceled venta.
func (v *Venta) AdjuntarImagen(p AdjuntarImagenParams) (*Imagen, error) {
	if v.IsCanceled() {
		return nil, ErrVentaCanceladaInmutable
	}
	img, err := newImagen(NewImagenParams{
		ID:          p.ID,
		Storage:     p.Storage,
		Mime:        p.Mime,
		SizeBytes:   p.SizeBytes,
		Descripcion: p.Descripcion,
		CreatedBy:   p.By,
		Now:         p.Now,
	})
	if err != nil {
		return nil, err
	}
	v.imagenes = append(v.imagenes, img)
	v.audit.MarkUpdated(p.By)
	v.pendingEvents = append(v.pendingEvents, NewImagenAdjuntadaEvent(NewImagenAdjuntadaEventParams{
		VentaID:    v.id,
		ImagenID:   img.ID(),
		StorageKey: img.Storage().Key(),
		Mime:       img.Mime(),
		SizeBytes:  img.SizeBytes(),
		Now:        p.Now,
	}))
	return img, nil
}

// EliminarImagen removes the imagen with the given ID and emits an
// ImagenEliminadaEvent. Refuses to mutate a canceled venta.
func (v *Venta) EliminarImagen(id, by uuid.UUID, now time.Time) error {
	if v.IsCanceled() {
		return ErrVentaCanceladaInmutable
	}
	idx := -1
	for i, img := range v.imagenes {
		if img.ID() == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrImagenNotFound
	}
	v.imagenes = append(v.imagenes[:idx], v.imagenes[idx+1:]...)
	v.audit.MarkUpdated(by)
	v.pendingEvents = append(v.pendingEvents, NewImagenEliminadaEvent(v.id, id, now))
	return nil
}

// Cancelar soft-cancels the venta. Refuses to cancel an already-canceled
// venta (returns ErrVentaYaCancelada). Bumps audit and emits a
// VentaCanceladaEvent.
func (v *Venta) Cancelar(reason string, by uuid.UUID, now time.Time) error {
	if v.IsCanceled() {
		return ErrVentaYaCancelada
	}
	c, err := NewCancelacion(now, by, reason)
	if err != nil {
		return err
	}
	v.cancelacion = &c
	v.audit.MarkUpdated(by)
	v.pendingEvents = append(v.pendingEvents, NewVentaCanceladaEvent(v.id, by, strings.TrimSpace(reason), now))
	return nil
}

// ─── Events buffer ─────────────────────────────────────────────────────────

// PendingEvents returns a copy of the events buffered since construction or
// the last ClearPendingEvents call.
func (v *Venta) PendingEvents() []Event {
	out := make([]Event, len(v.pendingEvents))
	copy(out, v.pendingEvents)
	return out
}

// ClearPendingEvents drops every buffered event.
func (v *Venta) ClearPendingEvents() { v.pendingEvents = nil }
