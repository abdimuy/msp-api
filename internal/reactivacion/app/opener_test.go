//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	reactivacionapp "github.com/abdimuy/msp-api/internal/reactivacion/app"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

func TestOpener_Generar_RecienLiquidado_ConNombre(t *testing.T) {
	t.Parallel()
	o := reactivacionapp.NewOpener()
	cuerpo, err := o.Generar(domain.SegmentoRecienLiquidado, "Minerva López")
	require.NoError(t, err)
	assert.NotEmpty(t, cuerpo)
	assert.Contains(t, cuerpo, "Minerva López")
}

func TestOpener_Generar_PorLiquidarHueco_ConNombre(t *testing.T) {
	t.Parallel()
	o := reactivacionapp.NewOpener()
	cuerpo, err := o.Generar(domain.SegmentoPorLiquidarHueco, "Rogelio Hernández")
	require.NoError(t, err)
	assert.NotEmpty(t, cuerpo)
	assert.Contains(t, cuerpo, "Rogelio Hernández")
}

func TestOpener_Generar_NombreVacio_SaludoGenerico(t *testing.T) {
	t.Parallel()
	o := reactivacionapp.NewOpener()
	cuerpo, err := o.Generar(domain.SegmentoRecienLiquidado, "")
	require.NoError(t, err)
	assert.NotEmpty(t, cuerpo)
	assert.NotContains(t, cuerpo, "Hola ,")
	assert.Contains(t, cuerpo, "¿Cómo está?")
}

func TestOpener_Generar_SegmentoInvalido(t *testing.T) {
	t.Parallel()
	o := reactivacionapp.NewOpener()
	cuerpo, err := o.Generar(domain.Segmento("no_existe"), "Alguien")
	require.ErrorIs(t, err, domain.ErrSegmentoInvalido)
	assert.Empty(t, cuerpo)
}

func TestOpener_Generar_SinMontos(t *testing.T) {
	t.Parallel()
	o := reactivacionapp.NewOpener()
	for _, seg := range []domain.Segmento{domain.SegmentoRecienLiquidado, domain.SegmentoPorLiquidarHueco} {
		cuerpo, err := o.Generar(seg, "Cliente Prueba")
		require.NoError(t, err)
		// Fase 2 never quotes a real amount — the templates are static.
		assert.NotContains(t, cuerpo, "$")
	}
}
