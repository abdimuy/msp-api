//nolint:misspell // Spanish vocabulary (productos, etc.) by convention.
package ventfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	platform "github.com/abdimuy/msp-api/internal/platform/domain"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// testNow returns a stable instant used across the integration tests so
// timestamps round-trip cleanly through ScanUTCTime regardless of process
// timezone.
func testNow() time.Time {
	return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
}

// seedUsuarioRow inserts a self-referential usuario row inside the active
// test tx and returns its UUID. The CREATED_BY column on MSP_USUARIOS is a
// self-FK, so the helper points it at the freshly-generated id. The returned
// ID is suitable as a foreign key target for venta CREATED_BY / UPDATED_BY
// columns and for VENDEDOR_USUARIO_ID.
func seedUsuarioRow(ctx context.Context, t *testing.T, pool *firebird.Pool) uuid.UUID {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	id := uuid.New()
	now := testNow()
	suffix := id.String()
	_, err := q.ExecContext(
		ctx,
		`INSERT INTO MSP_USUARIOS
		 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO,
		  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, ?, 'venta-test', TRUE, ?, ?, ?, ?)`,
		id.String(), "fb-venta-"+suffix, "venta-"+suffix+"@example.invalid",
		now, now, id.String(), id.String(),
	)
	require.NoError(t, err, "seed usuario for venta test")
	return id
}

// newVentaInput aggregates the inputs commonly tweaked in tests. The default
// shape is a CONTADO venta with one producto and one vendedor.
type newVentaInput struct {
	id        uuid.UUID
	createdBy uuid.UUID
	vendedor  uuid.UUID
	tipoVenta domain.TipoVenta
	fecha     time.Time
}

// buildVenta constructs a Venta entity ready for repo.Save. createdBy is the
// FK target for CREATED_BY/UPDATED_BY; vendedor is the FK target for the
// snapshot vendedor's VENDEDOR_USUARIO_ID.
func buildVenta(t *testing.T, in newVentaInput) *domain.Venta {
	t.Helper()
	if in.id == uuid.Nil {
		in.id = uuid.New()
	}
	if in.tipoVenta == "" {
		in.tipoVenta = domain.TipoVentaContado
	}
	if in.fecha.IsZero() {
		in.fecha = testNow()
	}
	nombre, err := domain.NewNombreCliente("Cliente Test")
	require.NoError(t, err)
	dir, err := domain.NewDireccion(domain.NewDireccionParams{
		Calle: "Av. Reforma", Colonia: "Centro", Poblacion: "CDMX", Ciudad: "CDMX",
	})
	require.NoError(t, err)
	gps, err := domain.NewGPSCoords(19.4326, -99.1332)
	require.NoError(t, err)
	montos, err := domain.NewMontoSnapshot(
		decimal.RequireFromString("1500.00"),
		decimal.RequireFromString("1300.00"),
		decimal.RequireFromString("1000.00"),
	)
	require.NoError(t, err)
	productoPrecios, err := domain.NewMontoSnapshot(
		decimal.RequireFromString("150.00"),
		decimal.RequireFromString("130.00"),
		decimal.RequireFromString("100.00"),
	)
	require.NoError(t, err)
	params := domain.CrearVentaParams{
		ID: in.id,
		Cliente: domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{
			Nombre: nombre,
		}),
		Direccion:      dir,
		GPS:            gps,
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		FechaVenta:     in.fecha,
		TipoVenta:      in.tipoVenta,
		Montos:         montos,
		Productos: []domain.CrearVentaProductoInput{{
			ID:         uuid.New(),
			ArticuloID: 100,
			Articulo:   "Producto Demo",
			Cantidad:   decimal.RequireFromString("2.0000"),
			Precios:    productoPrecios,
		}},
		Vendedores: []domain.CrearVentaVendedorInput{{
			ID:        uuid.New(),
			UsuarioID: in.vendedor,
			Email:     "vendedor-" + uuid.NewString() + "@example.invalid",
			Nombre:    "Vendedor Test",
		}},
		CreatedBy: in.createdBy,
		Now:       in.fecha,
	}
	if in.tipoVenta == domain.TipoVentaCredito {
		plan, planErr := domain.NewPlanCredito(
			12,
			decimal.RequireFromString("100.00"),
			decimal.RequireFromString("125.00"),
			domain.FrecPagoSemanal,
		)
		require.NoError(t, planErr)
		params.PlanCredito = &plan
		dc, dcErr := domain.NewDiaCobranzaSemana(domain.DiaSemanaLunes)
		require.NoError(t, dcErr)
		params.DiaCobranza = &dc
	}
	v, err := domain.CrearVenta(params)
	require.NoError(t, err)
	return v
}

// richVentaOptions configure buildRichVenta.
type richVentaOptions struct {
	withCombo       bool
	withCreditoMes  bool
	withTelefono    bool
	withAval        bool
	withNumExterior bool
	withZonaCliente bool
	withNota        bool
	withDescImagen  bool
}

