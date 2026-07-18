package app

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
)

// hydrateResumenUsuario builds a Usuario directly via HydrateUsuario so tests
// can pin down ID/Nombre/Email/Estatus without going through the validating
// constructors (irrelevant here — we only exercise the mapping).
func hydrateResumenUsuario(t *testing.T, nombre, email string, estatus domain.Estatus) *domain.Usuario {
	t.Helper()
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	id := uuid.New()
	nombreVO, err := domain.NewNombre(nombre)
	require.NoError(t, err)
	emailVO, err := domain.NewEmail(email)
	require.NoError(t, err)
	return domain.HydrateUsuario(domain.HydrateUsuarioParams{
		ID:        id,
		Nombre:    nombreVO,
		Email:     emailVO,
		Activo:    true,
		Estatus:   estatus,
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: uuid.New(),
		UpdatedBy: uuid.New(),
	})
}

// TestListarUsuarios covers pagination accumulation, mapping, empty results,
// and repo error propagation.
func TestListarUsuarios(t *testing.T) {
	t.Parallel()

	t.Run("multi_page_accumulation_and_mapping", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)

		u1 := hydrateResumenUsuario(t, "Guadalupe Hernandez Morales", "guadalupe.hernandez@muebleriamsp.mx", domain.EstatusFirebaseUser)
		u2 := hydrateResumenUsuario(t, "Jose Luis Ramirez Torres", "jose.ramirez@muebleriamsp.mx", domain.EstatusFirebaseUser)
		u3 := hydrateResumenUsuario(t, "Araceli Gonzalez Padilla", "araceli.gonzalez@muebleriamsp.mx", domain.EstatusVendedorOnly)

		h.usuarios.ListPages = []outbound.Page[*domain.Usuario]{
			{Items: []*domain.Usuario{u1, u2}, NextCursor: "cursor-1"},
			{Items: []*domain.Usuario{u3}, NextCursor: ""},
		}

		got, err := h.svc.ListarUsuarios(t.Context())
		require.NoError(t, err)
		require.Len(t, got, 3)

		assert.Equal(t, auth.UsuarioResumen{
			ID:      u1.ID(),
			Nombre:  "Guadalupe Hernandez Morales",
			Email:   "guadalupe.hernandez@muebleriamsp.mx",
			Estatus: "FIREBASE_USER",
		}, got[0])
		assert.Equal(t, auth.UsuarioResumen{
			ID:      u2.ID(),
			Nombre:  "Jose Luis Ramirez Torres",
			Email:   "jose.ramirez@muebleriamsp.mx",
			Estatus: "FIREBASE_USER",
		}, got[1])
		assert.Equal(t, auth.UsuarioResumen{
			ID:      u3.ID(),
			Nombre:  "Araceli Gonzalez Padilla",
			Email:   "araceli.gonzalez@muebleriamsp.mx",
			Estatus: "VENDEDOR_ONLY",
		}, got[2])
	})

	t.Run("single_page_cursor_empty_on_first_call", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := hydrateResumenUsuario(t, "Fernando Aguilar Vazquez", "fernando.aguilar@muebleriamsp.mx", domain.EstatusFirebaseUser)
		h.usuarios.ListPages = []outbound.Page[*domain.Usuario]{
			{Items: []*domain.Usuario{u}, NextCursor: ""},
		}

		got, err := h.svc.ListarUsuarios(t.Context())
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, u.ID(), got[0].ID)
	})

	t.Run("empty_result_returns_empty_slice_no_error", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.usuarios.ListPages = []outbound.Page[*domain.Usuario]{
			{Items: nil, NextCursor: ""},
		}

		got, err := h.svc.ListarUsuarios(t.Context())
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("repo_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		boom := errors.New("boom")
		h.usuarios.ListErr = boom

		got, err := h.svc.ListarUsuarios(t.Context())
		require.ErrorIs(t, err, boom)
		assert.Nil(t, got)
	})

	t.Run("stalled_cursor_does_not_loop_forever", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := hydrateResumenUsuario(t, "Rocio Delgado Sanchez", "rocio.delgado@muebleriamsp.mx", domain.EstatusFirebaseUser)
		// A misbehaving repo that keeps returning the same NextCursor forever
		// (never advancing) must not hang ListarUsuarios. The fake cycles this
		// single scripted page indefinitely, so the ONLY thing that can stop
		// the loop is ListarUsuarios' own guard comparing NextCursor against
		// the cursor it just requested with.
		h.usuarios.ListPages = []outbound.Page[*domain.Usuario]{
			{Items: []*domain.Usuario{u}, NextCursor: "stuck"},
		}

		got, err := h.svc.ListarUsuarios(t.Context())
		require.NoError(t, err)
		// Exactly two calls happen before the guard trips: the first request
		// (cursor "") advances to "stuck", the second request (cursor
		// "stuck") receives "stuck" again and the loop stops.
		assert.Len(t, got, 2)
		assert.Equal(t, 2, h.usuarios.listCall)
	})
}
