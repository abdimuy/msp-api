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
	platform "github.com/abdimuy/msp-api/internal/platform/domain"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ─── TipoVenta ─────────────────────────────────────────────────────────────

func TestParseTipoVenta_Valid(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"CONTADO", "CREDITO"} {
		got, err := domain.ParseTipoVenta(s)
		require.NoError(t, err)
		assert.Equal(t, s, got.String())
		assert.True(t, got.IsValid())
	}
}

func TestParseTipoVenta_Invalid(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "contado", "OTRO", "x"} {
		_, err := domain.ParseTipoVenta(s)
		require.Error(t, err)
		ae, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "tipo_venta_invalido", ae.Code)
	}
}

// ─── FrecPago ──────────────────────────────────────────────────────────────

func TestParseFrecPago_Valid(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"SEMANAL", "QUINCENAL", "MENSUAL"} {
		got, err := domain.ParseFrecPago(s)
		require.NoError(t, err)
		assert.Equal(t, s, got.String())
		assert.True(t, got.IsValid())
	}
}

func TestParseFrecPago_Invalid(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "semanal", "ANUAL"} {
		_, err := domain.ParseFrecPago(s)
		require.Error(t, err)
		ae, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "frec_pago_invalida", ae.Code)
	}
}

// ─── DiaSemana ─────────────────────────────────────────────────────────────

func TestParseDiaSemana_Valid(t *testing.T) {
	t.Parallel()
	days := []string{"LUNES", "MARTES", "MIERCOLES", "JUEVES", "VIERNES", "SABADO", "DOMINGO"}
	for _, s := range days {
		got, err := domain.ParseDiaSemana(s)
		require.NoError(t, err)
		assert.Equal(t, s, got.String())
		assert.True(t, got.IsValid())
	}
}

func TestParseDiaSemana_Invalid(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "lunes", "MONDAY"} {
		_, err := domain.ParseDiaSemana(s)
		require.Error(t, err)
		ae, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "dia_semana_invalido", ae.Code)
	}
}

// ─── PlanCredito ───────────────────────────────────────────────────────────

func TestNewPlanCredito_Valid(t *testing.T) {
	t.Parallel()
	p, err := domain.NewPlanCredito(12, decimal.NewFromInt(1000), decimal.NewFromInt(500), domain.FrecPagoMensual)
	require.NoError(t, err)
	assert.Equal(t, 12, p.PlazoMeses())
	assert.True(t, p.Enganche().Equal(decimal.NewFromInt(1000)))
	assert.True(t, p.Parcialidad().Equal(decimal.NewFromInt(500)))
	assert.Equal(t, domain.FrecPagoMensual, p.FrecPago())
}

func TestNewPlanCredito_Invalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		plazo       int
		enganche    decimal.Decimal
		parcialidad decimal.Decimal
		frec        domain.FrecPago
		code        string
	}{
		{"plazo zero", 0, decimal.Zero, decimal.NewFromInt(1), domain.FrecPagoMensual, "plazo_no_positivo"},
		{"plazo neg", -1, decimal.Zero, decimal.NewFromInt(1), domain.FrecPagoMensual, "plazo_no_positivo"},
		{"enganche neg", 1, decimal.NewFromInt(-1), decimal.NewFromInt(1), domain.FrecPagoMensual, "monto_negativo"},
		{"parcialidad neg", 1, decimal.Zero, decimal.NewFromInt(-1), domain.FrecPagoMensual, "monto_negativo"},
		{"frec invalida", 1, decimal.Zero, decimal.NewFromInt(1), "WTF", "frec_pago_invalida"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewPlanCredito(tc.plazo, tc.enganche, tc.parcialidad, tc.frec)
			require.Error(t, err)
			ae, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, tc.code, ae.Code)
		})
	}
}

