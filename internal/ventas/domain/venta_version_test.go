//nolint:misspell // ventas vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ─── construction ───────────────────────────────────────────────────────────

func TestVenta_Version_StartsAtZeroForFreshVenta(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	assert.Equal(t, 0, v.Version())
	assert.Equal(t, 0, v.PersistedVersion(),
		"a freshly-created venta has never been persisted — PersistedVersion must match Version at 0")
}

func TestHydrateVenta_SetsVersionAndPersistedVersionToTheGivenValue(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	hp := domain.HydrateVentaParams{
		ID: p.ID, Cliente: p.Cliente, Direccion: p.Direccion, GPS: p.GPS,
		FechaVenta: p.FechaVenta, TipoVenta: p.TipoVenta,
		Montos: domain.HydrateMontoSnapshot(decimal.NewFromInt(1), decimal.NewFromInt(1), decimal.NewFromInt(1)),
		Estado: domain.EstadoActive, Situacion: domain.SituacionBorrador,
		Sincronizacion: domain.SincronizacionPendiente,
		Version:        7,
		CreatedAt:      p.FechaVenta, UpdatedAt: p.FechaVenta,
		CreatedBy: p.CreatedBy, UpdatedBy: p.CreatedBy,
	}
	v := domain.HydrateVenta(hp)
	assert.Equal(t, 7, v.Version())
	assert.Equal(t, 7, v.PersistedVersion())
}

// ─── MarkVersionPersisted ───────────────────────────────────────────────────

func TestVenta_MarkVersionPersisted_SyncsPersistedVersionToCurrent(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	require.NoError(t, v.Cancelar("motivo", uuid.New(), time.Now()))
	assert.Equal(t, 1, v.Version())
	assert.Equal(t, 0, v.PersistedVersion(),
		"PersistedVersion must stay pinned at construction time until confirmed")

	v.MarkVersionPersisted()
	assert.Equal(t, v.Version(), v.PersistedVersion(),
		"MarkVersionPersisted must sync PersistedVersion to the current Version")
}

func TestVenta_MarkVersionPersisted_SurvivesSeveralMutationsBeforeFirstConfirm(t *testing.T) {
	// Pins the exact scenario Task G1 measured against Firebird: a caller may
	// apply several domain mutations (e.g. EnviarARevision + Aprobar) on a
	// brand-new aggregate before it is ever persisted. PersistedVersion must
	// stay at its construction-time value (0) through all of them, so that
	// the repository's very first INSERT — which writes Version() as-is —
	// and the very next confirm land on the same number.
	t.Parallel()
	by := uuid.New()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	v := buildValidVenta(t)
	require.NoError(t, v.EnviarARevision(by, now))
	require.NoError(t, v.Aprobar(by, now))
	assert.Equal(t, 2, v.Version())
	assert.Equal(t, 0, v.PersistedVersion())

	v.MarkVersionPersisted()
	assert.Equal(t, 2, v.PersistedVersion())
}

// ─── every mutator and transition bumps Version() by exactly 1 ─────────────

