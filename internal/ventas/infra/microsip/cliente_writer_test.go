//nolint:misspell // Spanish vocabulary (cliente, cobrador, zona, etc.) by convention.
package microsip_test

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/microsip"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// requireFBEnv skips the calling test when the FB_DATABASE env var is unset.
func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
	}
}

// ptr is a convenience helper to take the address of a string literal.
func ptr(s string) *string { return &s }

// defaultInput returns a MicrosipClienteInput with realistic Mexican-Spanish
// data and all catalog defaults for Mueblería Tehuacán.
func defaultInput(nombre string) outbound.MicrosipClienteInput {
	return outbound.MicrosipClienteInput{
		Nombre:                  nombre,
		Telefono:                ptr("2381863330"),
		Calle:                   "VICENTE GUERRERO",
		NumeroExterior:          ptr("99"),
		Colonia:                 "SAN PEDRO ACOQUIACO",
		Poblacion:               "TEHUACAN",
		CodigoPostal:            nil,
		Referencia:              nil,
		Latitud:                 nil,
		Longitud:                nil,
		ZonaClienteID:           21563,
		CobradorID:              11502,
		VendedorID:              88266,
		CiudadID:                outbound.DefaultCiudadID,
		EstadoID:                outbound.DefaultEstadoID,
		PaisID:                  outbound.DefaultPaisID,
		CondPagoID:              outbound.DefaultCondPagoID,
		TipoClienteID:           outbound.DefaultTipoClienteID,
		MonedaID:                outbound.DefaultMonedaID,
		ViaEmbarqueID:           outbound.DefaultViaEmbarqueID,
		ComprobanteDomicilioID:  outbound.DefaultComprobanteDomicilioID,
		IdentificacionOficialID: outbound.DefaultIdentificacionOficialID,
	}
}

func TestClienteWriter_Crear_HappyPath(t *testing.T) { //nolint:paralleltest // serial: Firebird MAX on CLAVES_CLIENTES would race across parallel txs
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewClienteWriter(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		in := defaultInput("LAURA HERNANDEZ MARTINEZ TEST 20260602")
		result, err := writer.Crear(ctx, in)
		require.NoError(t, err)
		require.NotZero(t, result.ClienteID)
		require.NotZero(t, result.DirCliID)
		require.NotEmpty(t, result.ClaveCliente)
		require.Len(t, result.ClaveCliente, 7, "clave_cliente debe ser 7 dígitos")

		q := firebird.GetQuerier(ctx, pool.DB)

		// Verify CLIENTES row.
		var nombre string
		err = q.QueryRowContext(ctx, `SELECT NOMBRE FROM CLIENTES WHERE CLIENTE_ID = ?`, result.ClienteID).Scan(&nombre)
		require.NoError(t, err)
		require.Equal(t, in.Nombre, nombre)

		// Verify CLAVES_CLIENTES row.
		var clave string
		var rolID int
		err = q.QueryRowContext(ctx, `SELECT CLAVE_CLIENTE, ROL_CLAVE_CLI_ID FROM CLAVES_CLIENTES WHERE CLIENTE_ID = ?`, result.ClienteID).Scan(&clave, &rolID)
		require.NoError(t, err)
		require.Equal(t, result.ClaveCliente, clave)
		require.Equal(t, 2, rolID)

		// Verify DIRS_CLIENTES row.
		var nombreConsig string
		err = q.QueryRowContext(ctx, `SELECT NOMBRE_CONSIG FROM DIRS_CLIENTES WHERE CLIENTE_ID = ?`, result.ClienteID).Scan(&nombreConsig)
		require.NoError(t, err)
		require.Equal(t, "Dirección principal", nombreConsig)

		// Verify LIBRES_CLIENTES row.
		var libresClienteID int
		err = q.QueryRowContext(ctx, `SELECT CLIENTE_ID FROM LIBRES_CLIENTES WHERE CLIENTE_ID = ?`, result.ClienteID).Scan(&libresClienteID)
		require.NoError(t, err)
		require.Equal(t, result.ClienteID, libresClienteID)
	})
}

