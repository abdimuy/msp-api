//nolint:paralleltest // Firebird integration tests must run serially.
package garfb_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

func contarTransaccionesDelProceso(
	ctx context.Context,
	pool *firebird.Pool,
) (int, error) {
	var total int

	err := firebird.RunInReadTx(ctx, pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, pool.DB)

		return q.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM MON$TRANSACTIONS t
JOIN MON$ATTACHMENTS a
  ON a.MON$ATTACHMENT_ID = t.MON$ATTACHMENT_ID
WHERE a.MON$REMOTE_PID = ?`,
			os.Getpid(),
		).Scan(&total)
	})

	return total, err
}

func ejecutarLecturasPublicas(
	ctx context.Context,
	t *testing.T,
	garantias *garfb.GarantiaRepo,
	eventos *garfb.EventoRepo,
	folios *garfb.FolioGenerator,
) {
	t.Helper()

	id := uuid.New()

	folio, err := domain.NewFolio(999999)
	require.NoError(t, err)

	for range 3 {
		_, err = garantias.Obtener(ctx, id)
		require.ErrorIs(t, err, domain.ErrGarantiaNoEncontrada)

		_, err = garantias.ObtenerPorFolio(ctx, folio)
		require.ErrorIs(t, err, domain.ErrGarantiaNoEncontrada)

		lista, err := eventos.ListarPorGarantia(ctx, id)
		require.NoError(t, err)
		require.Empty(t, lista)

		evento, err := eventos.ObtenerPorClaveIdempotencia(
			ctx,
			uuid.NewString(),
		)
		require.NoError(t, err)
		require.Nil(t, evento)

		_, err = folios.Siguiente(ctx)
		require.NoError(t, err)
	}
}

func TestLecturasPublicas_NoDejanTransaccionesAbiertas(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	garantias := garfb.NewGarantiaRepo(pool)
	eventos := garfb.NewEventoRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	time.Sleep(200 * time.Millisecond)

	antes, err := contarTransaccionesDelProceso(ctx, pool)
	require.NoError(t, err)

	// 15 lecturas públicas sin una transacción creada por el caller.
	ejecutarLecturasPublicas(
		ctx,
		t,
		garantias,
		eventos,
		folios,
	)

	time.Sleep(200 * time.Millisecond)

	despues, err := contarTransaccionesDelProceso(ctx, pool)
	require.NoError(t, err)

	require.Equal(t, antes, despues)

	// Control: las mismas 15 lecturas dentro de RunInReadTx.
	err = firebird.RunInReadTx(
		ctx,
		pool.DB,
		func(readCtx context.Context) error {
			ejecutarLecturasPublicas(
				readCtx,
				t,
				garantias,
				eventos,
				folios,
			)

			return nil
		},
	)
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	despuesControl, err := contarTransaccionesDelProceso(ctx, pool)
	require.NoError(t, err)

	require.Equal(t, antes, despuesControl)

	t.Logf(
		"transacciones: antes=%d despues=%d control=%d",
		antes,
		despues,
		despuesControl,
	)
}
