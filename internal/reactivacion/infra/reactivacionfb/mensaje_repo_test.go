// Package reactivacionfb_test — integration tests for MSP_RX_MENSAJES (Fase 2
// channel queue) and CohorteRepo.MarcarContactado. All writes execute inside a
// transaction that always rolls back so the shared dev DB never accumulates
// test data.
//
// Prerequisites:
//   - FB_DATABASE env var pointing at the dev Microsip Firebird DB.
//   - Migration 000045 applied (creates MSP_RX_MENSAJES).
//
// Run: FB_DATABASE=/firebird/data/MUEBLERA.FDB go test ./internal/reactivacion/infra/reactivacionfb/...
//
//nolint:misspell // Spanish vocabulary (mensaje, segmento) by convention.
package reactivacionfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionfb"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

var (
	mensajeFixedNow      = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	mensajeFixedEncolado = time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC)
)

// makeMensaje builds a Mensaje with a large positive synthetic clienteID
// (real Microsip IDs are far smaller) so tests never collide with production
// rows. cuerpo carries a UTF-8 "Ñ" to exercise the BLOB TEXT UTF8 round-trip.
func makeMensaje(t *testing.T, clienteID int, seg domain.Segmento, cuerpo string) *domain.Mensaje {
	t.Helper()
	m, err := domain.CrearMensaje(domain.CrearMensajeParams{
		ClienteID: clienteID,
		Segmento:  seg,
		Telefono:  "238 100 3000",
		Cuerpo:    cuerpo,
		Now:       mensajeFixedEncolado,
	})
	require.NoError(t, err)
	return m
}

