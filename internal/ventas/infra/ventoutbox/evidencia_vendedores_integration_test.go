//go:build !ci_skip_firebird

//nolint:misspell // domain vocabulary is Spanish (ventas, vendedores) per project convention.
package ventoutbox_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/outboxfb"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventoutbox"
)

// TestIntegracion_EvidenciaVendedores_AterrizaYEsReclamable answers, against
// the real table rather than a fake, the question a registration unit test
// cannot: does the evidence row actually land, and would the dispatcher ever
// pick it up?
//
// The failure it guards against is silent by construction. The dispatcher
// claims rows with EVENT_TYPE IN (<registered types>), so an event type
// nobody registered is never claimed, never processed, and accumulates in
// MSP_OUTBOX_EVENTS in state `new` forever — and MSP_OUTBOX_EVENTS is also
// the only history this system keeps of a venta's fases, which nobody purges.
// A row that cannot be claimed looks exactly like a row nobody has gotten to
// yet.
//
// It also pins the two things the column and the payload must satisfy:
// the event type fits EVENT_TYPE (VARCHAR(80)) and the payload survives the
// BLOB round trip with every key intact.
func TestIntegracion_EvidenciaVendedores_AterrizaYEsReclamable(t *testing.T) { //nolint:paralleltest // shared rollback-only tx
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		ventaID := uuid.New()
		by := uuid.New()
		ev := domain.NewVendedoresResueltosEvent(domain.VendedoresResueltosPayload{
			VentaID:          ventaID,
			By:               by,
			CamionetaID:      11341,
			Origen:           domain.OrigenVendedoresRoster,
			Motivo:           "ok",
			RosterEmails:     []string{"beto@muebleriamsp.mx", "israel.soto@msp.com"},
			RosterExcluidos:  1,
			EmailsSinUsuario: []string{"fantasma@muebleriamsp.mx"},
			ClienteEmails:    []string{"ana@muebleriamsp.mx"},
			SoloEnRoster:     []string{"beto@muebleriamsp.mx", "israel.soto@msp.com"},
			SoloEnCliente:    []string{"ana@muebleriamsp.mx"},
			Coinciden:        false,
			DuracionMS:       42,
		}, time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC))

		enq := ventoutbox.NewEnqueuer(pool)
		require.NoError(t, enq.Enqueue(ctx, "venta", ventaID, ev.EventType(), ev.Payload()))

		// 1. The row exists, unprocessed, with the type intact — no silent
		//    truncation by the 80-char column.
		q := firebird.GetQuerier(ctx, pool.DB)
		var (
			eventType string
			payload   []byte
			processed any
			failed    any
		)
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT EVENT_TYPE, PAYLOAD, PROCESSED_AT, FAILED_AT
			   FROM MSP_OUTBOX_EVENTS WHERE AGGREGATE_ID = ?`,
			ventaID.String(),
		).Scan(&eventType, &payload, &processed, &failed))

		assert.Equal(t, domain.EventTypeVentaVendedoresResueltos, trimFB(eventType))
		assert.Nil(t, processed)
		assert.Nil(t, failed)

		// 2. The payload survives the BLOB round trip whole.
		var got map[string]any
		require.NoError(t, json.Unmarshal(payload, &got))
		for _, k := range []string{
			"venta_id", "by", "camioneta_id", "origen", "motivo",
			"roster_emails", "roster_total", "roster_excluidos",
			"emails_sin_usuario", "cliente_emails", "cliente_total",
			"solo_en_roster", "solo_en_cliente", "coinciden", "duracion_ms",
		} {
			assert.Contains(t, got, k, "payload key %q must survive the round trip", k)
		}
		assert.Equal(t, ventaID.String(), got["venta_id"])
		assert.Equal(t, by.String(), got["by"])
		assert.InDelta(t, 11341, got["camioneta_id"], 0)

		// 3. The dispatcher's claim filter includes the type, so the row can
		//    leave `new`. Built from the registry the composition root
		//    populates, not from a hand-written list.
		reg := outboxfb.NewHandlerRegistry()
		for _, h := range ventoutbox.NewVentaReindexHandlers(
			newTestService(t, newStubVentaRepo(), &stubSearchIndex{}),
		) {
			reg.Register(h)
		}
		assert.Contains(t, reg.KnownTypes(), domain.EventTypeVentaVendedoresResueltos,
			"an unregistered type is never claimed and sits in `new` forever")

		// 4. And it must NOT move the venta's Fase: the column is derived from
		//    the outbox, and evidence is not a transition.
		assert.False(t, domain.EsEventoDeCambioDeFase(domain.EventTypeVentaVendedoresResueltos),
			"the evidence event must never register as a fase change")
	})
}

// trimFB drops the CHAR padding Firebird returns on fixed-width columns.
func trimFB(s string) string {
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}
