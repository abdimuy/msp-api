package app

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	platform "github.com/abdimuy/msp-api/internal/platform/domain"
)

// TestActualizar covers the happy path, validation failures, and missing
// usuario branch of Service.Actualizar.
func TestActualizar(t *testing.T) {
	t.Parallel()

	t.Run("happy_path_updates_fields_and_emits_event", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		by := uuid.New()

		updated, err := h.svc.Actualizar(t.Context(), ActualizarParams{
			ID:     u.ID(),
			Email:  "nuevo@example.com",
			Nombre: "Pedro Lopez",
		}, by)
		require.NoError(t, err)
		assert.Equal(t, "nuevo@example.com", updated.Email().Value())
		assert.Equal(t, "Pedro Lopez", updated.Nombre().Value())
		assert.Equal(t, by, updated.UpdatedBy())
		assert.Equal(t, []string{eventUserUpdated}, h.outbox.EventTypes())
	})

	t.Run("with_optional_telefono", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		tel := "+524491234567"
		updated, err := h.svc.Actualizar(t.Context(), ActualizarParams{
			ID:       u.ID(),
			Email:    u.Email().Value(),
			Nombre:   "Maria",
			Telefono: &tel,
		}, uuid.New())
		require.NoError(t, err)
		require.NotNil(t, updated.Telefono())
		assert.Equal(t, "4491234567", updated.Telefono().Value())
	})

	t.Run("blank_telefono_clears_field", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		empty := "  "
		updated, err := h.svc.Actualizar(t.Context(), ActualizarParams{
			ID:       u.ID(),
			Email:    u.Email().Value(),
			Nombre:   "Maria",
			Telefono: &empty,
		}, uuid.New())
		require.NoError(t, err)
		assert.Nil(t, updated.Telefono())
	})

	t.Run("invalid_telefono_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		bad := "abc"
		_, err := h.svc.Actualizar(t.Context(), ActualizarParams{
			ID:       u.ID(),
			Email:    u.Email().Value(),
			Nombre:   "Maria",
			Telefono: &bad,
		}, uuid.New())
		require.Error(t, err)
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("usuario_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		_, err := h.svc.Actualizar(t.Context(), ActualizarParams{
			ID:     uuid.New(),
			Email:  "x@example.com",
			Nombre: "X",
		}, uuid.New())
		require.ErrorIs(t, err, domain.ErrUsuarioNotFound)
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("invalid_email_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		_, err := h.svc.Actualizar(t.Context(), ActualizarParams{
			ID:     u.ID(),
			Email:  "not-an-email",
			Nombre: "Pedro",
		}, uuid.New())
		require.ErrorIs(t, err, domain.ErrEmailInvalido)
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("invalid_nombre_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		_, err := h.svc.Actualizar(t.Context(), ActualizarParams{
			ID:     u.ID(),
			Email:  u.Email().Value(),
			Nombre: "   ",
		}, uuid.New())
		require.ErrorIs(t, err, domain.ErrNombreRequerido)
	})

	t.Run("repo_update_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		boom := errors.New("boom")
		h.usuarios.UpdateErr = boom
		_, err := h.svc.Actualizar(t.Context(), ActualizarParams{
			ID:     u.ID(),
			Email:  u.Email().Value(),
			Nombre: "Pedro Lopez",
		}, uuid.New())
		require.ErrorIs(t, err, boom)
		assert.Empty(t, h.outbox.Calls)
	})
}

