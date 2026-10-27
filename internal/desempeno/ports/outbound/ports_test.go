package outbound_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
)

func TestProductionClock_DevuelveUTC(t *testing.T) {
	t.Parallel()
	antes := time.Now().UTC()
	got := outbound.ProductionClock{}.Now()
	assert.Equal(t, time.UTC, got.Location())
	assert.WithinDuration(t, antes, got, time.Second)
}