func TestPlanCredito_EqualsAndHydrate(t *testing.T) {
	t.Parallel()
	a, err := domain.NewPlanCredito(12, decimal.NewFromInt(100), decimal.NewFromInt(50), domain.FrecPagoSemanal)
	require.NoError(t, err)
	b, err := domain.NewPlanCredito(12, decimal.NewFromInt(100), decimal.NewFromInt(50), domain.FrecPagoSemanal)
	require.NoError(t, err)
	c, err := domain.NewPlanCredito(6, decimal.NewFromInt(100), decimal.NewFromInt(50), domain.FrecPagoSemanal)
	require.NoError(t, err)
	assert.True(t, a.Equals(b))
	assert.False(t, a.Equals(c))

	h := domain.HydratePlanCredito(0, decimal.NewFromInt(-99), decimal.NewFromInt(-1), "X")
	assert.Equal(t, 0, h.PlazoMeses())
	assert.Equal(t, domain.FrecPago("X"), h.FrecPago())
}

// ─── DiaCobranza ───────────────────────────────────────────────────────────

func TestNewDiaCobranzaSemana_Valid(t *testing.T) {
	t.Parallel()
	d, err := domain.NewDiaCobranzaSemana(domain.DiaSemanaMartes)
	require.NoError(t, err)
	assert.True(t, d.IsSemana())
	assert.False(t, d.IsMes())
	require.NotNil(t, d.Semana())
	assert.Equal(t, domain.DiaSemanaMartes, *d.Semana())
	assert.Nil(t, d.Mes())
}

func TestNewDiaCobranzaSemana_Invalid(t *testing.T) {
	t.Parallel()
	_, err := domain.NewDiaCobranzaSemana("INVALID")
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "dia_semana_invalido", ae.Code)
}

func TestNewDiaCobranzaMes_Valid(t *testing.T) {
	t.Parallel()
	for _, day := range []int{1, 15, 31} {
		d, err := domain.NewDiaCobranzaMes(day)
		require.NoError(t, err)
		assert.True(t, d.IsMes())
		assert.False(t, d.IsSemana())
		require.NotNil(t, d.Mes())
		assert.Equal(t, day, *d.Mes())
	}
}

func TestNewDiaCobranzaMes_Invalid(t *testing.T) {
	t.Parallel()
	for _, day := range []int{0, -1, 32, 100} {
		_, err := domain.NewDiaCobranzaMes(day)
		require.Error(t, err)
		ae, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "dia_mes_invalido", ae.Code)
	}
}

func TestDiaCobranza_EqualsAndHydrate(t *testing.T) {
	t.Parallel()
	a, err := domain.NewDiaCobranzaSemana(domain.DiaSemanaLunes)
	require.NoError(t, err)
	b, err := domain.NewDiaCobranzaSemana(domain.DiaSemanaLunes)
	require.NoError(t, err)
	c, err := domain.NewDiaCobranzaSemana(domain.DiaSemanaMartes)
	require.NoError(t, err)
	m, err := domain.NewDiaCobranzaMes(15)
	require.NoError(t, err)

	assert.True(t, a.Equals(b))
	assert.False(t, a.Equals(c))
	assert.False(t, a.Equals(m))
	assert.False(t, m.Equals(a))

	// Two month-based dias with different days.
	m2, err := domain.NewDiaCobranzaMes(20)
	require.NoError(t, err)
	assert.False(t, m.Equals(m2))

	// Hydrate accepts arbitrary input.
	day := 99
	h := domain.HydrateDiaCobranza(nil, &day)
	require.NotNil(t, h.Mes())
	assert.Equal(t, 99, *h.Mes())

	// Equals: two month-based dias with same day.
	m3, err := domain.NewDiaCobranzaMes(15)
	require.NoError(t, err)
	assert.True(t, m.Equals(m3))

	// Equals: both zero values (neither semana nor mes set) — through Hydrate.
	zero1 := domain.HydrateDiaCobranza(nil, nil)
	zero2 := domain.HydrateDiaCobranza(nil, nil)
	assert.True(t, zero1.Equals(zero2))
}

// ─── Direccion ─────────────────────────────────────────────────────────────

func validDireccionParams() domain.NewDireccionParams {
	num := "123"
	zona := 7
	return domain.NewDireccionParams{
		Calle:          "Av. Reforma",
		NumeroExterior: &num,
		Colonia:        "Centro",
		Poblacion:      "Cd. Mexico",
		Ciudad:         "CDMX",
		ZonaClienteID:  &zona,
	}
}

