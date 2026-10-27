package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// Contrato v2.1: un inicio de semana posterior a "ahora" deja la fila sin
// configurar con motivo inicio_semana_futuro (nunca un 422 del tablero) y las
// vistas del usuario responden 409 desempeno_usuario_sin_inicio_semana.
func TestUsuarioCobranza_ConfiguracionEn_InicioFuturo(t *testing.T) {
	t.Parallel()
	ahora := time.Date(2026, 10, 14, 22, 40, 0, 0, time.UTC)
	futuro := ahora.Add(time.Minute)
	u := domain.UsuarioCobranza{ZonaID: 21563, InicioSemana: &futuro}

	ok, motivo := u.ConfiguracionEn(ahora)
	assert.False(t, ok)
	assert.Equal(t, domain.MotivoInicioSemanaFuturo, motivo)
	assert.Equal(t, "inicio_semana_futuro", motivo)

	err := u.ExigirConfiguradoEn(ahora)
	require.ErrorIs(t, err, domain.ErrUsuarioSinInicioSemana, "mismo código 409 del contrato")
	require.ErrorIs(t, err, domain.ErrUsuarioInicioSemanaFuturo)
	assert.Contains(t, err.Error(), "posterior")

	justo := ahora
	u.InicioSemana = &justo
	ok, motivo = u.ConfiguracionEn(ahora)
	assert.True(t, ok, "inicio == ahora es una ventana de ancho cero, válida (VentanaCobro.kt:37-41)")
	assert.Empty(t, motivo)
	require.NoError(t, u.ExigirConfiguradoEn(ahora))

	ok, motivo = domain.UsuarioCobranza{InicioSemana: &futuro}.ConfiguracionEn(ahora)
	assert.False(t, ok)
	assert.Equal(t, domain.MotivoSinZona, motivo, "la zona se sigue reportando primero")
	require.ErrorIs(t, domain.UsuarioCobranza{ZonaID: 1}.ExigirConfiguradoEn(ahora), domain.ErrUsuarioSinInicioSemana)
}

func TestUsuarioCobranza_InicioSemanaFuturo(t *testing.T) {
	t.Parallel()
	ahora := time.Date(2026, 10, 14, 22, 40, 0, 0, time.UTC)
	futuro, pasado := ahora.Add(time.Second), ahora.Add(-time.Second)
	assert.True(t, domain.UsuarioCobranza{InicioSemana: &futuro}.InicioSemanaFuturo(ahora))
	assert.False(t, domain.UsuarioCobranza{InicioSemana: &pasado}.InicioSemanaFuturo(ahora))
	assert.False(t, domain.UsuarioCobranza{}.InicioSemanaFuturo(ahora), "sin inicio no es futuro")
}
