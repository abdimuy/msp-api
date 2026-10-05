package garfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garfb"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

func TestGarantiaRepo_Candado_Commit(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	repo := garfb.NewGarantiaRepo(pool)
	folios := garfb.NewFolioGenerator(pool)
	txManager := firebird.NewTxManager(pool.DB)

	ctx := context.Background()

	numero, err := folios.Siguiente(ctx)
	require.NoError(t, err)

	folio, err := domain.NewFolio(numero)
	require.NoError(t, err)

	origen, err := domain.ParseOrigenFolio("cliente")
	require.NoError(t, err)

	estadoCuenta, err := domain.ParseEstadoCuenta("liquidada")
	require.NoError(t, err)

	now := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)

	clienteID := 995001
	ventaID := 995002
	articuloID := 995003

	g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
		Folio:          folio,
		Origen:         origen,
		ClienteID:      &clienteID,
		VentaID:        &ventaID,
		EstadoCuenta:   &estadoCuenta,
		Description:    "Garantía para probar el candado",
		Calle:          "Avenida Reforma",
		NumeroExterior: "1001",
		Colonia:        "Centro",
		Localidad:      "Puebla",
		Ciudad:         "Puebla",
		CodigoPostal:   "72000",
		AbiertoPor:     "ruben",
		Now:            now,
		Actor: domain.ActorParams{
			Usuario:           "ruben",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   now,
		},
	})
	require.NoError(t, err)

	err = g.AgregarArticulo(
		domain.AgregarArticuloParams{
			ArticuloID:  &articuloID,
			Clave:       "LOCK-001",
			Description: "Artículo para prueba de bloqueo",
		},
		domain.ActorParams{
			Usuario:           "ruben",
			ClaveIdempotencia: uuid.NewString(),
			DeviceCreatedAt:   now.Add(time.Minute),
		},
		now.Add(time.Minute),
	)
	require.NoError(t, err)

	// The row must be committed so the second transaction can see it.

	err = txManager.RunInTx(ctx, func(txCtx context.Context) error {
		return repo.Crear(txCtx, g)
	})
	require.NoError(t, err)

	// This is the only test that commits, so it must clean up its rows.
	t.Cleanup(func() {
		cleanupErr := txManager.RunInTx(
			context.Background(),
			func(cleanCtx context.Context) error {
				q := firebird.GetQuerier(cleanCtx, pool.DB)

				_, err := q.ExecContext(
					cleanCtx,
					"DELETE FROM MSP_GA_EVENTO WHERE GARANTIA_ID = ?",
					g.ID().String(),
				)
				require.NoError(t, err)
				if err != nil {
					return err
				}

				_, err = q.ExecContext(
					cleanCtx,
					"DELETE FROM MSP_GA_ARTICULO WHERE GARANTIA_ID = ?",
					g.ID().String(),
				)
				require.NoError(t, err)
				if err != nil {
					return err
				}

				_, err = q.ExecContext(
					cleanCtx,
					"DELETE FROM MSP_GA_GARANTIA WHERE ID = ?",
					g.ID().String(),
				)
				require.NoError(t, err)
				if err != nil {
					return err
				}

				return nil
			},
		)

		require.NoError(t, cleanupErr)
	})

	// The first transaction acquires the lock.
	err = txManager.RunInTx(ctx, func(primerCtx context.Context) error {
		_, err := repo.ObtenerParaActualizar(primerCtx, g.ID())
		require.NoError(t, err)

		inicio := time.Now()

		// Use context.Background() so the second call does not inherit the first transaction.
		// This opens a real second transaction with NO WAIT.
		segundoErr := txManager.RunInTxNoWait(
			context.Background(),
			func(segundoCtx context.Context) error {
				_, err := repo.ObtenerParaActualizar(
					segundoCtx,
					g.ID(),
				)
				return err
			},
		)

		require.Error(t, segundoErr)

		// NO WAIT must return without waiting for the lock.
		require.Less(
			t,
			time.Since(inicio),
			2*time.Second,
		)

		// Firebird should report a lock conflict.
		appErr, ok := apperror.As(segundoErr)
		require.True(t, ok)
		require.Equal(
			t,
			"firebird_lock_conflict",
			appErr.Code,
		)

		return nil
	})
	require.NoError(t, err)
}
