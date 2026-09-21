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

// versionMutatorCase names one of the six edit mutators Task G2 wires
// ExpectedVersion into (checkVersion, called right after puedeEditarse in
// every one of them) and knows how to invoke it against a fresh, valid venta
// with a given If-Match precondition.
type versionMutatorCase struct {
	name string
	run  func(v *domain.Venta, expected *int) error
}

func versionMutatorCases(t *testing.T) []versionMutatorCase {
	t.Helper()
	montos, err := domain.NewMontoSnapshot(decimal.NewFromInt(10), decimal.NewFromInt(8), decimal.NewFromInt(5))
	require.NoError(t, err)
	one, two := 1, 2
	newNom, err := domain.NewNombreCliente("Cliente Nuevo")
	require.NoError(t, err)
	newSnap, err := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: newNom})
	require.NoError(t, err)

	return []versionMutatorCase{
		{
			name: "ActualizarHeader",
			run: func(v *domain.Venta, expected *int) error {
				p := validCrearVentaParams(t)
				return v.ActualizarHeader(domain.ActualizarHeaderParams{
					Direccion:       p.Direccion,
					GPS:             p.GPS,
					FechaVenta:      p.FechaVenta.Add(time.Hour),
					Nota:            p.Nota,
					By:              uuid.New(),
					Now:             time.Now(),
					ExpectedVersion: expected,
				})
			},
		},
		{
			name: "ActualizarCliente",
			run: func(v *domain.Venta, expected *int) error {
				return v.ActualizarCliente(domain.ActualizarClienteParams{
					Cliente:         newSnap,
					By:              uuid.New(),
					Now:             time.Now(),
					ExpectedVersion: expected,
				})
			},
		},
		{
			name: "ReemplazarProductos",
			run: func(v *domain.Venta, expected *int) error {
				return v.ReemplazarProductos(domain.ReemplazarProductosParams{
					Productos: []domain.CrearVentaProductoInput{{
						ID: uuid.New(), ArticuloID: 99, Articulo: "Nuevo",
						Cantidad: decimal.NewFromInt(3), Precios: montos,
						AlmacenOrigen: &one, AlmacenDestino: &two,
					}},
					By:              uuid.New(),
					Now:             time.Now(),
					ExpectedVersion: expected,
				})
			},
		},
		{
			name: "ReemplazarCombos",
			run: func(v *domain.Venta, expected *int) error {
				return v.ReemplazarCombos(domain.ReemplazarCombosParams{
					Combos: []domain.CrearVentaComboInput{{
						ID: uuid.New(), Nombre: "C", Precios: montos,
						Cantidad: decimal.NewFromInt(1), AlmacenOrigen: 1, AlmacenDestino: 2,
					}},
					By:              uuid.New(),
					Now:             time.Now(),
					ExpectedVersion: expected,
				})
			},
		},
		{
			name: "ReemplazarLineas",
			run: func(v *domain.Venta, expected *int) error {
				return v.ReemplazarLineas(domain.ReemplazarLineasParams{
					Combos: nil,
					Productos: []domain.CrearVentaProductoInput{{
						ID: uuid.New(), ArticuloID: 99, Articulo: "Nuevo",
						Cantidad: decimal.NewFromInt(3), Precios: montos,
						AlmacenOrigen: &one, AlmacenDestino: &two,
					}},
					By:              uuid.New(),
					Now:             time.Now(),
					ExpectedVersion: expected,
				})
			},
		},
		{
			name: "ReemplazarVendedores",
			run: func(v *domain.Venta, expected *int) error {
				return v.ReemplazarVendedores(domain.ReemplazarVendedoresParams{
					Vendedores: []domain.CrearVentaVendedorInput{{
						ID: uuid.New(), UsuarioID: uuid.New(), Email: "x@y.com", Nombre: "X",
					}},
					By:              uuid.New(),
					Now:             time.Now(),
					ExpectedVersion: expected,
				})
			},
		},
	}
}

// TestVenta_ExpectedVersion_NilMeansNoPrecondition is the compatibility
// contract with the desktop app: it never sends If-Match, so a nil
// ExpectedVersion must ALWAYS pass, for all six mutators, regardless of the
// venta's actual persisted version.
func TestVenta_ExpectedVersion_NilMeansNoPrecondition(t *testing.T) {
	t.Parallel()
	for _, tc := range versionMutatorCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := buildValidVenta(t)
			require.NoError(t, tc.run(v, nil),
				"a nil ExpectedVersion must never be rejected — this is the desktop compatibility contract")
		})
	}
}

func TestVenta_ExpectedVersion_MatchingPersistedVersionPasses(t *testing.T) {
	t.Parallel()
	for _, tc := range versionMutatorCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := buildValidVenta(t)
			expected := v.PersistedVersion()
			require.NoError(t, tc.run(v, &expected))
		})
	}
}

func TestVenta_ExpectedVersion_MismatchRejectsWithPreconditionFailed(t *testing.T) {
	t.Parallel()
	for _, tc := range versionMutatorCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := buildValidVenta(t)
			stale := v.PersistedVersion() + 99
			err := tc.run(v, &stale)
			require.ErrorIs(t, err, domain.ErrVentaVersionConflicto)
			ae, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, "venta_version_conflicto", ae.Code)
			assert.Equal(t, apperror.KindPreconditionFailed, ae.Kind,
				"must be 412, not 409 — this surface's 409 already means three other things")
		})
	}
}

// TestVenta_ExpectedVersion_GuardOrder_EstadoAntesQueVersion pins the
// contract that the state guard (puedeEditarse → 409 venta_no_editable)
// ALWAYS runs before the version guard (checkVersion → 412
// venta_version_conflicto). A venta that already left borrador must answer
// 409 even against a hopelessly stale If-Match, or a caller would retry a
// write that can never succeed no matter how many times it re-reads the
// version.
func TestVenta_ExpectedVersion_GuardOrder_EstadoAntesQueVersion(t *testing.T) {
	t.Parallel()
	for _, tc := range versionMutatorCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := buildValidVenta(t)
			require.NoError(t, v.Cancelar("motivo", uuid.New(), time.Now()))
			stale := 999999
			err := tc.run(v, &stale)
			require.Error(t, err)
			ae, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, "venta_no_editable", ae.Code,
				"the state guard must win over the version guard regardless of the precondition sent")
		})
	}
}
