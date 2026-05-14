//nolint:misspell // domain vocabulary is Spanish per project convention.
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

func TestNewNombreCliente_RejectsEmoji(t *testing.T) {
	t.Parallel()
	_, err := domain.NewNombreCliente("José 🎉")
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "string_unsafe_chars", ae.Code)
}

func TestNewNombreCliente_RejectsNUL(t *testing.T) {
	t.Parallel()
	_, err := domain.NewNombreCliente("José\x00Pérez")
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "string_unsafe_chars", ae.Code)
}

func TestNewNombreCliente_AcceptsExtendedLatin(t *testing.T) {
	t.Parallel()
	_, err := domain.NewNombreCliente("Müller Ñoño Pérez")
	require.NoError(t, err)
}

func TestNewCancelacion_RejectsEmoji(t *testing.T) {
	t.Parallel()
	_, err := domain.NewCancelacion(timeFixed(), uuid.New(), "cancel 🚨")
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "string_unsafe_chars", ae.Code)
}

func TestNewVendedorSnapshot_RejectsUnsafeChars(t *testing.T) {
	t.Parallel()
	_, err := domain.NewVendedorSnapshot(domain.NewVendedorSnapshotParams{
		UsuarioID: uuid.New(),
		Email:     "v@x.com",
		Nombre:    "Vendedor 🚀",
	})
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "string_unsafe_chars", ae.Code)

	// Email field too.
	_, err = domain.NewVendedorSnapshot(domain.NewVendedorSnapshotParams{
		UsuarioID: uuid.New(),
		Email:     "v\x00@x.com",
		Nombre:    "V",
	})
	require.Error(t, err)
	ae, ok = apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "string_unsafe_chars", ae.Code)
}

func TestNewDireccion_RejectsUnsafeChars(t *testing.T) {
	t.Parallel()
	p := domain.NewDireccionParams{
		Calle: "Av. 🌮", Colonia: "C", Poblacion: "P", Ciudad: "CDMX",
	}
	_, err := domain.NewDireccion(p)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "string_unsafe_chars", ae.Code)
}

func TestCrearVenta_RejectsProductoArticuloEmoji(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Productos[0].Articulo = "Bici 🚴"
	_, err := domain.CrearVenta(p)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "string_unsafe_chars", ae.Code)
}

func TestCrearVenta_RejectsComboNombreEmoji(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Combos = []domain.CrearVentaComboInput{{
		ID:             uuid.New(),
		Nombre:         "Combo 🎉",
		Precios:        p.Montos,
		Cantidad:       decimal.NewFromInt(1),
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
	}}
	_, err := domain.CrearVenta(p)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "string_unsafe_chars", ae.Code)
}

func TestCrearVenta_RejectsNotaEmoji(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	nota := "nota con 🚀"
	p.Nota = &nota
	_, err := domain.CrearVenta(p)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "string_unsafe_chars", ae.Code)
}

// timeFixed returns a deterministic time for tests that don't care about
// the exact instant.
func timeFixed() time.Time { return time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC) }
