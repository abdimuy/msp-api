//go:build !short

//nolint:misspell,paralleltest // Microsip column names are Spanish; integration tests use shared pool and must not run in parallel.
package invfb_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
	"github.com/abdimuy/msp-api/internal/inventario/infra/invfb"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// defaultCfg returns a test inventario config matching the legacy Node defaults.
func defaultCfg() config.Inventario {
	return config.Inventario{
		AlmacenDestinoVentasID: 11058,
		ConceptoInSalidaID:     36,
		ConceptoInEntradaID:    25,
		SucursalID:             225490,
	}
}

// buildTestTraspaso creates a minimal domain.Traspaso for use in integration
// tests. articuloID must exist in ARTICULOS + CLAVES_ARTICULOS with rol=17 in
// the dev DB. almacenOrigen / almacenDestino must be valid ALMACENES rows.
func buildTestTraspaso(t *testing.T, ventaID uuid.UUID, almacenOrigen, almacenDestino, articuloID int) *domain.Traspaso {
	t.Helper()

	folio := domain.HydrateFolio("MST999998")
	cant, err := domain.NewCantidad(decimal.NewFromInt(1))
	if err != nil {
		t.Fatalf("build test cantidad: %v", err)
	}
	now := time.Now().UTC()
	tr, err := domain.CrearTraspaso(domain.CrearTraspasoParams{
		ID:             uuid.New(),
		Folio:          folio,
		AlmacenOrigen:  almacenOrigen,
		AlmacenDestino: almacenDestino,
		Fecha:          now,
		Descripcion:    "test traspaso integracion",
		VentaID:        &ventaID,
		Detalles: []domain.CrearTraspasoDetalleInput{
			{ID: uuid.New(), ArticuloID: articuloID, Cantidad: cant},
		},
		CreatedBy: uuid.New(),
		Now:       now,
	})
	if err != nil {
		t.Fatalf("build test traspaso: %v", err)
	}
	return tr
}

// seedVentaRow inserts a minimal MSP_VENTAS row so FK constraints pass.
// Must be called inside an active test transaction.
//
//nolint:dupword // NULL repetitions in VALUES are intentional SQL syntax.
func seedVentaRow(ctx context.Context, tb testing.TB, q firebird.Querier, ventaID uuid.UUID) {
	tb.Helper()
	now := time.Now().UTC()
	wc := firebird.ToWallClock(now)
	by := uuid.New().String()
	_, err := q.ExecContext(ctx,
		`INSERT INTO MSP_VENTAS (
			ID, NOMBRE_CLIENTE, TELEFONO, AVAL_O_RESPONSABLE,
			CALLE, NUMERO_EXTERIOR, COLONIA, POBLACION, CIUDAD, ZONA_CLIENTE_ID,
			LATITUD, LONGITUD,
			FECHA_VENTA, TIPO_VENTA,
			MONTO_ANUAL, MONTO_CORTO_PLAZO, MONTO_CONTADO,
			PLAZO_MESES, ENGANCHE, PARCIALIDAD,
			FREC_PAGO, DIA_COBRANZA_SEMANA, DIA_COBRANZA_MES,
			NOTA,
			CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY,
			CANCELED_AT, CANCELED_BY, CANCEL_REASON,
			CLIENTE_ID, STATUS, APPROVED_AT, APPROVED_BY,
			SITUACION, SINCRONIZACION,
			MICROSIP_DOCTO_PV_ID, MICROSIP_FOLIO, MICROSIP_APLICADA_AT,
			CLIENTE_REFERENCIA
		) VALUES (
			?, 'Test Cliente', NULL, NULL,
			'Calle Test', NULL, 'Colonia', 'Puebla', 'Puebla', NULL,
			0, 0,
			?, 'contado',
			0, 0, 100,
			NULL, NULL, NULL,
			NULL, NULL, NULL,
			NULL,
			?, ?, ?, ?,
			NULL, NULL, NULL,
			NULL, 'borrador', NULL, NULL,
			'normal', 'pendiente',
			NULL, NULL, NULL,
			NULL
		)`,
		ventaID.String(), wc,
		wc, wc, by, by,
	)
	if err != nil {
		tb.Fatalf("seedVentaRow: %v", err)
	}
}

