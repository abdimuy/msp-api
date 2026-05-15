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
//
// Lifecycle: a venta is born in StatusBorrador. While in borrador it is
// freely editable through the Actualizar* / Reemplazar* methods. The
// terminal transitions are Cancelar (→ StatusCancelada) and approval
// (→ StatusAprobada, reserved for the future promotion-to-Microsip flow).
type Venta struct {
	id            uuid.UUID
	clienteID     *int
	cliente       ClienteSnapshot
	direccion     Direccion
	gps           GPSCoords
	fechaVenta    time.Time
	tipoVenta     TipoVenta
	montos        MontoSnapshot
	planCredito   *PlanCredito
	diaCobranza   *DiaCobranza
	nota          *string
	status        VentaStatus
	combos        []*Combo
	productos     []*Producto
	vendedores    []*Vendedor
	imagenes      []*Imagen
	audit         audit.Auditable
	cancelacion   *Cancelacion
	aprobacion    *Aprobacion
	pendingEvents []Event
}

// CrearVentaProductoInput is one producto line submitted to CrearVenta.
type CrearVentaProductoInput struct {
	ID             uuid.UUID
	ArticuloID     int
	Articulo       string
	Cantidad       decimal.Decimal
	Precios        MontoSnapshot
	ComboID        *uuid.UUID
	AlmacenOrigen  *int
	AlmacenDestino *int
}

// CrearVentaComboInput is one combo submitted to CrearVenta.
type CrearVentaComboInput struct {
	ID             uuid.UUID
	Nombre         string
	Precios        MontoSnapshot
	Cantidad       decimal.Decimal
	AlmacenOrigen  int
	AlmacenDestino int
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
	ID          uuid.UUID
	ClienteID   *int
	Cliente     ClienteSnapshot
	Direccion   Direccion
	GPS         GPSCoords
	FechaVenta  time.Time
	TipoVenta   TipoVenta
	Montos      MontoSnapshot
	PlanCredito *PlanCredito
	DiaCobranza *DiaCobranza
	Nota        *string
	Combos      []CrearVentaComboInput
	Productos   []CrearVentaProductoInput
	Vendedores  []CrearVentaVendedorInput
	CreatedBy   uuid.UUID
	Now         time.Time
}