func TestClienteWriter_Crear_LimiteCreditoYTelefono(t *testing.T) { //nolint:paralleltest // serial: Firebird MAX on CLAVES_CLIENTES would race across parallel txs
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	// WithLimiteCredito mirrors the production wiring (MICROSIP_CLIENTE_LIMITE_CREDITO=10000).
	writer := microsip.NewClienteWriter(pool).WithLimiteCredito(10000)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		in := defaultInput("PEDRO RAMOS SANCHEZ TEST 20260611")
		// Telefono ya viene canonizado a 10 dígitos por el VO upstream.
		in.Telefono = ptr("2381863330")

		result, err := writer.Crear(ctx, in)
		require.NoError(t, err)

		q := firebird.GetQuerier(ctx, pool.DB)

		// LIMITE_CREDITO debe ser el configurado (10000), no 0.
		var limite int
		err = q.QueryRowContext(ctx,
			`SELECT LIMITE_CREDITO FROM CLIENTES WHERE CLIENTE_ID = ?`, result.ClienteID,
		).Scan(&limite)
		require.NoError(t, err)
		require.Equal(t, 10000, limite, "LIMITE_CREDITO debe ser el valor configurado")

		// TELEFONO1 debe quedar en 10 dígitos (sin +52).
		var telefono string
		err = q.QueryRowContext(ctx,
			`SELECT TELEFONO1 FROM DIRS_CLIENTES WHERE CLIENTE_ID = ?`, result.ClienteID,
		).Scan(&telefono)
		require.NoError(t, err)
		require.Equal(t, "2381863330", telefono, "TELEFONO1 debe ser 10 dígitos")
	})
}

func TestClienteWriter_Crear_ConGPS(t *testing.T) { //nolint:paralleltest // serial: Firebird MAX on CLAVES_CLIENTES would race across parallel txs
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewClienteWriter(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		in := defaultInput("MARCO ANTONIO RAMIREZ LOPEZ TEST 20260602")
		in.Latitud = ptr("18.4671")
		in.Longitud = ptr("-97.3919")

		result, err := writer.Crear(ctx, in)
		require.NoError(t, err)

		q := firebird.GetQuerier(ctx, pool.DB)

		var latitud, longitud string
		err = q.QueryRowContext(ctx,
			`SELECT U_LATITUD, U_LONGITUD FROM LIBRES_CLIENTES WHERE CLIENTE_ID = ?`,
			result.ClienteID,
		).Scan(&latitud, &longitud)
		require.NoError(t, err)
		require.Equal(t, "18.4671", latitud)
		require.Equal(t, "-97.3919", longitud)
	})
}

func TestClienteWriter_Crear_ConReferencia(t *testing.T) { //nolint:paralleltest // serial: Firebird MAX on CLAVES_CLIENTES would race across parallel txs
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewClienteWriter(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		in := defaultInput("SOFIA GARCIA MENDOZA TEST 20260602")
		in.Referencia = ptr("CASA AZUL ESQUINA")

		result, err := writer.Crear(ctx, in)
		require.NoError(t, err)

		q := firebird.GetQuerier(ctx, pool.DB)

		var referencia string
		err = q.QueryRowContext(ctx,
			`SELECT REFERENCIA FROM LIBRES_CLIENTES WHERE CLIENTE_ID = ?`,
			result.ClienteID,
		).Scan(&referencia)
		require.NoError(t, err)
		require.Equal(t, "CASA AZUL ESQUINA", referencia)
	})
}

