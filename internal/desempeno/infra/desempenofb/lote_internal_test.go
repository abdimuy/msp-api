//nolint:misspell // vocabulario español por convención del proyecto.
package desempenofb

import (
	"database/sql"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Lo que el driver entrega: la hora de pared del negocio, sin zona real.
func columna(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse("2006-01-02 15:04:05.000000", s)
	require.NoError(t, err)
	return v
}

func TestVentanaPared_RepiteElFiltroSQL(t *testing.T) {
	t.Parallel()
	loc := firebird.BusinessTZ()
	desde := time.Date(2026, 2, 12, 20, 0, 0, 0, loc).UTC()
	hasta := time.Date(2026, 2, 18, 20, 0, 0, 0, loc).Add(150 * time.Microsecond).UTC() // el driver trunca a 100 µs
	v := nuevaVentanaPared(desde, hasta)
	assert.True(t, v.contiene(columna(t, "2026-02-12 20:00:00.000000")), "el desde es inclusivo")
	assert.False(t, v.contiene(columna(t, "2026-02-12 19:59:59.999900")))
	assert.True(t, v.contiene(columna(t, "2026-02-18 20:00:00.000100")), "hasta truncado a 100 µs, inclusivo")
	assert.False(t, v.contiene(columna(t, "2026-02-18 20:00:00.000200")), "lo que pasa del truncado queda fuera")
	// El desde también se trunca: 20:00:00.000150 llega a Firebird como
	// .000100, así que una fila en .000100 SÍ entra.
	v2 := nuevaVentanaPared(desde.Add(150*time.Microsecond), hasta)
	assert.True(t, v2.contiene(columna(t, "2026-02-12 20:00:00.000100")), "desde truncado a 100 µs")
	assert.False(t, v2.contiene(columna(t, "2026-02-12 20:00:00.000000")))
	assert.True(t, desdePared(columna(t, "2026-02-12 20:00:00.000100"), desde.Add(150*time.Microsecond)))
	assert.True(t, desdePared(columna(t, "2026-02-12 20:00:00.000000"), desde))
	assert.False(t, desdePared(columna(t, "2026-02-12 19:00:00.000000"), desde))
}

func TestAgruparZonas_UnaSemanaViejaNoEnsanchaLasDemas(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 2, 13, 2, 0, 0, 0, time.UTC)
	gs := agruparZonas(map[int]time.Time{
		21563: base,
		12271: base.Add(3 * time.Hour), // dentro de la tolerancia: mismo grupo
		6969:  base.Add(-30 * 24 * time.Hour),
		500:   base.Add(25 * time.Hour), // fuera de la tolerancia del grupo de base
	})
	require.Len(t, gs, 3)
	assert.Equal(t, []int{6969}, gs[0].zonas)
	assert.Equal(t, base.Add(-30*24*time.Hour), gs[0].desde)
	assert.Equal(t, []int{12271, 21563}, gs[1].zonas)
	assert.Equal(t, base, gs[1].desde, "el grupo se lee desde su zona más antigua")
	assert.Equal(t, []int{500}, gs[2].zonas)
}

func TestRamasPorGrupo_ArgumentosEnOrden(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 2, 13, 2, 0, 0, 0, time.UTC)
	hasta := base.Add(6 * 24 * time.Hour)
	q, args := unirRamas(ramasPorGrupo("p.ZONA_CLIENTE_ID", "p.FECHA",
		[]grupoDesde{{desde: base, zonas: []int{1, 2}}, {desde: hasta, zonas: []int{3}}}, hasta),
		func(f string) string { return "SELECT 1 FROM T WHERE " + f })
	assert.Equal(t, "SELECT 1 FROM T WHERE p.ZONA_CLIENTE_ID IN (?,?)\n  AND p.FECHA >= ?\n  AND p.FECHA <= ?"+
		"\nUNION ALL\nSELECT 1 FROM T WHERE p.ZONA_CLIENTE_ID IN (?)\n  AND p.FECHA >= ?\n  AND p.FECHA <= ?", q)
	assert.Equal(t, []any{
		1, 2, firebird.ToWallClock(base), firebird.ToWallClock(hasta),
		3, firebird.ToWallClock(hasta), firebird.ToWallClock(hasta),
	}, args)
}

func cuentaU(venta int, saldo string) domain.CuentaUniverso {
	return domain.CuentaUniverso{VentaID: venta, Saldo: decimal.RequireFromString(saldo)}
}

func TestUniversoDe_RepiteElPredicadoPorClave(t *testing.T) {
	t.Parallel()
	loc := firebird.BusinessTZ()
	inicio := time.Date(2026, 2, 12, 20, 0, 0, 0, loc).UTC()
	fup := func(s string) sql.NullTime { return sql.NullTime{Time: columna(t, s), Valid: true} }
	filas := []cuentaZona{
		{claveCuenta{zona: 7, fup: fup("2026-01-01 10:00:00.000000")}, cuentaU(30, "100.00")}, // saldo > 0
		{claveCuenta{zona: 7, fup: fup("2026-02-12 20:00:00.000000")}, cuentaU(10, "0.00")},   // saldada justo en el inicio
		{claveCuenta{zona: 7, fup: fup("2026-02-12 19:59:59.000000")}, cuentaU(20, "0.00")},   // saldada antes
		{claveCuenta{zona: 7}, cuentaU(40, "0.00")},                                           // saldada sin fecha
		{claveCuenta{zona: 8, fup: fup("2026-02-15 10:00:00.000000")}, cuentaU(5, "50.00")},   // otra zona
	}
	got := universoDe(filas, 7, inicio)
	ids := make([]int, 0, len(got))
	for _, c := range got {
		ids = append(ids, c.VentaID)
	}
	assert.Equal(t, []int{10, 30}, ids, "por DOCTO_CC_ID, sin la saldada vieja, la sin fecha ni la otra zona")
}

func TestOrdenarEmpates_PorHoraYLuegoID(t *testing.T) {
	t.Parallel()
	h := time.Date(2026, 2, 18, 15, 0, 0, 0, time.UTC)
	filas := []filaZona{
		{act: domain.Actividad{ID: "visita:b", Hora: h}},
		{act: domain.Actividad{ID: "visita:c", Hora: h.Add(-time.Minute)}},
		{act: domain.Actividad{ID: "visita:a", Hora: h}},
	}
	ordenarEmpates(filas)
	assert.Equal(t, "visita:c", filas[0].act.ID)
	assert.Equal(t, "visita:a", filas[1].act.ID, "en empate exacto gana el id menor")
	assert.Equal(t, "visita:b", filas[2].act.ID)
}
