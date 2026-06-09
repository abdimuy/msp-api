//nolint:misspell // domain vocabulary is Spanish (productos, vendedores, etc.) per project convention.
package app

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	platform "github.com/abdimuy/msp-api/internal/platform/domain"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// CrearVentaProductoInput is one producto line in the create-venta request.
// Fields are primitive types; VOs are constructed by IntoDomain.
type CrearVentaProductoInput struct {
	ID             uuid.UUID
	ArticuloID     int
	Articulo       string
	Cantidad       decimal.Decimal
	PrecioAnual    decimal.Decimal
	PrecioCorto    decimal.Decimal
	PrecioContado  decimal.Decimal
	ComboID        *uuid.UUID
	AlmacenOrigen  *int
	AlmacenDestino *int
}

// CrearVentaComboInput is one combo in the create-venta request.
type CrearVentaComboInput struct {
	ID             uuid.UUID
	Nombre         string
	PrecioAnual    decimal.Decimal
	PrecioCorto    decimal.Decimal
	PrecioContado  decimal.Decimal
	Cantidad       decimal.Decimal
	AlmacenOrigen  int
	AlmacenDestino int
}

// CrearVentaVendedorInput is one vendedor in the create-venta request.
type CrearVentaVendedorInput struct {
	ID        uuid.UUID
	UsuarioID uuid.UUID
	Email     string
	Nombre    string
}

// CrearVentaPlanCreditoInput carries the optional credit plan as primitive
// fields. Required when TipoVenta == "CREDITO".
type CrearVentaPlanCreditoInput struct {
	PlazoMeses  int
	Enganche    decimal.Decimal
	Parcialidad decimal.Decimal
	FrecPago    string
}

// CrearVentaDiaCobranzaInput carries the optional cobranza day. Exactly one
// of Semana / Mes must be set. Required when TipoVenta == "CREDITO".
type CrearVentaDiaCobranzaInput struct {
	Semana *string
	Mes    *int
}

// CrearVentaInput is the create-venta request DTO. All fields are primitives;
// VOs are constructed by intoDomain so handlers stay decoupled from the
// domain VO constructors.
type CrearVentaInput struct {
	ID                uuid.UUID
	ClienteID         *int
	ClienteNombre     string
	ClienteTel        *string
	ClienteAval       *string
	ClienteReferencia *string
	Calle             string
	NumeroExterior    *string
	Colonia           string
	Poblacion         string
	Ciudad            string
	ZonaClienteID     *int
	Latitud           float64
	Longitud          float64
	FechaVenta        time.Time
	TipoVenta         string
	PrecioAnual       decimal.Decimal
	PrecioCorto       decimal.Decimal
	PrecioContado     decimal.Decimal
	PlanCredito       *CrearVentaPlanCreditoInput
	DiaCobranza       *CrearVentaDiaCobranzaInput
	Nota              *string
	Combos            []CrearVentaComboInput
	Productos         []CrearVentaProductoInput
	Vendedores        []CrearVentaVendedorInput
}

// CrearVenta validates the input, builds the aggregate, persists it inside
// a Firebird transaction, and best-effort emits the buffered events to the
// outbox. Returns the persisted aggregate on success.
//
// When an InventarioService is wired (s.inventario != nil), the flow also:
//
//  1. Validates that every producto has sufficient existencia in its origin
//     almacén BEFORE the venta is persisted. The validation runs in its own
//     READ COMMITTED NO WAIT transaction so simultaneous "last item" sales
//     fail fast with apperror.NewValidation("articulo_sin_existencia").
//  2. Emits an automatic traspaso INSIDE the same Firebird transaction that
//     persists the venta. Both writes commit or roll back as a unit (the
//     inventario module's RunInTx is re-entrant via ctx).
//
// When s.inventario is nil (legacy / test code that does not exercise
// inventario), both steps are skipped and CrearVenta keeps its previous
// behavior.
func (s *Service) CrearVenta(ctx context.Context, in CrearVentaInput, by uuid.UUID) (*domain.Venta, error) {
	now := s.clock.Now()
	if err := s.validateClienteID(ctx, in.ClienteID); err != nil {
		return nil, err
	}
	if err := s.validateVendedorUsuarios(ctx, in.Vendedores); err != nil {
		return nil, err
	}
	if err := s.validateStockParaProductos(ctx, in.Productos); err != nil {
		return nil, err
	}
	params, err := in.intoDomain(by, now)
	if err != nil {
		return nil, err
	}
	venta, err := domain.CrearVenta(params)
	if err != nil {
		return nil, err
	}
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		if saveErr := s.ventas.Save(ctx, venta); saveErr != nil {
			return saveErr
		}
		return s.crearTraspasoParaVenta(ctx, venta, in.Productos, by, now)
	}); err != nil {
		return nil, err
	}
	s.drainEvents(ctx, venta)
	return venta, nil
}

