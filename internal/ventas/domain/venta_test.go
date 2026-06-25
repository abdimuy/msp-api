package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ─── Test helpers ──────────────────────────────────────────────────────────

// validCrearVentaParams builds a known-valid CrearVentaParams for CONTADO.
// Tests that want a CREDITO variant flip fields after copying.
func validCrearVentaParams(t *testing.T) domain.CrearVentaParams {
	t.Helper()
	nom, err := domain.NewNombreCliente("Juan Pérez")
	require.NoError(t, err)
	cliente, err := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nom})
	require.NoError(t, err)
	dir, err := domain.NewDireccion(domain.NewDireccionParams{
		Calle:     "Reforma",
		Colonia:   "Centro",
		Poblacion: "Cd.",
		Ciudad:    "CDMX",
	})
	require.NoError(t, err)
	gps, err := domain.NewGPSCoords(19.0, -99.0)
	require.NoError(t, err)
	montos, err := domain.NewMontoSnapshot(decimal.NewFromInt(100), decimal.NewFromInt(80), decimal.NewFromInt(50))
	require.NoError(t, err)

	one, two := 1, 2
	return domain.CrearVentaParams{
		ID:         uuid.New(),
		Cliente:    cliente,
		Direccion:  dir,
		GPS:        gps,
		FechaVenta: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		TipoVenta:  domain.TipoVentaContado,
		Productos: []domain.CrearVentaProductoInput{{
			ID:             uuid.New(),
			ArticuloID:     1,
			Articulo:       "Bici",
			Cantidad:       decimal.NewFromInt(1),
			Precios:        montos,
			AlmacenOrigen:  &one,
			AlmacenDestino: &two,
		}},
		Vendedores: []domain.CrearVentaVendedorInput{{
			ID:        uuid.New(),
			UsuarioID: uuid.New(),
			Email:     "v@x.com",
			Nombre:    "Vendedor",
		}},
		CreatedBy: uuid.New(),
		Now:       time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC),
	}
}

// validCreditoParams returns a fully-valid CREDITO CrearVentaParams.
func validCreditoParams(t *testing.T) domain.CrearVentaParams {
	t.Helper()
	p := validCrearVentaParams(t)
	p.TipoVenta = domain.TipoVentaCredito
	plan, err := domain.NewPlanCredito(12, decimal.NewFromInt(100), decimal.NewFromInt(50), domain.FrecPagoMensual)
	require.NoError(t, err)
	p.PlanCredito = &plan
	dc, err := domain.NewDiaCobranzaMes(15)
	require.NoError(t, err)
	p.DiaCobranza = &dc
	return p
}

func buildValidVenta(t *testing.T) *domain.Venta {
	t.Helper()
	v, err := domain.CrearVenta(validCrearVentaParams(t))
	require.NoError(t, err)
	return v
}

// ─── CrearVenta — happy path ───────────────────────────────────────────────

func TestCrearVenta_ContadoHappy(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	v, err := domain.CrearVenta(p)
	require.NoError(t, err)
	assert.Equal(t, p.ID, v.ID())
	assert.Equal(t, domain.TipoVentaContado, v.TipoVenta())
	assert.Nil(t, v.PlanCredito())
	assert.Nil(t, v.DiaCobranza())
	assert.False(t, v.IsCanceled())
	assert.Equal(t, 1, v.ProductosCount())
	assert.Equal(t, 1, v.VendedoresCount())
	assert.Equal(t, 0, v.CombosCount())
	assert.Equal(t, 0, v.ImagenesCount())

	events := v.PendingEvents()
	require.Len(t, events, 1)
	assert.Equal(t, "venta.creada", events[0].EventType())
	assert.Equal(t, v.ID(), events[0].AggregateID())
}

func TestCrearVenta_CreditoHappy(t *testing.T) {
	t.Parallel()
	p := validCreditoParams(t)
	v, err := domain.CrearVenta(p)
	require.NoError(t, err)
	require.NotNil(t, v.PlanCredito())
	require.NotNil(t, v.DiaCobranza())
	assert.True(t, v.DiaCobranza().IsMes())
}

// ─── CrearVenta — invariants ───────────────────────────────────────────────

