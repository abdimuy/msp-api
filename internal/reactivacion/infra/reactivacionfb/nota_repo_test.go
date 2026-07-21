// Integration tests for outbound.NotaReader.GetNotaCliente — read-only
// against the live dev Microsip CLIENTES.NOTAS column. No writes, so no
// WithTestTransaction wrapper is needed (mirrors TestRepo_LeerUniversoTehuacan).
//
// Prerequisites:
//   - FB_DATABASE env var pointing at the dev Microsip Firebird DB.
//
// Run: FB_DATABASE=/firebird/data/MUEBLERA.FDB go test ./internal/reactivacion/infra/reactivacionfb/...
//
//nolint:misspell // Spanish vocabulary (nota) by convention.
package reactivacionfb_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionfb"
)

//nolint:paralleltest // serial: shares the test pool.
func TestNotaRepo_GetNotaCliente_RealClienteConNota(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	var clienteID int
	err := pool.DB.QueryRowContext(
		context.Background(),
		`SELECT FIRST 1 CLIENTE_ID FROM CLIENTES WHERE NOTAS IS NOT NULL AND CHAR_LENGTH(NOTAS) > 0`,
	).Scan(&clienteID)
	if errors.Is(err, sql.ErrNoRows) {
		t.Skip("no cliente in the dev DB currently has a non-empty NOTAS to exercise this test")
	}
	require.NoError(t, err)

	repo := reactivacionfb.NewRepo(pool)
	nota, err := repo.GetNotaCliente(context.Background(), clienteID)
	require.NoError(t, err)
	require.NotEmpty(t, nota, "the cliente selected by the CHAR_LENGTH(NOTAS)>0 filter must yield a non-empty nota")
	assert.True(t, utf8.ValidString(nota), "GetNotaCliente must always return valid UTF-8 (Win1252-decoded)")
}

//nolint:paralleltest // serial: shares the test pool.
func TestNotaRepo_GetNotaCliente_UnknownClienteReturnsEmptyNoError(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	repo := reactivacionfb.NewRepo(pool)
	nota, err := repo.GetNotaCliente(context.Background(), 999999999)
	require.NoError(t, err)
	assert.Empty(t, nota)
}
