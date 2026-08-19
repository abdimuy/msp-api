// Integration test for GenerarReporteCliente + clientespdf.Render against the
// real Microsip Firebird database. READ-ONLY: all operations run inside a
// fbtestutil.WithTestTransaction that always rolls back — no writes are made
// to the shared dev DB.
//
//nolint:paralleltest // serial: shares rollback-only tx context.
//nolint:misspell    // Spanish domain vocabulary by project convention.
package clienteshttp_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clientesapp "github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/infra/clientesfb"
	"github.com/abdimuy/msp-api/internal/clientes/infra/clientespdf"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
)

// conceptoCobranza es el concepto de "Cobranza en ruta" del catálogo
// CONCEPTOS_CC, que `gbak -skip_data` conserva. Se usa para saldar las ventas
// sembradas; no es una fila de producción.
const conceptoCobranza = 87327

// TestReporteIntegration_ReporteCliente verifica que GenerarReporteCliente
// arme el reporte completo —ventas, conteos y saldos— y que clientespdf.Render
// produzca un PDF a partir de él.
//
// ANTES: la prueba se llamaba TestReporteIntegration_MinervaLopez y leía al
// cliente de producción 24037, afirmando que tenía 4 ventas, 3 liquidadas y 1
// activa. Esos números eran una FOTO de la base compartida: cualquier venta o
// abono nuevo de esa señora rompía la prueba sin que nadie tocara código, y
// contra la base reducida de 15 MB fallaba siempre porque sus movimientos son
// justamente lo que `-skip_data` omite.
//
// AHORA la prueba siembra las cuatro ventas y decide cuáles quedan saldadas, así
// que 4/3/1 dejan de ser observaciones y pasan a ser el contrato que se está
// verificando.
func TestReporteIntegration_ReporteCliente(t *testing.T) {
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set — apunta al dev DB de Microsip para correr tests de integración Firebird")
	}

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	// GenerarReporteCliente only uses repo — analytics, dirIndex, and clock are
	// not touched by this method, so nil is safe here.
	svc := clientesapp.NewService(repo, nil, nil, nil)

	genFijo := time.Date(2026, 6, 20, 14, 30, 0, 0, time.UTC)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, conceptoCobranza)

		clienteID := microsipseed.Cliente(t, q, "REPORTE PDF PRUEBA")

		const (
			ventasSembradas = 4
			liquidadas      = 3
		)
		total := decimal.NewFromInt(8800)
		for i := range ventasSembradas {
			venta := microsipseed.VentaCredito(t, q, clienteID, microsipseed.OpcionesVenta{
				Fecha: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i),
				Total: total,
			})
			// Las primeras tres se saldan por completo; la cuarta queda con saldo.
			abono := total
			if i >= liquidadas {
				abono = total.Div(decimal.NewFromInt(4)).Round(2)
			}
			microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
				ConceptoCCID: conceptoCobranza,
				Importe:      abono,
				Fecha:        venta.Fecha,
			})
		}

		rep, err := svc.GenerarReporteCliente(ctx, clienteID, nil)
		require.NoError(t, err, "error al generar el reporte del cliente")

		t.Logf("cliente: %s, ventas: %d", rep.Cliente.Nombre, len(rep.Ventas))
		assert.Len(t, rep.Ventas, ventasSembradas, "el reporte debe traer las ventas sembradas")
		assert.Equal(t, ventasSembradas, rep.TotalVentas, "TotalVentas")
		assert.Equal(t, liquidadas, rep.VentasLiquidadas, "VentasLiquidadas — las saldadas por completo")
		assert.Equal(t, ventasSembradas-liquidadas, rep.VentasActivas, "VentasActivas — la que conserva saldo")

		pdf, err := clientespdf.Render(rep, genFijo, "Juan Pérez")
		require.NoError(t, err, "error al renderizar el PDF")
		require.GreaterOrEqual(t, len(pdf), 4, "el PDF no debe estar vacío")
		assert.Equal(t, "%PDF", string(pdf[:4]), "los bytes del PDF deben iniciar con %%PDF")

		// Dump to file if REPORTE_PDF_OUT is set (for manual visual review).
		if out := os.Getenv("REPORTE_PDF_OUT"); out != "" {
			require.NoError(t, os.WriteFile(out, pdf, 0o644))
			t.Logf("PDF escrito en %s (%d bytes)", out, len(pdf))
		}
	})
}
