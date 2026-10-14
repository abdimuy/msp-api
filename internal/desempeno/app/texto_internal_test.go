package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizar(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "juana andrade lechuga", normalizar("  JUANA ANDRADE LECHUGA "))
	assert.Equal(t, "mendez nino", normalizar("MÉNDEZ NIÑO"))
	assert.Equal(t, normalizar("Pérez"), normalizar("perez"))
	assert.Empty(t, normalizar(""))
}
