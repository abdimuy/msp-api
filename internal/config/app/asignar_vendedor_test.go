package app_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/config/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

func TestAsignarVendedor_HappyPath_LlamaUpsert(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	usuarioID := uuid.New()
	catalogo.perteneceByPair[[2]int{101, 19985}] = true
	catalogo.perteneceByPair[[2]int{201, 19986}] = true

	err := svc.AsignarVendedor(context.Background(), usuarioID, intPtr(101), intPtr(201), nil)

	require.NoError(t, err)
	assert.Equal(t, 1, repo.upsertCall)
	stored, ok := repo.mappings[usuarioID]
	require.True(t, ok)
	require.NotNil(t, stored.ListaID1)
	assert.Equal(t, 101, *stored.ListaID1)
	require.NotNil(t, stored.ListaID2)
	assert.Equal(t, 201, *stored.ListaID2)
	assert.Nil(t, stored.ListaID3)
}

func TestAsignarVendedor_InvalidoNegativo_NoLlamaUpsert(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := newTestService()
	neg := -1

	err := svc.AsignarVendedor(context.Background(), uuid.New(), &neg, nil, nil)

	require.ErrorIs(t, err, domain.ErrVendedorListaIDInvalido)
	assert.Equal(t, 0, repo.upsertCall)
}

func TestAsignarVendedor_ListaIDNoPerteneceAlAtributo_ErrorValidacion_NoUpsert(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	usuarioID := uuid.New()
	// 201 belongs to attribute 19986 (slot 2), not 19985 (slot 1) — nothing marks it true for slot 1.
	catalogo.perteneceByPair[[2]int{201, 19986}] = true

	err := svc.AsignarVendedor(context.Background(), usuarioID, intPtr(201), nil, nil)

	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "vendedor_lista_id_no_pertenece", appErr.Code)
	assert.Equal(t, 0, repo.upsertCall)
}

func TestAsignarVendedor_FKViolation_MapeaANotFound(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	usuarioID := uuid.New()
	catalogo.perteneceByPair[[2]int{101, 19985}] = true
	repo.upsertErr = apperror.NewConflict("firebird_fk_violation", "referencia inválida")

	err := svc.AsignarVendedor(context.Background(), usuarioID, intPtr(101), nil, nil)

	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "usuario_no_existe", appErr.Code)
	assert.Equal(t, apperror.KindNotFound, appErr.Kind)
}

func TestAsignarVendedor_OtroErrorDeRepo_SePropagaSinTraducir(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.perteneceByPair[[2]int{101, 19985}] = true
	repo.upsertErr = apperror.NewInternal("firebird_error", "error de base de datos")

	err := svc.AsignarVendedor(context.Background(), uuid.New(), intPtr(101), nil, nil)

	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "firebird_error", appErr.Code)
	assert.NotEqual(t, "usuario_no_existe", appErr.Code)
}

func TestEliminarVendedor_DelegaAlRepo(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := newTestService()
	usuarioID := uuid.New()
	repo.mappings[usuarioID] = domain.VendedorMapping{UsuarioID: usuarioID}

	err := svc.EliminarVendedor(context.Background(), usuarioID)

	require.NoError(t, err)
	_, ok := repo.mappings[usuarioID]
	assert.False(t, ok)
}
