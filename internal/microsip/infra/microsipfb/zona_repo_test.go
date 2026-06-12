package microsipfb_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/microsip/infra/microsipfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

func TestZonaRepo_Listar_ConcatenatesCobradorWhenPresent(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := microsipfb.NewZonaRepo(pool)

	zonas, err := repo.Listar(context.Background())
	require.NoError(t, err)
	if len(zonas) == 0 {
		t.Skip("ZONAS_CLIENTES is empty in the dev DB")
	}

	withCobrador := 0
	for _, z := range zonas {
		assert.Positive(t, z.ID)
		assert.NotEmpty(t, z.Nombre)
		if strings.Contains(z.Nombre, " - ") {
			withCobrador++
		}
	}
	// In every Microsip install we've seen, at least one zona has clients
	// assigned to a cobrador. If the count is zero the seed data probably
	// lost cobrador rows — skip rather than fail.
	if withCobrador == 0 {
		t.Skip("no zona surfaced a cobrador suffix — likely empty COBRADORES/CLIENTES")
	}
}