func TestNewDireccion_Valid(t *testing.T) {
	t.Parallel()
	d, err := domain.NewDireccion(validDireccionParams())
	require.NoError(t, err)
	// Address text is folded to ALL CAPS by the domain (Microsip convention).
	assert.Equal(t, "AV. REFORMA", d.Calle())
	require.NotNil(t, d.NumeroExterior())
	assert.Equal(t, "123", *d.NumeroExterior())
	assert.Equal(t, "CENTRO", d.Colonia())
	assert.Equal(t, "CD. MEXICO", d.Poblacion())
	assert.Equal(t, "CDMX", d.Ciudad())
	require.NotNil(t, d.ZonaClienteID())
	assert.Equal(t, 7, *d.ZonaClienteID())
}

func TestNewDireccion_BlankNumeroExteriorNormalizesToNil(t *testing.T) {
	t.Parallel()
	p := validDireccionParams()
	blank := "   "
	p.NumeroExterior = &blank
	d, err := domain.NewDireccion(p)
	require.NoError(t, err)
	assert.Nil(t, d.NumeroExterior())
}

func TestNewDireccion_RejectsInvalid(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 301)
	longNumExt := strings.Repeat("y", 51)
	long120 := strings.Repeat("z", 121)
	cases := []struct {
		name string
		mut  func(p *domain.NewDireccionParams)
		code string
	}{
		{"calle empty", func(p *domain.NewDireccionParams) { p.Calle = "  " }, "calle_required"},
		{"calle too long", func(p *domain.NewDireccionParams) { p.Calle = long }, "calle_too_long"},
		{"numero too long", func(p *domain.NewDireccionParams) { p.NumeroExterior = &longNumExt }, "numero_exterior_too_long"},
		{"colonia empty", func(p *domain.NewDireccionParams) { p.Colonia = "" }, "colonia_required"},
		{"colonia too long", func(p *domain.NewDireccionParams) { p.Colonia = long120 }, "colonia_too_long"},
		{"poblacion empty", func(p *domain.NewDireccionParams) { p.Poblacion = "" }, "poblacion_required"},
		{"poblacion too long", func(p *domain.NewDireccionParams) { p.Poblacion = long120 }, "poblacion_too_long"},
		{"ciudad empty", func(p *domain.NewDireccionParams) { p.Ciudad = "" }, "ciudad_required"},
		{"ciudad too long", func(p *domain.NewDireccionParams) { p.Ciudad = long120 }, "ciudad_too_long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validDireccionParams()
			tc.mut(&p)
			_, err := domain.NewDireccion(p)
			require.Error(t, err)
			ae, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, tc.code, ae.Code)
		})
	}
}

func TestHydrateDireccion_Bypass(t *testing.T) {
	t.Parallel()
	d := domain.HydrateDireccion(domain.NewDireccionParams{Calle: ""})
	assert.Empty(t, d.Calle())
}

// ─── GPSCoords ─────────────────────────────────────────────────────────────

func TestNewGPSCoords_Valid(t *testing.T) {
	t.Parallel()
	cases := []struct{ lat, lng float64 }{
		{0, 0},
		{-90, -180},
		{90, 180},
		{19.4326, -99.1332},
	}
	for _, tc := range cases {
		g, err := domain.NewGPSCoords(tc.lat, tc.lng)
		require.NoError(t, err)
		assert.InDelta(t, tc.lat, g.Latitud(), 0)
		assert.InDelta(t, tc.lng, g.Longitud(), 0)
	}
}

func TestNewGPSCoords_Invalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		lat, lng float64
		code     string
	}{
		{-90.0001, 0, "gps_latitud_invalida"},
		{90.0001, 0, "gps_latitud_invalida"},
		{0, -180.0001, "gps_longitud_invalida"},
		{0, 180.0001, "gps_longitud_invalida"},
	}
	nan := 0.0 / func() float64 { return 0 }()
	cases = append(cases,
		struct {
			lat, lng float64
			code     string
		}{nan, 0, "gps_latitud_invalida"},
		struct {
			lat, lng float64
			code     string
		}{0, nan, "gps_longitud_invalida"},
	)
	for _, tc := range cases {
		_, err := domain.NewGPSCoords(tc.lat, tc.lng)
		require.Error(t, err)
		ae, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, tc.code, ae.Code)
	}
}

