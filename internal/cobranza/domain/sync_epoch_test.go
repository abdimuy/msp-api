//nolint:misspell // Spanish domain vocabulary (recurso, zona, ventas, pagos) per project convention.
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// TestEpochEfectivo cubre la matriz completa de presencia de filas: sin
// filas, solo global, solo zona, ambas, y una fila de OTRA zona que no debe
// contarse. El epoch efectivo es la suma global + zona porque un bump global
// tiene que mover a todas las zonas incluso si esa zona ya traía un bump
// propio.
func TestEpochEfectivo(t *testing.T) {
	t.Parallel()

	const zona = 12271

	tests := []struct {
		name string
		rows []domain.EpochRow
		want int
	}{
		{
			name: "sin filas devuelve 0",
			rows: nil,
			want: 0,
		},
		{
			name: "slice vacio devuelve 0",
			rows: []domain.EpochRow{},
			want: 0,
		},
		{
			name: "solo fila global",
			rows: []domain.EpochRow{
				{ZonaClienteID: domain.ZonaEpochGlobal, Epoch: 3},
			},
			want: 3,
		},
		{
			name: "solo fila de la zona",
			rows: []domain.EpochRow{
				{ZonaClienteID: zona, Epoch: 5},
			},
			want: 5,
		},
		{
			name: "global y zona se suman",
			rows: []domain.EpochRow{
				{ZonaClienteID: domain.ZonaEpochGlobal, Epoch: 3},
				{ZonaClienteID: zona, Epoch: 5},
			},
			want: 8,
		},
		{
			name: "fila de otra zona se ignora",
			rows: []domain.EpochRow{
				{ZonaClienteID: domain.ZonaEpochGlobal, Epoch: 2},
				{ZonaClienteID: 99999, Epoch: 40},
			},
			want: 2,
		},
		{
			name: "zona inexistente cae al global",
			rows: []domain.EpochRow{
				{ZonaClienteID: domain.ZonaEpochGlobal, Epoch: 7},
			},
			want: 7,
		},
		{
			name: "ambas en 0 devuelve 0",
			rows: []domain.EpochRow{
				{ZonaClienteID: domain.ZonaEpochGlobal, Epoch: 0},
				{ZonaClienteID: zona, Epoch: 0},
			},
			want: 0,
		},
		{
			name: "orden de las filas no importa",
			rows: []domain.EpochRow{
				{ZonaClienteID: zona, Epoch: 5},
				{ZonaClienteID: domain.ZonaEpochGlobal, Epoch: 3},
			},
			want: 8,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, domain.EpochEfectivo(tc.rows, zona))
		})
	}
}

// TestEpochEfectivo_ZonaCeroNoCuentaDosVeces protege el borde donde el ID de
// zona pedido coincide con el sentinel global: la fila global debe sumarse
// una sola vez, no duplicarse.
func TestEpochEfectivo_ZonaCeroNoCuentaDosVeces(t *testing.T) {
	t.Parallel()

	rows := []domain.EpochRow{{ZonaClienteID: domain.ZonaEpochGlobal, Epoch: 4}}

	assert.Equal(t, 4, domain.EpochEfectivo(rows, domain.ZonaEpochGlobal))
}

// TestEpochEfectivo_BumpGlobalMueveTodasLasZonas expresa la regla de negocio
// que justifica sumar en vez de tomar el máximo: subir el global mueve el
// epoch de una zona que ya tenía bump propio.
func TestEpochEfectivo_BumpGlobalMueveTodasLasZonas(t *testing.T) {
	t.Parallel()

	const zonaA, zonaB = 12271, 12272

	antes := []domain.EpochRow{
		{ZonaClienteID: domain.ZonaEpochGlobal, Epoch: 1},
		{ZonaClienteID: zonaA, Epoch: 9},
	}
	despues := []domain.EpochRow{
		{ZonaClienteID: domain.ZonaEpochGlobal, Epoch: 2}, // bump global
		{ZonaClienteID: zonaA, Epoch: 9},
	}

	assert.Greater(t, domain.EpochEfectivo(despues, zonaA), domain.EpochEfectivo(antes, zonaA),
		"un bump global debe mover la zona que ya tenia bump propio")
	assert.Greater(t, domain.EpochEfectivo(despues, zonaB), domain.EpochEfectivo(antes, zonaB),
		"un bump global debe mover tambien una zona sin fila propia")
}

// TestEpochEfectivo_BumpDeZonaNoMueveOtrasZonas es la contraparte: un bump
// por zona sólo afecta a esa zona.
func TestEpochEfectivo_BumpDeZonaNoMueveOtrasZonas(t *testing.T) {
	t.Parallel()

	const zonaA, zonaB = 12271, 12272

	rows := []domain.EpochRow{
		{ZonaClienteID: domain.ZonaEpochGlobal, Epoch: 1},
		{ZonaClienteID: zonaA, Epoch: 4},
	}

	assert.Equal(t, 5, domain.EpochEfectivo(rows, zonaA))
	assert.Equal(t, 1, domain.EpochEfectivo(rows, zonaB), "zonaB solo ve el global")
}

// TestRecursoSync_IsValid fija el conjunto canónico de recursos: los dos
// endpoints de sync que exponen sync_epoch, y nada más.
func TestRecursoSync_IsValid(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.RecursoSyncVentas.IsValid())
	assert.True(t, domain.RecursoSyncPagos.IsValid())
	assert.False(t, domain.RecursoSync("saldos").IsValid())
	assert.False(t, domain.RecursoSync("").IsValid())

	assert.Equal(t, "ventas", domain.RecursoSyncVentas.String())
	assert.Equal(t, "pagos", domain.RecursoSyncPagos.String())
}
