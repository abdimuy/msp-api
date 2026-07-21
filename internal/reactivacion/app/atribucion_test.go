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

var (
	cohorteFecha = time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	afterCohorte = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	beforeCohort = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
)

func TestAtribucion_CountsAndRates(t *testing.T) {
	t.Parallel()
	rows := []*domain.CohorteCliente{
		// Treatment (contacted), converted.
		cohorteRow(1, false, true, cohorteFecha, afterCohorte),
		// Treatment (contacted), not converted (bought before cohort).
		cohorteRow(2, false, true, cohorteFecha, beforeCohort),
		// Control, converted.
		cohorteRow(3, true, false, cohorteFecha, afterCohorte),
		// Control, not converted (no purchase recorded).
		cohorteRow(4, true, false, cohorteFecha, time.Time{}),
		// Treatment but NOT contacted → excluded from both groups.
		cohorteRow(5, false, false, cohorteFecha, afterCohorte),
	}
	repo := &fakeCohorteRepo{listResult: rows}
	svc := newTestService(&fakeUniversoReader{}, repo, app.Config{})

	res, err := svc.Atribucion(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 2, res.TreatmentTotal)
	assert.Equal(t, 1, res.TreatmentConvertidos)
	assert.Equal(t, 2, res.ControlTotal)
	assert.Equal(t, 1, res.ControlConvertidos)
	assert.Equal(t, "0.5", res.TasaTreatment.String())
	assert.Equal(t, "0.5", res.TasaControl.String())
	assert.True(t, res.Uplift.IsZero())

	// The attribution query must request BOTH groups.
	assert.False(t, repo.lastListParm.SoloTratamiento)
	assert.Empty(t, repo.lastListParm.Segmento)
}

func TestAtribucion_EmptyCohorteNoDivideByZero(t *testing.T) {
	t.Parallel()
	repo := &fakeCohorteRepo{listResult: nil}
	svc := newTestService(&fakeUniversoReader{}, repo, app.Config{})

	res, err := svc.Atribucion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, res.TreatmentTotal)
	assert.Equal(t, 0, res.ControlTotal)
	assert.True(t, res.TasaTreatment.IsZero())
	assert.True(t, res.TasaControl.IsZero())
	assert.True(t, res.Uplift.IsZero())
}

func TestAtribucion_PositiveUplift(t *testing.T) {
	t.Parallel()
	rows := []*domain.CohorteCliente{
		cohorteRow(1, false, true, cohorteFecha, afterCohorte), // treatment converts
		cohorteRow(2, false, true, cohorteFecha, afterCohorte), // treatment converts
		cohorteRow(3, true, false, cohorteFecha, beforeCohort), // control no convert
		cohorteRow(4, true, false, cohorteFecha, beforeCohort), // control no convert
	}
	svc := newTestService(&fakeUniversoReader{}, &fakeCohorteRepo{listResult: rows}, app.Config{})

	res, err := svc.Atribucion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "1", res.TasaTreatment.String())
	assert.True(t, res.TasaControl.IsZero())
	assert.Equal(t, "1", res.Uplift.String())
}

func TestAtribucion_RepoError(t *testing.T) {
	t.Parallel()
	repo := &fakeCohorteRepo{listErr: errors.New("boom")}
	svc := newTestService(&fakeUniversoReader{}, repo, app.Config{})
	_, err := svc.Atribucion(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "atribución")
}
