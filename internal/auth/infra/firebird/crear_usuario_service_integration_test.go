package firebird_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/app"
	"github.com/abdimuy/msp-api/internal/auth/domain"
	authfb "github.com/abdimuy/msp-api/internal/auth/infra/firebird"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	platform "github.com/abdimuy/msp-api/internal/platform/domain"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// newCrearService assembles the REAL app.Service over the real Firebird
// repository and a real *firebird.TxManager.
//
// This is the only place the office alta runs with a transaction at all: the
// unit tests build the service with a nil TxManager (see NewService's own
// doc), so Service.runInTx degenerates to calling fn directly and every
// assertion about "the lookup and the write share one transaction" is
// unexercised there. Here they do share one — the ambient test tx installed
// by WithTestTransaction, which runInTx joins re-entrantly, and which is
// rolled back at the end of the test.
//
// roles, permisos and firebase are nil on purpose: Crear touches none of
// them, and a nil outbox makes enqueueEvent a no-op so the test does not
// write MSP_OUTBOX_EVENTS.
func newCrearService(pool *firebird.Pool) *app.Service {
	return app.NewService(
		authfb.NewUsuarioRepo(pool),
		nil, nil,
		outbound.ProductionClock{},
		nil, nil,
		firebird.NewTxManager(pool.DB),
	)
}

// seedVendedorOnlyRow persists an active VENDEDOR_ONLY usuario — the row
// EnsureVendedoresByEmail mints when a cobrador names somebody as vendedor of
// a venta — already carrying a telefono and an almacen_id, which is the state
// the promotion has to preserve.
func seedVendedorOnlyRow(
	ctx context.Context,
	t *testing.T,
	repo *authfb.UsuarioRepo,
	createdBy uuid.UUID,
	emailAddr string,
) *domain.Usuario {
	t.Helper()
	email, err := domain.NewEmail(emailAddr)
	require.NoError(t, err)
	placeholder, err := domain.NewNombre("humberto.quintana")
	require.NoError(t, err)
	u := domain.NewVendedorUsuario(uuid.New(), email, placeholder, createdBy, testNow())

	tel, err := platform.NewTelefono("4491112233")
	require.NoError(t, err)
	almacen := 19
	u.Update(domain.UsuarioUpdate{
		Email:     email,
		Nombre:    placeholder,
		Telefono:  &tel,
		AlmacenID: &almacen,
	}, createdBy, testNow())

	require.NoError(t, repo.Save(ctx, u))
	return u
}