func TestCrearVenta_RejectsBadHeader(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mut  func(p *domain.CrearVentaParams)
		code string
	}{
		{"tipo invalido", func(p *domain.CrearVentaParams) { p.TipoVenta = "OTRO" }, "tipo_venta_invalido"},
		{"fecha zero", func(p *domain.CrearVentaParams) { p.FechaVenta = time.Time{} }, "fecha_venta_zero"},
		{"producto sin almacen origen", func(p *domain.CrearVentaParams) {
			p.Productos[0].AlmacenOrigen = nil
		}, "producto_almacen_origen_required"},
		{"sin cliente", func(p *domain.CrearVentaParams) { p.Cliente = domain.ClienteSnapshot{} }, "nombre_cliente_required"},
		{"sin productos", func(p *domain.CrearVentaParams) { p.Productos = nil }, "venta_productos_vacios"},
		{"sin vendedores", func(p *domain.CrearVentaParams) { p.Vendedores = nil }, "venta_vendedores_vacios"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validCrearVentaParams(t)
			tc.mut(&p)
			_, err := domain.CrearVenta(p)
			require.Error(t, err)
			ae, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, tc.code, ae.Code)
		})
	}
}

func TestCrearVenta_ContadoConPlan_Falla(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	plan, _ := domain.NewPlanCredito(12, decimal.Zero, decimal.NewFromInt(50), domain.FrecPagoMensual)
	p.PlanCredito = &plan
	_, err := domain.CrearVenta(p)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "plan_credito_no_permitido_en_contado", ae.Code)
}

func TestCrearVenta_ContadoConDiaCobranza_Falla(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	dc, _ := domain.NewDiaCobranzaMes(15)
	p.DiaCobranza = &dc
	_, err := domain.CrearVenta(p)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "plan_credito_no_permitido_en_contado", ae.Code)
}

func TestCrearVenta_CreditoSinPlan_Falla(t *testing.T) {
	t.Parallel()
	p := validCreditoParams(t)
	p.PlanCredito = nil
	_, err := domain.CrearVenta(p)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "plan_credito_required_en_credito", ae.Code)
}

func TestCrearVenta_CreditoSinDiaCobranza_Falla(t *testing.T) {
	t.Parallel()
	p := validCreditoParams(t)
	p.DiaCobranza = nil
	_, err := domain.CrearVenta(p)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "dia_cobranza_required_en_credito", ae.Code)
}

func TestCrearVenta_Semanal_RequiereDiaSemana(t *testing.T) {
	t.Parallel()
	p := validCreditoParams(t)
	plan, _ := domain.NewPlanCredito(12, decimal.Zero, decimal.NewFromInt(50), domain.FrecPagoSemanal)
	p.PlanCredito = &plan
	// dia is Mes — incoherent with SEMANAL.
	dc, _ := domain.NewDiaCobranzaMes(15)
	p.DiaCobranza = &dc
	_, err := domain.CrearVenta(p)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "dia_cobranza_incoherente_semanal", ae.Code)

	// Happy: semanal + dia semana.
	p2 := validCreditoParams(t)
	plan2, _ := domain.NewPlanCredito(12, decimal.Zero, decimal.NewFromInt(50), domain.FrecPagoSemanal)
	p2.PlanCredito = &plan2
	dcSem, _ := domain.NewDiaCobranzaSemana(domain.DiaSemanaLunes)
	p2.DiaCobranza = &dcSem
	_, err = domain.CrearVenta(p2)
	require.NoError(t, err)
}

func TestCrearVenta_QuincenalMensual_RequiereUnoUOtro(t *testing.T) {
	t.Parallel()
	// Quincenal + dia semana = OK.
	p := validCreditoParams(t)
	plan, _ := domain.NewPlanCredito(12, decimal.Zero, decimal.NewFromInt(50), domain.FrecPagoQuincenal)
	p.PlanCredito = &plan
	dcSem, _ := domain.NewDiaCobranzaSemana(domain.DiaSemanaMartes)
	p.DiaCobranza = &dcSem
	_, err := domain.CrearVenta(p)
	require.NoError(t, err)

	// Mensual + dia mes = OK.
	p2 := validCreditoParams(t)
	plan2, _ := domain.NewPlanCredito(12, decimal.Zero, decimal.NewFromInt(50), domain.FrecPagoMensual)
	p2.PlanCredito = &plan2
	dcMes, _ := domain.NewDiaCobranzaMes(10)
	p2.DiaCobranza = &dcMes
	_, err = domain.CrearVenta(p2)
	require.NoError(t, err)
}