// TestVenta_MarkUpdated_BumpsVersionByExactlyOne recorre TODOS los mutadores
// de edición y TODAS las transiciones de ciclo de vida que Task G1 hace pasar
// por el helper markUpdated, y comprueba que cada uno sube Version() en
// exactamente 1. Es una prueba que se pone roja sola cuando alguien agregue
// un mutador nuevo sin pasar por el helper, o cuando alguien quite el
// incremento.
func TestVenta_MarkUpdated_BumpsVersionByExactlyOne(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	montosParaLinea := func(t *testing.T) domain.MontoSnapshot {
		t.Helper()
		m, err := domain.NewMontoSnapshot(decimal.NewFromInt(10), decimal.NewFromInt(8), decimal.NewFromInt(5))
		require.NoError(t, err)
		return m
	}

	cases := []struct {
		name string
		// setup returns a venta staged in whatever situación the mutator under
		// test requires. It may itself call other mutators — Version() is
		// sampled AFTER setup returns, right before the mutator under test, so
		// setup-induced bumps never pollute the assertion.
		setup  func(t *testing.T) *domain.Venta
		mutate func(t *testing.T, v *domain.Venta) error
	}{
		{
			name:  "ActualizarHeader",
			setup: buildValidVenta,
			mutate: func(t *testing.T, v *domain.Venta) error {
				t.Helper()
				return v.ActualizarHeader(domain.ActualizarHeaderParams{
					Direccion: v.Direccion(), GPS: v.GPS(),
					FechaVenta: v.FechaVenta().Add(time.Hour),
					By:         by, Now: now,
				})
			},
		},
		{
			name:  "ActualizarCliente",
			setup: buildValidVenta,
			mutate: func(t *testing.T, v *domain.Venta) error {
				t.Helper()
				snap, err := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: v.Cliente().Nombre()})
				require.NoError(t, err)
				return v.ActualizarCliente(domain.ActualizarClienteParams{Cliente: snap, By: by, Now: now})
			},
		},
		{
			name:  "ReemplazarProductos",
			setup: buildValidVenta,
			mutate: func(t *testing.T, v *domain.Venta) error {
				t.Helper()
				one, two := 1, 2
				return v.ReemplazarProductos(domain.ReemplazarProductosParams{
					Productos: []domain.CrearVentaProductoInput{{
						ID: uuid.New(), ArticuloID: 1, Articulo: "Nuevo",
						Cantidad: decimal.NewFromInt(1), Precios: montosParaLinea(t),
						AlmacenOrigen: &one, AlmacenDestino: &two,
					}},
					By: by, Now: now,
				})
			},
		},
		{
			name:  "ReemplazarCombos",
			setup: buildValidVenta,
			mutate: func(t *testing.T, v *domain.Venta) error {
				t.Helper()
				return v.ReemplazarCombos(domain.ReemplazarCombosParams{Combos: nil, By: by, Now: now})
			},
		},
		{
			name:  "ReemplazarVendedores",
			setup: buildValidVenta,
			mutate: func(t *testing.T, v *domain.Venta) error {
				t.Helper()
				return v.ReemplazarVendedores(domain.ReemplazarVendedoresParams{
					Vendedores: []domain.CrearVentaVendedorInput{{
						ID: uuid.New(), UsuarioID: uuid.New(), Email: "x@y.com", Nombre: "X",
					}},
					By: by, Now: now,
				})
			},
		},
		{
			name:  "ReemplazarLineas",
			setup: buildValidVenta,
			mutate: func(t *testing.T, v *domain.Venta) error {
				t.Helper()
				one, two := 1, 2
				return v.ReemplazarLineas(domain.ReemplazarLineasParams{
					Productos: []domain.CrearVentaProductoInput{{
						ID: uuid.New(), ArticuloID: 1, Articulo: "Nuevo",
						Cantidad: decimal.NewFromInt(1), Precios: montosParaLinea(t),
						AlmacenOrigen: &one, AlmacenDestino: &two,
					}},
					By: by, Now: now,
				})
			},
		},
		{
			name:  "Cancelar",
			setup: buildValidVenta,
			mutate: func(t *testing.T, v *domain.Venta) error {
				t.Helper()
				return v.Cancelar("motivo", by, now)
			},
		},
		{
			name:  "EnviarARevision",
			setup: buildValidVenta,
			mutate: func(t *testing.T, v *domain.Venta) error {
				t.Helper()
				return v.EnviarARevision(by, now)
			},
		},
		{
			name: "Aprobar",
			setup: func(t *testing.T) *domain.Venta {
				t.Helper()
				v := buildValidVenta(t)
				require.NoError(t, v.EnviarARevision(by, now))
				return v
			},
			mutate: func(t *testing.T, v *domain.Venta) error {
				t.Helper()
				return v.Aprobar(by, now)
			},
		},
		{
			name: "RegresarABorrador",
			setup: func(t *testing.T) *domain.Venta {
				t.Helper()
				v := buildValidVenta(t)
				require.NoError(t, v.EnviarARevision(by, now))
				return v
			},
			mutate: func(t *testing.T, v *domain.Venta) error {
				t.Helper()
				return v.RegresarABorrador(by, now)
			},
		},
		{
			name: "MarcarAplicada",
			setup: func(t *testing.T) *domain.Venta {
				t.Helper()
				v := buildValidVenta(t)
				require.NoError(t, v.EnviarARevision(by, now))
				require.NoError(t, v.Aprobar(by, now))
				return v
			},
			mutate: func(t *testing.T, v *domain.Venta) error {
				t.Helper()
				return v.MarcarAplicada(123, "Y00002262", now, by)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := tc.setup(t)
			before := v.Version()
			require.NoError(t, tc.mutate(t, v), "%s must succeed", tc.name)
			assert.Equal(t, before+1, v.Version(), "%s must bump Version() by exactly 1", tc.name)
		})
	}
}

