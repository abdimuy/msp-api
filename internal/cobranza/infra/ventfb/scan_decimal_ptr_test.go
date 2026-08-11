package ventfb

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// LIBRES_CARGOS_CC.MONTO_A_CORTO_PLAZO is declared in Microsip as a scale-0
// INTEGER (the stored value already IS the peso amount). The firebirdsql
// driver hands NUMERIC/INTEGER with precision ≤ 9 back as int32. Reading it
// with scale 0 must keep the value intact (3200 = $3,200), NOT divide by 100.
func TestScanNullDecimal0Ptr_KeepsIntegerAmount(t *testing.T) {
	t.Parallel()

	got, err := scanNullDecimal0Ptr(int32(3200))

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Truef(t, got.Equal(decimal.NewFromInt(3200)), "want 3200, got %s", got)
}

func TestScanNullDecimal0Ptr_NilIsNil(t *testing.T) {
	t.Parallel()

	got, err := scanNullDecimal0Ptr(nil)

	require.NoError(t, err)
	require.Nil(t, got)
}

// Regression guard / documentation: reading the SAME integer with scale 2
// divides it by 100 — the "$3,200 shows as $32" bug. This is why
// MONTO_A_CORTO_PLAZO uses scanNullDecimal0Ptr while ENGANCHE /
// PRECIO_DE_CONTADO (genuine NUMERIC(_,2)) keep scanNullDecimal2Ptr.
func TestScanNullDecimal2Ptr_DividesIntegerByHundred(t *testing.T) {
	t.Parallel()

	got, err := scanNullDecimal2Ptr(int32(3200))

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Truef(t, got.Equal(decimal.NewFromInt(32)), "scale-2 read of 3200 = 32, got %s", got)
}
