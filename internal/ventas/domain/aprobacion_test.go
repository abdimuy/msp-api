package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestAprobacion_HappyPath(t *testing.T) {
	t.Parallel()
	now := time.Now()
	by := uuid.New()
	a, err := domain.NewAprobacion(now, by)
	require.NoError(t, err)
	assert.Equal(t, now, a.At())
	assert.Equal(t, by, a.By())
}

func TestAprobacion_RejectsZeroTime(t *testing.T) {
	t.Parallel()
	_, err := domain.NewAprobacion(time.Time{}, uuid.New())
	require.Error(t, err)
}

func TestAprobacion_RejectsNilBy(t *testing.T) {
	t.Parallel()
	_, err := domain.NewAprobacion(time.Now(), uuid.Nil)
	require.Error(t, err)
}

func TestHydrateAprobacion_Bypass(t *testing.T) {
	t.Parallel()
	now := time.Now()
	by := uuid.New()
	a := domain.HydrateAprobacion(now, by)
	assert.Equal(t, now, a.At())
	assert.Equal(t, by, a.By())
}
