//nolint:misspell // vocabulario español por convención del proyecto.
package desempenofb

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gruposEscalonados(n int) []grupoDesde {
	base := time.Date(2026, 2, 13, 2, 0, 0, 0, time.UTC)
	gs := make([]grupoDesde, n)
	for i := range gs {
		gs[i] = grupoDesde{desde: base.Add(-time.Duration(i) * 25 * time.Hour), zonas: []int{1000 + i}}
	}
	return gs
}

func ramasDe(q string) int { return strings.Count(q, "UNION ALL") + 1 }

// Ninguna sentencia por lote pasa de la mitad del límite de 256 contextos de
// Firebird, y entre todas cubren todos los grupos exactamente una vez.
func TestLote_SentenciasDentroDelLimiteDeContextos(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 9, ramasPorSentencia(contextosAplicados))
	assert.Equal(t, 1, ramasPorSentencia(1000), "nunca cero ramas")
	gs := gruposEscalonados(48)
	hasta := time.Date(2026, 2, 19, 2, 0, 0, 0, time.UTC)
	contextos := [3]int{contextosAplicados, contextosPendientes, contextosVisitas}
	for i, ls := range lecturasActividades(gs, hasta) {
		require.Greater(t, len(ls), 1, "con 48 grupos el tipo %d se parte en varias sentencias", i)
		total := 0
		for _, l := range ls {
			r := ramasDe(l.sql)
			assert.LessOrEqual(t, r*contextos[i], limiteContextosFirebird/2)
			assert.Len(t, l.args, strings.Count(l.sql, "?"))
			total += r
		}
		assert.Equal(t, 48, total, "tipo %d: una rama por grupo", i)
	}
	sts := sentenciasUniverso([]int{1, 2, 3}, gs)
	require.Greater(t, len(sts), 1)
	total := 0
	for i, st := range sts {
		r := ramasDe(st.sql)
		assert.LessOrEqual(t, r*contextosUniverso, limiteContextosFirebird/2)
		assert.Len(t, st.args, strings.Count(st.sql, "?"))
		assert.Equal(t, i == 0, strings.Contains(st.sql, "AND s.SALDO > 0"), "la rama de saldo va solo en la primera")
		total += r
	}
	assert.Equal(t, 48+1, total)
	assert.Len(t, partir([]int{1, 2, 3, 4, 5}, 2), 3)
	assert.Empty(t, partir([]int{}, 2))
}
