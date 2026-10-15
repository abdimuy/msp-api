//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutasfb

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ninguna sentencia de VentasPorZonas pasa de la mitad del límite de 256
// contextos de Firebird, y la rama de saldo > 0 va una sola vez.
func TestVentasLote_SentenciasDentroDelLimiteDeContextos(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 2, 13, 2, 0, 0, 0, time.UTC)
	gs := make([]grupoDesde, 48)
	for i := range gs {
		gs[i] = grupoDesde{desde: base.Add(-time.Duration(i) * 25 * time.Hour), zonas: []int{1000 + i}}
	}
	ramas := ramasPorGrupo("p.ZONA_CLIENTE_ID", "p.FECHA", gs, base.Add(6*24*time.Hour))
	tramos := partirVentas(ramas)
	require.Greater(t, len(tramos), 1)
	total, saldo := 0, 0
	for i, tr := range tramos {
		zs := []int{1, 2}
		if i > 0 {
			zs = nil
		}
		q, args := queryVentasLote(zs, tr)
		r := strings.Count(q, "UNION ALL") + 1
		assert.LessOrEqual(t, r*contextosVentas, limiteContextosFirebird/2)
		assert.Len(t, args, strings.Count(q, "?"))
		saldo += strings.Count(q, "AND s.SALDO > 0")
		total += len(tr)
	}
	assert.Equal(t, 48, total)
	assert.Equal(t, 1, saldo)
	for _, tr := range partir(ramas, ramasPorSentencia(contextosAbonos)) {
		q, args := queryAbonosLote(tr)
		assert.LessOrEqual(t, strings.Count(q, "UNION ALL")+1, limiteContextosFirebird/2)
		assert.Len(t, args, strings.Count(q, "?"))
	}
}