func TestGPSCoords_EqualsAndHydrate(t *testing.T) {
	t.Parallel()
	a, _ := domain.NewGPSCoords(1, 2)
	b, _ := domain.NewGPSCoords(1, 2)
	c, _ := domain.NewGPSCoords(1, 3)
	assert.True(t, a.Equals(b))
	assert.False(t, a.Equals(c))
	h := domain.HydrateGPSCoords(999, -999)
	assert.InDelta(t, 999.0, h.Latitud(), 0)
}

// ─── MontoSnapshot ─────────────────────────────────────────────────────────

func TestNewMontoSnapshot_Valid(t *testing.T) {
	t.Parallel()
	m, err := domain.NewMontoSnapshot(decimal.NewFromInt(100), decimal.NewFromInt(80), decimal.NewFromInt(50))
	require.NoError(t, err)
	assert.True(t, m.Anual().Equal(decimal.NewFromInt(100)))
	assert.True(t, m.CortoPlazo().Equal(decimal.NewFromInt(80)))
	assert.True(t, m.Contado().Equal(decimal.NewFromInt(50)))
}

func TestNewMontoSnapshot_Invalid(t *testing.T) {
	t.Parallel()
	cases := [][3]decimal.Decimal{
		{decimal.NewFromInt(-1), decimal.Zero, decimal.Zero},
		{decimal.Zero, decimal.NewFromInt(-1), decimal.Zero},
		{decimal.Zero, decimal.Zero, decimal.NewFromInt(-1)},
	}
	for _, tc := range cases {
		_, err := domain.NewMontoSnapshot(tc[0], tc[1], tc[2])
		require.Error(t, err)
		ae, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "monto_negativo", ae.Code)
	}
}

func TestMontoSnapshot_EqualsAndHydrate(t *testing.T) {
	t.Parallel()
	a, _ := domain.NewMontoSnapshot(decimal.NewFromInt(1), decimal.NewFromInt(2), decimal.NewFromInt(3))
	b, _ := domain.NewMontoSnapshot(decimal.NewFromInt(1), decimal.NewFromInt(2), decimal.NewFromInt(3))
	c, _ := domain.NewMontoSnapshot(decimal.NewFromInt(9), decimal.NewFromInt(2), decimal.NewFromInt(3))
	assert.True(t, a.Equals(b))
	assert.False(t, a.Equals(c))
	h := domain.HydrateMontoSnapshot(decimal.NewFromInt(-1), decimal.Zero, decimal.Zero)
	assert.True(t, h.Anual().Equal(decimal.NewFromInt(-1)))
}

// ─── NombreCliente / ClienteSnapshot ───────────────────────────────────────

func TestNewNombreCliente_Valid(t *testing.T) {
	t.Parallel()
	n, err := domain.NewNombreCliente("  Juan Pérez Reyes  ")
	require.NoError(t, err)
	// Person names are folded to ALL CAPS by the domain (Microsip convention);
	// Unicode case mapping handles accents (é→É).
	assert.Equal(t, "JUAN PÉREZ REYES", n.Value())
	assert.Equal(t, "JUAN PÉREZ REYES", n.String())
	assert.False(t, n.IsZero())
}

func TestNewNombreCliente_Invalid(t *testing.T) {
	t.Parallel()
	_, err := domain.NewNombreCliente("   ")
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "nombre_cliente_required", ae.Code)

	_, err = domain.NewNombreCliente(strings.Repeat("a", 201))
	require.Error(t, err)
	ae, ok = apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "nombre_cliente_too_long", ae.Code)
}