// TestCrearService_PromueveVendedorOnly_ContraFirebird drives the office alta
// end to end — real service, real transaction, real MSP_USUARIOS — for the
// case the endpoint exists to fix: the email already belongs to a row the
// phone created.
//
// It pins the two columns nobody looks at until they are gone. The alta form
// carries neither an almacen nor (here) a telefono, and Update replaces
// ALMACEN_ID and TELEFONO wholesale: a UsuarioUpdate built with nils would
// NULL both in the same UPDATE that promotes the row, silently corrupting a
// record that already worked. It also pins that CREATED_BY stays with
// whoever minted the vendedor row while UPDATED_BY moves to the alta's actor.
func TestCrearService_PromueveVendedorOnly_ContraFirebird(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)
	svc := newCrearService(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		suffix := uuid.NewString()

		// A real, DIFFERENT actor for the alta. Without this the vendedor's
		// CREATED_BY and the alta's `by` would be the same uuid and the
		// "CREATED_BY did not move" assertion would hold for free.
		actor := newUsuario(t, root, "actor-"+suffix)
		require.NoError(t, repo.Save(ctx, actor))
		require.NotEqual(t, root, actor.ID())

		emailAddr := "humberto.quintana-" + suffix + "@example.invalid"
		vendedor := seedVendedorOnlyRow(ctx, t, repo, root, emailAddr)
		before := countAllUsuarios(ctx, t, pool)

		u, err := svc.Crear(ctx, app.CrearParams{
			FirebaseUID: "fb-alta-" + suffix,
			Email:       emailAddr,
			Nombre:      "Humberto Quintana Ríos",
			Telefono:    nil, // the alta omits it: the stored one must survive
		}, actor.ID())
		require.NoError(t, err)
		require.Equal(t, vendedor.ID(), u.ID(), "la promoción conserva el mismo ID")

		// Re-read from the database — the entity in hand proves nothing about
		// what the UPDATE actually wrote.
		got, err := repo.FindByID(ctx, vendedor.ID())
		require.NoError(t, err)
		assert.Equal(t, domain.EstatusFirebaseUser, got.Estatus())
		assert.Equal(t, "fb-alta-"+suffix, got.FirebaseUID().Value())
		assert.Equal(t, "Humberto Quintana Ríos", got.Nombre().Value(),
			"el nombre del alta reemplaza al derivado del correo, acentos incluidos")
		assert.Equal(t, emailAddr, got.Email().Value())
		assert.True(t, got.Activo())

		require.NotNil(t, got.Telefono(), "un alta sin teléfono NO debe borrar el guardado")
		assert.Equal(t, "4491112233", got.Telefono().Value())
		require.NotNil(t, got.AlmacenID(), "la promoción NO debe borrar ALMACEN_ID")
		assert.Equal(t, 19, *got.AlmacenID())

		assert.Equal(t, root, got.CreatedBy(), "CREATED_BY es de quien creó la fila del vendedor")
		assert.NotEqual(t, actor.ID(), got.CreatedBy())
		assert.Equal(t, actor.ID(), got.UpdatedBy(), "UPDATED_BY sí avanza al actor del alta")

		// The uid is a live lookup key now, and no twin was born.
		byFUID, err := repo.FindByFirebaseUID(ctx, "fb-alta-"+suffix)
		require.NoError(t, err)
		assert.Equal(t, vendedor.ID(), byFUID.ID())
		assert.Equal(t, before, countAllUsuarios(ctx, t, pool), "no debe nacer una segunda fila")
	})
}

// TestCrearService_CorreoLibre_ContraFirebird is the other half of the same
// command: a free email must still take the plain INSERT path, land a
// FIREBASE_USER row and attribute CREATED_BY to the actor (whose own row is
// what satisfies FK_MSP_USUARIOS_CREATED_BY).
func TestCrearService_CorreoLibre_ContraFirebird(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)
	svc := newCrearService(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		suffix := uuid.NewString()
		actor := newUsuario(t, root, "actor-libre-"+suffix)
		require.NoError(t, repo.Save(ctx, actor))
		before := countAllUsuarios(ctx, t, pool)

		emailAddr := "gabriel.roque-" + suffix + "@example.invalid"
		tel := "+52 449 987 6543"
		u, err := svc.Crear(ctx, app.CrearParams{
			FirebaseUID: "fb-libre-" + suffix,
			Email:       emailAddr,
			Nombre:      "Gabriel Roque",
			Telefono:    &tel,
		}, actor.ID())
		require.NoError(t, err)

		got, err := repo.FindByID(ctx, u.ID())
		require.NoError(t, err)
		assert.Equal(t, domain.EstatusFirebaseUser, got.Estatus())
		assert.Equal(t, "fb-libre-"+suffix, got.FirebaseUID().Value())
		assert.Equal(t, emailAddr, got.Email().Value())
		require.NotNil(t, got.Telefono())
		assert.Equal(t, "4499876543", got.Telefono().Value())
		assert.Equal(t, actor.ID(), got.CreatedBy(), "CREATED_BY es el actor del alta")
		assert.Equal(t, before+1, countAllUsuarios(ctx, t, pool), "debe nacer exactamente una fila")

		// A second alta on the same uid must be the 409, not a duplicate row:
		// the email is free, so the pre-check never fires and
		// UQ_MSP_USUARIOS_FIREBASE_UID is the only thing standing in the way.
		_, err = svc.Crear(ctx, app.CrearParams{
			FirebaseUID: "fb-libre-" + suffix,
			Email:       "otro-" + suffix + "@example.invalid",
			Nombre:      "Otro Alta",
		}, actor.ID())
		require.ErrorIs(t, err, domain.ErrUsuarioYaExiste)
		assert.Equal(t, before+1, countAllUsuarios(ctx, t, pool), "el 409 no debe dejar fila")
	})
}
