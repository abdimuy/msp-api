//nolint:misspell // flota vocabulary is Spanish (camioneta, roster) per project convention.
package flotafirestore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These are internal tests on purpose. The mapping of a raw Firestore value
// to a camioneta is where this adapter earns its keep, and it can be pinned
// without a live Firestore — which the whole point of a snapshot module is
// not to depend on for its correctness.

// TestComoCamioneta_FormasRealesDelPadron covers every shape the field has
// actually been observed in, plus the ones the Firebase console makes it easy
// to type. Measured against the dev project on 2026-08-28 over 62 documents:
// 35 int64, 26 with the field absent, 1 with an explicit null.
func TestComoCamioneta_FormasRealesDelPadron(t *testing.T) {
	t.Parallel()

	t.Run("int64 positivo es el caso normal", func(t *testing.T) {
		t.Parallel()
		got := comoCamioneta(int64(11341))
		require.NotNil(t, got)
		assert.Equal(t, 11341, *got)
	})

	t.Run("campo ausente llega como nil", func(t *testing.T) {
		t.Parallel()
		// data[campo] on a map without the key yields a nil any.
		assert.Nil(t, comoCamioneta(nil))
	})

	t.Run("cero no es una camioneta", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, comoCamioneta(int64(0)))
	})

	t.Run("negativo no es una camioneta", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, comoCamioneta(int64(-11341)))
	})

	t.Run("float entero se acepta", func(t *testing.T) {
		t.Parallel()
		got := comoCamioneta(float64(51057))
		require.NotNil(t, got)
		assert.Equal(t, 51057, *got)
	})

	t.Run("float con decimales se rechaza, no se redondea", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, comoCamioneta(11341.7),
			"un id no es una medición: redondear inventaría una camioneta ajena")
	})

	t.Run("texto se rechaza", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, comoCamioneta("11341"),
			"la consola deja teclear un string; no se adivina su intención")
	})

	t.Run("booleano se rechaza", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, comoCamioneta(true))
	})
}

// TestComoTexto verifies the identity fields degrade to "" instead of
// blowing up the whole photograph.
func TestComoTexto(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "eliseo@msp.com", comoTexto("eliseo@msp.com"))
	assert.Empty(t, comoTexto(nil))
	assert.Empty(t, comoTexto(int64(7)))
	assert.Empty(t, comoTexto([]any{"Eliseo"}))
}

// TestNoopRosterClient_FallaEnVezDeDevolverVacio pins the choice that keeps a
// misconfigured deployment from looking like a legitimate observation. An
// empty roster is a MEANINGFUL value here — it is what the mass-deletion rail
// checks for — so the unconfigured stand-in must not produce one.
func TestNoopRosterClient_FallaEnVezDeDevolverVacio(t *testing.T) {
	t.Parallel()

	roster, err := NoopRosterClient{}.LeerRoster(context.Background())
	require.ErrorIs(t, err, ErrRosterNoConfigurado)
	assert.Nil(t, roster)
}
