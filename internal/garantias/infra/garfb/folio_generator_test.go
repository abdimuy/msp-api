package garfb_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/infra/garfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

func TestFolioGenerator_Siguiente(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	generator := garfb.NewFolioGenerator(pool)
	ctx := context.Background()

	primero, err := generator.Siguiente(ctx)
	require.NoError(t, err)

	segundo, err := generator.Siguiente(ctx)
	require.NoError(t, err)

	require.NotEqual(t, primero, segundo)
	require.Greater(t, segundo, primero)
}
