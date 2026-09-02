//nolint:misspell // Spanish vocabulary (parcialidad, microsip) by convention.
package microsip

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// LIBRES_CARGOS_CC.PARCIALIDAD is declared NUMERIC(4,0) but Firebird stores it
// in a SMALLINT and enforces the STORAGE range, not the declared precision —
// so the real ceiling is 32,767.
//
// Measured on the live catalog, because the declared precision misleads in
// BOTH directions: MONTO_A_CORTO_PLAZO in the same row is declared
// NUMERIC(5,0) and already holds 127,000. The ceiling that matters is the
// storage type's, and for PARCIALIDAD it is close: the largest parcialidad on
// record is 31,950 and five rows are already above 30,000.

func TestCheckParcialidadFits_AceptaElTopeExacto(t *testing.T) {
	t.Parallel()

	require.NoError(t, checkParcialidadFits(decimal.NewFromInt(32767)),
		"32,767 is the last value a SMALLINT holds; rejecting it would refuse a valid venta")
}

func TestCheckParcialidadFits_AceptaElMaximoRealDeProduccion(t *testing.T) {
	t.Parallel()

	require.NoError(t, checkParcialidadFits(decimal.RequireFromString("31950.00")),
		"the largest parcialidad on record must keep going through")
}

// TestCheckParcialidadFits_RechazaLoQueNoCabe is the case that motivated the
// guard: today a parcialidad of 33,000 is bound straight into the INSERT, the
// statement fails on the storage range, and the whole aplicar goes down with an
// error that never names the column.
func TestCheckParcialidadFits_RechazaLoQueNoCabe(t *testing.T) {
	t.Parallel()

	err := checkParcialidadFits(decimal.RequireFromString("33000.00"))

	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.Truef(t, ok, "expected a typed apperror, got %T: %v", err, err)
	assert.Equal(t, "parcialidad_exceeds_microsip_max", ae.Code)

	// The failed-intent screen shows the message and nothing else, so it has to
	// carry both numbers: what was sent and what fits.
	assert.Containsf(t, ae.Message, "33000.00",
		"the message must name the value received; got %q", ae.Message)
	assert.Containsf(t, ae.Message, "32767",
		"the message must name the ceiling; got %q", ae.Message)
	assert.Equal(t, strings.ToLower(ae.Message), ae.Message,
		"user-facing messages are lowercase per CLAUDE.md §3")
	assert.Falsef(t, strings.HasSuffix(ae.Message, "."),
		"user-facing messages carry no trailing period; got %q", ae.Message)
}

func TestCheckParcialidadFits_RechazaJustoArribaDelTope(t *testing.T) {
	t.Parallel()

	require.Error(t, checkParcialidadFits(decimal.NewFromInt(32768)),
		"32,768 is the first value a SMALLINT cannot hold")
}
