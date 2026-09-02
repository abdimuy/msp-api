//nolint:misspell // Spanish vocabulary (nota, aval, microsip) by convention.
package microsip

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// LIBRES_CARGOS_CC.OBSERVACIONES is VARCHAR(99) and AVAL_O_RESPONSABLE is
// VARCHAR(50), both CHARACTER SET NONE (RDB$CHARACTER_SET_ID = 0). The domain
// allows five and four times that: maxNotaLength = 500, maxAvalLength = 200,
// and the DTO publishes maxLength:"500".
//
// Firebird does NOT truncate the overflow, it rejects the statement. Verified
// without writing anything, via EXECUTE BLOCK with a VARCHAR(5) and ten
// characters:
//
//	SQLSTATE = 22001 / string right truncation / expected length 5, actual 10
//
// So an over-long nota kills phase 7 and takes the whole aplicar with it.

func TestCheckNotaFits_AceptaElTopeExacto(t *testing.T) {
	t.Parallel()

	require.NoError(t, checkNotaFits(strings.Repeat("A", 99)),
		"99 bytes is the last nota that fits; rejecting it would refuse a valid venta")
}

// TestCheckNotaFits_RechazaLaNotaQueYaExiste is not hypothetical. The
// development database holds a CREDITO venta, still in borrador, whose NOTA is
// 230 characters long. The day it is approved and applied, phase 7 dies with an
// error that names neither the column nor the value.
func TestCheckNotaFits_RechazaLaNotaQueYaExiste(t *testing.T) {
	t.Parallel()

	err := checkNotaFits(strings.Repeat("A", 230))

	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.Truef(t, ok, "expected a typed apperror, got %T: %v", err, err)
	assert.Equal(t, "nota_exceeds_microsip_max", ae.Code)
	assert.Containsf(t, ae.Message, "230", "the message must name the size received; got %q", ae.Message)
	assert.Containsf(t, ae.Message, "99", "the message must name the ceiling; got %q", ae.Message)
	assert.Equal(t, strings.ToLower(ae.Message), ae.Message, "user-facing messages are lowercase")
	assert.Falsef(t, strings.HasSuffix(ae.Message, "."), "no trailing period; got %q", ae.Message)
}

// TestCheckNotaFits_MideBytesNoRunas is the trap the column width hides. The
// column is CHARACTER SET NONE and the writer binds the Go string with no
// EncodeWin1252 in between, so what travels is UTF-8: an accent costs two
// bytes. A 60-rune nota of accented characters is 120 bytes and does NOT fit,
// even though 60 < 99.
func TestCheckNotaFits_MideBytesNoRunas(t *testing.T) {
	t.Parallel()

	nota := strings.Repeat("ó", 60)
	require.Len(t, []rune(nota), 60, "60 runes")
	require.Len(t, nota, 120, "but 120 bytes")

	err := checkNotaFits(nota)

	require.Error(t, err, "a 60-rune nota of accents must be rejected: it is 120 bytes")
	ae, _ := apperror.As(err)
	assert.Contains(t, ae.Message, "120", "the message must report the byte size, not the rune count")
}

func TestCheckAvalFits_AceptaElTopeExacto(t *testing.T) {
	t.Parallel()

	require.NoError(t, checkAvalFits(strings.Repeat("A", 50)))
}

func TestCheckAvalFits_RechazaLoQueNoCabe(t *testing.T) {
	t.Parallel()

	err := checkAvalFits(strings.Repeat("A", 51))

	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.Truef(t, ok, "expected a typed apperror, got %T: %v", err, err)
	assert.Equal(t, "aval_exceeds_microsip_max", ae.Code)
	assert.Containsf(t, ae.Message, "51", "the message must name the size received; got %q", ae.Message)
	assert.Containsf(t, ae.Message, "50", "the message must name the ceiling; got %q", ae.Message)
}

// TestCheckAvalFits_AceptaVacio: the aval is optional, and the writer binds
// NULL when the cliente has none.
func TestCheckAvalFits_AceptaVacio(t *testing.T) {
	t.Parallel()

	require.NoError(t, checkAvalFits(""))
}
