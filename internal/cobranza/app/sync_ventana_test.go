//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/app"
)

// Estas pruebas fijan la regla que faltaba: `desde` deja de ser opcional y el
// default lo pone el servidor, UNA sola vez, para TODOS los canales.
//
// El defecto que cubren: mientras cada endpoint decidía por su cuenta qué
// hacer con un `desde` vacío, el sync y el inventario podían mirar ventanas
// distintas. El reconciliador compara un conjunto contra el otro, así que dos
// ventanas distintas significan que declara fantasma justo lo que el sync
// acaba de entregar.

// TestResolveSyncDesde_DefaultDeServidor fija el contrato de la función que
// resuelve la ventana: zero → now - DefaultVentanaDias; valor explícito →
// se respeta tal cual.
func TestResolveSyncDesde_DefaultDeServidor(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 15, 10, 30, 0, 0, time.UTC)
	clock := fixedClock{T: now}

	got := app.ResolveSyncDesde(time.Time{}, clock)
	assert.False(t, got.IsZero(), "un desde vacío no puede seguir significando «sin ventana»")
	assert.True(t, now.AddDate(0, 0, -app.DefaultVentanaDias).Equal(got),
		"el default es now - DefaultVentanaDias; got=%s", got)

	explicito := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	assert.True(t, explicito.Equal(app.ResolveSyncDesde(explicito, clock)),
		"un desde explícito manda sobre el default")
}

// TestVentanaDesde_LosSeisCanalesResuelvenLaMismaVentana es la prueba que
// impide que los canales vuelvan a separarse. Llama a los seis puntos de
// entrada con `desde` ausente y exige que los seis lleguen al repo con
// exactamente el mismo instante, distinto de zero.
func TestVentanaDesde_LosSeisCanalesResuelvenLaMismaVentana(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 10, 30, 0, 0, time.UTC)
	esperada := now.AddDate(0, 0, -app.DefaultVentanaDias)

	saldos := newFakeSaldosRepo()
	pagos := newFakePagosRepo()
	ventas := &fakeVentasRepo{}
	svc := app.NewService(saldos, pagos, ventas, fixedClock{T: now}, nil, nil, nil, nil, nil, nil)

	pagosR := &fakePagosReconcileRepo{}
	saldosR := &fakeSaldosReconcileRepo{}
	svc.WithReconcilePorts(pagosR, saldosR)

	_, err := svc.SyncPagosPorZona(ctx, 21563, time.Time{}, 0, 1000, nil)
	require.NoError(t, err)
	_, err = svc.SyncVentasPorZona(ctx, 21563, time.Time{}, 0, 1000, nil)
	require.NoError(t, err)
	_, err = svc.DigestPagosPorZona(ctx, 21563, time.Time{})
	require.NoError(t, err)
	_, _, err = svc.ListIDsPagosPorZona(ctx, 21563, 0, 1000, time.Time{})
	require.NoError(t, err)
	_, err = svc.DigestSaldosPorZona(ctx, 21563, time.Time{})
	require.NoError(t, err)
	_, _, err = svc.ListIDsSaldosPorZona(ctx, 21563, 0, 1000, time.Time{})
	require.NoError(t, err)

	canales := map[string]time.Time{
		"sync/pagos":    pagos.lastSyncCall.desde,
		"sync/ventas":   ventas.lastDesde,
		"pagos/digest":  pagosR.lastDesde,
		"saldos/digest": saldosR.lastDesde,
	}
	for nombre, got := range canales {
		assert.False(t, got.IsZero(), "%s: la ventana no puede llegar vacía al repo", nombre)
		assert.True(t, esperada.Equal(got),
			"%s: esperaba la ventana por defecto %s, llegó %s", nombre, esperada, got)
	}
	// ListIDs comparte el campo lastDesde con Digest en los fakes; se lee al
	// final para que el valor sea el de la última llamada (la de ListIDs).
	assert.True(t, esperada.Equal(pagosR.lastDesde), "pagos/ids debe usar la misma ventana")
	assert.True(t, esperada.Equal(saldosR.lastDesde), "saldos/ids debe usar la misma ventana")
}

// TestVentanaDesde_ExplicitoSeRespetaEnTodosLosCanales comprueba la otra
// mitad: cuando el cliente sí manda `desde`, ningún canal lo pisa con el
// default.
func TestVentanaDesde_ExplicitoSeRespetaEnTodosLosCanales(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 10, 30, 0, 0, time.UTC)
	desde := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	saldos := newFakeSaldosRepo()
	pagos := newFakePagosRepo()
	ventas := &fakeVentasRepo{}
	svc := app.NewService(saldos, pagos, ventas, fixedClock{T: now}, nil, nil, nil, nil, nil, nil)
	pagosR := &fakePagosReconcileRepo{}
	saldosR := &fakeSaldosReconcileRepo{}
	svc.WithReconcilePorts(pagosR, saldosR)

	_, err := svc.SyncPagosPorZona(ctx, 21563, time.Time{}, 0, 1000, &desde)
	require.NoError(t, err)
	_, err = svc.SyncVentasPorZona(ctx, 21563, time.Time{}, 0, 1000, &desde)
	require.NoError(t, err)
	_, _, err = svc.ListIDsPagosPorZona(ctx, 21563, 0, 1000, desde)
	require.NoError(t, err)
	_, _, err = svc.ListIDsSaldosPorZona(ctx, 21563, 0, 1000, desde)
	require.NoError(t, err)

	assert.True(t, desde.Equal(pagos.lastSyncCall.desde), "sync/pagos debe respetar el desde explícito")
	assert.True(t, desde.Equal(ventas.lastDesde), "sync/ventas debe respetar el desde explícito")
	assert.True(t, desde.Equal(pagosR.lastDesde), "pagos/ids debe respetar el desde explícito")
	assert.True(t, desde.Equal(saldosR.lastDesde), "saldos/ids debe respetar el desde explícito")
}
