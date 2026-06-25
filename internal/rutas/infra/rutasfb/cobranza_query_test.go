//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutasfb

import (
	"strings"
	"testing"
)

// TestQueryVentasPorZona_FiltraConceptosDeCobranza guards the ABONO_SEMANA sum
// against counting non-collection concepts. Only 87327 (Cobranza en ruta) is the
// cobrador's actual route collection. Every other concept must be excluded —
// notably 27969 (Condonaciones, debt forgiveness, NOT a payment), plus cobro en
// mostrador (155), cobro (11), enganche (24533), cancelaciones/fugas/mal-cliente
// (27966/27967/27968), devoluciones (12/13) and ajustes (15).
func TestQueryVentasPorZona_FiltraConceptosDeCobranza(t *testing.T) {
	t.Parallel()

	if !strings.Contains(queryVentasPorZona, "CONCEPTO_CC_ID = 87327") {
		t.Errorf("ABONO_SEMANA debe filtrar solo CONCEPTO_CC_ID = 87327 (cobranza en ruta); query:\n%s",
			queryVentasPorZona)
	}
	if strings.Contains(queryVentasPorZona, "27969") {
		t.Error("no debe contar 27969 (Condonaciones) como pago")
	}
}

// TestQueryVentasPorZona_UsaPrecioTotal guards that the credit total comes from
// PRECIO_TOTAL, not the misleadingly-named TOTAL_IMPORTE column (which is the sum
// of payments — using it inflated "atraso" to the full balance, ~6% of cartera).
func TestQueryVentasPorZona_UsaPrecioTotal(t *testing.T) {
	t.Parallel()

	if !strings.Contains(queryVentasPorZona, "s.PRECIO_TOTAL") {
		t.Errorf("el total del crédito debe leerse de s.PRECIO_TOTAL; query:\n%s", queryVentasPorZona)
	}
	if strings.Contains(queryVentasPorZona, "s.TOTAL_IMPORTE") {
		t.Error("no debe usar s.TOTAL_IMPORTE (son pagos, no el total del crédito)")
	}
}