// validateStockParaProductos delegates to the configured InventarioService
// (if any) so callers see "articulo_sin_existencia" before the venta is
// persisted. Productos that omit AlmacenOrigen are skipped — they belong to
// combos that carry their own origen at the combo level (combos are
// validated separately in a future iteration).
func (s *Service) validateStockParaProductos(ctx context.Context, productos []CrearVentaProductoInput) error {
	if s.inventario == nil || len(productos) == 0 {
		return nil
	}
	items := make([]outbound.InventarioStockItem, 0, len(productos))
	for _, p := range productos {
		if p.AlmacenOrigen == nil {
			continue
		}
		items = append(items, outbound.InventarioStockItem{
			ArticuloID:    p.ArticuloID,
			AlmacenOrigen: *p.AlmacenOrigen,
			Cantidad:      p.Cantidad,
		})
	}
	if len(items) == 0 {
		return nil
	}
	return s.inventario.ValidarStockParaVenta(ctx, items)
}

// crearTraspasoParaVenta emits the automatic traspaso that reserves the
// venta's productos in the configured destino almacén. Skips when no
// InventarioService is wired or when no producto carries an AlmacenOrigen.
//
// For v1 we group all productos under their common AlmacenOrigen and reject
// ventas whose productos span multiple origenes. Splitting into N traspasos
// is left for a follow-up if real-world data ever needs it.
func (s *Service) crearTraspasoParaVenta(
	ctx context.Context,
	venta *domain.Venta,
	productos []CrearVentaProductoInput,
	by uuid.UUID,
	now time.Time,
) error {
	if s.inventario == nil {
		return nil
	}
	almacenOrigen := 0
	detalles := make([]outbound.InventarioTraspasoDetalle, 0, len(productos))
	for _, p := range productos {
		if p.AlmacenOrigen == nil {
			continue
		}
		if almacenOrigen == 0 {
			almacenOrigen = *p.AlmacenOrigen
		} else if almacenOrigen != *p.AlmacenOrigen {
			return apperror.NewValidation(
				"productos_multiples_almacenes_origen",
				"los productos de la venta tienen distintos almacenes de origen; no se puede generar un traspaso único",
			)
		}
		detalles = append(detalles, outbound.InventarioTraspasoDetalle{
			ArticuloID: p.ArticuloID,
			Cantidad:   p.Cantidad,
		})
	}
	if len(detalles) == 0 {
		return nil
	}
	_, err := s.inventario.CrearTraspasoParaVenta(ctx, outbound.InventarioCrearTraspasoParams{
		VentaID:       venta.ID(),
		AlmacenOrigen: almacenOrigen,
		Fecha:         now,
		Descripcion:   "Traspaso automático por venta " + venta.ID().String(),
		Detalles:      detalles,
		CreatedBy:     by,
	})
	return err
}

