// Integration tests for outbound.ClienteFactsReader (reads MSP_RX_COHORTE).
// All writes execute inside a transaction that always rolls back so the
// shared dev DB never accumulates test data.
//
// Prerequisites:
//   - FB_DATABASE env var pointing at the dev Microsip Firebird DB.
//   - Migration 000044 applied (creates MSP_RX_COHORTE).
//
// Run: FB_DATABASE=/firebird/data/MUEBLERA.FDB go test ./internal/reactivacion/infra/reactivacionfb/...
//
//nolint:misspell // Spanish vocabulary (cohorte, segmento) by convention.
package reactivacionfb_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionfb"
)

//nolint:paralleltest // serial: shares rollback-only tx.
func TestClienteFactsRepo_GetFacts_RoundTripsFromCohorte(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		cohorteRepo := reactivacionfb.NewRepo(pool)

		const clienteID = 901000401
		c := makeCohorte(t, clienteID, domain.SegmentoPorLiquidarHueco, "450.00", false, false)
		if err := cohorteRepo.UpsertCohorte(ctx, []*domain.CohorteCliente{c}); err != nil {
			t.Skipf("UpsertCohorte failed — migration 000044 may not be applied: %v", err)
		}

		factsRepo := reactivacionfb.NewRepo(pool)
		facts, err := factsRepo.GetFacts(ctx, clienteID)
		require.NoError(t, err)
		require.NotNil(t, facts)

		assert.Equal(t, "Cliente Prueba Ñandú", facts.Nombre, "UTF-8 name must round-trip")
		assert.Equal(t, domain.SegmentoPorLiquidarHueco.String(), facts.Segmento)
		assert.Equal(t, "238 100 2000", facts.Telefono)
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestClienteFactsRepo_GetFacts_UnknownClienteReturnsNilNil(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		facts, err := repo.GetFacts(ctx, 999999999)
		require.NoError(t, err)
		assert.Nil(t, facts)
	})
}
