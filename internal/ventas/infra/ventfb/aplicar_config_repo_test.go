//nolint:misspell // Spanish vocabulary (zona, cajero, frecuencia, etc.) by convention.
package ventfb_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

func TestAplicarConfigRepo_CajaCajero_Hit(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewAplicarConfigRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		got, err := repo.CajaCajero(ctx, 21563)
		require.NoError(t, err)
		require.Equal(t, outbound.CajaCajero{CajaID: 22198, CajeroID: 22392, VendedorID: 88266, CobradorID: 11502}, got)
	})
}

func TestAplicarConfigRepo_CajaCajero_Miss(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewAplicarConfigRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := repo.CajaCajero(ctx, 999999)
		require.ErrorIs(t, err, domain.ErrZonaSinCaja)
	})
}

func TestAplicarConfigRepo_CajaCajero_RetornaCobradorID(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewAplicarConfigRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		got, err := repo.CajaCajero(ctx, 21563)
		require.NoError(t, err)
		require.NotEqual(t, 0, got.CobradorID, "el backfill debió haber asignado un cobrador (o -1)")
	})
}

func TestAplicarConfigRepo_FormaDePagoID_Hit(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewAplicarConfigRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		id, err := repo.FormaDePagoID(ctx, "SEMANAL")
		require.NoError(t, err)
		require.Equal(t, 33824, id)

		id, err = repo.FormaDePagoID(ctx, "QUINCENAL")
		require.NoError(t, err)
		require.Equal(t, 33825, id)

		id, err = repo.FormaDePagoID(ctx, "MENSUAL")
		require.NoError(t, err)
		require.Equal(t, 33826, id)
	})
}

func TestAplicarConfigRepo_FormaDePagoID_Miss(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewAplicarConfigRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := repo.FormaDePagoID(ctx, "NOPE")
		require.ErrorIs(t, err, domain.ErrFrecuenciaSinFormaPago)
	})
}

func TestAplicarConfigRepo_CreditoEnMesesID_Hit(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewAplicarConfigRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		id, err := repo.CreditoEnMesesID(ctx, 12)
		require.NoError(t, err)
		require.Equal(t, 33828, id)

		id, err = repo.CreditoEnMesesID(ctx, 6)
		require.NoError(t, err)
		require.Equal(t, 33830, id)
	})
}

func TestAplicarConfigRepo_CreditoEnMesesID_Miss(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewAplicarConfigRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := repo.CreditoEnMesesID(ctx, 999)
		require.ErrorIs(t, err, domain.ErrPlazoSinCreditoMeses)
	})
}

func TestAplicarConfigRepo_NumeroDeVendedoresID_Hit(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewAplicarConfigRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		id, err := repo.NumeroDeVendedoresID(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, 47558, id)

		id, err = repo.NumeroDeVendedoresID(ctx, 2)
		require.NoError(t, err)
		require.Equal(t, 47559, id)

		id, err = repo.NumeroDeVendedoresID(ctx, 3)
		require.NoError(t, err)
		require.Equal(t, 47560, id)
	})
}

func TestAplicarConfigRepo_NumeroDeVendedoresID_Miss(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewAplicarConfigRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := repo.NumeroDeVendedoresID(ctx, 99)
		require.ErrorIs(t, err, domain.ErrNumVendedoresSinMapeo)
	})
}

func TestAplicarConfigRepo_Defaults_Hit(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewAplicarConfigRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		got, err := repo.Defaults(ctx)
		require.NoError(t, err)
		require.Equal(t, outbound.AplicarDefaults{
			SucursalID:          225490,
			FormaCobroContadoID: 67,
			FormaCobroCreditoID: 71,
		}, got)
	})
}
