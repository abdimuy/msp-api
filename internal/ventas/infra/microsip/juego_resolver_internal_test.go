//nolint:misspell // Spanish vocabulary (articulo, juego, componente, etc.) by convention.
package microsip

import (
	"errors"
	"fmt"
	"testing"

	"github.com/nakagami/firebirdsql"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ─── buildRecipeSet ───────────────────────────────────────────────────────────

func TestBuildRecipeSet_Empty(t *testing.T) {
	t.Parallel()
	s := buildRecipeSet(nil)
	assert.Empty(t, s)
}

func TestBuildRecipeSet_SingleComponent(t *testing.T) {
	t.Parallel()
	c, err := domain.NewRecetaComponente(42, decimal.RequireFromString("3.5"))
	require.NoError(t, err)

	s := buildRecipeSet([]domain.RecetaComponente{c})
	require.Len(t, s, 1)

	key := recipeKey{articuloID: 42, unidades: "3.500000"}
	_, ok := s[key]
	assert.True(t, ok, "expected key {42, 3.500000} to be present")
}

func TestBuildRecipeSet_MultipleComponents(t *testing.T) {
	t.Parallel()
	c1, err := domain.NewRecetaComponente(1, decimal.RequireFromString("1"))
	require.NoError(t, err)
	c2, err := domain.NewRecetaComponente(2, decimal.RequireFromString("2.5"))
	require.NoError(t, err)
	c3, err := domain.NewRecetaComponente(100, decimal.RequireFromString("0.25"))
	require.NoError(t, err)

	s := buildRecipeSet([]domain.RecetaComponente{c1, c2, c3})
	require.Len(t, s, 3)

	assert.Contains(t, s, recipeKey{articuloID: 1, unidades: "1.000000"})
	assert.Contains(t, s, recipeKey{articuloID: 2, unidades: "2.500000"})
	assert.Contains(t, s, recipeKey{articuloID: 100, unidades: "0.250000"})
}

func TestBuildRecipeSet_UsesRecipeKeyScale(t *testing.T) {
	t.Parallel()
	// Verify that the key uses recipeKeyScale=6 decimal places.
	c, err := domain.NewRecetaComponente(7, decimal.RequireFromString("1"))
	require.NoError(t, err)

	s := buildRecipeSet([]domain.RecetaComponente{c})

	// Only the 6-decimal-place key should be present.
	assert.Contains(t, s, recipeKey{articuloID: 7, unidades: "1.000000"})
	assert.NotContains(t, s, recipeKey{articuloID: 7, unidades: "1"})
	assert.NotContains(t, s, recipeKey{articuloID: 7, unidades: "1.00"})
}

// ─── firmaSuffix ──────────────────────────────────────────────────────────────

func TestFirmaSuffix_Length(t *testing.T) {
	t.Parallel()
	// SHA-256 first 4 bytes → 8 hex chars.
	suffix := firmaSuffix("any-recipe-firma-string")
	assert.Len(t, suffix, 8, "firmaSuffix must be exactly 8 hex characters")
}

func TestFirmaSuffix_Deterministic(t *testing.T) {
	t.Parallel()
	firma := "ARTICULO:101:1.000000|ARTICULO:202:2.000000"
	s1 := firmaSuffix(firma)
	s2 := firmaSuffix(firma)
	assert.Equal(t, s1, s2, "firmaSuffix must be deterministic for the same input")
}

func TestFirmaSuffix_DifferentInputsDifferentOutput(t *testing.T) {
	t.Parallel()
	a := firmaSuffix("recipe-A")
	b := firmaSuffix("recipe-B")
	assert.NotEqual(t, a, b, "different inputs should (almost always) produce different suffixes")
}

func TestFirmaSuffix_IsHex(t *testing.T) {
	t.Parallel()
	suffix := firmaSuffix("test-firma")
	for _, ch := range suffix {
		assert.True(t, (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f'),
			"firmaSuffix must be lowercase hex, got char %q", ch)
	}
}

// ─── isNombreDuplicado ────────────────────────────────────────────────────────

func TestIsNombreDuplicado_Nil(t *testing.T) {
	t.Parallel()
	assert.False(t, isNombreDuplicado(nil))
}

func TestIsNombreDuplicado_UnrelatedError(t *testing.T) {
	t.Parallel()
	assert.False(t, isNombreDuplicado(errors.New("some unrelated error")))
}

func TestIsNombreDuplicado_UniqueViolation_DirectApperror(t *testing.T) {
	t.Parallel()
	// Case 1a: apperror.Error with Code "firebird_unique_violation" directly.
	err := apperror.NewConflict("firebird_unique_violation", "registro duplicado")
	assert.True(t, isNombreDuplicado(err))
}

func TestIsNombreDuplicado_UniqueViolation_WrappedByApperror(t *testing.T) {
	t.Parallel()
	// Case 1b: apperror.Error wrapping an underlying cause — simulates MapError output.
	rawFb := &firebirdsql.FbError{GDSCodes: []int{335544349}, Message: "attempt to store duplicate value"}
	err := apperror.NewConflict("firebird_unique_violation", "registro duplicado").
		WithSource("firebird").
		WithError(rawFb)
	assert.True(t, isNombreDuplicado(err))
}

func TestIsNombreDuplicado_TriggerException_FbErrorWrappedInApperror(t *testing.T) {
	t.Parallel()
	// Case 2: Microsip trigger EX_LLAVE_DUPLICADA — the FbError is wrapped inside
	// an apperror.Error (as MapError does via WithError). errors.As must traverse
	// apperror.Error.Unwrap() to reach the *FbError.
	fbErr := &firebirdsql.FbError{
		GDSCodes: []int{335544665},
		Message:  "exception EX_LLAVE_DUPLICADA: clave duplicada en articulos",
	}
	err := apperror.NewInternal("firebird_error", "error de base de datos").
		WithSource("firebird").
		WithError(fbErr)
	assert.True(t, isNombreDuplicado(err))
}

func TestIsNombreDuplicado_TriggerException_FbErrorDirect(t *testing.T) {
	t.Parallel()
	// Case 2 variant: raw *FbError without apperror wrapper.
	fbErr := &firebirdsql.FbError{
		GDSCodes: []int{0},
		Message:  "EX_LLAVE_DUPLICADA raised by trigger ARTICULOS_BEFINSUPD",
	}
	assert.True(t, isNombreDuplicado(fbErr))
}

func TestIsNombreDuplicado_FbError_UnrelatedTrigger(t *testing.T) {
	t.Parallel()
	// A FbError whose message does NOT contain EX_LLAVE_DUPLICADA and is NOT
	// a unique-violation code should return false.
	fbErr := &firebirdsql.FbError{
		GDSCodes: []int{335544466},
		Message:  "violation of FOREIGN KEY constraint on table ARTICULOS",
	}
	err := apperror.NewConflict("firebird_fk_violation", "referencia inválida").
		WithSource("firebird").
		WithError(fbErr)
	assert.False(t, isNombreDuplicado(err))
}

func TestIsNombreDuplicado_OtherApperrorCode(t *testing.T) {
	t.Parallel()
	// An apperror with a different code is not a duplicate.
	err := apperror.NewConflict("firebird_lock_conflict", "operación bloqueada, intente de nuevo")
	assert.False(t, isNombreDuplicado(err))
}

func TestIsNombreDuplicado_WrappedErrorChain(t *testing.T) {
	t.Parallel()
	// errors.As must traverse the full chain. Wrap the apperror in a fmt.Errorf.
	rawErr := apperror.NewConflict("firebird_unique_violation", "registro duplicado")
	wrapped := fmt.Errorf("juego resolver: create: %w", rawErr)
	assert.True(t, isNombreDuplicado(wrapped),
		"isNombreDuplicado should detect violation through errors.As chain")
}

func TestIsNombreDuplicado_FbErrorWrappedInStdError(t *testing.T) {
	t.Parallel()
	// FbError wrapped in a standard fmt.Errorf chain (no apperror in between).
	fbErr := &firebirdsql.FbError{
		GDSCodes: []int{0},
		Message:  "EX_LLAVE_DUPLICADA: nombre ya existe",
	}
	wrapped := fmt.Errorf("microsip insert: %w", fbErr)
	assert.True(t, isNombreDuplicado(wrapped))
}

// ─── errors.As traversal through apperror.Error ───────────────────────────────

// TestApperrorUnwrapTraversal verifies that errors.As can reach a *FbError
// wrapped inside an apperror.Error, confirming the Unwrap() contract used by
// isNombreDuplicado case 2.
func TestApperrorUnwrapTraversal(t *testing.T) {
	t.Parallel()
	fbErr := &firebirdsql.FbError{GDSCodes: []int{1}, Message: "EX_LLAVE_DUPLICADA"}
	appErr := apperror.NewInternal("firebird_error", "error de base de datos").WithError(fbErr)

	var got *firebirdsql.FbError
	require.ErrorAs(t, appErr, &got, "errors.As must reach *FbError through apperror.Error.Unwrap()")
	assert.Equal(t, fbErr.Message, got.Message)
}
