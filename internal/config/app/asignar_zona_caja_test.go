package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/config/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

func TestAsignarZonaCaja_HappyPath_LlamaUpsert(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.zonasExistentes[12271] = true
	catalogo.cajasExistentes[12151] = true
	catalogo.cajerosExistentes[22368] = true
	catalogo.vendedoresExistentes[88240] = true
	catalogo.cobradoresExistentes[11294] = true

	err := svc.AsignarZonaCaja(context.Background(), 12271, 12151, 22368, 88240, 11294)

	require.NoError(t, err)
	assert.Equal(t, 1, repo.upsertZonaCajaCall)
	stored, ok := repo.zonaCajaConfigs[12271]
	require.True(t, ok)
	assert.Equal(t, 12151, stored.CajaID)
	assert.Equal(t, 22368, stored.CajeroID)
	assert.Equal(t, 88240, stored.VendedorID)
	assert.Equal(t, 11294, stored.CobradorID)
}

func TestAsignarZonaCaja_TodosLosSlotsSentinela_OmiteChecksDeCatalogo(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.zonasExistentes[12271] = true
	// No catalog existence flags set for caja/cajero/vendedor/cobrador — must
	// not be consulted because every slot is the sentinel.

	err := svc.AsignarZonaCaja(context.Background(), 12271, -1, -1, -1, -1)

	require.NoError(t, err)
	assert.Equal(t, 1, repo.upsertZonaCajaCall)
}

func TestAsignarZonaCaja_IDInvalido_NoLlamaUpsert(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := newTestService()

	err := svc.AsignarZonaCaja(context.Background(), 12271, 0, -1, -1, -1)

	require.ErrorIs(t, err, domain.ErrZonaCajaIDInvalido)
	assert.Equal(t, 0, repo.upsertZonaCajaCall)
}

func TestAsignarZonaCaja_ZonaNoExiste_NotFound_NoUpsert(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.zonasExistentes[12271] = false // explicit, but zero-value is already false

	err := svc.AsignarZonaCaja(context.Background(), 12271, -1, -1, -1, -1)

	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "zona_no_existe", appErr.Code)
	assert.Equal(t, apperror.KindNotFound, appErr.Kind)
	assert.Equal(t, 0, repo.upsertZonaCajaCall)
}

func TestAsignarZonaCaja_CajaNoExisteEnCatalogo_ErrorValidacion_NoUpsert(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.zonasExistentes[12271] = true
	// caja 12151 left absent from catalogo.cajasExistentes.

	err := svc.AsignarZonaCaja(context.Background(), 12271, 12151, -1, -1, -1)

	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "caja_no_existe", appErr.Code)
	assert.Equal(t, apperror.KindValidation, appErr.Kind)
	assert.Equal(t, 0, repo.upsertZonaCajaCall)
}

func TestAsignarZonaCaja_CajeroNoExisteEnCatalogo_ErrorValidacion_NoUpsert(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.zonasExistentes[12271] = true
	catalogo.cajasExistentes[12151] = true

	err := svc.AsignarZonaCaja(context.Background(), 12271, 12151, 22368, -1, -1)

	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "cajero_no_existe", appErr.Code)
	assert.Equal(t, 0, repo.upsertZonaCajaCall)
}

func TestAsignarZonaCaja_VendedorNoExisteEnCatalogo_ErrorValidacion_NoUpsert(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.zonasExistentes[12271] = true
	catalogo.cajasExistentes[12151] = true
	catalogo.cajerosExistentes[22368] = true

	err := svc.AsignarZonaCaja(context.Background(), 12271, 12151, 22368, 88240, -1)

	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "vendedor_no_existe", appErr.Code)
	assert.Equal(t, 0, repo.upsertZonaCajaCall)
}

func TestAsignarZonaCaja_ZonaExisteError_Propaga_NoUpsert(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.zonaExisteErr = errors.New("firebird down")

	err := svc.AsignarZonaCaja(context.Background(), 12271, -1, -1, -1, -1)

	require.Error(t, err)
	assert.Equal(t, 0, repo.upsertZonaCajaCall)
}

func TestAsignarZonaCaja_CajaExisteError_Propaga_NoUpsert(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.zonasExistentes[12271] = true
	catalogo.cajaExisteErr = errors.New("firebird down")

	err := svc.AsignarZonaCaja(context.Background(), 12271, 12151, -1, -1, -1)

	require.Error(t, err)
	assert.Equal(t, 0, repo.upsertZonaCajaCall)
}

func TestAsignarZonaCaja_CajeroExisteError_Propaga_NoUpsert(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.zonasExistentes[12271] = true
	catalogo.cajasExistentes[12151] = true
	catalogo.cajeroExisteErr = errors.New("firebird down")

	err := svc.AsignarZonaCaja(context.Background(), 12271, 12151, 22368, -1, -1)

	require.Error(t, err)
	assert.Equal(t, 0, repo.upsertZonaCajaCall)
}

func TestAsignarZonaCaja_VendedorExisteError_Propaga_NoUpsert(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.zonasExistentes[12271] = true
	catalogo.cajasExistentes[12151] = true
	catalogo.cajerosExistentes[22368] = true
	catalogo.vendedorExisteErr = errors.New("firebird down")

	err := svc.AsignarZonaCaja(context.Background(), 12271, 12151, 22368, 88240, -1)

	require.Error(t, err)
	assert.Equal(t, 0, repo.upsertZonaCajaCall)
}

func TestAsignarZonaCaja_CobradorExisteError_Propaga_NoUpsert(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.zonasExistentes[12271] = true
	catalogo.cajasExistentes[12151] = true
	catalogo.cajerosExistentes[22368] = true
	catalogo.vendedoresExistentes[88240] = true
	catalogo.cobradorExisteErr = errors.New("firebird down")

	err := svc.AsignarZonaCaja(context.Background(), 12271, 12151, 22368, 88240, 11294)

	require.Error(t, err)
	assert.Equal(t, 0, repo.upsertZonaCajaCall)
}

func TestAsignarZonaCaja_UpsertError_Propaga(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.zonasExistentes[12271] = true
	repo.upsertZonaCajaErr = apperror.NewInternal("firebird_error", "error de base de datos")

	err := svc.AsignarZonaCaja(context.Background(), 12271, -1, -1, -1, -1)

	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "firebird_error", appErr.Code)
}

func TestAsignarZonaCaja_CobradorNoExisteEnCatalogo_ErrorValidacion_NoUpsert(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.zonasExistentes[12271] = true
	catalogo.cajasExistentes[12151] = true
	catalogo.cajerosExistentes[22368] = true
	catalogo.vendedoresExistentes[88240] = true

	err := svc.AsignarZonaCaja(context.Background(), 12271, 12151, 22368, 88240, 11294)

	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "cobrador_no_existe", appErr.Code)
	assert.Equal(t, 0, repo.upsertZonaCajaCall)
}
