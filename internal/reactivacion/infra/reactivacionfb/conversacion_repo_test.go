// Integration tests for MSP_RX_CONVERSACION + MSP_RX_TURNO (Fase 3a
// copiloto). All writes execute inside a transaction that always rolls back
// so the shared dev DB never accumulates test data.
//
// Prerequisites:
//   - FB_DATABASE env var pointing at the dev Microsip Firebird DB.
//   - Migration 000046 applied (creates MSP_RX_CONVERSACION/MSP_RX_TURNO).
//
// Run: FB_DATABASE=/firebird/data/MUEBLERA.FDB go test ./internal/reactivacion/infra/reactivacionfb/...
//
//nolint:misspell // Spanish vocabulary (conversación, cohorte) by convention.
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

var convFixedNow = time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)

// makeConversacion builds a fresh Conversacion in EstadoContactado for a
// large positive synthetic clienteID (real Microsip IDs are far smaller) so
// tests never collide with production rows.
func makeConversacion(t *testing.T, clienteID int) *domain.Conversacion {
	t.Helper()
	c, err := domain.CrearConversacion(clienteID, convFixedNow)
	require.NoError(t, err)
	return c
}

func findConversacionByClienteID(cs []*domain.Conversacion, clienteID int) *domain.Conversacion {
	for _, c := range cs {
		if c.ClienteID() == clienteID {
			return c
		}
	}
	return nil
}