// intoDomain translates the request DTO into a domain.CrearVentaParams,
// constructing every VO along the way.
func (in CrearVentaInput) intoDomain(by uuid.UUID, now time.Time) (domain.CrearVentaParams, error) {
	tipo, err := domain.ParseTipoVenta(in.TipoVenta)
	if err != nil {
		return domain.CrearVentaParams{}, err
	}
	gps, err := domain.NewGPSCoords(in.Latitud, in.Longitud)
	if err != nil {
		return domain.CrearVentaParams{}, err
	}
	dir, err := domain.NewDireccion(domain.NewDireccionParams{
		Calle:          in.Calle,
		NumeroExterior: in.NumeroExterior,
		Colonia:        in.Colonia,
		Poblacion:      in.Poblacion,
		Ciudad:         in.Ciudad,
		ZonaClienteID:  in.ZonaClienteID,
	})
	if err != nil {
		return domain.CrearVentaParams{}, err
	}
	cliente, err := buildClienteSnapshot(in)
	if err != nil {
		return domain.CrearVentaParams{}, err
	}
	montos, err := domain.NewMontoSnapshot(in.PrecioAnual, in.PrecioCorto, in.PrecioContado)
	if err != nil {
		return domain.CrearVentaParams{}, err
	}
	plan, err := buildOptionalPlanCredito(in.PlanCredito)
	if err != nil {
		return domain.CrearVentaParams{}, err
	}
	dia, err := buildOptionalDiaCobranza(in.DiaCobranza)
	if err != nil {
		return domain.CrearVentaParams{}, err
	}
	combos := buildComboInputs(in.Combos)
	productos, err := buildProductoInputs(in.Productos)
	if err != nil {
		return domain.CrearVentaParams{}, err
	}
	vendedores := buildVendedorInputs(in.Vendedores)
	return domain.CrearVentaParams{
		ID:          in.ID,
		ClienteID:   in.ClienteID,
		Cliente:     cliente,
		Direccion:   dir,
		GPS:         gps,
		FechaVenta:  in.FechaVenta,
		TipoVenta:   tipo,
		Montos:      montos,
		PlanCredito: plan,
		DiaCobranza: dia,
		Nota:        in.Nota,
		Combos:      combos,
		Productos:   productos,
		Vendedores:  vendedores,
		CreatedBy:   by,
		Now:         now,
	}, nil
}

// buildClienteSnapshot constructs the ClienteSnapshot VO from the primitive
// fields of the request DTO.
func buildClienteSnapshot(in CrearVentaInput) (domain.ClienteSnapshot, error) {
	nombre, err := domain.NewNombreCliente(in.ClienteNombre)
	if err != nil {
		return domain.ClienteSnapshot{}, err
	}
	tel, err := optionalTelefono(in.ClienteTel)
	if err != nil {
		return domain.ClienteSnapshot{}, err
	}
	aval, err := optionalNombreCliente(in.ClienteAval)
	if err != nil {
		return domain.ClienteSnapshot{}, err
	}
	return domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{
		Nombre:     nombre,
		Telefono:   tel,
		Aval:       aval,
		Referencia: trimOptionalString(in.ClienteReferencia),
	})
}