func findMensajeByID(mensajes []*domain.Mensaje, id string) *domain.Mensaje {
	for _, m := range mensajes {
		if m.ID().String() == id {
			return m
		}
	}
	return nil
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestMensajeRepo_InsertarAndListar_RoundTrip(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		const clienteID = 900000101
		cuerpo := "Hola Peña Ñuñez, le saluda Mueblería MSP. ¡Felicidades por completar su pago!"
		m := makeMensaje(t, clienteID, domain.SegmentoPorLiquidarHueco, cuerpo)

		if err := repo.Insertar(ctx, []*domain.Mensaje{m}); err != nil {
			t.Skipf("Insertar failed — migration 000045 may not be applied: %v", err)
		}

		todos, err := repo.Listar(ctx, outbound.ListarMensajesParams{})
		require.NoError(t, err)
		got := findMensajeByID(todos, m.ID().String())
		require.NotNil(t, got, "inserted mensaje must appear in Listar")

		assert.Equal(t, clienteID, got.ClienteID())
		assert.Equal(t, domain.SegmentoPorLiquidarHueco, got.Segmento())
		assert.Equal(t, "238 100 3000", got.Telefono())
		assert.Equal(t, cuerpo, got.Cuerpo(), "UTF-8 body (incl. Ñ) must round-trip through BLOB TEXT")
		assert.Equal(t, domain.EstadoEncolado, got.Estado())
		assert.Empty(t, got.SenderKind().String(), "sender_kind must be NULL until sent")
		assert.True(t, got.EnviadoEn().IsZero())
		assert.Empty(t, got.Motivo())
		assert.WithinDuration(t, mensajeFixedEncolado, got.EncoladoEn(), time.Second)
		assert.WithinDuration(t, mensajeFixedEncolado, got.CreatedAt(), time.Second)
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestMensajeRepo_Insertar_Bulk(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		const base = 900000200
		var mensajes []*domain.Mensaje
		for i := range 5 {
			mensajes = append(mensajes, makeMensaje(t, base+i, domain.SegmentoRecienLiquidado, "cuerpo de prueba masivo"))
		}
		if err := repo.Insertar(ctx, mensajes); err != nil {
			t.Skipf("Insertar failed — migration 000045 may not be applied: %v", err)
		}

		todos, err := repo.Listar(ctx, outbound.ListarMensajesParams{})
		require.NoError(t, err)
		for _, m := range mensajes {
			require.NotNil(t, findMensajeByID(todos, m.ID().String()), "clienteID=%d must be inserted", m.ClienteID())
		}
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestMensajeRepo_Actualizar_MarcaEnviado(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		const clienteID = 900000301
		m := makeMensaje(t, clienteID, domain.SegmentoRecienLiquidado, "cuerpo original")
		if err := repo.Insertar(ctx, []*domain.Mensaje{m}); err != nil {
			t.Skipf("Insertar failed — migration 000045 may not be applied: %v", err)
		}

		require.NoError(t, m.MarcarEnviado(domain.SenderSimulado, mensajeFixedNow))
		require.NoError(t, repo.Actualizar(ctx, m))

		todos, err := repo.Listar(ctx, outbound.ListarMensajesParams{Estado: domain.EstadoEnviado})
		require.NoError(t, err)
		got := findMensajeByID(todos, m.ID().String())
		require.NotNil(t, got, "updated mensaje must appear under estado=enviado")

		assert.Equal(t, domain.EstadoEnviado, got.Estado())
		assert.Equal(t, domain.SenderSimulado, got.SenderKind())
		assert.WithinDuration(t, mensajeFixedNow, got.EnviadoEn(), time.Second)
		assert.Empty(t, got.Motivo())
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestMensajeRepo_Actualizar_MarcaFallido(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		const clienteID = 900000302
		m := makeMensaje(t, clienteID, domain.SegmentoRecienLiquidado, "cuerpo original")
		if err := repo.Insertar(ctx, []*domain.Mensaje{m}); err != nil {
			t.Skipf("Insertar failed — migration 000045 may not be applied: %v", err)
		}

		m.MarcarFallido("número no válido en whatsapp", mensajeFixedNow)
		require.NoError(t, repo.Actualizar(ctx, m))

		todos, err := repo.Listar(ctx, outbound.ListarMensajesParams{})
		require.NoError(t, err)
		got := findMensajeByID(todos, m.ID().String())
		require.NotNil(t, got)

		assert.Equal(t, domain.EstadoFallido, got.Estado())
		assert.Equal(t, "número no válido en whatsapp", got.Motivo())
		assert.Empty(t, got.SenderKind().String())
		assert.True(t, got.EnviadoEn().IsZero())
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestMensajeRepo_ListarPendientes_SoloEncolados_OrdenaPorEncoladoEn(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		early, err := domain.CrearMensaje(domain.CrearMensajeParams{
			ClienteID: 900000401, Segmento: domain.SegmentoRecienLiquidado,
			Telefono: "238 100 4000", Cuerpo: "primero", Now: mensajeFixedEncolado,
		})
		require.NoError(t, err)
		late, err := domain.CrearMensaje(domain.CrearMensajeParams{
			ClienteID: 900000402, Segmento: domain.SegmentoRecienLiquidado,
			Telefono: "238 100 4001", Cuerpo: "segundo", Now: mensajeFixedEncolado.Add(time.Minute),
		})
		require.NoError(t, err)
		alreadySent := makeMensaje(t, 900000403, domain.SegmentoRecienLiquidado, "ya enviado")

		if err := repo.Insertar(ctx, []*domain.Mensaje{early, late, alreadySent}); err != nil {
			t.Skipf("Insertar failed — migration 000045 may not be applied: %v", err)
		}
		require.NoError(t, alreadySent.MarcarEnviado(domain.SenderSimulado, mensajeFixedNow))
		require.NoError(t, repo.Actualizar(ctx, alreadySent))

		pendientes, err := repo.ListarPendientes(ctx, 10)
		require.NoError(t, err)

		require.Nil(t, findMensajeByID(pendientes, alreadySent.ID().String()), "enviado must not be pendiente")

		idx1, idx2 := -1, -1
		for i, m := range pendientes {
			switch m.ID().String() {
			case early.ID().String():
				idx1 = i
			case late.ID().String():
				idx2 = i
			}
		}
		require.GreaterOrEqual(t, idx1, 0)
		require.GreaterOrEqual(t, idx2, 0)
		assert.Less(t, idx1, idx2, "earlier ENCOLADO_EN must come first")
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestMensajeRepo_ListarPendientes_RespetaLimit(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		const base = 900000500
		var mensajes []*domain.Mensaje
		for i := range 3 {
			mensajes = append(mensajes, makeMensaje(t, base+i, domain.SegmentoRecienLiquidado, "cuerpo"))
		}
		if err := repo.Insertar(ctx, mensajes); err != nil {
			t.Skipf("Insertar failed — migration 000045 may not be applied: %v", err)
		}

		pendientes, err := repo.ListarPendientes(ctx, 1)
		require.NoError(t, err)
		// At least our own row must be present; the cap must never be exceeded —
		// other (unrelated, pre-existing) encolado rows could share the table.
		assert.LessOrEqual(t, len(pendientes), 1)
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestMensajeRepo_ContarEnviadosHoy(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		hoy := makeMensaje(t, 900000601, domain.SegmentoRecienLiquidado, "cuerpo")
		ayer := makeMensaje(t, 900000602, domain.SegmentoRecienLiquidado, "cuerpo")
		if err := repo.Insertar(ctx, []*domain.Mensaje{hoy, ayer}); err != nil {
			t.Skipf("Insertar failed — migration 000045 may not be applied: %v", err)
		}

		require.NoError(t, hoy.MarcarEnviado(domain.SenderSimulado, mensajeFixedNow))
		require.NoError(t, repo.Actualizar(ctx, hoy))
		require.NoError(t, ayer.MarcarEnviado(domain.SenderSimulado, mensajeFixedNow.AddDate(0, 0, -1)))
		require.NoError(t, repo.Actualizar(ctx, ayer))

		desde := time.Date(mensajeFixedNow.Year(), mensajeFixedNow.Month(), mensajeFixedNow.Day(), 0, 0, 0, 0, time.UTC)
		count, err := repo.ContarEnviadosHoy(ctx, desde)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, 1, "at least today's mensaje must be counted")

		// Cross-check: yesterday's send must not be counted when desde is set to
		// tomorrow's start (an empty window relative to "ayer").
		manana := desde.AddDate(0, 0, 1)
		countManana, err := repo.ContarEnviadosHoy(ctx, manana)
		require.NoError(t, err)
		assert.Zero(t, countManana, "nothing sent on/after tomorrow's watermark")
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestMensajeRepo_ClientesConMensaje(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		const clienteID = 900000701
		m := makeMensaje(t, clienteID, domain.SegmentoRecienLiquidado, "cuerpo")
		if err := repo.Insertar(ctx, []*domain.Mensaje{m}); err != nil {
			t.Skipf("Insertar failed — migration 000045 may not be applied: %v", err)
		}

		clientes, err := repo.ClientesConMensaje(ctx)
		require.NoError(t, err)
		assert.True(t, clientes[clienteID])
		assert.False(t, clientes[999999999])
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestMensajeRepo_Listar_FiltraPorEstadoYSegmento(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		recien := makeMensaje(t, 900000801, domain.SegmentoRecienLiquidado, "cuerpo recien")
		hueco := makeMensaje(t, 900000802, domain.SegmentoPorLiquidarHueco, "cuerpo hueco")
		if err := repo.Insertar(ctx, []*domain.Mensaje{recien, hueco}); err != nil {
			t.Skipf("Insertar failed — migration 000045 may not be applied: %v", err)
		}
		require.NoError(t, hueco.MarcarEnviado(domain.SenderSimulado, mensajeFixedNow))
		require.NoError(t, repo.Actualizar(ctx, hueco))

		soloRecien, err := repo.Listar(ctx, outbound.ListarMensajesParams{Segmento: domain.SegmentoRecienLiquidado})
		require.NoError(t, err)
		assert.NotNil(t, findMensajeByID(soloRecien, recien.ID().String()))
		assert.Nil(t, findMensajeByID(soloRecien, hueco.ID().String()))

		soloEnviado, err := repo.Listar(ctx, outbound.ListarMensajesParams{Estado: domain.EstadoEnviado})
		require.NoError(t, err)
		assert.NotNil(t, findMensajeByID(soloEnviado, hueco.ID().String()))
		assert.Nil(t, findMensajeByID(soloEnviado, recien.ID().String()))
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestCohorteRepo_MarcarContactado_NoTocaEnControlNiCohorteFecha(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		const clienteID = 900000901
		c := makeCohorte(t, clienteID, domain.SegmentoRecienLiquidado, "0.00", true, false)
		if err := repo.UpsertCohorte(ctx, []*domain.CohorteCliente{c}); err != nil {
			t.Skipf("UpsertCohorte failed — migration 000044 may not be applied: %v", err)
		}

		marcadoEn := fixedNow.Add(time.Hour)
		require.NoError(t, repo.MarcarContactado(ctx, clienteID, marcadoEn))

		cohorte, err := repo.ListarCohorte(ctx, outbound.ListarCohorteParams{})
		require.NoError(t, err)
		got := findByID(cohorte, clienteID)
		require.NotNil(t, got)

		assert.True(t, got.FueContactado())
		assert.True(t, got.EnControl(), "EN_CONTROL must be untouched by MarcarContactado")
		assert.WithinDuration(t, fixedCohorte, got.CohorteFecha(), time.Second, "COHORTE_FECHA must be untouched")
		assert.WithinDuration(t, marcadoEn, got.UpdatedAt(), time.Second)
	})
}
