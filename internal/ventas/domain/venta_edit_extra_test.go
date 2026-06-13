//nolint:misspell // ventas vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// rebuildVentaWithSituacion rebuilds an aggregate via HydrateVenta with the
// supplied situacion — used to drive the "not borrador" rejection branches
// since the domain has no app-level transition to Aprobada yet.
func rebuildVentaWithSituacion(t *testing.T, src *domain.Venta, situacion domain.Situacion) *domain.Venta {
	t.Helper()
	a := src.Audit()
	return domain.HydrateVenta(domain.HydrateVentaParams{
		ID: src.ID(), ClienteID: src.ClienteID(),
		Cliente: src.Cliente(), Direccion: src.Direccion(), GPS: src.GPS(),
		FechaVenta: src.FechaVenta(), TipoVenta: src.TipoVenta(), Montos: src.Montos(),
		PlanCredito: src.PlanCredito(), DiaCobranza: src.DiaCobranza(), Nota: src.Nota(),
		Estado: domain.EstadoActive, Situacion: situacion, Sincronizacion: domain.SincronizacionPendiente,
		Combos: src.CombosForRepo(), Productos: src.ProductosForRepo(),
		Vendedores: src.VendedoresForRepo(), Imagenes: src.ImagenesForRepo(),
		Cancelacion: src.Cancelacion(), Aprobacion: src.Aprobacion(),
		CreatedAt: a.CreatedAt(), UpdatedAt: a.UpdatedAt(),
		CreatedBy: a.CreatedBy(), UpdatedBy: a.UpdatedBy(),
	})
}

func TestVenta_ActualizarHeader_RejectsIfAprobada(t *testing.T) {
	t.Parallel()
	v := rebuildVentaWithSituacion(t, buildValidVenta(t), domain.SituacionAprobada)
	err := v.ActualizarHeader(domain.ActualizarHeaderParams{
		Direccion: v.Direccion(), GPS: v.GPS(),
		FechaVenta: v.FechaVenta(),
		By:         uuid.New(), Now: time.Now(),
	})
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "venta_no_editable", ae.Code)
}

func TestVenta_ActualizarCliente_RejectsIfAprobada(t *testing.T) {
	t.Parallel()
	v := rebuildVentaWithSituacion(t, buildValidVenta(t), domain.SituacionAprobada)
	snap, _ := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: v.Cliente().Nombre()})
	err := v.ActualizarCliente(domain.ActualizarClienteParams{
		Cliente: snap, By: uuid.New(), Now: time.Now(),
	})
	require.Error(t, err)
}

func TestVenta_ReemplazarProductos_RejectsIfAprobada(t *testing.T) {
	t.Parallel()
	v := rebuildVentaWithSituacion(t, buildValidVenta(t), domain.SituacionAprobada)
	one, two := 1, 2
	montos, _ := domain.NewMontoSnapshot(decimal.NewFromInt(1), decimal.NewFromInt(1), decimal.NewFromInt(1))
	err := v.ReemplazarProductos(domain.ReemplazarProductosParams{
		Productos: []domain.CrearVentaProductoInput{{
			ID: uuid.New(), ArticuloID: 1, Articulo: "X", Cantidad: decimal.NewFromInt(1),
			Precios: montos, AlmacenOrigen: &one, AlmacenDestino: &two,
		}},
		By: uuid.New(), Now: time.Now(),
	})
	require.Error(t, err)
}

func TestVenta_ReemplazarCombos_RejectsIfAprobada(t *testing.T) {
	t.Parallel()
	v := rebuildVentaWithSituacion(t, buildValidVenta(t), domain.SituacionAprobada)
	err := v.ReemplazarCombos(domain.ReemplazarCombosParams{
		Combos: nil, By: uuid.New(), Now: time.Now(),
	})
	require.Error(t, err)
}

func TestVenta_ReemplazarVendedores_RejectsIfAprobada(t *testing.T) {
	t.Parallel()
	v := rebuildVentaWithSituacion(t, buildValidVenta(t), domain.SituacionAprobada)
	err := v.ReemplazarVendedores(domain.ReemplazarVendedoresParams{
		Vendedores: []domain.CrearVentaVendedorInput{{
			ID: uuid.New(), UsuarioID: uuid.New(), Email: "x@y.com", Nombre: "X",
		}},
		By: uuid.New(), Now: time.Now(),
	})
	require.Error(t, err)
}