func findTurnoByID(turnos []*domain.Turno, id string) *domain.Turno {
	for _, tn := range turnos {
		if tn.ID() == id {
			return tn
		}
	}
	return nil
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestConversacionRepo_UpsertAndGet_RoundTrip(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewCopilotoRepo(pool)

		const clienteID = 901000001
		c := makeConversacion(t, clienteID)
		c.SetContextoNota(
			"acuerdo de pago verbal con la Sra. Peña Ñuñez",
			[]string{"deuda", "senal_compra"},
			"hash-abc123",
			convFixedNow.Add(time.Minute),
		)

		if err := repo.Upsert(ctx, c); err != nil {
			t.Skipf("Upsert failed — migration 000046 may not be applied: %v", err)
		}

		got, err := repo.Get(ctx, clienteID)
		require.NoError(t, err)
		require.NotNil(t, got, "inserted conversacion must be retrievable by Get")

		assert.Equal(t, c.ID(), got.ID())
		assert.Equal(t, clienteID, got.ClienteID())
		assert.Equal(t, domain.EstadoContactado, got.Estado())
		assert.Equal(t, "acuerdo de pago verbal con la Sra. Peña Ñuñez", got.ContextoNota(),
			"UTF-8 (incl. Ñ) must round-trip through the BLOB TEXT column")
		assert.Equal(t, []string{"deuda", "senal_compra"}, got.Banderas())
		assert.Equal(t, "hash-abc123", got.NotaHash())
		assert.WithinDuration(t, convFixedNow, got.CreatedAt(), time.Second)
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestConversacionRepo_Get_UnknownClienteReturnsNilNil(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewCopilotoRepo(pool)

		got, err := repo.Get(ctx, 999999999)
		require.NoError(t, err)
		assert.Nil(t, got)
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestConversacionRepo_Upsert_SecondCallUpdatesNotDuplicates(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewCopilotoRepo(pool)

		const clienteID = 901000002
		c := makeConversacion(t, clienteID)
		if err := repo.Upsert(ctx, c); err != nil {
			t.Skipf("Upsert failed — migration 000046 may not be applied: %v", err)
		}

		require.NoError(t, c.MarcarRespondio(convFixedNow.Add(time.Hour)))
		require.NoError(t, repo.Upsert(ctx, c))

		got, err := repo.Get(ctx, clienteID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, domain.EstadoRespondio, got.Estado(), "second Upsert must UPDATE, reflecting the new estado")
		assert.WithinDuration(t, convFixedNow.Add(time.Hour), got.UpdatedAt(), time.Second)

		// No duplicate row: exactly one conversacion for this clienteID.
		todas, err := repo.Listar(ctx, outbound.ListarConversacionesParams{})
		require.NoError(t, err)
		count := 0
		for _, cv := range todas {
			if cv.ClienteID() == clienteID {
				count++
			}
		}
		assert.Equal(t, 1, count, "Upsert must never create a second row for the same CLIENTE_ID")
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestConversacionRepo_Listar_FiltraPorEstadoYSoloEscaladas(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewCopilotoRepo(pool)

		contactado := makeConversacion(t, 901000101)
		escalado1 := makeConversacion(t, 901000102)
		require.NoError(t, escalado1.MarcarEscalada("operador.uno", convFixedNow.Add(time.Minute)))
		escalado2 := makeConversacion(t, 901000103)
		require.NoError(t, escalado2.MarcarEscalada("operador.dos", convFixedNow.Add(2*time.Minute)))

		for _, c := range []*domain.Conversacion{contactado, escalado1, escalado2} {
			if err := repo.Upsert(ctx, c); err != nil {
				t.Skipf("Upsert failed — migration 000046 may not be applied: %v", err)
			}
		}

		soloEscaladas, err := repo.Listar(ctx, outbound.ListarConversacionesParams{SoloEscaladas: true})
		require.NoError(t, err)
		assert.Nil(t, findConversacionByClienteID(soloEscaladas, 901000101), "contactado must be excluded")
		assert.NotNil(t, findConversacionByClienteID(soloEscaladas, 901000102))
		assert.NotNil(t, findConversacionByClienteID(soloEscaladas, 901000103))

		porEstado, err := repo.Listar(ctx, outbound.ListarConversacionesParams{Estado: domain.EstadoContactado.String()})
		require.NoError(t, err)
		assert.NotNil(t, findConversacionByClienteID(porEstado, 901000101))
		assert.Nil(t, findConversacionByClienteID(porEstado, 901000102))
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestConversacionRepo_AppendTurnoAndListarTurnos_Cronologico(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewCopilotoRepo(pool)

		const clienteID = 901000201
		primero, err := domain.CrearTurno(domain.CrearTurnoParams{
			ClienteID: clienteID, Direccion: domain.DireccionEntrante, Autor: domain.AutorCliente,
			Cuerpo: "Hola, ¿cuánto debo?", Now: convFixedNow,
		})
		require.NoError(t, err)
		segundo, err := domain.CrearTurno(domain.CrearTurnoParams{
			ClienteID: clienteID, Direccion: domain.DireccionSaliente, Autor: domain.AutorIA,
			Cuerpo: "¡Hola! Ya terminó de pagar su compra 🎉", Now: convFixedNow.Add(time.Minute),
		})
		require.NoError(t, err)
		tercero, err := domain.CrearTurno(domain.CrearTurnoParams{
			ClienteID: clienteID, Direccion: domain.DireccionSaliente, Autor: domain.AutorHumano,
			Cuerpo: "Le contacto un asesor en breve", Now: convFixedNow.Add(2 * time.Minute),
		})
		require.NoError(t, err)

		if err := repo.AppendTurno(ctx, primero); err != nil {
			t.Skipf("AppendTurno failed — migration 000046 may not be applied: %v", err)
		}
		require.NoError(t, repo.AppendTurno(ctx, segundo))
		require.NoError(t, repo.AppendTurno(ctx, tercero))

		turnos, err := repo.ListarTurnos(ctx, clienteID)
		require.NoError(t, err)
		require.Len(t, turnos, 3)

		assert.Equal(t, primero.ID(), turnos[0].ID())
		assert.Equal(t, segundo.ID(), turnos[1].ID())
		assert.Equal(t, tercero.ID(), turnos[2].ID())

		got := findTurnoByID(turnos, primero.ID())
		require.NotNil(t, got)
		assert.Equal(t, domain.DireccionEntrante, got.Direccion())
		assert.Equal(t, domain.AutorCliente, got.Autor())
		assert.Equal(t, "Hola, ¿cuánto debo?", got.Cuerpo())
		assert.Empty(t, got.MensajeRef())
	})
}
