//nolint:misspell // ventas vocabulary is Spanish (vendedores, camioneta) per project convention.
package ventfirestore

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPuedeVender pins the MODULOS rule, which is the one judgement call in
// this adapter.
//
// The rule is asymmetric on purpose: an explicit MODULOS that omits VENTAS
// excludes the person, but a MISSING, empty or malformed MODULOS does not.
// Measured against the roster on 2026-08-27, all 35 people with a camioneta
// assigned carry VENTAS, so the strict half excludes nobody today; the lax
// half is what stops a document the desktop wrote without the field from
// silently costing a venta its vendedor.
func TestPuedeVender(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nombre string
		raw    any
		quiere bool
	}{
		{"campo ausente", nil, true},
		{"arreglo vacio", []any{}, true},
		{"no es arreglo", "VENTAS", true},
		{"arreglo de otro tipo", []any{1, 2}, false},
		{"trae VENTAS", []any{"COBRO", "VENTAS", "GARANTIAS"}, true},
		{"solo VENTAS", []any{"VENTAS"}, true},
		{"VENTAS en minusculas", []any{"ventas"}, true},
		{"VENTAS con espacios", []any{" VENTAS "}, true},
		{"solo cobra", []any{"COBRO", "GARANTIAS"}, false},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.quiere, puedeVender(c.raw))
		})
	}
}

// TestToStr pins the defensive read of a Firestore field: anything that is
// not a string reads as empty rather than panicking on a type assertion.
func TestToStr(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "hola", toStr("hola"))
	assert.Empty(t, toStr(nil))
	assert.Empty(t, toStr(int64(3)))
	assert.Empty(t, toStr([]any{"x"}))
}