func TestVenta_ActualizarHeader_PlanRequiredForCredito(t *testing.T) {
	t.Parallel()
	// CREDITO venta — drop the plan via ActualizarHeader → should reject.
	p := validCreditoParams(t)
	v, err := domain.CrearVenta(p)
	require.NoError(t, err)
	err = v.ActualizarHeader(domain.ActualizarHeaderParams{
		Direccion: v.Direccion(), GPS: v.GPS(),
		FechaVenta:  v.FechaVenta(),
		PlanCredito: nil, DiaCobranza: nil, // CREDITO without plan → invalid
		By: uuid.New(), Now: time.Now(),
	})
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "plan_credito_required_en_credito", ae.Code)
}

func TestVenta_ActualizarHeader_PlanNotAllowedForContado(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t) // CONTADO
	plan, _ := domain.NewPlanCredito(12, decimal.Zero, decimal.NewFromInt(100), domain.FrecPagoMensual)
	dc, _ := domain.NewDiaCobranzaMes(10)
	err := v.ActualizarHeader(domain.ActualizarHeaderParams{
		Direccion: v.Direccion(), GPS: v.GPS(),
		FechaVenta:  v.FechaVenta(),
		PlanCredito: &plan, DiaCobranza: &dc,
		By: uuid.New(), Now: time.Now(),
	})
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "plan_credito_no_permitido_en_contado", ae.Code)
}

func TestVenta_ActualizarHeader_FechaZeroRejected(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	err := v.ActualizarHeader(domain.ActualizarHeaderParams{
		Direccion: v.Direccion(), GPS: v.GPS(),
		FechaVenta: time.Time{},
		By:         uuid.New(), Now: time.Now(),
	})
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "fecha_venta_zero", ae.Code)
}

func TestVenta_ReemplazarCombos_OrphansProductoRejected(t *testing.T) {
	t.Parallel()
	// Build a venta with a combo + a producto inside that combo.
	p := validCrearVentaParams(t)
	montos, _ := domain.NewMontoSnapshot(decimal.NewFromInt(10), decimal.NewFromInt(8), decimal.NewFromInt(5))
	comboID := uuid.New()
	p.Combos = []domain.CrearVentaComboInput{{
		ID: comboID, Nombre: "Bundle", Precios: montos,
		Cantidad: decimal.NewFromInt(1), AlmacenOrigen: 1, AlmacenDestino: 2,
	}}
	p.Productos = []domain.CrearVentaProductoInput{{
		ID: uuid.New(), ArticuloID: 1, Articulo: "Cama", Cantidad: decimal.NewFromInt(1),
		Precios: montos, ComboID: &comboID,
	}}
	v, err := domain.CrearVenta(p)
	require.NoError(t, err)

	// Now try to drop combos → producto.combo_id orphaned.
	err = v.ReemplazarCombos(domain.ReemplazarCombosParams{
		Combos: nil, By: uuid.New(), Now: time.Now(),
	})
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "producto_combo_referencia_invalida", ae.Code)
}

func TestVenta_HydrateWithAprobacionRoundtrip(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	by := uuid.New()
	approval, err := domain.NewAprobacion(at, by)
	require.NoError(t, err)
	montos, _ := domain.NewMontoSnapshot(decimal.NewFromInt(1), decimal.NewFromInt(1), decimal.NewFromInt(1))
	v := domain.HydrateVenta(domain.HydrateVentaParams{
		ID: uuid.New(), TipoVenta: domain.TipoVentaContado,
		Estado: domain.EstadoActive, Situacion: domain.SituacionAprobada, Sincronizacion: domain.SincronizacionPendiente,
		Montos:     montos,
		Aprobacion: &approval,
	})
	require.NotNil(t, v.Aprobacion())
	assert.Equal(t, at, v.Aprobacion().At())
	assert.Equal(t, by, v.Aprobacion().By())
	assert.Equal(t, domain.SituacionAprobada, v.Situacion())
}

func TestVenta_AllSituacionesAreValid(t *testing.T) {
	t.Parallel()
	for _, s := range []domain.Situacion{domain.SituacionBorrador, domain.SituacionAprobada, domain.SituacionCancelada} {
		assert.True(t, s.IsValid(), "situacion=%q must be valid", s)
		got, err := domain.ParseSituacion(string(s))
		require.NoError(t, err, "ParseSituacion(%q)", s)
		assert.Equal(t, s, got)
	}
}