// TestDesactivar covers the happy soft-delete path plus the not-found branch.
func TestDesactivar(t *testing.T) {
	t.Parallel()

	t.Run("happy_path_mangles_email_and_emits_event", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		oldEmail := u.Email().Value()
		originalFUID := u.FirebaseUID().Value()
		by := uuid.New()

		require.NoError(t, h.svc.Desactivar(t.Context(), u.ID(), by))
		assert.False(t, u.Activo())
		assert.Contains(t, u.Email().Value(), "deleted-")
		assert.Contains(t, u.Email().Value(), oldEmail)
		assert.Contains(t, u.FirebaseUID().Value(), "deleted-")
		assert.Equal(t, []string{eventUserDeactivated}, h.outbox.EventTypes())

		// The event payload must carry the ORIGINAL firebase_uid (captured
		// before the rename) so the downstream handler can find the
		// account on Firebase.
		require.Len(t, h.outbox.Calls, 1)
		payload, ok := h.outbox.Calls[0].Payload.(map[string]any)
		require.True(t, ok, "payload must be a map")
		assert.Equal(t, originalFUID, payload["firebase_uid"])
		assert.NotContains(t, payload["firebase_uid"], "deleted-",
			"event must carry the pre-rename uid, not the placeholder")
		assert.Equal(t, u.ID(), payload["usuario_id"])
		assert.Equal(t, by, payload["deactivated_by"])
	})

	t.Run("usuario_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		err := h.svc.Desactivar(t.Context(), uuid.New(), uuid.New())
		require.ErrorIs(t, err, domain.ErrUsuarioNotFound)
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("repo_update_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		h.usuarios.UpdateErr = errors.New("boom")
		require.Error(t, h.svc.Desactivar(t.Context(), u.ID(), uuid.New()))
		assert.Empty(t, h.outbox.Calls)
	})
}

// TestObtenerListar exercises the trivial read-through methods.
func TestObtenerListar(t *testing.T) {
	t.Parallel()

	t.Run("obtener_returns_usuario", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		got, err := h.svc.Obtener(t.Context(), u.ID())
		require.NoError(t, err)
		assert.Equal(t, u.ID(), got.ID())
	})

	t.Run("obtener_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		_, err := h.svc.Obtener(t.Context(), uuid.New())
		require.ErrorIs(t, err, domain.ErrUsuarioNotFound)
	})

	t.Run("listar_returns_page", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.seedUsuario(t)
		h.seedUsuario(t)
		page, err := h.svc.Listar(t.Context(), outbound.ListParams{PageSize: 10})
		require.NoError(t, err)
		assert.Len(t, page.Items, 2)
	})
}

// TestAsignarRevocarRol covers role assignment lifecycle.
func TestAsignarRevocarRol(t *testing.T) {
	t.Parallel()

	t.Run("asignar_happy_path", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		r := h.seedRol(t, "vendedor")
		by := uuid.New()

		require.NoError(t, h.svc.AsignarRolAUsuario(t.Context(), u.ID(), r.ID(), by))
		_, ok := h.usuarios.RoleLinks[u.ID()][r.ID()]
		assert.True(t, ok)
		assert.Equal(t, []string{eventRoleAssigned}, h.outbox.EventTypes())
	})

	t.Run("asignar_usuario_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		r := h.seedRol(t, "vendedor")
		err := h.svc.AsignarRolAUsuario(t.Context(), uuid.New(), r.ID(), uuid.New())
		require.ErrorIs(t, err, domain.ErrUsuarioNotFound)
	})

	t.Run("asignar_rol_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		err := h.svc.AsignarRolAUsuario(t.Context(), u.ID(), uuid.New(), uuid.New())
		require.ErrorIs(t, err, domain.ErrRolNotFound)
	})

	t.Run("revocar_happy_path", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		r := h.seedRol(t, "vendedor")
		require.NoError(t, h.svc.AsignarRolAUsuario(t.Context(), u.ID(), r.ID(), uuid.New()))
		h.outbox.Calls = nil

		require.NoError(t, h.svc.RevocarRolDeUsuario(t.Context(), u.ID(), r.ID()))
		_, ok := h.usuarios.RoleLinks[u.ID()][r.ID()]
		assert.False(t, ok)
		assert.Equal(t, []string{eventRoleRevoked}, h.outbox.EventTypes())
	})

	t.Run("revocar_usuario_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		err := h.svc.RevocarRolDeUsuario(t.Context(), uuid.New(), uuid.New())
		require.ErrorIs(t, err, domain.ErrUsuarioNotFound)
	})
}

