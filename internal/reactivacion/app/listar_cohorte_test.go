//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/app"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

func TestListarCohorte_PassesFilters(t *testing.T) {
	t.Parallel()
	rows := []*domain.CohorteCliente{
		cohorteRow(1, false, false, buildNow, time.Time{}),
	}
	repo := &fakeCohorteRepo{listResult: rows}
	svc := newTestService(&fakeUniversoReader{}, repo, app.Config{})

	got, err := svc.ListarCohorte(context.Background(), app.ListarCohorteParams{
		Segmento:        "por_liquidar_hueco",
		SoloTratamiento: true,
	})
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, domain.SegmentoPorLiquidarHueco, repo.lastListParm.Segmento)
	assert.True(t, repo.lastListParm.SoloTratamiento)
}

func TestListarCohorte_NoSegmentoFilter(t *testing.T) {
	t.Parallel()
	repo := &fakeCohorteRepo{listResult: nil}
	svc := newTestService(&fakeUniversoReader{}, repo, app.Config{})

	_, err := svc.ListarCohorte(context.Background(), app.ListarCohorteParams{})
	require.NoError(t, err)
	assert.Empty(t, repo.lastListParm.Segmento)
	assert.False(t, repo.lastListParm.SoloTratamiento)
}

func TestListarCohorte_InvalidSegmento(t *testing.T) {
	t.Parallel()
	svc := newTestService(&fakeUniversoReader{}, &fakeCohorteRepo{}, app.Config{})
	_, err := svc.ListarCohorte(context.Background(), app.ListarCohorteParams{Segmento: "no_existe"})
	require.ErrorIs(t, err, domain.ErrSegmentoInvalido)
}

func TestListarCohorte_RepoError(t *testing.T) {
	t.Parallel()
	repo := &fakeCohorteRepo{listErr: errors.New("boom")}
	svc := newTestService(&fakeUniversoReader{}, repo, app.Config{})
	_, err := svc.ListarCohorte(context.Background(), app.ListarCohorteParams{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listar")
}