// TestVenta_ChildOnlyMutators_DoNotBumpVersion pins the deliberate exclusion
// from Task G1: AdjuntarImagen, EliminarImagen and AsignarClienteMicrosip
// keep calling audit.MarkUpdated directly and must NOT bump Version(). None
// of the three, on their own, reach a VERSION-guarded UPDATE on MSP_VENTAS
// (imagen inserts/deletes touch only MSP_VENTAS_IMAGENES; the cliente_id
// link set by AsignarClienteMicrosip rides along in the same physical
// AplicarVenta UPDATE that MarcarAplicada already bumps) — routing them
// through markUpdated too would double-bump that single write and break the
// repository's compare-and-swap precondition (measured against Firebird:
// see Task G1's report).
func TestVenta_ChildOnlyMutators_DoNotBumpVersion(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	t.Run("AdjuntarImagen", func(t *testing.T) {
		t.Parallel()
		v := buildValidVenta(t)
		before := v.Version()
		storage, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "ventas/x/1.jpg")
		require.NoError(t, err)
		_, err = v.AdjuntarImagen(domain.AdjuntarImagenParams{
			ID: uuid.New(), Storage: storage, Mime: "image/jpeg", SizeBytes: 100,
			By: by, Now: now,
		})
		require.NoError(t, err)
		assert.Equal(t, before, v.Version())
	})

	t.Run("EliminarImagen", func(t *testing.T) {
		t.Parallel()
		v := buildValidVenta(t)
		storage, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "ventas/x/1.jpg")
		require.NoError(t, err)
		img, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
			ID: uuid.New(), Storage: storage, Mime: "image/jpeg", SizeBytes: 100,
			By: by, Now: now,
		})
		require.NoError(t, err)
		before := v.Version()
		require.NoError(t, v.EliminarImagen(img.ID(), by, now))
		assert.Equal(t, before, v.Version())
	})

	t.Run("AsignarClienteMicrosip", func(t *testing.T) {
		t.Parallel()
		v := buildValidVenta(t)
		before := v.Version()
		require.NoError(t, v.AsignarClienteMicrosip(12345, by))
		assert.Equal(t, before, v.Version())
	})
}

// This file deliberately does not test a client-supplied "expected version"
// precondition at the domain level (no checkVersion, no ExpectedVersion
// param on any mutator's Params struct) — that wiring, and the 412 HTTP
// surface, is Task G2. Task G1 only lands the column, the domain counter,
// and the repository-level compare-and-swap that protects every write
// unconditionally, regardless of whether the caller ever asks for it (see
// internal/ventas/infra/ventfb for that half of the contract, and
// domain.ErrVentaVersionConflicto in errors.go for the sentinel it returns).