func TestNombreCliente_EqualsHydrate(t *testing.T) {
	t.Parallel()
	a, _ := domain.NewNombreCliente("Foo")
	b, _ := domain.NewNombreCliente("Foo")
	c, _ := domain.NewNombreCliente("Bar")
	assert.True(t, a.Equals(b))
	assert.False(t, a.Equals(c))

	h := domain.HydrateNombreCliente("")
	assert.True(t, h.IsZero())
}

func TestNewClienteSnapshot_Valid(t *testing.T) {
	t.Parallel()
	nom, err := domain.NewNombreCliente("Juan")
	require.NoError(t, err)
	tel, err := platform.NewTelefono("+524491234567")
	require.NoError(t, err)
	aval, err := domain.NewNombreCliente("Maria")
	require.NoError(t, err)

	c, err := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{
		Nombre: nom, Telefono: &tel, Aval: &aval,
	})
	require.NoError(t, err)
	// Cliente nombre is folded to ALL CAPS by the domain (Microsip convention).
	assert.Equal(t, "JUAN", c.Nombre().Value())
	require.NotNil(t, c.Telefono())
	require.NotNil(t, c.Aval())
}

func TestNewClienteSnapshot_RejectsEmptyNombre(t *testing.T) {
	t.Parallel()
	_, err := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{})
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "nombre_cliente_required", ae.Code)
}

func TestNewClienteSnapshot_RejectsAvalTooLong(t *testing.T) {
	t.Parallel()
	nom, _ := domain.NewNombreCliente("Juan")
	aval := domain.HydrateNombreCliente(strings.Repeat("a", 201))
	_, err := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nom, Aval: &aval})
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "aval_too_long", ae.Code)
}

func TestHydrateClienteSnapshot_Bypasses(t *testing.T) {
	t.Parallel()
	c := domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{})
	assert.True(t, c.Nombre().IsZero())
}

// ─── VendedorSnapshot ──────────────────────────────────────────────────────

func TestNewVendedorSnapshot_Valid(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	v, err := domain.NewVendedorSnapshot(domain.NewVendedorSnapshotParams{
		UsuarioID: id, Email: "  foo@bar.com  ", Nombre: " Juan  ",
	})
	require.NoError(t, err)
	assert.Equal(t, id, v.UsuarioID())
	assert.Equal(t, "foo@bar.com", v.Email())
	assert.Equal(t, "Juan", v.Nombre())
}

func TestNewVendedorSnapshot_Invalid(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	cases := []struct {
		name, email, nombre, code string
	}{
		{"email empty", "", "Juan", "vendedor_email_required"},
		{"email too long", strings.Repeat("a", 256), "Juan", "vendedor_email_too_long"},
		{"nombre empty", "x@y.com", "", "vendedor_nombre_required"},
		{"nombre too long", "x@y.com", strings.Repeat("a", 201), "vendedor_nombre_too_long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewVendedorSnapshot(domain.NewVendedorSnapshotParams{
				UsuarioID: id, Email: tc.email, Nombre: tc.nombre,
			})
			require.Error(t, err)
			ae, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, tc.code, ae.Code)
		})
	}
}

func TestVendedorSnapshot_Equals(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	a, _ := domain.NewVendedorSnapshot(domain.NewVendedorSnapshotParams{UsuarioID: id, Email: "x@y.com", Nombre: "A"})
	b, _ := domain.NewVendedorSnapshot(domain.NewVendedorSnapshotParams{UsuarioID: id, Email: "x@y.com", Nombre: "A"})
	c, _ := domain.NewVendedorSnapshot(domain.NewVendedorSnapshotParams{UsuarioID: id, Email: "z@y.com", Nombre: "A"})
	assert.True(t, a.Equals(b))
	assert.False(t, a.Equals(c))

	h := domain.HydrateVendedorSnapshot(domain.NewVendedorSnapshotParams{UsuarioID: id, Email: "", Nombre: ""})
	assert.Empty(t, h.Email())
}

// ─── ImagenStorage ─────────────────────────────────────────────────────────

