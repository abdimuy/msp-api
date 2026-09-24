//nolint:misspell // Spanish vocabulary by project convention.
package ventfb_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// TestE2E_RecomputeSaldoVenta_SobreLapidaHuerfana responde UNA pregunta, y la
// responde midiendo en vez de leyendo PSQL:
//
//	¿MSP_RECOMPUTE_SALDO_VENTA convierte una lápida huérfana en fila normal,
//	poniéndole CARGO_CANCELADO = 'N'?
//
// La sospecha venía de leer migrations-firebird/000023_recompute_changelog.up.sql:
// el primer SELECT … INTO (líneas 194-197) lee de DOCTOS_CC, que en una lápida
// huérfana ya no tiene fila; v_cancelado queda NULL; el IF (v_cancelado = 'S')
// da UNKNOWN y no entra a la rama de lápida; la ejecución caería al upsert
// normal de las líneas 280-289, que escribe 'N' literal.
//
// Si eso pasara, la fila quedaría con CARGO_CANCELADO='N', SALDO=0 y
// FECHA_ULT_PAGO=NULL: invisible para los tres canales a la vez (el sync y
// by-ids exigen SALDO>0, FECHA_ULT_PAGO en ventana o CARGO_CANCELADO='S'; el
// digest exige 'N' + SALDO>0). El teléfono se quedaría con la venta fantasma
// y ningún canal podría rescatarlo.
//
// El paso 4 es el CONTROL POSITIVO: corre el MISMO camino (EXECUTE PROCEDURE
// desde esta misma transacción) sobre un cargo VIVO cuya fila de caché fue
// ensuciada a mano con 'S'. Si la proc no la devuelve a 'N', entonces esta
// prueba no sabe detectar un volteo y su "no voltea" del paso 3 no vale nada.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_RecomputeSaldoVenta_SobreLapidaHuerfana(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, zonaID := seedZonedCliente(t, q)

		// ── 1. Cargo vivo ────────────────────────────────────────────────
		cargoID := insertCargoDoctosCC(t, q, clienteID, "LAP-RCP",
			decimal.RequireFromString("4200.00"))

		// ── 2. Borrado físico: oficina elimina la venta en Microsip ──────
		_, err := q.ExecContext(ctx,
			`DELETE FROM IMPORTES_DOCTOS_CC WHERE DOCTO_CC_ID = ?`, cargoID)
		require.NoError(t, err, "DELETE IMPORTES_DOCTOS_CC")
		_, err = q.ExecContext(ctx,
			`DELETE FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, cargoID)
		require.NoError(t, err, "DELETE DOCTOS_CC")

		antes := leerFilaSaldo(ctx, t, q, cargoID)
		t.Logf("lápida recién creada:  cancelado=%q saldo=%s fecha_ult_pago=%v cliente_id=%v",
			antes.cancelado, antes.saldo, fmtFecha(antes.fechaUltPago), fmtInt(antes.clienteID))
		require.Equal(t, "S", antes.cancelado, "prerequisito: el borrado físico deja lápida")

		var quedan int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, cargoID).Scan(&quedan))
		require.Zero(t, quedan, "prerequisito: el cargo ya no existe en DOCTOS_CC")

		erroresAntes := contarSaldosErrors(ctx, t, q, cargoID)

		// ── 3. LA PREGUNTA: recompute sobre la lápida huérfana ───────────
		_, err = q.ExecContext(ctx,
			`EXECUTE PROCEDURE MSP_RECOMPUTE_SALDO_VENTA(?)`, cargoID)
		require.NoError(t, err,
			"la proc no debe propagar error: tiene WHEN ANY DO que lo traga")

		despues := leerFilaSaldo(ctx, t, q, cargoID)
		erroresDespues := contarSaldosErrors(ctx, t, q, cargoID)

		t.Logf("tras EXECUTE PROCEDURE: cancelado=%q saldo=%s fecha_ult_pago=%v cliente_id=%v",
			despues.cancelado, despues.saldo, fmtFecha(despues.fechaUltPago), fmtInt(despues.clienteID))
		t.Logf("MSP_SALDOS_ERRORS para el cargo %d: antes=%d despues=%d",
			cargoID, erroresAntes, erroresDespues)
		if erroresDespues > erroresAntes {
			t.Logf("último error registrado: %s", ultimoSaldosError(ctx, t, q, cargoID))
		}

		// ¿QUÉ la detuvo? El WHEN ANY DO sólo guarda SQLCODE/GDSCODE, no la
		// columna. Reproducimos aquí el único efecto del upsert normal que
		// puede fallar sobre una huérfana —escribir NULL en una columna
		// NOT NULL— para que el mensaje del motor lo nombre.
		_, verr := q.ExecContext(ctx,
			`UPDATE MSP_SALDOS_VENTAS SET CLIENTE_ID = NULL WHERE DOCTO_CC_ID = ?`, cargoID)
		t.Logf("quién frena el upsert normal: %v", verr)

		// ── 4. ¿Sigue viajando por los canales? ──────────────────────────
		repo := cobranzaventfb.NewVentasRepo(pool)
		saldos := cobranzaventfb.NewSaldosRepo(pool)
		desde := time.Now().Add(-7 * 24 * time.Hour)

		porIDs, err := repo.ByIDs(ctx, zonaID, []int{cargoID}, desde)
		require.NoError(t, err)
		t.Logf("canal by-ids: %d filas", len(porIDs))

		ids, _, err := saldos.ListIDs(ctx, zonaID, 0, 5000, desde)
		require.NoError(t, err)
		enDigest := false
		for _, id := range ids {
			if id == cargoID {
				enDigest = true
				break
			}
		}
		t.Logf("canal digest/inventario: presente=%v", enDigest)

		// ── 5. CONTROL POSITIVO ──────────────────────────────────────────
		// Mismo EXECUTE PROCEDURE, mismo transaction, sobre un cargo VIVO
		// cuya fila de caché fue ensuciada a mano. Si la proc no la limpia,
		// el "no voltea" de arriba es un falso negativo del arnés.
		vivoID := insertCargoDoctosCC(t, q, clienteID, "LAP-CTRL",
			decimal.RequireFromString("5100.00"))
		_, err = q.ExecContext(ctx,
			`UPDATE MSP_SALDOS_VENTAS SET CARGO_CANCELADO = 'S', SALDO = 0
			  WHERE DOCTO_CC_ID = ?`, vivoID)
		require.NoError(t, err, "ensuciar la fila del cargo vivo")

		sucia := leerFilaSaldo(ctx, t, q, vivoID)
		require.Equal(t, "S", sucia.cancelado, "control positivo: la fila quedó ensuciada")

		_, err = q.ExecContext(ctx,
			`EXECUTE PROCEDURE MSP_RECOMPUTE_SALDO_VENTA(?)`, vivoID)
		require.NoError(t, err)

		limpia := leerFilaSaldo(ctx, t, q, vivoID)
		t.Logf("control positivo (cargo vivo %d): cancelado=%q saldo=%s",
			vivoID, limpia.cancelado, limpia.saldo)
		require.Equal(t, "N", limpia.cancelado,
			"CONTROL POSITIVO: sobre un cargo vivo la proc SÍ escribe 'N'; si esto falla "+
				"la prueba no sabe detectar un volteo y el resultado de arriba no prueba nada")
		require.True(t, limpia.saldo.Equal(decimal.RequireFromString("5100.00")),
			"control positivo: la proc recalculó el saldo; got=%s", limpia.saldo)

		// ── Veredicto ────────────────────────────────────────────────────
		require.Equal(t, "S", despues.cancelado,
			"MSP_RECOMPUTE_SALDO_VENTA volteó la lápida huérfana del cargo %d a 'N': "+
				"la fila deja de viajar por los tres canales y el teléfono conserva la "+
				"venta fantasma para siempre", cargoID)
	})
}

// filaSaldo es la lectura cruda de las columnas que deciden si una fila del
// caché viaja o no.
type filaSaldo struct {
	cancelado    string
	saldo        decimal.Decimal
	fechaUltPago *time.Time
	clienteID    *int
	updatedAt    time.Time
}

func leerFilaSaldo(ctx context.Context, t *testing.T, q firebird.Querier, cargoID int) filaSaldo {
	t.Helper()
	var f filaSaldo
	err := q.QueryRowContext(ctx,
		`SELECT CARGO_CANCELADO, SALDO, FECHA_ULT_PAGO, CLIENTE_ID, UPDATED_AT
		   FROM MSP_SALDOS_VENTAS WHERE DOCTO_CC_ID = ?`, cargoID).
		Scan(&f.cancelado, &f.saldo, &f.fechaUltPago, &f.clienteID, &f.updatedAt)
	require.NoError(t, err, "leerFilaSaldo(%d)", cargoID)
	return f
}

func contarSaldosErrors(ctx context.Context, t *testing.T, q firebird.Querier, cargoID int) int {
	t.Helper()
	var n int
	require.NoError(t, q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MSP_SALDOS_ERRORS WHERE CARGO_ID = ?`, cargoID).Scan(&n))
	return n
}

func ultimoSaldosError(ctx context.Context, t *testing.T, q firebird.Querier, cargoID int) string {
	t.Helper()
	var msg *string
	require.NoError(t, q.QueryRowContext(ctx,
		`SELECT FIRST 1 ERROR_MSG FROM MSP_SALDOS_ERRORS
		  WHERE CARGO_ID = ? ORDER BY ERROR_ID DESC`, cargoID).Scan(&msg))
	if msg == nil {
		return "<null>"
	}
	return *msg
}

func fmtFecha(t *time.Time) string {
	if t == nil {
		return "<null>"
	}
	return t.Format(time.RFC3339)
}

func fmtInt(v *int) string {
	if v == nil {
		return "<null>"
	}
	return strconv.Itoa(*v)
}