// TestCrear covers Service.Crear: the office alta path that binds the Firebase
// uid from birth so the person's first login resolves to this row instead of
// minting a duplicate.
func TestCrear(t *testing.T) {
	t.Parallel()

	const (
		crearFUID   = "fuid-alta-oficina"
		crearEmail  = "gabriel.roque@muebleriamsp.mx"
		crearNombre = "Gabriel Roque"
	)

	t.Run("happy_path_with_telefono", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		by := uuid.New()
		tel := "+52 449 123 4567"

		u, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: crearFUID,
			Email:       crearEmail,
			Nombre:      crearNombre,
			Telefono:    &tel,
		}, by)
		require.NoError(t, err)

		assert.Equal(t, crearFUID, u.FirebaseUID().Value())
		assert.Equal(t, crearEmail, u.Email().Value())
		assert.Equal(t, crearNombre, u.Nombre().Value())
		assert.Equal(t, domain.EstatusFirebaseUser, u.Estatus())
		assert.True(t, u.Activo())
		// The actor is the creator, not the new row itself — CREATED_BY has an
		// FK to MSP_USUARIOS.ID that the actor satisfies.
		assert.Equal(t, by, u.CreatedBy())
		assert.NotEqual(t, u.ID(), u.CreatedBy())
		assert.Equal(t, h.clock.T, u.CreatedAt())
		require.NotNil(t, u.Telefono())
		assert.Equal(t, "4491234567", u.Telefono().Value(), "telefono must be normalized to 10 MX digits")
		assert.Nil(t, u.AlmacenID())

		// The row actually landed, and is reachable by both unique keys.
		stored, findErr := h.usuarios.FindByFirebaseUID(t.Context(), crearFUID)
		require.NoError(t, findErr)
		assert.Equal(t, u.ID(), stored.ID())
		stored, findErr = h.usuarios.FindByEmail(t.Context(), crearEmail)
		require.NoError(t, findErr)
		assert.Equal(t, u.ID(), stored.ID())
	})

	t.Run("happy_path_without_telefono", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		by := uuid.New()

		u, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: crearFUID,
			Email:       crearEmail,
			Nombre:      crearNombre,
		}, by)
		require.NoError(t, err)
		assert.Nil(t, u.Telefono())
		assert.Equal(t, domain.EstatusFirebaseUser, u.Estatus())
		assert.True(t, u.Activo())
		assert.Equal(t, by, u.CreatedBy())
	})

	t.Run("blank_telefono_stored_as_nil", func(t *testing.T) {
		t.Parallel()
		for _, blank := range []string{"", "   ", "\t\n"} {
			h := newHarness(t, false)
			b := blank
			u, err := h.svc.Crear(t.Context(), CrearParams{
				FirebaseUID: crearFUID,
				Email:       crearEmail,
				Nombre:      crearNombre,
				Telefono:    &b,
			}, uuid.New())
			require.NoError(t, err, "blank telefono %q must clear the field, not fail", blank)
			assert.Nil(t, u.Telefono())
		}
	})

	t.Run("email_duplicado_returns_conflict", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		existing := h.seedUsuario(t)

		_, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: "fuid-distinto-" + uuid.NewString(),
			Email:       existing.Email().Value(),
			Nombre:      crearNombre,
		}, uuid.New())
		require.ErrorIs(t, err, domain.ErrUsuarioYaExiste)
		assert.Empty(t, h.outbox.Calls, "a rejected alta must not emit an event")
	})

	t.Run("firebase_uid_duplicado_returns_conflict", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		existing := h.seedUsuario(t)

		_, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: existing.FirebaseUID().Value(),
			Email:       "otro-" + uuid.NewString() + "@example.com",
			Nombre:      crearNombre,
		}, uuid.New())
		require.ErrorIs(t, err, domain.ErrUsuarioYaExiste)
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("validation_errors_save_nothing", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name    string
			params  CrearParams
			wantErr error
		}{
			{
				name:    "email_invalido",
				params:  CrearParams{FirebaseUID: crearFUID, Email: "no-es-correo", Nombre: crearNombre},
				wantErr: domain.ErrEmailInvalido,
			},
			{
				name:    "nombre_vacio",
				params:  CrearParams{FirebaseUID: crearFUID, Email: crearEmail, Nombre: "   "},
				wantErr: domain.ErrNombreRequerido,
			},
			{
				name:    "firebase_uid_vacio",
				params:  CrearParams{FirebaseUID: "", Email: crearEmail, Nombre: crearNombre},
				wantErr: domain.ErrFirebaseUIDRequerido,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				h := newHarness(t, false)
				_, err := h.svc.Crear(t.Context(), tc.params, uuid.New())
				require.ErrorIs(t, err, tc.wantErr)
				assert.Empty(t, h.usuarios.ByID, "nothing may be persisted on a validation failure")
				assert.Empty(t, h.usuarios.ByEmail)
				assert.Empty(t, h.usuarios.ByFUID)
				assert.Empty(t, h.outbox.Calls)
			})
		}

		t.Run("telefono_invalido", func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, false)
			bad := "123"
			_, err := h.svc.Crear(t.Context(), CrearParams{
				FirebaseUID: crearFUID,
				Email:       crearEmail,
				Nombre:      crearNombre,
				Telefono:    &bad,
			}, uuid.New())
			require.Error(t, err)
			assert.Empty(t, h.usuarios.ByID)
			assert.Empty(t, h.outbox.Calls)
		})
	})

	t.Run("emits_exactly_one_user_created_event", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		by := uuid.New()

		u, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: crearFUID,
			Email:       crearEmail,
			Nombre:      crearNombre,
		}, by)
		require.NoError(t, err)

		require.Len(t, h.outbox.Calls, 1)
		assert.Equal(t, []string{eventUserCreated}, h.outbox.EventTypes())
		call := h.outbox.Calls[0]
		assert.Equal(t, outboxAggregateUsuario, call.Aggregate)
		assert.Equal(t, u.ID(), call.AggregateID)

		payload, ok := call.Payload.(map[string]any)
		require.True(t, ok, "payload must be a map, got %T", call.Payload)
		assert.Equal(t, map[string]any{
			"usuario_id":   u.ID(),
			"email":        crearEmail,
			"firebase_uid": crearFUID,
			"created_by":   by,
			"promoted":     false,
		}, payload)
	})

	t.Run("outbox_failure_does_not_fail_the_alta", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.outbox.Err = errors.New("outbox down")

		u, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: crearFUID,
			Email:       crearEmail,
			Nombre:      crearNombre,
		}, uuid.New())
		require.NoError(t, err, "the outbox is best-effort: its failure must not roll back the alta")
		require.NotNil(t, u)

		stored, findErr := h.usuarios.FindByID(t.Context(), u.ID())
		require.NoError(t, findErr)
		assert.Equal(t, u.ID(), stored.ID())
		assert.Len(t, h.outbox.Calls, 1, "the enqueue was attempted")
	})

	t.Run("repo_save_error_propagates_and_emits_nothing", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		boom := errors.New("boom")
		h.usuarios.SaveErr = boom

		_, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: crearFUID,
			Email:       crearEmail,
			Nombre:      crearNombre,
		}, uuid.New())
		require.ErrorIs(t, err, boom)
		assert.Empty(t, h.outbox.Calls)
	})
}

