//nolint:misspell // Spanish vocabulary per project convention.
package clientesfb

import (
	"strings"
	"testing"
)

// TestQueryListarDirectorio_IncluyeEstatusV guards the directory filter against a
// regression. ESTATUS='V' clients are real (96.6% have DOCTOS_PV sales) and some
// carry active cobranza saldo (e.g. cliente 121278, MARIA ROSALINA JUAREZ
// MARTINEZ). They were previously excluded as assumed "vendor-route pseudo-clients"
// and never appeared in the directory search. The directory — and therefore the
// Meilisearch index it feeds — must include 'A', 'B' and 'V', dropping only 'C'.
func TestQueryListarDirectorio_IncluyeEstatusV(t *testing.T) {
	t.Parallel()

	if !strings.Contains(queryListarDirectorioCompletoBase, "ESTATUS IN ('A', 'B', 'V')") {
		t.Errorf("la query del directorio debe filtrar ESTATUS IN ('A', 'B', 'V'); query:\n%s",
			queryListarDirectorioCompletoBase)
	}
	if strings.Contains(queryListarDirectorioCompletoBase, "'C'") {
		t.Error("el directorio no debe incluir clientes cancelados ('C')")
	}
}
