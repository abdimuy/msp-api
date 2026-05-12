package ventfb

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// TestIsUniqueViolation_True checks that an apperror.Error with the
// firebird_unique_violation code is recognized as a unique-violation.
func TestIsUniqueViolation_True(t *testing.T) {
	t.Parallel()
	err := apperror.NewConflict("firebird_unique_violation", "registro duplicado")
	assert.True(t, isUniqueViolation(err))
}

// TestIsUniqueViolation_False covers the two negative branches: a plain
// non-apperror error and an apperror with a different code.
func TestIsUniqueViolation_False(t *testing.T) {
	t.Parallel()
	assert.False(t, isUniqueViolation(errors.New("plain error")))
	other := apperror.NewConflict("firebird_fk_violation", "referencia inválida")
	assert.False(t, isUniqueViolation(other))
}