// CrearVenta validates the inputs, builds the aggregate in StatusBorrador,
// and emits a VentaCreadaEvent.
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
	if err := validateProductoComboReferences(productos, combos); err != nil {
		return nil, err
	}
	v := &Venta{
		id:          p.ID,
		clienteID:   p.ClienteID,
		cliente:     p.Cliente,
		direccion:   p.Direccion,
		gps:         p.GPS,
		fechaVenta:  p.FechaVenta,
		tipoVenta:   p.TipoVenta,
		montos:      p.Montos,
		planCredito: p.PlanCredito,
		diaCobranza: p.DiaCobranza,
		nota:        nota,
		status:      StatusBorrador,
		combos:      combos,
		productos:   productos,
		vendedores:  vendedores,
		imagenes:    nil,
		audit:       audit.NewAuditable(p.Now, p.CreatedBy),
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

// validateProductoComboReferences ensures every Producto.ComboID points to
// a Combo present in the venta's combos slice.
func validateProductoComboReferences(productos []*Producto, combos []*Combo) error {
	if len(productos) == 0 {
		return nil
	}
	known := make(map[uuid.UUID]struct{}, len(combos))
	for _, c := range combos {
		known[c.ID()] = struct{}{}
	}
	for _, p := range productos {
		cid := p.ComboID()
		if cid == nil {
			continue
		}
		if _, ok := known[*cid]; !ok {
			return ErrProductoComboReferenciaInvalida
		}
	}
	return nil
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
			Cantidad:       c.Cantidad,
			AlmacenOrigen:  c.AlmacenOrigen,
			AlmacenDestino: c.AlmacenDestino,
			CreatedBy:      createdBy, Now: now,
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
			ID:             pr.ID,
			ArticuloID:     pr.ArticuloID,
			Articulo:       pr.Articulo,
			Cantidad:       pr.Cantidad,
			Precios:        pr.Precios,
			ComboID:        pr.ComboID,
			AlmacenOrigen:  pr.AlmacenOrigen,
			AlmacenDestino: pr.AlmacenDestino,
			CreatedBy:      createdBy,
			Now:            now,
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
	ID          uuid.UUID
	ClienteID   *int
	Cliente     ClienteSnapshot
	Direccion   Direccion
	GPS         GPSCoords
	FechaVenta  time.Time
	TipoVenta   TipoVenta
	Montos      MontoSnapshot
	PlanCredito *PlanCredito
	DiaCobranza *DiaCobranza
	Nota        *string
	Status      VentaStatus
	Combos      []*Combo
	Productos   []*Producto
	Vendedores  []*Vendedor
	Imagenes    []*Imagen
	Cancelacion *Cancelacion
	Aprobacion  *Aprobacion
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedBy   uuid.UUID
	UpdatedBy   uuid.UUID
}

// HydrateVenta rebuilds a Venta from persistence without validation.
func HydrateVenta(p HydrateVentaParams) *Venta {
	return &Venta{
		id:          p.ID,
		clienteID:   p.ClienteID,
		cliente:     p.Cliente,
		direccion:   p.Direccion,
		gps:         p.GPS,
		fechaVenta:  p.FechaVenta,
		tipoVenta:   p.TipoVenta,
		montos:      p.Montos,
		planCredito: p.PlanCredito,
		diaCobranza: p.DiaCobranza,
		nota:        p.Nota,
		status:      p.Status,
		combos:      p.Combos,
		productos:   p.Productos,
		vendedores:  p.Vendedores,
		imagenes:    p.Imagenes,
		audit:       audit.HydrateAuditable(p.CreatedAt, p.UpdatedAt, p.CreatedBy, p.UpdatedBy),
		cancelacion: p.Cancelacion,
		aprobacion:  p.Aprobacion,
	}
}

// ─── Accessors ─────────────────────────────────────────────────────────────

// ID returns the venta's primary key.
func (v *Venta) ID() uuid.UUID { return v.id }

// ClienteID returns the optional Microsip cliente identifier.
func (v *Venta) ClienteID() *int { return v.clienteID }

// Cliente returns the cliente snapshot.
func (v *Venta) Cliente() ClienteSnapshot { return v.cliente }

// Direccion returns the dirección snapshot.
func (v *Venta) Direccion() Direccion { return v.direccion }

// GPS returns the gps coordinates.
func (v *Venta) GPS() GPSCoords { return v.gps }

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

// Status returns the current lifecycle stage.
func (v *Venta) Status() VentaStatus { return v.status }

// Audit returns a copy of the audit subrecord.
func (v *Venta) Audit() audit.Auditable { return v.audit }

// Cancelacion returns the cancellation record or nil when not canceled.
func (v *Venta) Cancelacion() *Cancelacion { return v.cancelacion }

// Aprobacion returns the approval record or nil when not approved.
func (v *Venta) Aprobacion() *Aprobacion { return v.aprobacion }

// IsCanceled reports whether the venta has been canceled.
func (v *Venta) IsCanceled() bool { return v.cancelacion != nil }

// puedeEditarse reports whether the venta is in a state that accepts edits.
// Only StatusBorrador allows edits — aprobada and cancelada are terminal
// states for the purposes of mutation.
func (v *Venta) puedeEditarse() bool { return v.status == StatusBorrador }

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

// Cancelar soft-cancels the venta and transitions status to StatusCancelada.
//
// Only StatusBorrador may be canceled. An already-canceled venta returns
// ErrVentaYaCancelada; an aprobada venta returns ErrVentaNoEditable —
// once approved the venta is terminal (either pushed to Microsip or
// awaiting the push), and direct cancellation would diverge the two
// sources of truth. The correct flow for an approved venta is to first
// revert the approval (future operation), then cancel from borrador.
func (v *Venta) Cancelar(reason string, by uuid.UUID, now time.Time) error {
	if v.IsCanceled() {
		return ErrVentaYaCancelada
	}
	if v.status != StatusBorrador {
		return ErrVentaNoEditable
	}
	c, err := NewCancelacion(now, by, reason)
	if err != nil {
		return err
	}
	v.cancelacion = &c
	v.status = StatusCancelada
	v.audit.MarkUpdated(by)
	v.pendingEvents = append(v.pendingEvents, NewVentaCanceladaEvent(v.id, by, strings.TrimSpace(reason), now))
	return nil
}

// ─── Edit methods (only valid in StatusBorrador) ───────────────────────────

// ActualizarHeaderParams carries the editable header fields. TipoVenta is
// intentionally absent — changing CONTADO↔CREDITO requires cancel + recreate.
type ActualizarHeaderParams struct {
	Direccion   Direccion
	GPS         GPSCoords
	FechaVenta  time.Time
	Montos      MontoSnapshot
	PlanCredito *PlanCredito
	DiaCobranza *DiaCobranza
	Nota        *string
	By          uuid.UUID
	Now         time.Time
}

// ActualizarHeader mutates the venta's header fields. Only valid while the
// venta is in StatusBorrador; emits VentaHeaderActualizadoEvent.
func (v *Venta) ActualizarHeader(p ActualizarHeaderParams) error {
	if !v.puedeEditarse() {
		return ErrVentaNoEditable
	}
	if p.FechaVenta.IsZero() {
		return ErrFechaVentaZero
	}
	if err := validateCreditoCoherenciaForTipo(v.tipoVenta, p.PlanCredito, p.DiaCobranza); err != nil {
		return err
	}
	nota, err := validateNota(p.Nota)
	if err != nil {
		return err
	}
	v.direccion = p.Direccion
	v.gps = p.GPS
	v.fechaVenta = p.FechaVenta
	v.montos = p.Montos
	v.planCredito = p.PlanCredito
	v.diaCobranza = p.DiaCobranza
	v.nota = nota
	v.audit.MarkUpdated(p.By)
	v.pendingEvents = append(v.pendingEvents, NewVentaHeaderActualizadoEvent(v.id, p.By, p.Now))
	return nil
}

// validateCreditoCoherenciaForTipo is the post-creation variant of
// validateCreditoCoherencia: it takes the existing TipoVenta and the new
// plan/dia values rather than a CrearVentaParams.
func validateCreditoCoherenciaForTipo(tipo TipoVenta, plan *PlanCredito, dia *DiaCobranza) error {
	switch tipo {
	case TipoVentaContado:
		if plan != nil || dia != nil {
			return ErrPlanCreditoNoPermitidoEnContado
		}
		return nil
	case TipoVentaCredito:
		if plan == nil {
			return ErrPlanCreditoRequiredEnCredito
		}
		if dia == nil {
			return ErrDiaCobranzaRequeridoEnCredito
		}
		return validateDiaCobranzaForFrec(plan.FrecPago(), dia)
	}
	return ErrTipoVentaInvalido
}

// ActualizarClienteParams carries the editable cliente fields.
type ActualizarClienteParams struct {
	ClienteID *int
	Cliente   ClienteSnapshot
	By        uuid.UUID
	Now       time.Time
}

// ActualizarCliente mutates the venta's cliente snapshot and optional
// cliente_id link. Only valid in StatusBorrador.
func (v *Venta) ActualizarCliente(p ActualizarClienteParams) error {
	if !v.puedeEditarse() {
		return ErrVentaNoEditable
	}
	if p.Cliente.Nombre().IsZero() {
		return ErrNombreClienteRequerido
	}
	v.clienteID = p.ClienteID
	v.cliente = p.Cliente
	v.audit.MarkUpdated(p.By)
	v.pendingEvents = append(v.pendingEvents, NewVentaClienteActualizadoEvent(v.id, p.By, p.Now))
	return nil
}

// ReemplazarProductosParams carries the new productos and combos. Because
// productos may reference combos via combo_id, both collections must be
// replaced atomically.
type ReemplazarProductosParams struct {
	Productos []CrearVentaProductoInput
	By        uuid.UUID
	Now       time.Time
}

// ReemplazarProductos replaces the productos collection wholesale. The
// existing combos collection is preserved; producto.combo_id references
// must still resolve to a known combo.
func (v *Venta) ReemplazarProductos(p ReemplazarProductosParams) error {
	if !v.puedeEditarse() {
		return ErrVentaNoEditable
	}
	if len(p.Productos) == 0 {
		return ErrVentaProductosVacios
	}
	productos, err := buildProductos(p.Productos, p.By, p.Now)
	if err != nil {
		return err
	}
	if err := validateProductoComboReferences(productos, v.combos); err != nil {
		return err
	}
	v.productos = productos
	v.audit.MarkUpdated(p.By)
	v.pendingEvents = append(v.pendingEvents, NewVentaProductosReemplazadosEvent(v.id, len(productos), p.By, p.Now))
	return nil
}

// ReemplazarCombosParams carries the new combos collection.
type ReemplazarCombosParams struct {
	Combos []CrearVentaComboInput
	By     uuid.UUID
	Now    time.Time
}

// ReemplazarCombos replaces the combos collection wholesale. Productos that
// reference dropped combos become invalid — callers must reemplazar
// productos in the same transaction or this method will return
// ErrProductoComboReferenciaInvalida.
func (v *Venta) ReemplazarCombos(p ReemplazarCombosParams) error {
	if !v.puedeEditarse() {
		return ErrVentaNoEditable
	}
	combos, err := buildCombos(p.Combos, p.By, p.Now)
	if err != nil {
		return err
	}
	if err := validateProductoComboReferences(v.productos, combos); err != nil {
		return err
	}
	v.combos = combos
	v.audit.MarkUpdated(p.By)
	v.pendingEvents = append(v.pendingEvents, NewVentaCombosReemplazadosEvent(v.id, len(combos), p.By, p.Now))
	return nil
}

// ReemplazarVendedoresParams carries the new vendedores collection.
type ReemplazarVendedoresParams struct {
	Vendedores []CrearVentaVendedorInput
	By         uuid.UUID
	Now        time.Time
}

// ReemplazarVendedores replaces the vendedores collection wholesale.
func (v *Venta) ReemplazarVendedores(p ReemplazarVendedoresParams) error {
	if !v.puedeEditarse() {
		return ErrVentaNoEditable
	}
	if len(p.Vendedores) == 0 {
		return ErrVentaVendedoresVacios
	}
	vendedores, err := buildVendedores(p.Vendedores, p.By, p.Now)
	if err != nil {
		return err
	}
	v.vendedores = vendedores
	v.audit.MarkUpdated(p.By)
	v.pendingEvents = append(v.pendingEvents, NewVentaVendedoresReemplazadosEvent(v.id, len(vendedores), p.By, p.Now))
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