// trimOptionalString trims whitespace from an optional string pointer. nil or
// all-whitespace inputs return nil; any other value is returned trimmed. Full
// NFC normalization and length validation happen inside domain constructors
// (NewClienteSnapshot → trimOptionalBounded).
func trimOptionalString(s *string) *string {
	if s == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// optionalTelefono parses an optional telefono string. nil/blank → nil; any
// other value goes through platform.NewTelefono.
func optionalTelefono(s *string) (*platform.Telefono, error) {
	if s == nil {
		return nil, nil //nolint:nilnil // optional value: nil ptr signals "not provided".
	}
	trimmed := *s
	if trimmed == "" {
		return nil, nil //nolint:nilnil // optional value: empty input treated as not provided.
	}
	t, err := platform.NewTelefono(trimmed)
	if err != nil {
		return nil, apperror.NewValidation(
			"telefono_invalid",
			"el teléfono debe estar en formato E.164 (p. ej. +524491234567)",
		).WithError(err)
	}
	return &t, nil
}

// optionalNombreCliente parses an optional aval/responsable name. nil/blank →
// nil; any other value goes through domain.NewNombreCliente.
func optionalNombreCliente(s *string) (*domain.NombreCliente, error) {
	if s == nil {
		return nil, nil //nolint:nilnil // optional value: nil ptr signals "not provided".
	}
	if *s == "" {
		return nil, nil //nolint:nilnil // optional value: empty input treated as not provided.
	}
	n, err := domain.NewNombreCliente(*s)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// buildOptionalPlanCredito constructs a *PlanCredito from the optional input
// sub-struct. Nil input returns nil — the domain validator enforces the
// presence rule against the TipoVenta.
func buildOptionalPlanCredito(in *CrearVentaPlanCreditoInput) (*domain.PlanCredito, error) {
	if in == nil {
		return nil, nil //nolint:nilnil // optional value: nil signals "no plan".
	}
	frec, err := domain.ParseFrecPago(in.FrecPago)
	if err != nil {
		return nil, err
	}
	// The Android app does not capture the credit term — the office assigns it.
	// A venta arriving with plazo_meses unset (0, or a stray negative) takes the
	// default term so it is created as a borrador the office can complete, rather
	// than being rejected and queued as a failed intent.
	plazoMeses := in.PlazoMeses
	if plazoMeses <= 0 {
		plazoMeses = domain.DefaultPlazoMeses
	}
	plan, err := domain.NewPlanCredito(plazoMeses, in.Enganche, in.Parcialidad, frec)
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

// buildOptionalDiaCobranza constructs a *DiaCobranza from the optional input
// sub-struct. Exactly one of Semana/Mes must be set when the struct is
// supplied; the domain validator enforces the (frec_pago, dia) coherence.
func buildOptionalDiaCobranza(in *CrearVentaDiaCobranzaInput) (*domain.DiaCobranza, error) {
	if in == nil {
		return nil, nil //nolint:nilnil // optional value: nil signals "no dia cobranza".
	}
	switch {
	case in.Semana != nil && in.Mes == nil:
		dia, err := domain.ParseDiaSemana(*in.Semana)
		if err != nil {
			return nil, err
		}
		dc, err := domain.NewDiaCobranzaSemana(dia)
		if err != nil {
			return nil, err
		}
		return &dc, nil
	case in.Mes != nil && in.Semana == nil:
		dc, err := domain.NewDiaCobranzaMes(*in.Mes)
		if err != nil {
			return nil, err
		}
		return &dc, nil
	default:
		return nil, domain.ErrDiaCobranzaIncoherenteQuincenalMensual
	}
}

// buildComboInputs translates the request combo slice into the domain shape.
func buildComboInputs(in []CrearVentaComboInput) []domain.CrearVentaComboInput {
	out := make([]domain.CrearVentaComboInput, 0, len(in))
	for _, c := range in {
		out = append(out, domain.CrearVentaComboInput{
			ID:             c.ID,
			Nombre:         c.Nombre,
			Precios:        domain.HydrateMontoSnapshot(c.PrecioAnual, c.PrecioCorto, c.PrecioContado),
			Cantidad:       c.Cantidad,
			AlmacenOrigen:  c.AlmacenOrigen,
			AlmacenDestino: c.AlmacenDestino,
		})
	}
	return out
}

// buildProductoInputs translates the request producto slice into the domain
// shape. Validates the per-line MontoSnapshot via NewMontoSnapshot to surface
// negative-price errors as soon as possible.
func buildProductoInputs(in []CrearVentaProductoInput) ([]domain.CrearVentaProductoInput, error) {
	out := make([]domain.CrearVentaProductoInput, 0, len(in))
	for _, p := range in {
		precios, err := domain.NewMontoSnapshot(p.PrecioAnual, p.PrecioCorto, p.PrecioContado)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.CrearVentaProductoInput{
			ID:             p.ID,
			ArticuloID:     p.ArticuloID,
			Articulo:       p.Articulo,
			Cantidad:       p.Cantidad,
			Precios:        precios,
			ComboID:        p.ComboID,
			AlmacenOrigen:  p.AlmacenOrigen,
			AlmacenDestino: p.AlmacenDestino,
		})
	}
	return out, nil
}

// buildVendedorInputs translates the request vendedor slice into the domain
// shape. Per-line validation happens inside domain.CrearVenta via
// NewVendedorSnapshot.
func buildVendedorInputs(in []CrearVentaVendedorInput) []domain.CrearVentaVendedorInput {
	out := make([]domain.CrearVentaVendedorInput, 0, len(in))
	for _, v := range in {
		out = append(out, domain.CrearVentaVendedorInput{
			ID:        v.ID,
			UsuarioID: v.UsuarioID,
			Email:     v.Email,
			Nombre:    v.Nombre,
		})
	}
	return out
}
