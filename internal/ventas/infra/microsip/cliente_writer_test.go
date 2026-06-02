//nolint:misspell // Spanish vocabulary (cliente, cobrador, zona, etc.) by convention.
package microsip_test

import (
	"context"
	"database/sql"
	"fmt"
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

		// Parse both clave values and verify result2 == result1 + 1.
		n1, err := strconv.Atoi(result1.ClaveCliente)
		require.NoError(t, err, "result1.ClaveCliente debe ser numérico")

		n2, err := strconv.Atoi(result2.ClaveCliente)
		require.NoError(t, err, "result2.ClaveCliente debe ser numérico")
		require.Equal(t, n1+1, n2, "segunda clave debe ser primera + 1 (numérico)")

		expected := fmt.Sprintf("%07d", n1+1)
		require.Equal(t, expected, result2.ClaveCliente, "segunda clave debe tener padding 7 dígitos")
	})
}
