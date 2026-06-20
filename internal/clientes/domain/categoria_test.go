//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
)

// TestClasificarConcepto_AllKnownIDs exercises every CONCEPTO_CC_ID from
// task-be1a-brief fact #2, verifying deterministic classification.
func TestClasificarConcepto_AllKnownIDs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		conceptoCCID  int
		wantCategoria domain.Categoria
	}{
		// pago — cobranza set (must match migration 000010: {11, 155, 87327})
		{11, domain.CategoriaIngresoPago},
		{155, domain.CategoriaIngresoPago},
		{87327, domain.CategoriaIngresoPago},
		// enganche
		{24533, domain.CategoriaIngresoEnganche},
		// condonacion
		{25116, domain.CategoriaCondonacion},
		{27969, domain.CategoriaCondonacion},
		// perdida
		{27966, domain.CategoriaPerdida},
		{27967, domain.CategoriaPerdida},
		{27968, domain.CategoriaPerdida},
		{25117, domain.CategoriaPerdida},
		// otro — any unknown ID
		{0, domain.CategoriaOtro},
		{99999, domain.CategoriaOtro},
		{-1, domain.CategoriaOtro},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("id=%d", tc.conceptoCCID), func(t *testing.T) {
			t.Parallel()
			got := domain.ClasificarConcepto(tc.conceptoCCID)
			assert.Equal(t, tc.wantCategoria, got,
				"ClasificarConcepto(%d) = %q, want %q", tc.conceptoCCID, got, tc.wantCategoria)
		})
	}
}

// TestCategoria_EsIngreso verifies EsIngreso for each category value.
func TestCategoria_EsIngreso(t *testing.T) {
	t.Parallel()
	tests := []struct {
		categoria   domain.Categoria
		wantIngreso bool
	}{
		{domain.CategoriaIngresoPago, true},
		{domain.CategoriaIngresoEnganche, true},
		{domain.CategoriaCondonacion, false},
		{domain.CategoriaPerdida, false},
		{domain.CategoriaOtro, true},
	}

	for _, tc := range tests {
		t.Run(string(tc.categoria), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.wantIngreso, tc.categoria.EsIngreso(),
				"Categoria(%q).EsIngreso() should be %v", tc.categoria, tc.wantIngreso)
		})
	}
}
