//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/app"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

var buildNow = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

func universoRow(clienteID int, seg, saldo string) outbound.ClienteUniverso {
	return outbound.ClienteUniverso{
		ClienteID:         clienteID,
		Nombre:            "Cliente Prueba",
		Telefono:          "238 111 2222",
		Segmento:          mustSeg(seg),
		Saldo:             decimal.RequireFromString(saldo),
		PorLiquidarPct:    decimal.Zero,
		FechaUltimaCompra: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}
}

func newTestService(reader outbound.UniversoReader, repo outbound.CohorteRepo, cfg app.Config) *app.Service {
	return app.NewService(reader, repo, fixedClock{now: buildNow}, nil, cfg)
}

func TestConstruirCohorte_Success(t *testing.T) {
	t.Parallel()
	reader := &fakeUniversoReader{universo: []outbound.ClienteUniverso{
		universoRow(101, "recien_liquidado", "0"),
		universoRow(102, "por_liquidar_hueco", "1200.50"),
	}}
	repo := &fakeCohorteRepo{}
	svc := newTestService(reader, repo, app.Config{ControlPct: 50})

	res, err := svc.ConstruirCohorte(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, res.Procesados)
	assert.Equal(t, buildNow, res.CohorteFecha)
	require.Len(t, repo.upserted, 2)
	for _, c := range repo.upserted {
		assert.Equal(t, buildNow, c.CohorteFecha())
		assert.False(t, c.FueContactado())
	}
}

func TestConstruirCohorte_ControlSplitStable(t *testing.T) {
	t.Parallel()
	var rows []outbound.ClienteUniverso
	for id := 1; id <= 400; id++ {
		rows = append(rows, universoRow(id, "recien_liquidado", "0"))
	}
	repo := &fakeCohorteRepo{}
	svc := newTestService(&fakeUniversoReader{universo: rows}, repo, app.Config{ControlPct: 50})

	_, err := svc.ConstruirCohorte(context.Background())
	require.NoError(t, err)

	control := 0
	for _, c := range repo.upserted {
		if c.EnControl() {
			control++
		}
	}
	// ~50% split, generous band.
	assert.Greater(t, control, 400*30/100)
	assert.Less(t, control, 400*70/100)
}

func TestConstruirCohorte_PreservesExistingFlags(t *testing.T) {
	t.Parallel()
	reader := &fakeUniversoReader{universo: []outbound.ClienteUniverso{
		universoRow(101, "recien_liquidado", "0"),
		universoRow(102, "recien_liquidado", "0"),
	}}
	// 101 is stored as NOT control and already contacted. With ControlPct=100 the
	// deterministic default would make it control — the rebuild must keep the
	// stored flags instead.
	repo := &fakeCohorteRepo{
		controlFlags:    map[int]bool{101: false},
		contactadoFlags: map[int]bool{101: true},
	}
	svc := newTestService(reader, repo, app.Config{ControlPct: 100}) // default: all-control

	_, err := svc.ConstruirCohorte(context.Background())
	require.NoError(t, err)

	byID := map[int]bool{}
	contact := map[int]bool{}
	for _, c := range repo.upserted {
		byID[c.ClienteID()] = c.EnControl()
		contact[c.ClienteID()] = c.FueContactado()
	}
	assert.False(t, byID[101], "stored control flag preserved despite ControlPct=100")
	assert.True(t, contact[101], "existing contactado flag preserved")
	assert.True(t, byID[102], "new cliente gets deterministic assignment (pct=100 → control)")
	assert.False(t, contact[102], "new cliente is not contacted")
}

func TestConstruirCohorte_ReaderError(t *testing.T) {
	t.Parallel()
	svc := newTestService(&fakeUniversoReader{err: errors.New("boom")}, &fakeCohorteRepo{}, app.Config{})
	_, err := svc.ConstruirCohorte(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "universo")
}

func TestConstruirCohorte_ControlFlagsError(t *testing.T) {
	t.Parallel()
	repo := &fakeCohorteRepo{controlErr: errors.New("boom")}
	svc := newTestService(&fakeUniversoReader{universo: []outbound.ClienteUniverso{universoRow(1, "recien_liquidado", "0")}}, repo, app.Config{})
	_, err := svc.ConstruirCohorte(context.Background())
	require.Error(t, err)
}

func TestConstruirCohorte_ContactadoFlagsError(t *testing.T) {
	t.Parallel()
	repo := &fakeCohorteRepo{contactadoErr: errors.New("boom")}
	svc := newTestService(&fakeUniversoReader{universo: []outbound.ClienteUniverso{universoRow(1, "recien_liquidado", "0")}}, repo, app.Config{})
	_, err := svc.ConstruirCohorte(context.Background())
	require.Error(t, err)
}

func TestConstruirCohorte_UpsertError(t *testing.T) {
	t.Parallel()
	repo := &fakeCohorteRepo{upsertErr: errors.New("boom")}
	svc := newTestService(&fakeUniversoReader{universo: []outbound.ClienteUniverso{universoRow(1, "recien_liquidado", "0")}}, repo, app.Config{})
	_, err := svc.ConstruirCohorte(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "guardar")
}

func TestConstruirCohorte_InvalidUniverseRowFailsFast(t *testing.T) {
	t.Parallel()
	bad := universoRow(0, "recien_liquidado", "0") // ClienteID 0 → domain rejects
	repo := &fakeCohorteRepo{}
	svc := newTestService(&fakeUniversoReader{universo: []outbound.ClienteUniverso{bad}}, repo, app.Config{})
	_, err := svc.ConstruirCohorte(context.Background())
	require.Error(t, err)
	assert.Empty(t, repo.upserted, "nothing persisted on build failure")
}

func TestConstruirEnSegundoPlano_SingleFlight(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	reader := &fakeUniversoReader{
		universo: []outbound.ClienteUniverso{universoRow(1, "recien_liquidado", "0")},
		gate:     gate,
	}
	repo := &fakeCohorteRepo{}
	svc := newTestService(reader, repo, app.Config{})

	require.True(t, svc.ConstruirEnSegundoPlano(), "first call starts the build")
	// Wait until the goroutine is parked on the gate.
	require.Eventually(t, func() bool { return reader.Calls() == 1 }, time.Second, time.Millisecond)
	assert.False(t, svc.ConstruirEnSegundoPlano(), "second call is rejected while the first runs")

	close(gate)
	require.Eventually(t, func() bool { return repo.upsertedCount() == 1 }, time.Second, time.Millisecond)
}