func TestParseStorageKind(t *testing.T) {
	t.Parallel()
	got, err := domain.ParseStorageKind("FILESYSTEM")
	require.NoError(t, err)
	assert.Equal(t, domain.StorageKindFilesystem, got)
	assert.Equal(t, "FILESYSTEM", got.String())
	assert.True(t, got.IsValid())

	// R2 was removed from the Go enum; only FILESYSTEM round-trips. The DB
	// CHECK constraint still permits "R2" for forward compatibility but the
	// Go code never produces or accepts it.
	for _, bad := range []string{"R2", "S3", "", "filesystem"} {
		_, err = domain.ParseStorageKind(bad)
		require.Errorf(t, err, "ParseStorageKind(%q) must fail", bad)
		ae, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "storage_kind_invalido", ae.Code)
	}
}

func TestIsAllowedMime(t *testing.T) {
	t.Parallel()
	for _, m := range []string{"image/jpeg", "image/png", "image/gif", "image/webp"} {
		assert.True(t, domain.IsAllowedMime(m))
	}
	for _, m := range []string{"", "image/bmp", "application/pdf", "text/plain"} {
		assert.False(t, domain.IsAllowedMime(m))
	}
}

func TestNewImagenStorage_Valid(t *testing.T) {
	t.Parallel()
	s, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "ventas/2026/05/abc.jpg")
	require.NoError(t, err)
	assert.Equal(t, domain.StorageKindFilesystem, s.Kind())
	assert.Equal(t, "ventas/2026/05/abc.jpg", s.Key())
}

func TestNewImagenStorage_Invalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		kind domain.StorageKind
		key  string
		code string
	}{
		{"kind invalido", "X", "ok.jpg", "storage_kind_invalido"},
		{"key empty", domain.StorageKindFilesystem, "", "storage_key_invalida"},
		{"key whitespace", domain.StorageKindFilesystem, "  ", "storage_key_invalida"},
		{"key leading slash", domain.StorageKindFilesystem, "/a/b.jpg", "storage_key_invalida"},
		{"key traversal", domain.StorageKindFilesystem, "a/../b.jpg", "storage_key_invalida"},
		{"key null byte", domain.StorageKindFilesystem, "a\x00b.jpg", "storage_key_invalida"},
		{"key too long", domain.StorageKindFilesystem, strings.Repeat("a", 501), "storage_key_invalida"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewImagenStorage(tc.kind, tc.key)
			require.Error(t, err)
			ae, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, tc.code, ae.Code)
		})
	}
}

func TestImagenStorage_EqualsHydrate(t *testing.T) {
	t.Parallel()
	a, _ := domain.NewImagenStorage(domain.StorageKindFilesystem, "k1.jpg")
	b, _ := domain.NewImagenStorage(domain.StorageKindFilesystem, "k1.jpg")
	c, _ := domain.NewImagenStorage(domain.StorageKindFilesystem, "k2.jpg")
	assert.True(t, a.Equals(b))
	assert.False(t, a.Equals(c))

	h := domain.HydrateImagenStorage(domain.StorageKindFilesystem, "")
	assert.Empty(t, h.Key())
}

// ─── Cancelacion ───────────────────────────────────────────────────────────

func TestNewCancelacion_Valid(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	by := uuid.New()
	c, err := domain.NewCancelacion(at, by, "cliente se arrepintió")
	require.NoError(t, err)
	assert.Equal(t, at, c.At())
	assert.Equal(t, by, c.By())
	assert.Equal(t, "cliente se arrepintió", c.Reason())
}

func TestNewCancelacion_Invalid(t *testing.T) {
	t.Parallel()
	at := time.Now()
	by := uuid.New()
	_, err := domain.NewCancelacion(at, by, "   ")
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "reason_cancelacion_required", ae.Code)

	_, err = domain.NewCancelacion(at, by, strings.Repeat("a", 501))
	require.Error(t, err)
	ae, ok = apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "reason_cancelacion_too_long", ae.Code)
}

func TestHydrateCancelacion(t *testing.T) {
	t.Parallel()
	at := time.Now()
	by := uuid.New()
	c := domain.HydrateCancelacion(at, by, "")
	assert.Empty(t, c.Reason())
}
