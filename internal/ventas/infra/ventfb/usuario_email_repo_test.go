//nolint:misspell // Spanish vocabulary (usuarios, vendedores) by convention.
package ventfb_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

// seedUsuarioConEmail inserts a usuario row with the exact EMAIL given (the
// caller controls its case) and returns its id. Everything rolls back with
// the enclosing test transaction.
func seedUsuarioConEmail(
	ctx context.Context, t *testing.T, pool *firebird.Pool, email, nombre string,
) uuid.UUID {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	id := uuid.New()
	now := testNow()
	_, err := q.ExecContext(
		ctx,
		`INSERT INTO MSP_USUARIOS
		 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO, ESTATUS,
		  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, ?, ?, TRUE, 'FIREBASE_USER', ?, ?, ?, ?)`,
		id.String(), "fb-vend-"+id.String(), email, nombre,
		now, now, id.String(), id.String(),
	)
	require.NoError(t, err, "seed usuario with email %q", email)
	return id
}

// TestUsuarioEmailRepo_ResuelveIdentidadReal exercises the hit path against a
// real MSP_USUARIOS row: the address goes in, the id and nombre come back
// keyed by the canonical address.
func TestUsuarioEmailRepo_ResuelveIdentidadReal(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewUsuarioEmailRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		email := "vendedor-" + uuid.NewString() + "@muebleriamsp.mx"
		id := seedUsuarioConEmail(ctx, t, pool, email, "ANA LAURA MENDEZ")

		out, err := repo.UsuariosPorEmail(ctx, []string{email})
		require.NoError(t, err)
		require.Contains(t, out, email)
		assert.Equal(t, id, out[email].ID)
		assert.Equal(t, "ANA LAURA MENDEZ", out[email].Nombre)
	})
}

// TestUsuarioEmailRepo_MayusculasEnAmbosLados is the integration half of the
// latent defect. Both the stored address and the probe are written in mixed
// case — independently — and the row must still be found, keyed canonically.
//
// A plain `EMAIL IN (?)` passes the "capitalized probe" half of this and
// fails the "capitalized row" half, which is why the query lowercases the
// COLUMN and not only the argument.
func TestUsuarioEmailRepo_MayusculasEnAmbosLados(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewUsuarioEmailRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		local := "Vendedor-" + uuid.NewString()
		guardado := local + "@MuebleriaMSP.MX" // as a legacy row could hold it
		canonical := strings.ToLower(guardado)
		id := seedUsuarioConEmail(ctx, t, pool, guardado, "BETO SUAREZ")

		// Probed with yet another spelling, plus stray whitespace.
		out, err := repo.UsuariosPorEmail(ctx, []string{"  " + strings.ToUpper(guardado) + " "})
		require.NoError(t, err)
		require.Contains(t, out, canonical,
			"the map must be keyed by the canonical address, whatever case either side used")
		assert.Equal(t, id, out[canonical].ID)
	})
}

// TestUsuarioEmailRepo_OmiteLosQueNoExisten pins that an address with no row
// is simply absent — the caller reports it rather than receiving a zero id it
// could mistake for an identity.
func TestUsuarioEmailRepo_OmiteLosQueNoExisten(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewUsuarioEmailRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		email := "vendedor-" + uuid.NewString() + "@muebleriamsp.mx"
		id := seedUsuarioConEmail(ctx, t, pool, email, "ANA")
		fantasma := "fantasma-" + uuid.NewString() + "@muebleriamsp.mx"

		out, err := repo.UsuariosPorEmail(ctx, []string{email, fantasma, email})
		require.NoError(t, err)
		assert.Len(t, out, 1, "duplicates collapse and misses are omitted")
		assert.Equal(t, id, out[email].ID)
		assert.NotContains(t, out, fantasma)
	})
}

// TestUsuarioEmailRepo_EntradaVaciaNoConsulta pins the short-circuit: an empty
// or all-blank input returns an empty map without building a query with zero
// placeholders (which Firebird would reject).
func TestUsuarioEmailRepo_EntradaVaciaNoConsulta(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewUsuarioEmailRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		out, err := repo.UsuariosPorEmail(ctx, nil)
		require.NoError(t, err)
		assert.Empty(t, out)

		out, err = repo.UsuariosPorEmail(ctx, []string{"", "   "})
		require.NoError(t, err)
		assert.Empty(t, out)
	})
}
