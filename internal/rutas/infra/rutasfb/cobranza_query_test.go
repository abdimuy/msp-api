//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutasfb

import (
	"strings"
	"testing"
)

// TestQueryVentasPorZona_FiltraConceptosDeCobranza guards the ABONO_SEMANA sum
// against counting non-collection payments. MSP_PAGOS_VENTAS caches many concepts
// beyond route collection — cobro en mostrador (155), enganche (24533),
// devoluciones (12/13), ajustes (15), condonación por pronto pago (25116) — which
// would inflate cobertura/ponderado. Only 87327 (cobranza en ruta) and 27969
// (abono mostrador) count, matching the mobile sync's "cobranza activa" filter.
func TestQueryVentasPorZona_FiltraConceptosDeCobranza(t *testing.T) {
	t.Parallel()

	if !strings.Contains(queryVentasPorZona, "CONCEPTO_CC_ID IN (87327, 27969)") {
		t.Errorf("ABONO_SEMANA debe filtrar CONCEPTO_CC_ID IN (87327, 27969); query:\n%s",
			queryVentasPorZona)
	}
}
