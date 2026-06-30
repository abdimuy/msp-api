//nolint:misspell // Spanish vocabulary (ventas, contado, zona, caja, etc.) by convention.
package ventfb_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// buildAplicarContadoSinZona builds a CONTADO venta in APROBADA state whose
// dirección has NO zona_cliente_id — the exact shape that used to fail with
// ErrVentaSinZona before the contado-fixed-caja fix. A known ClienteID is set
// so the auto-create-cliente branch (nil microsipCliente in the harness) is not
// exercised; this isolates the zona precondition + caja-resolution change.
func buildAplicarContadoSinZona(t *testing.T, userID uuid.UUID) *domain.Venta {
	t.Helper()
	nombre, err := domain.NewNombreCliente("Ramírez Soto Patricia")
	require.NoError(t, err)
	cid := testClienteID
	// NOTE: ZonaClienteID intentionally omitted (nil) — this is the contado case.
	dir, err := domain.NewDireccion(domain.NewDireccionParams{
		Calle:     "Calle Reforma",
		Colonia:   "Centro",
		Poblacion: "Tehuacán",
		Ciudad:    "Puebla",
	})
	require.NoError(t, err)
	gps, err := domain.NewGPSCoords(18.46, -97.39)
	require.NoError(t, err)
	precioProducto, err := domain.NewMontoSnapshot(
		decimal.RequireFromString("4320.00"),
		decimal.RequireFromString("3900.00"),
		decimal.RequireFromString("3500.00"),
	)
	require.NoError(t, err)
	almOrigen := testAlmacenID
	almDestino := 11059
	v, err := domain.CrearVenta(domain.CrearVentaParams{
		ID:        uuid.New(),
		ClienteID: &cid,
		Cliente: domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{
			Nombre: nombre,
		}),
		Direccion:  dir,
		GPS:        gps,
		FechaVenta: testNow(),
		TipoVenta:  domain.TipoVentaContado,
		Productos: []domain.CrearVentaProductoInput{{
			ID:             uuid.New(),
			ArticuloID:     testArticuloID,
			Articulo:       "Cantinera Caoba",
			Cantidad:       decimal.RequireFromString("1.0000"),
			Precios:        precioProducto,
			AlmacenOrigen:  &almOrigen,
			AlmacenDestino: &almDestino,
		}},
		Vendedores: []domain.CrearVentaVendedorInput{{
			ID:        uuid.New(),
			UsuarioID: userID,
			Email:     "ruta25@muebleriamsp.mx",
			Nombre:    "Vendedor Ruta 25",
		}},
		CreatedBy: userID,
		Now:       testNow(),
	})
	require.NoError(t, err)
	require.Nil(t, v.Direccion().ZonaClienteID(), "fixture must have nil zona")
	attachAplicarEvidencia(t, v, userID)
	require.NoError(t, v.EnviarARevision(userID, testNow()))
	require.NoError(t, v.Aprobar(userID, testNow()))
	return v
}

// TestE2E_AplicarVenta_Contado_SinZona_AplicaEnMicrosip is the end-to-end proof
// of the contado fix: a CONTADO venta WITHOUT a zona is applied through the full
// Service.AplicarVenta path against the real Microsip DB. Before the fix this
// failed with ErrVentaSinZona (the apply flow required a zona to resolve the
// caja). After the fix it must apply against the fixed mostrador caja
// (MSP_CFG_APLICAR.CAJA_CONTADO_ID), producing a real applied DOCTOS_PV.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_AplicarVenta_Contado_SinZona_AplicaEnMicrosip(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		h := newAplicarE2EHarness(ctx, t, pool)

		q := firebird.GetQuerier(ctx, pool.DB)
		requireCatalog(t, q)

		// The fixed contado caja must be configured (CAJA_CONTADO_ID set).
		var cajaContado int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT CAJA_CONTADO_ID FROM MSP_CFG_APLICAR WHERE ID = 1`,
		).Scan(&cajaContado), "MSP_CFG_APLICAR.CAJA_CONTADO_ID debe estar configurada")
		require.Positive(t, cajaContado, "CAJA_CONTADO_ID debe ser > 0 (config aplicada)")

		v := buildAplicarContadoSinZona(t, userID)
		ventaID := h.persistAprobada(ctx, t, v)

		// The whole point: this used to return ErrVentaSinZona.
		result, err := h.svc.AplicarVenta(ctx, ventaID, userID)
		require.NoError(t, err, "contado sin zona debe aplicar (antes fallaba con venta_sin_zona)")
		require.NotNil(t, result.MicrosipDoctoPVID(), "la venta debe quedar aplicada con DOCTO_PV_ID")

		doctoPVID := *result.MicrosipDoctoPVID()

		// Verify the DOCTOS_PV is fully applied on the FIXED contado caja.
		var aplicado string
		var cajaID int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT APLICADO, CAJA_ID FROM DOCTOS_PV WHERE DOCTO_PV_ID = ?`, doctoPVID,
		).Scan(&aplicado, &cajaID), "must read DOCTOS_PV header")

		assert.Equal(t, "S", aplicado, "DOCTOS_PV.APLICADO debe ser 'S' (aplicada hasta el final)")
		assert.Equal(t, cajaContado, cajaID, "DOCTOS_PV.CAJA_ID debe ser la caja fija de contado")

		folio := ""
		if result.MicrosipFolio() != nil {
			folio = *result.MicrosipFolio()
		}
		t.Logf("contado sin zona aplicado: DoctoPVID=%d Folio=%s CajaID=%d",
			doctoPVID, folio, cajaID)
	})
}