func TestCrearVenta_NotaTooLong(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	long := strings.Repeat("a", 501)
	p.Nota = &long
	_, err := domain.CrearVenta(p)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "nota_too_long", ae.Code)
}

func TestCrearVenta_NotaWhitespaceNormalizes(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	blank := "   "
	p.Nota = &blank
	v, err := domain.CrearVenta(p)
	require.NoError(t, err)
	assert.Nil(t, v.Nota())
}

// ─── Mutators ──────────────────────────────────────────────────────────────

func TestVenta_AdjuntarImagen_HappyAndEvent(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	storage, _ := domain.NewImagenStorage(domain.StorageKindFilesystem, "ventas/x.jpg")
	by := uuid.New()
	now := time.Now()

	img, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID: uuid.New(), Storage: storage, Mime: "image/jpeg",
		SizeBytes: 100, By: by, Now: now,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, v.ImagenesCount())
	a := v.Audit()
	assert.Equal(t, by, a.UpdatedBy())

	events := v.PendingEvents()
	require.Len(t, events, 2)
	assert.Equal(t, "venta.imagen_adjuntada", events[1].EventType())
	payload := events[1].Payload()
	assert.Equal(t, img.ID().String(), payload["imagen_id"])
}

func TestVenta_AdjuntarImagen_RefusesIfCanceled(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	require.NoError(t, v.Cancelar("ya no", uuid.New(), time.Now()))
	storage, _ := domain.NewImagenStorage(domain.StorageKindFilesystem, "ventas/x.jpg")
	_, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID: uuid.New(), Storage: storage, Mime: "image/jpeg",
		SizeBytes: 1, By: uuid.New(), Now: time.Now(),
	})
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "venta_cancelada_inmutable", ae.Code)
}

func TestVenta_EliminarImagen_Happy(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	storage, _ := domain.NewImagenStorage(domain.StorageKindFilesystem, "ventas/x.jpg")
	img, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID: uuid.New(), Storage: storage, Mime: "image/jpeg",
		SizeBytes: 1, By: uuid.New(), Now: time.Now(),
	})
	require.NoError(t, err)
	assert.Equal(t, 1, v.ImagenesCount())

	by := uuid.New()
	require.NoError(t, v.EliminarImagen(img.ID(), by, time.Now()))
	assert.Equal(t, 0, v.ImagenesCount())
	a := v.Audit()
	assert.Equal(t, by, a.UpdatedBy())

	events := v.PendingEvents()
	require.Len(t, events, 3)
	assert.Equal(t, "venta.imagen_eliminada", events[2].EventType())
}

func TestVenta_EliminarImagen_NotFound(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	err := v.EliminarImagen(uuid.New(), uuid.New(), time.Now())
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "imagen_not_found", ae.Code)
}

func TestVenta_EliminarImagen_RefusesIfCanceled(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	storage, _ := domain.NewImagenStorage(domain.StorageKindFilesystem, "ventas/x.jpg")
	img, _ := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID: uuid.New(), Storage: storage, Mime: "image/jpeg",
		SizeBytes: 1, By: uuid.New(), Now: time.Now(),
	})
	require.NoError(t, v.Cancelar("razon", uuid.New(), time.Now()))
	err := v.EliminarImagen(img.ID(), uuid.New(), time.Now())
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "venta_cancelada_inmutable", ae.Code)
}

func TestVenta_Cancelar_Happy(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	by := uuid.New()
	now := time.Now()
	require.NoError(t, v.Cancelar("cliente cambió de opinión", by, now))
	assert.True(t, v.IsCanceled())
	require.NotNil(t, v.Cancelacion())
	assert.Equal(t, by, v.Cancelacion().By())
	assert.Equal(t, "cliente cambió de opinión", v.Cancelacion().Reason())
	a := v.Audit()
	assert.Equal(t, by, a.UpdatedBy())

	events := v.PendingEvents()
	require.Len(t, events, 2)
	assert.Equal(t, "venta.cancelada", events[1].EventType())
}

func TestVenta_Cancelar_TwiceRejects(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	require.NoError(t, v.Cancelar("razon", uuid.New(), time.Now()))
	err := v.Cancelar("otra", uuid.New(), time.Now())
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "venta_ya_cancelada", ae.Code)
}

