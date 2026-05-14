//nolint:misspell // domain vocabulary is Spanish (producto, etc.) per project convention.
package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// maxArticuloNombreLength is the byte width of the articulo snapshot column.
const maxArticuloNombreLength = 200

// Producto is one line item of a venta. It is a child entity of the Venta
// aggregate root. The articulo name is captured as a snapshot so historical
// invoices remain readable even if the article in the catálogo is renamed.
//
// Almacen invariant: stand-alone productos (ComboID == nil) carry their own
// almacenOrigen and almacenDestino. Productos that belong to a combo
// (ComboID != nil) inherit the warehouses from the parent combo and must
// have nil almacen pointers.
type Producto struct {
	id             uuid.UUID
	articuloID     int
	articulo       string
	cantidad       decimal.Decimal
	precios        MontoSnapshot
	comboID        *uuid.UUID
	almacenOrigen  *int
	almacenDestino *int
	audit          audit.Auditable
}

// NewProductoParams carries the inputs to newProducto.
type NewProductoParams struct {
	ID             uuid.UUID
	ArticuloID     int
	Articulo       string
	Cantidad       decimal.Decimal
	Precios        MontoSnapshot
	ComboID        *uuid.UUID
	AlmacenOrigen  *int
	AlmacenDestino *int
	CreatedBy      uuid.UUID
	Now            time.Time
}

// newProducto validates and constructs a Producto. Package-private.
func newProducto(p NewProductoParams) (*Producto, error) {
	articulo := strings.TrimSpace(p.Articulo)
	if articulo == "" {
		return nil, ErrProductoArticuloRequerido
	}
	if len(articulo) > maxArticuloNombreLength {
		return nil, ErrProductoArticuloDemasiadoLargo
	}
	if err := validateSafeChars(articulo); err != nil {
		return nil, err
	}
	if p.Cantidad.Sign() <= 0 {
		return nil, ErrCantidadNoPositiva
	}
	if err := validateCantidadScale(p.Cantidad); err != nil {
		return nil, err
	}
	if err := validateMontoSnapshotScale(p.Precios); err != nil {
		return nil, err
	}
	if err := validateProductoAlmacenes(p.ComboID, p.AlmacenOrigen, p.AlmacenDestino); err != nil {
		return nil, err
	}
	return &Producto{
		id:             p.ID,
		articuloID:     p.ArticuloID,
		articulo:       articulo,
		cantidad:       p.Cantidad,
		precios:        p.Precios,
		comboID:        p.ComboID,
		almacenOrigen:  p.AlmacenOrigen,
		almacenDestino: p.AlmacenDestino,
		audit:          audit.NewAuditable(p.Now, p.CreatedBy),
	}, nil
}

// validateProductoAlmacenes enforces the (combo_id, almacenes) coherence:
// stand-alone productos (combo_id nil) require both almacenes; productos in
// a combo (combo_id non-nil) must have nil almacenes.
func validateProductoAlmacenes(comboID *uuid.UUID, origen, destino *int) error {
	if comboID == nil {
		if origen == nil || *origen <= 0 {
			return ErrProductoAlmacenOrigenRequerido
		}
		if destino == nil || *destino <= 0 {
			return ErrProductoAlmacenDestinoRequerido
		}
		if *origen == *destino {
			return ErrVentaAlmacenesIguales
		}
		return nil
	}
	if origen != nil || destino != nil {
		return ErrProductoEnComboNoLlevaAlmacen
	}
	return nil
}

// HydrateProductoParams carries the persisted shape of a Producto.
type HydrateProductoParams struct {
	ID             uuid.UUID
	ArticuloID     int
	Articulo       string
	Cantidad       decimal.Decimal
	Precios        MontoSnapshot
	ComboID        *uuid.UUID
	AlmacenOrigen  *int
	AlmacenDestino *int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CreatedBy      uuid.UUID
	UpdatedBy      uuid.UUID
}

// HydrateProducto rebuilds a Producto from persistence without validation.
func HydrateProducto(p HydrateProductoParams) *Producto {
	return &Producto{
		id:             p.ID,
		articuloID:     p.ArticuloID,
		articulo:       p.Articulo,
		cantidad:       p.Cantidad,
		precios:        p.Precios,
		comboID:        p.ComboID,
		almacenOrigen:  p.AlmacenOrigen,
		almacenDestino: p.AlmacenDestino,
		audit:          audit.HydrateAuditable(p.CreatedAt, p.UpdatedAt, p.CreatedBy, p.UpdatedBy),
	}
}

// ID returns the producto line ID.
func (p *Producto) ID() uuid.UUID { return p.id }

// ArticuloID returns the Microsip articulo identifier.
func (p *Producto) ArticuloID() int { return p.articuloID }

// Articulo returns the snapshot of the articulo display name.
func (p *Producto) Articulo() string { return p.articulo }

// Cantidad returns the line quantity.
func (p *Producto) Cantidad() decimal.Decimal { return p.cantidad }

// Precios returns the three-price snapshot for this line.
func (p *Producto) Precios() MontoSnapshot { return p.precios }

// ComboID returns the optional parent combo ID.
func (p *Producto) ComboID() *uuid.UUID { return p.comboID }

// AlmacenOrigen returns the origin warehouse pointer; nil for productos
// that belong to a combo.
func (p *Producto) AlmacenOrigen() *int { return p.almacenOrigen }

// AlmacenDestino returns the destination warehouse pointer; nil for
// productos that belong to a combo.
func (p *Producto) AlmacenDestino() *int { return p.almacenDestino }

// Audit returns a copy of the producto's audit subrecord.
func (p *Producto) Audit() audit.Auditable { return p.audit }
