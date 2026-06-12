// Package microsip — see venta_writer.go for package-level doc.
//
//nolint:misspell // Microsip column names are Spanish by convention.
package microsip

import (
	"context"
	"fmt"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ─── SQL constants ────────────────────────────────────────────────────────────

// selectNextClaveCliente claims the next CLAVE_CLIENTE value from the
// Firebird generator ID_CATALOGOS. The generator is monotonic, race-safe by
// construction, and avoids any table scan of CLAVES_CLIENTES.
//
// Why not MAX(CAST(CLAVE_CLIENTE AS INTEGER))+1: the dev DB MUEBLERA.FDB has
// at least one corrupt data page in CLAVES_CLIENTES that triggers the engine
// bugcheck `internal Firebird consistency check (wrong record length, file:
// vio.cpp line: 1448)` on any full-table scan (CLAVES_CLIENTES has no index
// on ROL_CLAVE_CLI_ID, so every filtered query scans all pages). Using the
// generator sidesteps the table entirely.
//
// Consequence vs Microsip GUI: the GUI assigns claves like "0044523" by
// scanning MAX+1. The generator currently sits in the millions, so claves we
// create here will be 7-digit strings like "3075474". Functionally
// equivalent — Microsip stores CLAVE_CLIENTE as VARCHAR(20), uniqueness is
// guarded by CLAVES_CLIENTES_AK1, and downstream reads (e.g. lookupClaveCliente
// in venta_writer.go) treat it as an opaque string.
const selectNextClaveCliente = `SELECT GEN_ID(ID_CATALOGOS, 1) FROM RDB$DATABASE`

// insertCliente inserts the CLIENTES header row (16 columns, verified SQL).
//
//nolint:gosec // SQL constant, not user input.
const insertCliente = `INSERT INTO CLIENTES
  (CLIENTE_ID, NOMBRE, ESTATUS,
   COBRAR_IMPUESTOS, GENERAR_INTERESES, EMITIR_EDOCTA, DIFERIR_CFDI_COBROS,
   LIMITE_CREDITO, MONEDA_ID, COND_PAGO_ID, TIPO_CLIENTE_ID,
   ZONA_CLIENTE_ID, COBRADOR_ID, VENDEDOR_ID,
   RETIENE_IMPUESTOS, SUJETO_IEPS)
VALUES
  (?, ?, 'A',
   'S', 'S', 'S', FALSE,
   ?, ?, ?, ?,
   ?, ?, ?,
   'N', 'N')`

// insertClaveCliente inserts the CLAVES_CLIENTES row.
// CLAVE_CLIENTE_ID = -1 → trigger CLAVES_CLIENTES_BEFINS replaces it.
//
//nolint:gosec // SQL constant, not user input.
const insertClaveCliente = `INSERT INTO CLAVES_CLIENTES
  (CLAVE_CLIENTE_ID, CLAVE_CLIENTE, CLIENTE_ID, ROL_CLAVE_CLI_ID)
VALUES (-1, ?, ?, ?)`

// insertDirCliente inserts the DIRS_CLIENTES row (16 columns, verified SQL).
//
//nolint:gosec // SQL constant, not user input.
const insertDirCliente = `INSERT INTO DIRS_CLIENTES
  (DIR_CLI_ID, CLIENTE_ID, NOMBRE_CONSIG, CALLE,
   CIUDAD_ID, ESTADO_ID, PAIS_ID,
   TELEFONO1, VIA_EMBARQUE_ID,
   ES_DIR_PPAL, USAR_PARA_ENVIOS, USAR_PARA_FACTURAR,
   NOMBRE_CALLE, NUM_EXTERIOR, COLONIA, POBLACION)
VALUES
  (?, ?, 'Dirección principal', ?,
   ?, ?, ?,
   ?, ?,
   'S', 'S', 'S',
   ?, ?, ?, ?)`

// insertLibresClienteBase inserts the LIBRES_CLIENTES row (base columns only).
//
//nolint:gosec // SQL constant, not user input.
const insertLibresClienteBase = `INSERT INTO LIBRES_CLIENTES
  (CLIENTE_ID, COMPROBANTE_DE_DOMICILIO, IDENTIFICACION_OFICIAL, LOCALIDAD)
VALUES (?, ?, ?, ?)`

// insertLibresClienteConOpcionales inserts LIBRES_CLIENTES with optional
// REFERENCIA, U_LATITUD, and U_LONGITUD columns.
//
//nolint:gosec // SQL constant, not user input.
const insertLibresClienteConOpcionales = `INSERT INTO LIBRES_CLIENTES
  (CLIENTE_ID, COMPROBANTE_DE_DOMICILIO, IDENTIFICACION_OFICIAL, LOCALIDAD,
   REFERENCIA, U_LATITUD, U_LONGITUD)
VALUES (?, ?, ?, ?, ?, ?, ?)`

// ─── ClienteWriter ────────────────────────────────────────────────────────────

// ClienteWriter implements outbound.MicrosipClienteWriter against the Microsip
// Firebird database.
type ClienteWriter struct {
	pool *firebird.Pool
	// limiteCredito is the LIMITE_CREDITO stamped on every auto-created CLIENTES
	// row. Injected from config (MICROSIP_CLIENTE_LIMITE_CREDITO, default 10000)
	// via WithLimiteCredito; zero when unset.
	limiteCredito int
}

// NewClienteWriter builds a ClienteWriter wired to the given Firebird pool.
func NewClienteWriter(pool *firebird.Pool) *ClienteWriter {
	return &ClienteWriter{pool: pool}
}

// WithLimiteCredito configures the LIMITE_CREDITO assigned to auto-created
// CLIENTES rows. Returns w for fluent wiring at the composition root.
func (w *ClienteWriter) WithLimiteCredito(limite int) *ClienteWriter {
	w.limiteCredito = limite
	return w
}

// Compile-time check.
var _ outbound.MicrosipClienteWriter = (*ClienteWriter)(nil)

// Crear materializes a new cliente into Microsip's CLIENTES family within the
// caller's ambient transaction. Phases:
//  1. Claim CLIENTE_ID and DIR_CLI_ID from GEN_ID(ID_DOCTOS).
//  2. Compute next CLAVE_CLIENTE (WITH LOCK for concurrency safety).
//  3. INSERT CLIENTES.
//  4. INSERT CLAVES_CLIENTES (CLAVE_CLIENTE_ID=-1 → trigger assigns real ID).
//  5. Compose CALLE string. INSERT DIRS_CLIENTES.
//  6. INSERT LIBRES_CLIENTES (with optional fields when non-nil).
func (w *ClienteWriter) Crear(ctx context.Context, in outbound.MicrosipClienteInput) (outbound.MicrosipClienteResult, error) {
	q := firebird.GetQuerier(ctx, w.pool.DB)

	ids, err := w.claimIDs(ctx, q)
	if err != nil {
		return outbound.MicrosipClienteResult{}, err
	}

	if err := w.execInsertCliente(ctx, q, ids.clienteID, in); err != nil {
		return outbound.MicrosipClienteResult{}, err
	}

	if err := w.execInsertClaveCliente(ctx, q, ids.claveCliente, ids.clienteID); err != nil {
		return outbound.MicrosipClienteResult{}, err
	}

	if err := w.execInsertDirCliente(ctx, q, ids.dirCliID, ids.clienteID, in); err != nil {
		return outbound.MicrosipClienteResult{}, err
	}

	if err := w.execInsertLibresCliente(ctx, q, ids.clienteID, in); err != nil {
		return outbound.MicrosipClienteResult{}, err
	}

	return outbound.MicrosipClienteResult{
		ClienteID:    ids.clienteID,
		DirCliID:     ids.dirCliID,
		ClaveCliente: ids.claveCliente,
	}, nil
}

// ─── Phase helpers ────────────────────────────────────────────────────────────

// claimIDsResult groups the three IDs returned by claimIDs.
type claimIDsResult struct {
	clienteID    int
	dirCliID     int
	claveCliente string
}

// claimIDs claims CLIENTE_ID, DIR_CLI_ID from the shared generator and
// computes the next CLAVE_CLIENTE.
func (w *ClienteWriter) claimIDs(ctx context.Context, q firebird.Querier) (claimIDsResult, error) {
	clienteID, err := nextID(ctx, q)
	if err != nil {
		return claimIDsResult{}, fmt.Errorf("microsip crear cliente: claim cliente_id: %w", err)
	}

	dirCliID, err := nextID(ctx, q)
	if err != nil {
		return claimIDsResult{}, fmt.Errorf("microsip crear cliente: claim dir_cli_id: %w", err)
	}

	clave, err := nextClaveCliente(ctx, q)
	if err != nil {
		return claimIDsResult{}, fmt.Errorf("microsip crear cliente: next clave_cliente: %w", err)
	}

	return claimIDsResult{clienteID: clienteID, dirCliID: dirCliID, claveCliente: clave}, nil
}

// execInsertCliente runs the CLIENTES INSERT.
func (w *ClienteWriter) execInsertCliente(ctx context.Context, q firebird.Querier, clienteID int, in outbound.MicrosipClienteInput) error {
	_, err := q.ExecContext(ctx, insertCliente,
		clienteID, in.Nombre,
		w.limiteCredito, in.MonedaID, in.CondPagoID, in.TipoClienteID,
		in.ZonaClienteID, in.CobradorID, in.VendedorID,
	)
	if err != nil {
		return fmt.Errorf("microsip crear cliente: insert clientes: %w", firebird.MapError(err))
	}
	return nil
}

// execInsertClaveCliente runs the CLAVES_CLIENTES INSERT.
func (w *ClienteWriter) execInsertClaveCliente(ctx context.Context, q firebird.Querier, clave string, clienteID int) error {
	_, err := q.ExecContext(ctx, insertClaveCliente,
		clave, clienteID, outbound.DefaultRolClaveClientePrincipal,
	)
	if err != nil {
		return fmt.Errorf("microsip crear cliente: insert claves_clientes: %w", firebird.MapError(err))
	}
	return nil
}

// execInsertDirCliente composes CALLE and runs the DIRS_CLIENTES INSERT.
func (w *ClienteWriter) execInsertDirCliente(ctx context.Context, q firebird.Querier, dirCliID, clienteID int, in outbound.MicrosipClienteInput) error {
	calle := buildCalle(in)

	var telefono any
	if in.Telefono != nil {
		telefono = *in.Telefono
	}

	var numExterior any
	if in.NumeroExterior != nil {
		numExterior = *in.NumeroExterior
	}

	_, err := q.ExecContext(ctx, insertDirCliente,
		dirCliID, clienteID, calle,
		in.CiudadID, in.EstadoID, in.PaisID,
		telefono, in.ViaEmbarqueID,
		in.Calle, numExterior, in.Colonia, in.Poblacion,
	)
	if err != nil {
		return fmt.Errorf("microsip crear cliente: insert dirs_clientes: %w", firebird.MapError(err))
	}
	return nil
}

// execInsertLibresCliente runs the LIBRES_CLIENTES INSERT, choosing the SQL
// variant with or without optional columns.
func (w *ClienteWriter) execInsertLibresCliente(ctx context.Context, q firebird.Querier, clienteID int, in outbound.MicrosipClienteInput) error {
	if in.Referencia == nil && in.Latitud == nil && in.Longitud == nil {
		_, err := q.ExecContext(ctx, insertLibresClienteBase,
			clienteID, in.ComprobanteDomicilioID, in.IdentificacionOficialID, outbound.DefaultLocalidad,
		)
		if err != nil {
			return fmt.Errorf("microsip crear cliente: insert libres_clientes: %w", firebird.MapError(err))
		}
		return nil
	}

	var referencia, latitud, longitud any
	if in.Referencia != nil {
		referencia = *in.Referencia
	}
	if in.Latitud != nil {
		latitud = *in.Latitud
	}
	if in.Longitud != nil {
		longitud = *in.Longitud
	}

	_, err := q.ExecContext(ctx, insertLibresClienteConOpcionales,
		clienteID, in.ComprobanteDomicilioID, in.IdentificacionOficialID, outbound.DefaultLocalidad,
		referencia, latitud, longitud,
	)
	if err != nil {
		return fmt.Errorf("microsip crear cliente: insert libres_clientes (con opcionales): %w", firebird.MapError(err))
	}
	return nil
}

// ─── Pure helpers ─────────────────────────────────────────────────────────────

// nextClaveCliente claims the next CLAVE_CLIENTE value from the
// ID_CATALOGOS generator and returns it zero-padded to 7 digits.
func nextClaveCliente(ctx context.Context, q firebird.Querier) (string, error) {
	var nextID int
	if err := q.QueryRowContext(ctx, selectNextClaveCliente).Scan(&nextID); err != nil {
		return "", fmt.Errorf("GEN clave_cliente: %w", firebird.MapError(err))
	}
	return fmt.Sprintf("%07d", nextID), nil
}

// buildCalle composes the CALLE field for DIRS_CLIENTES:
// NOMBRE_CALLE + " " + NUM_EXT (when present) + "\n" + COLONIA + ", " + POBLACION.
func buildCalle(in outbound.MicrosipClienteInput) string {
	numExt := ""
	if in.NumeroExterior != nil && *in.NumeroExterior != "" {
		numExt = " " + *in.NumeroExterior
	}
	return fmt.Sprintf("%s%s\n%s, %s", in.Calle, numExt, in.Colonia, in.Poblacion)
}