func TestVenta_Cancelar_RejectsEmptyReason(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	err := v.Cancelar("   ", uuid.New(), time.Now())
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "reason_cancelacion_required", ae.Code)
}

// ─── Iterators / counts / hydrate ──────────────────────────────────────────

func TestVenta_Iterators(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	count := 0
	for range v.Productos() {
		count++
	}
	assert.Equal(t, 1, count)
	count = 0
	for range v.Vendedores() {
		count++
	}
	assert.Equal(t, 1, count)
	count = 0
	for range v.Combos() {
		count++
	}
	assert.Equal(t, 0, count)
	count = 0
	for range v.Imagenes() {
		count++
	}
	assert.Equal(t, 0, count)
}

func TestVenta_ProductosIteratorEarlyStop(t *testing.T) {
	t.Parallel()
	// Build a venta with two productos and stop after the first.
	p := validCrearVentaParams(t)
	montos, _ := domain.NewMontoSnapshot(decimal.NewFromInt(10), decimal.NewFromInt(8), decimal.NewFromInt(5))
	one2, two2 := 1, 2
	p.Productos = append(p.Productos, domain.CrearVentaProductoInput{
		ID: uuid.New(), ArticuloID: 2, Articulo: "Otro", Cantidad: decimal.NewFromInt(1), Precios: montos,
		AlmacenOrigen: &one2, AlmacenDestino: &two2,
	})
	p.Combos = []domain.CrearVentaComboInput{{
		ID: uuid.New(), Nombre: "C1", Precios: montos,
		Cantidad: decimal.NewFromInt(1), AlmacenOrigen: 1, AlmacenDestino: 2,
	}, {
		ID: uuid.New(), Nombre: "C2", Precios: montos,
		Cantidad: decimal.NewFromInt(1), AlmacenOrigen: 1, AlmacenDestino: 2,
	}}
	v, err := domain.CrearVenta(p)
	require.NoError(t, err)
	require.Equal(t, 2, v.CombosCount())
	require.Equal(t, 2, v.ProductosCount())

	// Stop combos iterator after first.
	count := 0
	for range v.Combos() {
		count++
		break
	}
	assert.Equal(t, 1, count)

	// Stop productos iterator after first.
	count = 0
	for range v.Productos() {
		count++
		break
	}
	assert.Equal(t, 1, count)

	// Add an extra vendedor and an imagen so the vendedor and imagen
	// iterators also exercise their early-stop branch.
	p2 := validCrearVentaParams(t)
	p2.Vendedores = append(p2.Vendedores, domain.CrearVentaVendedorInput{
		ID: uuid.New(), UsuarioID: uuid.New(), Email: "v2@x.com", Nombre: "V2",
	})
	v2, err := domain.CrearVenta(p2)
	require.NoError(t, err)
	require.Equal(t, 2, v2.VendedoresCount())
	count = 0
	for range v2.Vendedores() {
		count++
		break
	}
	assert.Equal(t, 1, count)

	storage, _ := domain.NewImagenStorage(domain.StorageKindFilesystem, "k1.jpg")
	_, err = v2.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID: uuid.New(), Storage: storage, Mime: "image/jpeg", SizeBytes: 1, By: uuid.New(), Now: time.Now(),
	})
	require.NoError(t, err)
	_, err = v2.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID: uuid.New(), Storage: storage, Mime: "image/jpeg", SizeBytes: 1, By: uuid.New(), Now: time.Now(),
	})
	require.NoError(t, err)
	count = 0
	for range v2.Imagenes() {
		count++
		break
	}
	assert.Equal(t, 1, count)
}

func TestVenta_ForRepoSlices(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	assert.Len(t, v.ProductosForRepo(), 1)
	assert.Len(t, v.VendedoresForRepo(), 1)
	assert.Empty(t, v.CombosForRepo())
	assert.Empty(t, v.ImagenesForRepo())
}

func TestHydrateVenta_Bypass(t *testing.T) {
	t.Parallel()
	now := time.Now()
	v := domain.HydrateVenta(domain.HydrateVentaParams{
		ID:        uuid.New(),
		TipoVenta: "WTF",
		CreatedAt: now,
		UpdatedAt: now,
	})
	assert.Equal(t, domain.TipoVenta("WTF"), v.TipoVenta())
	assert.False(t, v.IsCanceled())
}

