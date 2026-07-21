//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/app"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

var envioNow = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

// demoGobernador builds a Gobernador using the demo profile (always permits,
// unless the daily cap is reached) with a fixed seed for determinism.
func demoGobernador() *app.Gobernador {
	return app.NewGobernador(app.PerfilConfig(app.PerfilDemo), rand.New(rand.NewSource(1)))
}

// tratamientoRow builds a treatment (EnControl=false) cohorte row.
func tratamientoRow(clienteID int, seg domain.Segmento, nombre string) *domain.CohorteCliente {
	return domain.HydrateCohorteCliente(domain.HydrateCohorteClienteParams{
		ID:                    uuid.New(),
		ClienteID:             clienteID,
		Nombre:                nombre,
		Telefono:              "238 111 2222",
		Segmento:              seg,
		EnControl:             false,
		FueContactado:         false,
		CohorteFecha:          envioNow,
		FechaUltimaCompraBase: envioNow.AddDate(0, -2, 0),
		Saldo:                 decimal.Zero,
		PorLiquidarPct:        decimal.Zero,
		CreatedAt:             envioNow,
		UpdatedAt:             envioNow,
	})
}

// ─── EncolarCohorte ─────────────────────────────────────────────────────────

func TestEncolarCohorte_SoloTratamiento(t *testing.T) {
	t.Parallel()
	repo := &fakeCohorteRepo{lastListParm: outbound.ListarCohorteParams{}}
	repo.listResult = []*domain.CohorteCliente{
		tratamientoRow(101, domain.SegmentoRecienLiquidado, "Minerva López"),
	}
	mensajeRepo := &fakeMensajeRepo{}
	svc := newTestService(&fakeUniversoReader{}, repo, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	res, err := svc.EncolarCohorte(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, res.Encolados)
	assert.True(t, repo.lastListParm.SoloTratamiento, "must query only the treatment group")

	require.Len(t, mensajeRepo.insertadosSnapshot(), 1)
	got := mensajeRepo.insertadosSnapshot()[0]
	assert.Equal(t, 101, got.ClienteID())
	assert.NotEmpty(t, got.Cuerpo())
	assert.Contains(t, got.Cuerpo(), "Minerva López")
	assert.Equal(t, domain.EstadoEncolado, got.Estado())
}

func TestEncolarCohorte_GeneraCuerpoPorSegmento(t *testing.T) {
	t.Parallel()
	repo := &fakeCohorteRepo{}
	repo.listResult = []*domain.CohorteCliente{
		tratamientoRow(201, domain.SegmentoRecienLiquidado, "Rogelio Hernández"),
		tratamientoRow(202, domain.SegmentoPorLiquidarHueco, "Araceli Domínguez"),
	}
	mensajeRepo := &fakeMensajeRepo{}
	svc := newTestService(&fakeUniversoReader{}, repo, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	res, err := svc.EncolarCohorte(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, res.Encolados)
	for _, m := range mensajeRepo.insertadosSnapshot() {
		assert.NotEmpty(t, m.Cuerpo())
	}
}

func TestEncolarCohorte_Idempotente(t *testing.T) {
	t.Parallel()
	repo := &fakeCohorteRepo{}
	repo.listResult = []*domain.CohorteCliente{
		tratamientoRow(301, domain.SegmentoRecienLiquidado, "Cliente Uno"),
	}
	mensajeRepo := &fakeMensajeRepo{}
	svc := newTestService(&fakeUniversoReader{}, repo, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	_, err := svc.EncolarCohorte(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, mensajeRepo.insertadosCount())

	res2, err := svc.EncolarCohorte(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, res2.Encolados, "second run must not duplicate an already-queued cliente")
	assert.Equal(t, 1, mensajeRepo.insertadosCount())
}

func TestEncolarCohorte_ClientesConMensajeError(t *testing.T) {
	t.Parallel()
	repo := &fakeCohorteRepo{}
	mensajeRepo := &fakeMensajeRepo{clientesConMensajeErr: errors.New("boom")}
	svc := newTestService(&fakeUniversoReader{}, repo, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	_, err := svc.EncolarCohorte(context.Background())
	require.Error(t, err)
}

func TestEncolarCohorte_InsertarError(t *testing.T) {
	t.Parallel()
	repo := &fakeCohorteRepo{}
	repo.listResult = []*domain.CohorteCliente{tratamientoRow(401, domain.SegmentoRecienLiquidado, "Cliente")}
	mensajeRepo := &fakeMensajeRepo{insertarErr: errors.New("boom")}
	svc := newTestService(&fakeUniversoReader{}, repo, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	_, err := svc.EncolarCohorte(context.Background())
	require.Error(t, err)
}

func TestEncolarCohorte_CohorteListError(t *testing.T) {
	t.Parallel()
	repo := &fakeCohorteRepo{listErr: errors.New("boom")}
	mensajeRepo := &fakeMensajeRepo{}
	svc := newTestService(&fakeUniversoReader{}, repo, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	_, err := svc.EncolarCohorte(context.Background())
	require.Error(t, err)
}

// ─── DrenarCola ─────────────────────────────────────────────────────────────

func mensajeEncolado(t *testing.T, clienteID int, seg domain.Segmento) *domain.Mensaje {
	t.Helper()
	m, err := domain.CrearMensaje(domain.CrearMensajeParams{
		ClienteID: clienteID,
		Segmento:  seg,
		Telefono:  "238 555 6666",
		Cuerpo:    "cuerpo de prueba",
		Now:       envioNow,
	})
	require.NoError(t, err)
	return m
}

func TestDrenarCola_AutoSendOn_EnviaTodos(t *testing.T) {
	t.Parallel()
	m1 := mensajeEncolado(t, 501, domain.SegmentoRecienLiquidado)
	m2 := mensajeEncolado(t, 502, domain.SegmentoPorLiquidarHueco)
	mensajeRepo := &fakeMensajeRepo{insertados: []*domain.Mensaje{m1, m2}}
	cohorteRepo := &fakeCohorteRepo{}
	sender := &fakeSender{kind: domain.SenderSimulado}
	svc := newTestService(&fakeUniversoReader{}, cohorteRepo, app.Config{}).
		WithCanal(mensajeRepo, sender, app.NewOpener(), demoGobernador(), true)

	res, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, 2, res.Enviados)
	assert.Equal(t, 0, res.Fallidos)
	assert.Equal(t, 0, res.Saltados)

	assert.Equal(t, 2, sender.enviadoCount())
	assert.Equal(t, 2, cohorteRepo.contactadosCount())
	assert.True(t, cohorteRepo.wasContactado(501))
	assert.True(t, cohorteRepo.wasContactado(502))
	assert.Equal(t, domain.EstadoEnviado, m1.Estado())
	assert.Equal(t, domain.SenderSimulado, m1.SenderKind())
	assert.False(t, m1.EnviadoEn().IsZero())
}

func TestDrenarCola_AutoSendOff_NoEnviaNada(t *testing.T) {
	t.Parallel()
	m1 := mensajeEncolado(t, 601, domain.SegmentoRecienLiquidado)
	mensajeRepo := &fakeMensajeRepo{insertados: []*domain.Mensaje{m1}}
	cohorteRepo := &fakeCohorteRepo{}
	sender := &fakeSender{}
	svc := newTestService(&fakeUniversoReader{}, cohorteRepo, app.Config{}).
		WithCanal(mensajeRepo, sender, app.NewOpener(), demoGobernador(), false)

	res, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Enviados)
	assert.Equal(t, 1, res.Saltados)
	assert.Equal(t, 0, sender.enviadoCount())
	assert.Equal(t, 0, cohorteRepo.contactadosCount())
	assert.Equal(t, domain.EstadoEncolado, m1.Estado(), "message must remain queued")
}

func TestDrenarCola_NoPendientes(t *testing.T) {
	t.Parallel()
	mensajeRepo := &fakeMensajeRepo{}
	svc := newTestService(&fakeUniversoReader{}, &fakeCohorteRepo{}, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	res, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, app.DrenarResult{}, res)
}

func TestDrenarCola_GobernadorNiegaTopeDiario_ParaLaTanda(t *testing.T) {
	t.Parallel()
	m1 := mensajeEncolado(t, 701, domain.SegmentoRecienLiquidado)
	m2 := mensajeEncolado(t, 702, domain.SegmentoRecienLiquidado)
	mensajeRepo := &fakeMensajeRepo{insertados: []*domain.Mensaje{m1, m2}}
	cohorteRepo := &fakeCohorteRepo{}
	sender := &fakeSender{}

	cfg := app.PerfilConfig(app.PerfilDemo)
	cfg.TopeDiario = 0 // exhausted from the start
	gobernador := app.NewGobernador(cfg, rand.New(rand.NewSource(1)))

	svc := newTestService(&fakeUniversoReader{}, cohorteRepo, app.Config{}).
		WithCanal(mensajeRepo, sender, app.NewOpener(), gobernador, true)

	res, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Enviados)
	assert.Equal(t, 2, res.Saltados)
	assert.Equal(t, 0, sender.enviadoCount())
	assert.Equal(t, 0, cohorteRepo.contactadosCount())
}

func TestDrenarCola_SenderFalla_MensajeFallido_NoContactado(t *testing.T) {
	t.Parallel()
	m1 := mensajeEncolado(t, 801, domain.SegmentoRecienLiquidado)
	mensajeRepo := &fakeMensajeRepo{insertados: []*domain.Mensaje{m1}}
	cohorteRepo := &fakeCohorteRepo{}
	sender := &fakeSender{failClienteIDs: map[int]bool{801: true}}
	svc := newTestService(&fakeUniversoReader{}, cohorteRepo, app.Config{}).
		WithCanal(mensajeRepo, sender, app.NewOpener(), demoGobernador(), true)

	res, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Enviados)
	assert.Equal(t, 1, res.Fallidos)
	assert.Equal(t, domain.EstadoFallido, m1.Estado())
	assert.NotEmpty(t, m1.Motivo())
	assert.Equal(t, 0, cohorteRepo.contactadosCount(), "a failed send must never mark FUE_CONTACTADO")
	assert.Empty(t, m1.SenderKind().String())
}

func TestDrenarCola_ListarPendientesError(t *testing.T) {
	t.Parallel()
	mensajeRepo := &fakeMensajeRepo{listarPendientesErr: errors.New("boom")}
	svc := newTestService(&fakeUniversoReader{}, &fakeCohorteRepo{}, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	_, err := svc.DrenarCola(context.Background(), 10)
	require.Error(t, err)
}

func TestDrenarCola_ContarEnviadosHoyError(t *testing.T) {
	t.Parallel()
	m1 := mensajeEncolado(t, 901, domain.SegmentoRecienLiquidado)
	mensajeRepo := &fakeMensajeRepo{insertados: []*domain.Mensaje{m1}, contarEnviadosHoyErr: errors.New("boom")}
	svc := newTestService(&fakeUniversoReader{}, &fakeCohorteRepo{}, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	_, err := svc.DrenarCola(context.Background(), 10)
	require.Error(t, err)
}

func TestDrenarCola_ActualizarError(t *testing.T) {
	t.Parallel()
	m1 := mensajeEncolado(t, 902, domain.SegmentoRecienLiquidado)
	mensajeRepo := &fakeMensajeRepo{insertados: []*domain.Mensaje{m1}, actualizarErr: errors.New("boom")}
	svc := newTestService(&fakeUniversoReader{}, &fakeCohorteRepo{}, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	_, err := svc.DrenarCola(context.Background(), 10)
	require.Error(t, err)
}

func TestDrenarCola_MarcarContactadoError(t *testing.T) {
	t.Parallel()
	m1 := mensajeEncolado(t, 903, domain.SegmentoRecienLiquidado)
	mensajeRepo := &fakeMensajeRepo{insertados: []*domain.Mensaje{m1}}
	cohorteRepo := &fakeCohorteRepo{marcarContactadoErr: errors.New("boom")}
	svc := newTestService(&fakeUniversoReader{}, cohorteRepo, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	_, err := svc.DrenarCola(context.Background(), 10)
	require.Error(t, err)
}

// ─── EncolarEnSegundoPlano ──────────────────────────────────────────────────

func TestEncolarEnSegundoPlano_RunsAndCompletes(t *testing.T) {
	t.Parallel()
	repo := &fakeCohorteRepo{}
	repo.listResult = []*domain.CohorteCliente{tratamientoRow(1001, domain.SegmentoRecienLiquidado, "Cliente Prueba")}
	mensajeRepo := &fakeMensajeRepo{}
	svc := newTestService(&fakeUniversoReader{}, repo, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	require.True(t, svc.EncolarEnSegundoPlano())
	require.Eventually(t, func() bool { return mensajeRepo.insertadosCount() == 1 }, time.Second, time.Millisecond)
}

func TestEncolarEnSegundoPlano_SingleFlight(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	repo := &fakeCohorteRepo{listGate: gate}
	repo.listResult = []*domain.CohorteCliente{tratamientoRow(1002, domain.SegmentoRecienLiquidado, "Cliente Prueba")}
	mensajeRepo := &fakeMensajeRepo{}
	svc := newTestService(&fakeUniversoReader{}, repo, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	require.True(t, svc.EncolarEnSegundoPlano(), "first call starts the run")
	require.Eventually(t, func() bool { return repo.ListCalls() == 1 }, time.Second, time.Millisecond)
	assert.False(t, svc.EncolarEnSegundoPlano(), "second call is rejected while the first runs")

	close(gate)
	require.Eventually(t, func() bool { return mensajeRepo.insertadosCount() == 1 }, time.Second, time.Millisecond)
}

func TestEncolarCohorte_SegmentoInvalidoEnCohorte(t *testing.T) {
	t.Parallel()
	// Built directly with Hydrate to bypass domain validation — simulates a
	// corrupted MSP_RX_COHORTE row reaching the app layer.
	bad := domain.HydrateCohorteCliente(domain.HydrateCohorteClienteParams{
		ID:           uuid.New(),
		ClienteID:    1003,
		Nombre:       "Cliente Corrupto",
		Telefono:     "238 000 0000",
		Segmento:     domain.Segmento("no_existe"),
		CohorteFecha: envioNow,
		CreatedAt:    envioNow,
		UpdatedAt:    envioNow,
	})
	repo := &fakeCohorteRepo{listResult: []*domain.CohorteCliente{bad}}
	mensajeRepo := &fakeMensajeRepo{}
	svc := newTestService(&fakeUniversoReader{}, repo, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	_, err := svc.EncolarCohorte(context.Background())
	require.Error(t, err)
	assert.Empty(t, mensajeRepo.insertadosSnapshot(), "nothing persisted when a row fails to build")
}

// ─── ListarMensajes ─────────────────────────────────────────────────────────

func TestListarMensajes_FiltraPorEstadoYSegmento(t *testing.T) {
	t.Parallel()
	mensajeRepo := &fakeMensajeRepo{insertados: []*domain.Mensaje{
		mensajeEncolado(t, 1, domain.SegmentoRecienLiquidado),
	}}
	svc := newTestService(&fakeUniversoReader{}, &fakeCohorteRepo{}, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	got, err := svc.ListarMensajes(context.Background(), app.ListarMensajesParams{
		Estado:   "encolado",
		Segmento: "recien_liquidado",
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestListarMensajes_EstadoInvalido(t *testing.T) {
	t.Parallel()
	mensajeRepo := &fakeMensajeRepo{}
	svc := newTestService(&fakeUniversoReader{}, &fakeCohorteRepo{}, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	_, err := svc.ListarMensajes(context.Background(), app.ListarMensajesParams{Estado: "no_existe"})
	require.ErrorIs(t, err, domain.ErrEstadoMensajeInvalido)
}

func TestListarMensajes_SegmentoInvalido(t *testing.T) {
	t.Parallel()
	mensajeRepo := &fakeMensajeRepo{}
	svc := newTestService(&fakeUniversoReader{}, &fakeCohorteRepo{}, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	_, err := svc.ListarMensajes(context.Background(), app.ListarMensajesParams{Segmento: "no_existe"})
	require.ErrorIs(t, err, domain.ErrSegmentoInvalido)
}

func TestListarMensajes_RepoError(t *testing.T) {
	t.Parallel()
	mensajeRepo := &fakeMensajeRepo{listarErr: errors.New("boom")}
	svc := newTestService(&fakeUniversoReader{}, &fakeCohorteRepo{}, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	_, err := svc.ListarMensajes(context.Background(), app.ListarMensajesParams{})
	require.Error(t, err)
}
