//nolint:misspell // Spanish domain vocabulary (nota) by project convention.
package reactivacionfb

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// Compile-time check: Repo must satisfy outbound.NotaReader.
var _ outbound.NotaReader = (*Repo)(nil)

// selectNotaCliente reads the cobrador's free-text note off the Microsip
// CLIENTES table (legacy, Windows-1252 BLOB).
const selectNotaCliente = `SELECT NOTAS FROM CLIENTES WHERE CLIENTE_ID = ?`

// notaMaxRunes caps the cobrador's free-text note before it reaches the LLM
// prompt: long enough to carry payment agreements / shared-address context,
// short enough to bound prompt size. Mirrors
// internal/analytics/infra/analyticsfb/repo.go's constant of the same name —
// this is the ONLY place in the reactivación module that decodes Win1252
// (the Microsip legacy note BLOB); every MSP_RX_* column is UTF8 and never
// uses firebird.Win1252.
const notaMaxRunes = 800

// GetNotaCliente returns the cobrador's free-text note (CLIENTES.NOTAS) for a
// client, decoded from Windows-1252 (BLOB Sub_Type 1), NFC-normalized, trimmed,
// and capped to notaMaxRunes runes. A client with no note (or no row) yields ""
// with no error — the note is optional qualitative context for the copiloto,
// never a hard dependency.
func (r *Repo) GetNotaCliente(ctx context.Context, clienteID int) (string, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	var notaRaw firebird.Win1252 // Win1252 handles nil→"" at scan time.
	err := q.QueryRowContext(ctx, selectNotaCliente, clienteID).Scan(&notaRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", firebird.MapError(err)
	}
	nota := strings.TrimSpace(norm.NFC.String(string(notaRaw)))
	if utf8.RuneCountInString(nota) > notaMaxRunes {
		nota = string([]rune(nota)[:notaMaxRunes])
	}
	return nota, nil
}
