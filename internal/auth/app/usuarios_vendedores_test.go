package app

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// TestEnsureVendedoresByEmail_CreatesNewVendedor verifies that an unknown email
// produces a VENDEDOR_ONLY usuario with the correct fields.
func TestEnsureVendedoresByEmail_CreatesNewVendedor(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	createdBy := uuid.New()

	results, err := h.svc.EnsureVendedoresByEmail(t.Context(), []string{"carlos@muebleriamsp.mx"}, createdBy)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "carlos@muebleriamsp.mx", results[0].Email)
	assert.NotEqual(t, uuid.Nil, results[0].UsuarioID)

	saved, ok := h.usuarios.ByID[results[0].UsuarioID]
	require.True(t, ok, "usuario must be persisted in fake repo")
	assert.Equal(t, domain.EstatusVendedorOnly, saved.Estatus())
	assert.True(t, saved.FirebaseUID().IsZero(), "VENDEDOR_ONLY has no Firebase UID")
	assert.True(t, saved.Activo())
	assert.Equal(t, "carlos", saved.Nombre().Value(), "nombre should be local-part of email")
	assert.Equal(t, createdBy, saved.CreatedBy())
	assert.Equal(t, h.clock.T, saved.CreatedAt())
	assert.Equal(t, h.clock.T, saved.UpdatedAt())
}

// TestEnsureVendedoresByEmail_Idempotent verifies that calling the method twice
// with the same email does not create duplicate rows and returns the same ID.
func TestEnsureVendedoresByEmail_Idempotent(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	createdBy := uuid.New()
	email := "idempotent@muebleriamsp.mx"

	first, err := h.svc.EnsureVendedoresByEmail(t.Context(), []string{email}, createdBy)
	require.NoError(t, err)
	require.Len(t, first, 1)

	second, err := h.svc.EnsureVendedoresByEmail(t.Context(), []string{email}, createdBy)
	require.NoError(t, err)
	require.Len(t, second, 1)

	assert.Equal(t, first[0].UsuarioID, second[0].UsuarioID, "same ID on second call")
	assert.Len(t, h.usuarios.ByID, 1, "only one row should exist in the fake repo")
}

// TestEnsureVendedoresByEmail_ExistingFirebaseUser_OK verifies that a
// pre-existing FIREBASE_USER with a matching email is reused without creating
// a new row.
func TestEnsureVendedoresByEmail_ExistingFirebaseUser_OK(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)

	// Seed a FIREBASE_USER the same way SyncFromFirebase would leave one.
	existing := h.seedUsuario(t)
	emailStr := existing.Email().Value()
	initialCount := len(h.usuarios.ByID)

	results, err := h.svc.EnsureVendedoresByEmail(t.Context(), []string{emailStr}, uuid.New())
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, emailStr, results[0].Email)
	assert.Equal(t, existing.ID(), results[0].UsuarioID, "must return pre-existing user's ID")
	assert.Len(t, h.usuarios.ByID, initialCount, "no new row should be created")
}

// TestEnsureVendedoresByEmail_InactiveVendedor_Returns409 verifies that a
// deactivated usuario causes ErrVendedorEmailInactivo and blocks the call.
func TestEnsureVendedoresByEmail_InactiveVendedor_Returns409(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)

	// Build a VENDEDOR_ONLY, persist it, then deactivate it.
	id := uuid.New()
	email, err := domain.NewEmail("inactivo@muebleriamsp.mx")
	require.NoError(t, err)
	nombre, err := domain.NewNombre("inactivo")
	require.NoError(t, err)
	u := domain.NewVendedorUsuario(id, email, nombre, uuid.New(), h.clock.T)
	require.NoError(t, h.usuarios.Save(t.Context(), u))

	u.Desactivar(uuid.New(), h.clock.T)
	require.NoError(t, h.usuarios.Update(t.Context(), u))

	_, callErr := h.svc.EnsureVendedoresByEmail(t.Context(), []string{"inactivo@muebleriamsp.mx"}, uuid.New())
	require.ErrorIs(t, callErr, domain.ErrVendedorEmailInactivo)
}

// TestEnsureVendedoresByEmail_InvalidEmail_Returns422 verifies that a
// syntactically bad email causes a validation error and nothing is persisted.
func TestEnsureVendedoresByEmail_InvalidEmail_Returns422(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)

	_, err := h.svc.EnsureVendedoresByEmail(t.Context(), []string{"not-an-email"}, uuid.New())
	require.Error(t, err)

	appErr, ok := apperror.As(err)
	require.True(t, ok, "error must be an apperror")
	assert.Equal(t, apperror.KindValidation, appErr.Kind)
	assert.Empty(t, h.usuarios.ByID, "no rows should be saved on validation failure")
}

// TestEnsureVendedoresByEmail_MultipleEmails_MixedResults verifies that a
// slice containing one known and one unknown email is processed correctly: the
// known user's ID is preserved and a new VENDEDOR_ONLY row is created for the
// unknown one, in order.
func TestEnsureVendedoresByEmail_MultipleEmails_MixedResults(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	createdBy := uuid.New()

	// Pre-seed email A as a FIREBASE_USER.
	existing := h.seedUsuario(t)
	emailA := existing.Email().Value()
	emailB := "nuevo@muebleriamsp.mx"

	results, err := h.svc.EnsureVendedoresByEmail(t.Context(), []string{emailA, emailB}, createdBy)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// First result: the pre-existing user.
	assert.Equal(t, emailA, results[0].Email)
	assert.Equal(t, existing.ID(), results[0].UsuarioID, "must reuse pre-existing ID for emailA")

	// Second result: a freshly created VENDEDOR_ONLY.
	assert.Equal(t, emailB, results[1].Email)
	assert.NotEqual(t, uuid.Nil, results[1].UsuarioID)
	assert.NotEqual(t, existing.ID(), results[1].UsuarioID, "emailB must have a different ID")

	freshB, ok := h.usuarios.ByID[results[1].UsuarioID]
	require.True(t, ok, "emailB usuario must be in the fake repo")
	assert.Equal(t, domain.EstatusVendedorOnly, freshB.Estatus())
}
