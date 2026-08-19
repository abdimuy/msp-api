//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// direccionConZona builds a hydrated Direccion carrying the given zona id
// (nil for "no zona captured").
func direccionConZona(zonaID *int) domain.Direccion {
	return domain.HydrateDireccion(domain.NewDireccionParams{
		Calle:         "AV. REFORMA",
		Colonia:       "CENTRO",
		Poblacion:     "CUAUHTEMOC",
		Ciudad:        "CDMX",
		ZonaClienteID: zonaID,
	})
}

func TestToDireccionDTO_ZonaCliente_NilMapLeavesFieldAbsent(t *testing.T) {
	t.Parallel()
	zona := 7
	dto := toDireccionDTO(direccionConZona(&zona), nil)

	require.NotNil(t, dto.ZonaClienteID)
	assert.Equal(t, 7, *dto.ZonaClienteID)
	assert.Nil(t, dto.ZonaCliente, "a nil zona map must leave zona_cliente absent")
}

func TestToDireccionDTO_ZonaCliente_EmptyMapLeavesFieldAbsent(t *testing.T) {
	t.Parallel()
	zona := 7
	dto := toDireccionDTO(direccionConZona(&zona), map[int]string{})

	assert.Nil(t, dto.ZonaCliente, "an empty zona map must leave zona_cliente absent")
}

func TestToDireccionDTO_ZonaCliente_ResolvedNameIsSet(t *testing.T) {
	t.Parallel()
	zona := 7
	dto := toDireccionDTO(direccionConZona(&zona), map[int]string{7: "TEHUACAN NORTE"})

	require.NotNil(t, dto.ZonaCliente)
	assert.Equal(t, "TEHUACAN NORTE", *dto.ZonaCliente)
}

func TestToDireccionDTO_ZonaCliente_MissFromMapLeavesFieldAbsent(t *testing.T) {
	t.Parallel()
	zona := 7
	// The map resolved a DIFFERENT zona: id 7 is a miss and must not degrade
	// into an empty string (the frontend distinguishes "unknown" from "").
	dto := toDireccionDTO(direccionConZona(&zona), map[int]string{9: "OTRA ZONA"})

	assert.Nil(t, dto.ZonaCliente, "an unresolved id must leave zona_cliente absent, not empty")
}

func TestToDireccionDTO_ZonaCliente_NoZonaIDLeavesFieldAbsent(t *testing.T) {
	t.Parallel()
	dto := toDireccionDTO(direccionConZona(nil), map[int]string{7: "TEHUACAN NORTE"})

	assert.Nil(t, dto.ZonaClienteID)
	assert.Nil(t, dto.ZonaCliente)
}
