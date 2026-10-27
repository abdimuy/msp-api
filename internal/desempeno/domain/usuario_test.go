package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestUsuarioCobranza_Configuracion(t *testing.T) {
	t.Parallel()
	inicio := time.Date(2026, 10, 9, 13, 46, 0, 0, time.UTC)
	ok, motivo := domain.UsuarioCobranza{ZonaID: 21563, InicioSemana: &inicio}.Configuracion()
	assert.True(t, ok)
	assert.Empty(t, motivo)

	ok, motivo = domain.UsuarioCobranza{ZonaID: 0, InicioSemana: nil}.Configuracion()
	assert.False(t, ok)
	assert.Equal(t, "sin_zona", motivo, "la zona se reporta primero")

	ok, motivo = domain.UsuarioCobranza{ZonaID: 21563}.Configuracion()
	assert.False(t, ok)
	assert.Equal(t, "sin_inicio_semana", motivo)

	require.ErrorIs(t, domain.UsuarioCobranza{}.ExigirConfigurado(), domain.ErrUsuarioSinZona)
	require.ErrorIs(t, domain.UsuarioCobranza{ZonaID: 1}.ExigirConfigurado(), domain.ErrUsuarioSinInicioSemana)
	require.NoError(t, domain.UsuarioCobranza{ZonaID: 1, InicioSemana: &inicio}.ExigirConfigurado())
}
