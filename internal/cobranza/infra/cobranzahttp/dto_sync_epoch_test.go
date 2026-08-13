//nolint:misspell // Spanish domain vocabulary (venta, pago, zona) per project convention.
package cobranzahttp

// White-box tests for the `sync_epoch` field on the two sync page bodies.
// They pin BOTH the Go value and the JSON wire key: the mobile client keys off
// the literal `sync_epoch`, so a rename is a breaking contract change.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// emptyVentasPage builds a sync page with no items — the epoch must travel
// even on an empty page, otherwise a client that is already up to date would
// never learn it has to resync.
func emptyVentasPage() outbound.SyncPage[domain.Venta] {
	return outbound.SyncPage[domain.Venta]{
		Items:        nil,
		MaxUpdatedAt: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		ServerNow:    time.Date(2026, 8, 10, 12, 0, 1, 0, time.UTC),
		HasMore:      false,
	}
}

// emptyPagosPage is the pagos counterpart of emptyVentasPage.
func emptyPagosPage() outbound.SyncPage[domain.Pago] {
	return outbound.SyncPage[domain.Pago]{
		Items:        nil,
		MaxUpdatedAt: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		ServerNow:    time.Date(2026, 8, 10, 12, 0, 1, 0, time.UTC),
		HasMore:      false,
	}
}

func TestToSyncVentasBody_LlevaSyncEpoch(t *testing.T) {
	t.Parallel()

	body := toSyncVentasBody(emptyVentasPage(), nil, 42)

	assert.Equal(t, 42, body.SyncEpoch)

	raw, err := json.Marshal(body)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"sync_epoch":42`)
}

func TestToSyncPagosBody_LlevaSyncEpoch(t *testing.T) {
	t.Parallel()

	body := toSyncPagosBody(emptyPagosPage(), 7)

	assert.Equal(t, 7, body.SyncEpoch)

	raw, err := json.Marshal(body)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"sync_epoch":7`)
}

// TestSyncBodies_SyncEpochCeroSigueSerializando protege contra que alguien
// agregue `omitempty`: el cliente necesita leer el 0 explícito para
// distinguir "nunca se forzó nada" de "el servidor no lo manda".
func TestSyncBodies_SyncEpochCeroSigueSerializando(t *testing.T) {
	t.Parallel()

	rawVentas, err := json.Marshal(toSyncVentasBody(emptyVentasPage(), nil, 0))
	require.NoError(t, err)
	assert.Contains(t, string(rawVentas), `"sync_epoch":0`)

	rawPagos, err := json.Marshal(toSyncPagosBody(emptyPagosPage(), 0))
	require.NoError(t, err)
	assert.Contains(t, string(rawPagos), `"sync_epoch":0`)
}