// seedVendedorOnly persists an active VENDEDOR_ONLY usuario for `email` whose
// nombre is the email-derived placeholder EnsureVendedoresByEmail would have
// produced (the local-part, lowercase). It returns the stored entity.
func (h *testHarness) seedVendedorOnly(t *testing.T, email string) *domain.Usuario {
	t.Helper()
	em, err := domain.NewEmail(email)
	require.NoError(t, err)
	nombre, err := domain.NewNombre(deriveNombreFromToken("", em.Value()))
	require.NoError(t, err)
	u := domain.NewVendedorUsuario(uuid.New(), em, nombre, uuid.New(), h.clock.T)
	require.NoError(t, h.usuarios.Save(t.Context(), u))
	return u
}

// TestCrear_PromocionDeVendedorOnly covers the second way a MSP_USUARIOS row
// is born: EnsureVendedoresByEmail minted it from the phone when a cobrador
// named the person as vendedor of a venta — no firebase_uid, and a nombre
// derived from the email's local-part. The office alta must promote that row
// in place instead of colliding with UQ_MSP_USUARIOS_EMAIL.
func TestCrear_PromocionDeVendedorOnly(t *testing.T) {
	t.Parallel()

	const (
		promoEmail  = "humberto.quintana@muebleriamsp.mx"
		promoFUID   = "fuid-humberto-promovido"
		promoNombre = "Humberto Quintana Ríos"
	)

	t.Run("happy_path_keeps_the_id_and_takes_the_real_nombre", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		vendedor := h.seedVendedorOnly(t, promoEmail)
		// Snapshot the values BEFORE the call. The fake repo hands the service
		// the very pointer it stores, so `vendedor` and the returned entity are
		// the same object: comparing against vendedor.X() after the fact would
		// be comparing the result with itself.
		previousID := vendedor.ID()
		previousCreatedBy := vendedor.CreatedBy()
		previousCreatedAt := vendedor.CreatedAt()
		by := uuid.New()

		// Precondition: this is exactly the ugly row the phone leaves behind.
		require.Equal(t, "humberto.quintana", vendedor.Nombre().Value())
		require.Equal(t, domain.EstatusVendedorOnly, vendedor.Estatus())
		require.True(t, vendedor.FirebaseUID().IsZero())

		u, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: promoFUID,
			Email:       promoEmail,
			Nombre:      promoNombre,
		}, by)
		require.NoError(t, err)

		// THE invariant: the row is promoted, never replaced. Every venta
		// already attributed to this usuario_id must keep pointing at it.
		assert.Equal(t, previousID, u.ID(), "promotion must reuse the SAME row id")
		assert.Equal(t, domain.EstatusFirebaseUser, u.Estatus())
		assert.Equal(t, promoFUID, u.FirebaseUID().Value())
		assert.Equal(t, promoNombre, u.Nombre().Value(), "the alta's nombre must replace the email-derived one")
		assert.Equal(t, promoEmail, u.Email().Value())
		assert.True(t, u.Activo())
		assert.Equal(t, by, u.UpdatedBy(), "the actor of the alta owns the write")
		assert.Equal(t, previousCreatedBy, u.CreatedBy(), "CREATED_BY belongs to whoever minted the vendedor row")
		assert.NotEqual(t, by, u.CreatedBy(), "the alta's actor must NOT overwrite CREATED_BY")
		assert.Equal(t, previousCreatedAt, u.CreatedAt(), "CREATED_AT belongs to the original row")

		// Exactly one row exists, reachable by BOTH unique keys.
		assert.Len(t, h.usuarios.ByID, 1, "promotion must not mint a second row")
		byEmail, findErr := h.usuarios.FindByEmail(t.Context(), promoEmail)
		require.NoError(t, findErr)
		assert.Equal(t, previousID, byEmail.ID())
		byFUID, findErr := h.usuarios.FindByFirebaseUID(t.Context(), promoFUID)
		require.NoError(t, findErr)
		assert.Equal(t, previousID, byFUID.ID())
	})

	t.Run("sin_telefono_en_el_alta_conserva_el_previo", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		vendedor := h.seedVendedorOnly(t, promoEmail)

		// Give the vendedor row a phone, as if the office had filled it in.
		previo, err := platform.NewTelefono("4491112233")
		require.NoError(t, err)
		vendedor.Update(domain.UsuarioUpdate{
			Email:    vendedor.Email(),
			Nombre:   vendedor.Nombre(),
			Telefono: &previo,
		}, uuid.New(), h.clock.T)
		require.NoError(t, h.usuarios.Update(t.Context(), vendedor))

		u, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: promoFUID,
			Email:       promoEmail,
			Nombre:      promoNombre,
			Telefono:    nil,
		}, uuid.New())
		require.NoError(t, err)

		require.NotNil(t, u.Telefono(), "an omitted telefono must NOT erase the stored one")
		assert.Equal(t, "4491112233", u.Telefono().Value())
	})

	// The alta form carries no almacen. The promotion therefore has to feed
	// Update the almacenID the row ALREADY has — a UsuarioUpdate built with a
	// nil AlmacenID silently NULLs the column, and nobody looks at almacen_id
	// until a vendedor's ventas stop resolving a warehouse.
	t.Run("almacen_id_previo_sobrevive_a_la_promocion", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		vendedor := h.seedVendedorOnly(t, promoEmail)

		almacen := 19
		tel, err := platform.NewTelefono("4491112233")
		require.NoError(t, err)
		vendedor.Update(domain.UsuarioUpdate{
			Email:     vendedor.Email(),
			Nombre:    vendedor.Nombre(),
			Telefono:  &tel,
			AlmacenID: &almacen,
		}, uuid.New(), h.clock.T)
		require.NoError(t, h.usuarios.Update(t.Context(), vendedor))
		require.NotNil(t, vendedor.AlmacenID(), "precondition: the row carries an almacen")

		u, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: promoFUID,
			Email:       promoEmail,
			Nombre:      promoNombre,
		}, uuid.New())
		require.NoError(t, err)

		require.NotNil(t, u.AlmacenID(), "the promotion must NOT wipe almacen_id")
		assert.Equal(t, 19, *u.AlmacenID())

		// And it survived the write, not just the in-memory entity.
		stored, findErr := h.usuarios.FindByFirebaseUID(t.Context(), promoFUID)
		require.NoError(t, findErr)
		require.NotNil(t, stored.AlmacenID())
		assert.Equal(t, 19, *stored.AlmacenID())
		require.NotNil(t, stored.Telefono())
		assert.Equal(t, "4491112233", stored.Telefono().Value())
	})

	t.Run("con_telefono_en_el_alta_se_escribe", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.seedVendedorOnly(t, promoEmail)
		tel := "+52 449 987 6543"

		u, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: promoFUID,
			Email:       promoEmail,
			Nombre:      promoNombre,
			Telefono:    &tel,
		}, uuid.New())
		require.NoError(t, err)

		require.NotNil(t, u.Telefono())
		assert.Equal(t, "4499876543", u.Telefono().Value())
	})

	t.Run("vendedor_only_inactivo_es_conflicto_y_no_muta_nada", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		vendedor := h.seedVendedorOnly(t, promoEmail)
		vendedor.Desactivar(uuid.New(), h.clock.T)
		require.NoError(t, h.usuarios.Update(t.Context(), vendedor))

		_, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: promoFUID,
			Email:       promoEmail,
			Nombre:      promoNombre,
		}, uuid.New())
		// Somebody deactivated the row on purpose — reactivating it silently is
		// exactly what EnsureVendedoresByEmail refuses to do. It surfaces as the
		// ordinary collision conflict, NOT as domain.ErrUsuarioInactivo: that one
		// is a 403 the authn middleware already uses for the CALLER's own
		// account, and the desktop renders it as "your account is disabled".
		require.ErrorIs(t, err, domain.ErrUsuarioYaExiste)
		appErr, ok := apperror.As(err)
		require.True(t, ok, "expected an apperror, got %T", err)
		assert.Equal(t, apperror.KindConflict, appErr.Kind)
		assert.Equal(t, "usuario_ya_existe", appErr.Code,
			"the desktop keys its single collision message off this code")

		stored, findErr := h.usuarios.FindByEmail(t.Context(), promoEmail)
		require.NoError(t, findErr)
		assert.False(t, stored.Activo(), "the row must stay deactivated")
		assert.Equal(t, domain.EstatusVendedorOnly, stored.Estatus())
		assert.True(t, stored.FirebaseUID().IsZero(), "no uid may be attached to a deactivated row")
		assert.Equal(t, "humberto.quintana", stored.Nombre().Value(), "the nombre must not move either")
		assert.Len(t, h.usuarios.ByID, 1)
		assert.Empty(t, h.outbox.Calls, "a rejected alta must not emit an event")
	})

	// The 409 for "an inactive row holds the email" only fires for rows
	// deactivated OUTSIDE this API. Service.Desactivar mangles EMAIL and
	// FIREBASE_UID to "deleted-<id>-…" precisely to free the UNIQUE slots, so
	// after a baja through the API the email is genuinely free and the alta
	// creates a NEW row. Verified against Firebird, not just here.
	//
	// This test also guards the fake: it stores the entity POINTER, so an
	// Update that removed the stale index keys by reading them off the
	// already-mutated object removed nothing, and the fake kept answering
	// FindByEmail with the soft-deleted row — a 409 where production returns
	// 201. Any fake that reintroduces that bug fails here.
	t.Run("baja_por_el_api_libera_el_correo_y_el_alta_crea_fila_nueva", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		vendedor := h.seedVendedorOnly(t, promoEmail)
		require.NoError(t, h.svc.Desactivar(t.Context(), vendedor.ID(), uuid.New()))
		h.outbox.Calls = nil

		u, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: promoFUID,
			Email:       promoEmail,
			Nombre:      promoNombre,
		}, uuid.New())
		require.NoError(t, err, "Desactivar renamed the email away, so the slot is free")
		assert.NotEqual(t, vendedor.ID(), u.ID(), "a soft-deleted row is never revived")
		assert.Equal(t, domain.EstatusFirebaseUser, u.Estatus())
		assert.True(t, u.Activo())
		assert.Len(t, h.usuarios.ByID, 2, "the tombstone stays, the new row is added")

		require.Len(t, h.outbox.Calls, 1)
		payload, ok := h.outbox.Calls[0].Payload.(map[string]any)
		require.True(t, ok, "payload must be a map, got %T", h.outbox.Calls[0].Payload)
		assert.Equal(t, false, payload["promoted"], "this is a creation, not a promotion")

		// The tombstone kept its mangled identity and stayed deactivated.
		tomb, findErr := h.usuarios.FindByID(t.Context(), vendedor.ID())
		require.NoError(t, findErr)
		assert.False(t, tomb.Activo())
		assert.NotEqual(t, promoEmail, tomb.Email().Value())
	})

	t.Run("correo_ya_es_firebase_user_es_conflicto", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		existing := h.seedUsuario(t)

		_, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: "fuid-otro-" + uuid.NewString(),
			Email:       existing.Email().Value(),
			Nombre:      "Nombre Nuevo",
		}, uuid.New())
		require.ErrorIs(t, err, domain.ErrUsuarioYaExiste)

		stored, findErr := h.usuarios.FindByEmail(t.Context(), existing.Email().Value())
		require.NoError(t, findErr)
		assert.Equal(t, existing.FirebaseUID().Value(), stored.FirebaseUID().Value(), "the uid must not be re-pointed")
		assert.Equal(t, existing.Nombre().Value(), stored.Nombre().Value())
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("firebase_uid_de_otra_fila_es_conflicto_no_error_interno", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		otro := h.seedUsuario(t) // owns the uid already
		h.seedVendedorOnly(t, promoEmail)

		_, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: otro.FirebaseUID().Value(),
			Email:       promoEmail,
			Nombre:      promoNombre,
		}, uuid.New())
		// Must be the 409 conflict, NOT a 500 from UQ_MSP_USUARIOS_FIREBASE_UID.
		require.ErrorIs(t, err, domain.ErrUsuarioYaExiste)
		appErr, ok := apperror.As(err)
		require.True(t, ok, "expected an apperror, got %T", err)
		assert.Equal(t, apperror.KindConflict, appErr.Kind)

		// Neither row moved.
		vend, findErr := h.usuarios.FindByEmail(t.Context(), promoEmail)
		require.NoError(t, findErr)
		assert.Equal(t, domain.EstatusVendedorOnly, vend.Estatus())
		assert.True(t, vend.FirebaseUID().IsZero())
		stillOtro, findErr := h.usuarios.FindByFirebaseUID(t.Context(), otro.FirebaseUID().Value())
		require.NoError(t, findErr)
		assert.Equal(t, otro.ID(), stillOtro.ID(), "the uid must stay with its original owner")
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("el_evento_marca_el_camino_promoted", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		vendedor := h.seedVendedorOnly(t, promoEmail)
		by := uuid.New()

		u, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: promoFUID,
			Email:       promoEmail,
			Nombre:      promoNombre,
		}, by)
		require.NoError(t, err)

		require.Len(t, h.outbox.Calls, 1)
		call := h.outbox.Calls[0]
		assert.Equal(t, outboxAggregateUsuario, call.Aggregate)
		assert.Equal(t, eventUserCreated, call.EventType, "one event type covers both paths")
		assert.Equal(t, vendedor.ID(), call.AggregateID, "the event points at the promoted row")

		payload, ok := call.Payload.(map[string]any)
		require.True(t, ok, "payload must be a map, got %T", call.Payload)
		assert.Equal(t, map[string]any{
			"usuario_id":   u.ID(),
			"email":        promoEmail,
			"firebase_uid": promoFUID,
			"created_by":   by,
			"promoted":     true,
		}, payload)
	})

	t.Run("un_error_del_repo_al_promover_no_emite_evento", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.seedVendedorOnly(t, promoEmail)
		boom := errors.New("boom")
		h.usuarios.UpdateErr = boom

		_, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: promoFUID,
			Email:       promoEmail,
			Nombre:      promoNombre,
		}, uuid.New())
		require.ErrorIs(t, err, boom)
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("un_error_inesperado_de_findbyemail_se_propaga", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		boom := errors.New("firebird caído")
		h.usuarios.FindErr = boom

		_, err := h.svc.Crear(t.Context(), CrearParams{
			FirebaseUID: promoFUID,
			Email:       promoEmail,
			Nombre:      promoNombre,
		}, uuid.New())
		require.ErrorIs(t, err, boom, "a lookup failure must not be read as 'email libre'")
		assert.Empty(t, h.usuarios.ByID, "nothing may be created when the lookup is unreliable")
		assert.Empty(t, h.outbox.Calls)
	})
}
