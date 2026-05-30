//go:build aplicar_persist

//nolint:misspell // Spanish vocabulary (ventas, batería, etc.) by convention.
package ventfb_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/microsip"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// TestE2E_AplicarVenta_3Contado_Persist applies THREE CONTADO ventas to the
// real Microsip dev DB and COMMITS each one — every venta lands as a real
// DOCTOS_PV (with its inventory + CxC cascade + folio advance) that you can
// inspect from Microsip.
//
// Build-tag gated (`-tags aplicar_persist`) so it never runs as part of the
// regular `make test-firebird-ventas` / pre-push gate. Recovery if you want to
// undo the persisted sales: `make fb-restore NAME=clean-with-admin` (the fresh
// baseline taken on 2026-05-29).
//
// Each venta runs in its own transaction (independent commits), mirroring how
// a real user would create them one by one. Uses article 510 (16% IVA) so the
// IVA split is visible in Microsip's UI.
func TestE2E_AplicarVenta_3Contado_Persist(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	txMgr := firebird.NewTxManager(pool.DB)
	writer := microsip.NewVentaWriter(pool)
	cfg := ventfb.NewAplicarConfigRepo(pool)

	ctx := context.Background()
	defs, err := cfg.Defaults(ctx)
	require.NoError(t, err, "MSP_CFG_APLICAR must be seeded")
	cc, err := cfg.CajaCajero(ctx, testZonaID)
	require.NoError(t, err, "zona %d must be mapped in MSP_CFG_ZONA_CAJA", testZonaID)

	type applied struct {
		doctoPVID int
		folio     string
	}
	results := make([]applied, 0, 3)

	for i := 1; i <= 3; i++ {
		userID := uuid.New()
		nombre := fmt.Sprintf("Batería Cinsa #%d (smoke 2026-05-29)", i)
		v := buildAplicarContadoConArticulo(t, userID, testArticuloID16Pct, nombre)

		var doctoPVID int
		var folio string
		err := txMgr.RunInTx(ctx, func(ctx context.Context) error {
			in := outbound.MicrosipVentaInput{
				Venta:                v,
				CajaID:               cc.CajaID,
				CajeroID:             cc.CajeroID,
				VendedorID:           cc.VendedorID,
				SucursalID:           defs.SucursalID,
				FormaCobroID:         defs.FormaCobroContadoID,
				NumeroDeVendedoresID: numVendedores1ID,
			}
			res, err := writer.Aplicar(ctx, in)
			if err != nil {
				return err
			}
			doctoPVID = res.DoctoPVID
			folio = res.Folio
			return nil
		})
		require.NoErrorf(t, err, "venta #%d should commit", i)

		results = append(results, applied{doctoPVID: doctoPVID, folio: folio})
		t.Logf("✔ venta #%d PERSISTED → DOCTO_PV_ID=%d FOLIO=%s  (artículo: %s)",
			i, doctoPVID, folio, nombre)
	}

	t.Log("─── resumen ──────────────────────────────────────────────────────────")
	t.Logf("3 ventas contado aplicadas y COMMITEADAS en Microsip dev (caja RUTA25):")
	for i, r := range results {
		t.Logf("  #%d  DOCTO_PV_ID=%d  FOLIO=%s", i+1, r.doctoPVID, r.folio)
	}
	t.Log("Para revertir: make fb-restore NAME=clean-with-admin")
}