func TestClienteWriter_Crear_SinTelefono(t *testing.T) { //nolint:paralleltest // serial: Firebird MAX on CLAVES_CLIENTES would race across parallel txs
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewClienteWriter(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		in := defaultInput("JOSE LUIS MARTINEZ PEREZ TEST 20260602")
		in.Telefono = nil

		result, err := writer.Crear(ctx, in)
		require.NoError(t, err)

		q := firebird.GetQuerier(ctx, pool.DB)

		var telefono sql.NullString
		err = q.QueryRowContext(ctx,
			`SELECT TELEFONO1 FROM DIRS_CLIENTES WHERE CLIENTE_ID = ?`,
			result.ClienteID,
		).Scan(&telefono)
		require.NoError(t, err)
		require.False(t, telefono.Valid, "TELEFONO1 debe ser NULL cuando no se proporciona")
	})
}

// TestClienteWriter_Crear_ZonaNullInsert verifies that passing the sentinel -1
// for ZonaClienteID, CobradorID, and VendedorID writes NULL to the CLIENTES
// and DIRS_CLIENTES rows, confirming those columns are nullable in Microsip.
func TestClienteWriter_Crear_ZonaNullInsert(t *testing.T) { //nolint:paralleltest // serial: Firebird MAX on CLAVES_CLIENTES would race across parallel txs
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewClienteWriter(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		in := defaultInput("CARMEN RUIZ ESPINOZA TEST NULL 20260629")
		// Sentinel -1 maps to SQL NULL for these three optional FK columns.
		in.ZonaClienteID = -1
		in.CobradorID = -1
		in.VendedorID = -1

		result, err := writer.Crear(ctx, in)
		require.NoError(t, err, "crear with NULL zona/cobrador/vendedor must succeed")
		require.NotZero(t, result.ClienteID)

		q := firebird.GetQuerier(ctx, pool.DB)

		// ZONA_CLIENTE_ID, COBRADOR_ID, VENDEDOR_ID must all be NULL in CLIENTES.
		var zonaID, cobradorID, vendedorID sql.NullInt64
		err = q.QueryRowContext(ctx,
			`SELECT ZONA_CLIENTE_ID, COBRADOR_ID, VENDEDOR_ID FROM CLIENTES WHERE CLIENTE_ID = ?`,
			result.ClienteID,
		).Scan(&zonaID, &cobradorID, &vendedorID)
		require.NoError(t, err)
		require.False(t, zonaID.Valid, "ZONA_CLIENTE_ID must be NULL when sentinel -1 is passed")
		require.False(t, cobradorID.Valid, "COBRADOR_ID must be NULL when sentinel -1 is passed")
		require.False(t, vendedorID.Valid, "VENDEDOR_ID must be NULL when sentinel -1 is passed")
	})
}

func TestClienteWriter_NextClaveCliente_Incremental(t *testing.T) { //nolint:paralleltest // serial: two sequential Crear calls inside same tx verify +1 increment
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewClienteWriter(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		in1 := defaultInput("ANA MARIA TORRES RAMOS TEST A 20260602")
		result1, err := writer.Crear(ctx, in1)
		require.NoError(t, err)

		in2 := defaultInput("ANA MARIA TORRES RAMOS TEST B 20260602")
		result2, err := writer.Crear(ctx, in2)
		require.NoError(t, err)

		// Parse both clave values and verify result2 > result1. Strict +1 is
		// not guaranteed because nextClaveCliente shares the ID_CATALOGOS
		// generator with Microsip triggers (CLAVES_CLIENTES_BEFINS assigns
		// CLAVE_CLIENTE_ID via the same generator on each INSERT), so gaps
		// of >=2 between two Crear() calls are expected.
		n1, err := strconv.Atoi(result1.ClaveCliente)
		require.NoError(t, err, "result1.ClaveCliente debe ser numérico")

		n2, err := strconv.Atoi(result2.ClaveCliente)
		require.NoError(t, err, "result2.ClaveCliente debe ser numérico")
		require.Greater(t, n2, n1, "segunda clave debe ser estrictamente mayor que la primera")

		require.Len(t, result2.ClaveCliente, 7, "segunda clave debe tener padding 7 dígitos")
	})
}
