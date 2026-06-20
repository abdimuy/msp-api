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
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clientesapp "github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/infra/clientesfb"
	"github.com/abdimuy/msp-api/internal/clientes/infra/clientespdf"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

// TestReporteIntegration_MinervaLopez verifies that GenerarReporteCliente
// correctly fetches all ventas and payment details for cliente 24037
// (MINERVA LOPEZ MERINO, ~45 ventas) and that clientespdf.Render produces a
// valid PDF from the assembled data. READ-ONLY.
func TestReporteIntegration_MinervaLopez(t *testing.T) {
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set — apunta al dev DB de Microsip para correr tests de integración Firebird")
	}

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	// GenerarReporteCliente only uses repo — analytics, dirIndex, and clock are
	// not touched by this method, so nil is safe here.
	svc := clientesapp.NewService(repo, nil, nil, nil)

	genFijo := time.Date(2026, 6, 20, 14, 30, 0, 0, time.UTC)

	// Client can be overridden for manual visual review of other clients.
	clienteID := 24037
	if v := os.Getenv("REPORTE_CLIENTE_ID"); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil {
			clienteID = n
		}
	}

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		rep, err := svc.GenerarReporteCliente(ctx, clienteID, nil)
		require.NoError(t, err, "error al generar el reporte del cliente")

		t.Logf("cliente: %s, ventas: %d", rep.Cliente.Nombre, len(rep.Ventas))
		if clienteID == 24037 {
			assert.Greater(t, len(rep.Ventas), 10, "cliente 24037 debe tener más de 10 ventas")
		}

		pdf, err := clientespdf.Render(rep, genFijo)
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