func TestVenta_PendingEventsAndClear(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	got := v.PendingEvents()
	require.Len(t, got, 1)
	// Mutating returned copy doesn't affect entity.
	got[0] = nil
	again := v.PendingEvents()
	require.Len(t, again, 1)
	require.NotNil(t, again[0])

	v.ClearPendingEvents()
	assert.Empty(t, v.PendingEvents())
}

func TestVenta_AccessorsRoundtrip(t *testing.T) {
	t.Parallel()
	p := validCreditoParams(t)
	p.Nota = strPtr("una nota")
	num := "123"
	p.Direccion, _ = domain.NewDireccion(domain.NewDireccionParams{
		Calle: "Reforma", NumeroExterior: &num, Colonia: "C", Poblacion: "P", Ciudad: "Cd",
	})
	v, err := domain.CrearVenta(p)
	require.NoError(t, err)
	assert.Equal(t, p.Cliente.Nombre().Value(), v.Cliente().Nombre().Value())
	assert.Equal(t, p.Direccion.Calle(), v.Direccion().Calle())
	assert.InDelta(t, p.GPS.Latitud(), v.GPS().Latitud(), 0)
	assert.Equal(t, p.FechaVenta, v.FechaVenta())
	require.NotNil(t, v.Nota())
	// Nota is folded to ALL CAPS by the domain (Microsip convention).
	assert.Equal(t, "UNA NOTA", *v.Nota())
}

// ─── AsignarClienteMicrosip ────────────────────────────────────────────────

// buildAprobadaVentaSinCliente returns a Venta in SituacionAprobada with no
// clienteID set — the precondition for AsignarClienteMicrosip.
func buildAprobadaVentaSinCliente(t *testing.T) *domain.Venta {
	t.Helper()
	now := time.Now().Add(-time.Hour)
	return domain.HydrateVenta(domain.HydrateVentaParams{
		ID:             uuid.New(),
		TipoVenta:      domain.TipoVentaContado,
		Estado:         domain.EstadoActive,
		Situacion:      domain.SituacionAprobada,
		Sincronizacion: domain.SincronizacionPendiente,
		CreatedAt:      now,
		UpdatedAt:      now,
		CreatedBy:      uuid.New(),
		UpdatedBy:      uuid.New(),
	})
}

func TestAsignarClienteMicrosip_HappyPath(t *testing.T) {
	t.Parallel()
	v := buildAprobadaVentaSinCliente(t)
	require.Nil(t, v.ClienteID(), "fixture must have no clienteID")

	auditBefore := v.Audit()
	before := auditBefore.UpdatedAt()
	by := uuid.New()
	err := v.AsignarClienteMicrosip(123, by)
	require.NoError(t, err)

	require.NotNil(t, v.ClienteID())
	assert.Equal(t, 123, *v.ClienteID())
	auditAfter := v.Audit()
	assert.Equal(t, by, auditAfter.UpdatedBy())
	assert.True(t, auditAfter.UpdatedAt().After(before), "UpdatedAt should have advanced")
}

func TestAsignarClienteMicrosip_YaAsignado_Rechazado(t *testing.T) {
	t.Parallel()
	existing := 42
	now := time.Now().Add(-time.Hour)
	v := domain.HydrateVenta(domain.HydrateVentaParams{
		ID:             uuid.New(),
		ClienteID:      &existing,
		TipoVenta:      domain.TipoVentaContado,
		Estado:         domain.EstadoActive,
		Situacion:      domain.SituacionAprobada,
		Sincronizacion: domain.SincronizacionPendiente,
		CreatedAt:      now,
		UpdatedAt:      now,
		CreatedBy:      uuid.New(),
		UpdatedBy:      uuid.New(),
	})
	require.NotNil(t, v.ClienteID())

	err := v.AsignarClienteMicrosip(99, uuid.New())
	require.Error(t, err)
	require.ErrorIs(t, err, domain.ErrClienteYaAsignado)
	// original value must remain unchanged
	assert.Equal(t, 42, *v.ClienteID())
}

func TestAsignarClienteMicrosip_IDInvalido(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		id   int
	}{
		{"zero", 0},
		{"negative", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := buildAprobadaVentaSinCliente(t)
			err := v.AsignarClienteMicrosip(tc.id, uuid.New())
			require.Error(t, err)
			assert.ErrorIs(t, err, domain.ErrClienteIDInvalido)
		})
	}
}