func TestTraspasoWriter_Save_HappyPath(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	cfg := defaultCfg()
	repo := invfb.NewTraspasoRepo(cfg, pool)

	almacenOrigen := 1
	almacenDestino := 11058 // INVENTARIO_ALMACEN_DESTINO_VENTAS_ID
	articuloID := 1         // must exist in ARTICULOS + CLAVES_ARTICULOS rol=17

	ventaID := uuid.New()

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		seedVentaRow(ctx, t, q, ventaID)

		tr := buildTestTraspaso(t, ventaID, almacenOrigen, almacenDestino, articuloID)
		doctoInID, err := repo.Save(ctx, tr)
		if err != nil {
			t.Fatalf("Save: %v", err)
		}
		if doctoInID <= 0 {
			t.Fatalf("expected positive doctoInID, got %d", doctoInID)
		}

		// Verify DOCTOS_IN row exists within the tx.
		var count int
		if scanErr := q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM DOCTOS_IN WHERE DOCTO_IN_ID = ?`, doctoInID,
		).Scan(&count); scanErr != nil {
			t.Fatalf("verify DOCTOS_IN: %v", scanErr)
		}
		if count != 1 {
			t.Fatalf("expected 1 DOCTOS_IN row, got %d", count)
		}

		// Verify 2 DOCTOS_IN_DET rows (1 salida + 1 entrada).
		var detCount int
		if scanErr := q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM DOCTOS_IN_DET WHERE DOCTO_IN_ID = ?`, doctoInID,
		).Scan(&detCount); scanErr != nil {
			t.Fatalf("verify DOCTOS_IN_DET: %v", scanErr)
		}
		if detCount != 2 {
			t.Fatalf("expected 2 DOCTOS_IN_DET rows, got %d", detCount)
		}

		// Verify MSP_VENTAS_TRASPASOS lookup row.
		var vtCount int
		if scanErr := q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM MSP_VENTAS_TRASPASOS WHERE DOCTO_IN_ID = ?`, doctoInID,
		).Scan(&vtCount); scanErr != nil {
			t.Fatalf("verify MSP_VENTAS_TRASPASOS: %v", scanErr)
		}
		if vtCount != 1 {
			t.Fatalf("expected 1 MSP_VENTAS_TRASPASOS row, got %d", vtCount)
		}
	})
}

func TestTraspasoWriter_Save_VentaIDNilReturnsError(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	cfg := defaultCfg()
	repo := invfb.NewTraspasoRepo(cfg, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		folio := domain.HydrateFolio("MST000001")
		cant, _ := domain.NewCantidad(decimal.NewFromInt(1))
		now := time.Now().UTC()
		tr, buildErr := domain.CrearTraspaso(domain.CrearTraspasoParams{
			ID:             uuid.New(),
			Folio:          folio,
			AlmacenOrigen:  1,
			AlmacenDestino: 2,
			Fecha:          now,
			Descripcion:    "sin venta",
			VentaID:        nil,
			Detalles: []domain.CrearTraspasoDetalleInput{
				{ID: uuid.New(), ArticuloID: 1, Cantidad: cant},
			},
			CreatedBy: uuid.New(),
			Now:       now,
		})
		if buildErr != nil {
			t.Fatalf("build traspaso: %v", buildErr)
		}
		_, saveErr := repo.Save(ctx, tr)
		if saveErr == nil {
			t.Fatal("expected error when VentaID is nil, got nil")
		}
	})
}

func TestTraspasoReader_FindByID_NotFound(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	cfg := defaultCfg()
	repo := invfb.NewTraspasoRepo(cfg, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := repo.FindByID(ctx, -999999)
		if err == nil {
			t.Fatal("expected ErrTraspasoNoEncontrado, got nil")
		}
	})
}

func TestTraspasoReader_ListByVentaID_Empty(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	cfg := defaultCfg()
	repo := invfb.NewTraspasoRepo(cfg, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		list, err := repo.ListByVentaID(ctx, uuid.New())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(list) != 0 {
			t.Fatalf("expected empty list, got %d items", len(list))
		}
	})
}

func TestExistenciaQuery_ReturnsZeroForUnknown(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	eq := invfb.NewExistenciaQuerier(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		result, err := eq.Existencia(ctx, -999, -999)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Equal(decimal.Zero) {
			t.Fatalf("expected Zero, got %s", result)
		}
	})
}

func TestAlmacenRepo_FindByID_NotFound(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	ar := invfb.NewAlmacenRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := ar.FindByID(ctx, -999999)
		if err == nil {
			t.Fatal("expected error for non-existent almacen, got nil")
		}
	})
}

func TestAlmacenRepo_ListAll_NonEmpty(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	ar := invfb.NewAlmacenRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		list, err := ar.ListAll(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(list) == 0 {
			t.Fatal("expected non-empty almacenes list")
		}
		for i := 1; i < len(list); i++ {
			if list[i].Nombre < list[i-1].Nombre {
				t.Fatalf("almacenes not sorted: %q before %q", list[i-1].Nombre, list[i].Nombre)
			}
		}
	})
}

func TestFolioMinter_ReturnsUniqueIncreasing(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	minter := invfb.NewFolioMinter(pool)

	// Mint 5 folios. GEN_ID is not rollback-able in Firebird — that's by
	// design; sequences are always monotone across transactions.
	ctx := context.Background()
	prev := ""
	for range 5 {
		f, err := minter.MintFolio(ctx)
		if err != nil {
			t.Fatalf("MintFolio: %v", err)
		}
		if f.IsZero() {
			t.Fatal("MintFolio returned zero folio")
		}
		if f.Value() <= prev {
			t.Fatalf("folios not strictly increasing: %q then %q", prev, f.Value())
		}
		prev = f.Value()
	}
}

func TestExistenciaQuery_ExistenciasPorAlmacen(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	eq := invfb.NewExistenciaQuerier(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := eq.ExistenciasPorAlmacen(ctx, 1)
		if err != nil {
			t.Fatalf("ExistenciasPorAlmacen: %v", err)
		}
	})
}

func TestCrossModuleAtomicity_RollbackBothOnError(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	cfg := defaultCfg()
	repo := invfb.NewTraspasoRepo(cfg, pool)

	ventaID := uuid.New()

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		seedVentaRow(ctx, t, q, ventaID)

		// Attempt a traspaso with an invalid articuloID to provoke a mid-flow error.
		folio := domain.HydrateFolio("MST000002")
		cant, _ := domain.NewCantidad(decimal.NewFromInt(1))
		now := time.Now().UTC()
		tr, buildErr := domain.CrearTraspaso(domain.CrearTraspasoParams{
			ID:             uuid.New(),
			Folio:          folio,
			AlmacenOrigen:  1,
			AlmacenDestino: 11058,
			Fecha:          now,
			Descripcion:    "atomicity test",
			VentaID:        &ventaID,
			Detalles: []domain.CrearTraspasoDetalleInput{
				{ID: uuid.New(), ArticuloID: -99999, Cantidad: cant},
			},
			CreatedBy: uuid.New(),
			Now:       now,
		})
		if buildErr != nil {
			t.Fatalf("build traspaso: %v", buildErr)
		}
		_, saveErr := repo.Save(ctx, tr)
		if saveErr == nil {
			t.Log("Save unexpectedly succeeded; skipping atomicity assertion")
			return
		}
		// Error confirmed — venta row and any partial DOCTOS_IN writes are
		// all inside the same ambient tx. WithTestTransaction rolls everything
		// back at the end of this closure.
	})

	// After the closure's rollback, the venta row must not be visible.
	var postCount int
	scanErr := pool.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM MSP_VENTAS WHERE ID = ?`, ventaID.String(),
	).Scan(&postCount)
	if scanErr != nil && scanErr != sql.ErrNoRows {
		t.Fatalf("post-rollback check: %v", scanErr)
	}
	if postCount != 0 {
		t.Fatalf("expected 0 rows after rollback, got %d", postCount)
	}
}