// buildRichVenta constructs a venta exercising every optional branch in the
// repository's INSERT/SELECT paths: telefono, aval, numero exterior,
// zona_cliente_id, nota, combo + producto-with-combo-FK, plan credito by day
// of month, and optional descripcion in an imagen.
//
//nolint:funlen // wide options matrix.
func buildRichVenta(t *testing.T, createdBy, vendedor uuid.UUID, opts richVentaOptions) *domain.Venta {
	t.Helper()
	nombre, err := domain.NewNombreCliente("Cliente Rico")
	require.NoError(t, err)
	var aval *domain.NombreCliente
	if opts.withAval {
		a := domain.HydrateNombreCliente("Avalista Pérez")
		aval = &a
	}
	cliente := domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{
		Nombre: nombre, Aval: aval,
	})
	var numExt *string
	if opts.withNumExterior {
		s := "42-B"
		numExt = &s
	}
	var zona *int
	if opts.withZonaCliente {
		z := 7
		zona = &z
	}
	dir, err := domain.NewDireccion(domain.NewDireccionParams{
		Calle: "Av. Insurgentes", NumeroExterior: numExt, Colonia: "Roma",
		Poblacion: "CDMX", Ciudad: "CDMX", ZonaClienteID: zona,
	})
	require.NoError(t, err)
	gps, err := domain.NewGPSCoords(19.0, -99.0)
	require.NoError(t, err)
	montos, err := domain.NewMontoSnapshot(
		decimal.RequireFromString("2000.00"),
		decimal.RequireFromString("1800.00"),
		decimal.RequireFromString("1500.00"),
	)
	require.NoError(t, err)
	productoPrecios, err := domain.NewMontoSnapshot(
		decimal.RequireFromString("200.00"),
		decimal.RequireFromString("180.00"),
		decimal.RequireFromString("150.00"),
	)
	require.NoError(t, err)
	var nota *string
	if opts.withNota {
		s := "una nota larga"
		nota = &s
	}
	var combos []domain.CrearVentaComboInput
	var productoComboID *uuid.UUID
	if opts.withCombo {
		comboID := uuid.New()
		combos = append(combos, domain.CrearVentaComboInput{
			ID: comboID, Nombre: "Combo Demo", Precios: montos,
		})
		productoComboID = &comboID
	}
	params := domain.CrearVentaParams{
		ID: uuid.New(), Cliente: cliente, Direccion: dir, GPS: gps,
		AlmacenOrigen: 1, AlmacenDestino: 2, FechaVenta: testNow(),
		TipoVenta: domain.TipoVentaContado, Montos: montos, Nota: nota,
		Combos: combos,
		Productos: []domain.CrearVentaProductoInput{{
			ID: uuid.New(), ArticuloID: 200, Articulo: "Mesa",
			Cantidad: decimal.RequireFromString("1.0000"), Precios: productoPrecios,
			ComboID: productoComboID,
		}},
		Vendedores: []domain.CrearVentaVendedorInput{{
			ID: uuid.New(), UsuarioID: vendedor,
			Email: "rico-" + uuid.NewString() + "@example.invalid", Nombre: "Vendedor Rico",
		}},
		CreatedBy: createdBy, Now: testNow(),
	}
	if opts.withCreditoMes {
		// override tipo, plan, dia cobranza by month
		params.TipoVenta = domain.TipoVentaCredito
		plan, planErr := domain.NewPlanCredito(
			6,
			decimal.RequireFromString("50.00"),
			decimal.RequireFromString("250.00"),
			domain.FrecPagoMensual,
		)
		require.NoError(t, planErr)
		params.PlanCredito = &plan
		dc, dcErr := domain.NewDiaCobranzaMes(15)
		require.NoError(t, dcErr)
		params.DiaCobranza = &dc
	}
	v, err := domain.CrearVenta(params)
	require.NoError(t, err)
	if opts.withTelefono {
		// rebuild cliente with telefono — domain doesn't expose a setter so
		// we Hydrate a fresh aggregate over the same fields.
		tel := platform.HydrateTelefono("5551234567")
		newCliente := domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{
			Nombre: nombre, Telefono: &tel, Aval: aval,
		})
		a := v.Audit()
		v = domain.HydrateVenta(domain.HydrateVentaParams{
			ID: v.ID(), Cliente: newCliente, Direccion: v.Direccion(), GPS: v.GPS(),
			AlmacenOrigen: v.AlmacenOrigen(), AlmacenDestino: v.AlmacenDestino(),
			FechaVenta: v.FechaVenta(), TipoVenta: v.TipoVenta(), Montos: v.Montos(),
			PlanCredito: v.PlanCredito(), DiaCobranza: v.DiaCobranza(), Nota: v.Nota(),
			Combos: v.CombosForRepo(), Productos: v.ProductosForRepo(),
			Vendedores: v.VendedoresForRepo(), Imagenes: v.ImagenesForRepo(),
			Cancelacion: v.Cancelacion(),
			CreatedAt:   a.CreatedAt(), UpdatedAt: a.UpdatedAt(),
			CreatedBy: a.CreatedBy(), UpdatedBy: a.UpdatedBy(),
		})
	}
	return v
}

// buildImagen constructs an Imagen via the aggregate's AdjuntarImagen path
// using a fresh, never-saved venta and then extracts the imagen entity. Used
// in tests that exercise InsertImagen/DeleteImagen on an already-saved venta.
func buildImagen(t *testing.T, createdBy uuid.UUID) *domain.Imagen {
	t.Helper()
	return buildImagenWithDesc(t, createdBy, nil)
}

// buildImagenWithDesc is like buildImagen but accepts an optional descripcion
// so tests can exercise the nullable column branch.
func buildImagenWithDesc(t *testing.T, createdBy uuid.UUID, desc *string) *domain.Imagen {
	t.Helper()
	tmp := buildVenta(t, newVentaInput{
		createdBy: createdBy,
		vendedor:  createdBy,
	})
	storage, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "ventas/test/img-"+uuid.NewString()+".jpg")
	require.NoError(t, err)
	img, err := tmp.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:          uuid.New(),
		Storage:     storage,
		Mime:        domain.MimeJPEG,
		SizeBytes:   1024,
		Descripcion: desc,
		By:          createdBy,
		Now:         testNow(),
	})
	require.NoError(t, err)
	return img
}
