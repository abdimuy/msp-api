//nolint:misspell // Spanish domain vocabulary (cohorte, segmento) by project convention.
package reactivacionfb

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// rowScanner is the minimal surface satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// ─── ClienteUniverso row mapper ────────────────────────────────────────────────

// universoRowRaw is the intermediate scan target for one row of
// selectUniversoTehuacan. Microsip text columns (CLIENTES.NOMBRE,
// DIRS_CLIENTES.TELEFONO1) are CHARACTER SET NONE but arrive as UTF-8 because the
// connection uses charset=UTF8 — scanned as plain string / sql.NullString, NOT
// firebird.Win1252 (which would double-decode). Column order must match
// selectUniversoTehuacan exactly.
type universoRowRaw struct {
	clienteID         int
	nombre            string // CLIENTES.NOMBRE — transliterated to UTF-8 by Firebird
	telefono          sql.NullString
	segmento          string // ASCII literal emitted by the CASE expression
	saldoRaw          any    // NUMERIC(18,2)
	porLiquidarPctRaw any    // NUMERIC(5,2)
	fechaUltimaCompra any    // TIMESTAMP nullable
}

func (r *universoRowRaw) scanFrom(s rowScanner) error {
	return s.Scan(
		&r.clienteID,
		&r.nombre,
		&r.telefono,
		&r.segmento,
		&r.saldoRaw,
		&r.porLiquidarPctRaw,
		&r.fechaUltimaCompra,
	)
}

func assembleUniverso(r *universoRowRaw) (outbound.ClienteUniverso, error) {
	seg, err := domain.ParseSegmento(r.segmento)
	if err != nil {
		return outbound.ClienteUniverso{}, err
	}
	saldo, err := firebird.ScanDecimal(r.saldoRaw, 2)
	if err != nil {
		return outbound.ClienteUniverso{}, err
	}
	porLiquidarPct, err := scanNullableDecimal(r.porLiquidarPctRaw)
	if err != nil {
		return outbound.ClienteUniverso{}, err
	}
	fechaUltimaCompra, err := scanNullableTime(r.fechaUltimaCompra)
	if err != nil {
		return outbound.ClienteUniverso{}, err
	}
	return outbound.ClienteUniverso{
		ClienteID:         r.clienteID,
		Nombre:            r.nombre,
		Telefono:          nullStringVal(r.telefono),
		Segmento:          seg,
		Saldo:             saldo,
		PorLiquidarPct:    porLiquidarPct,
		FechaUltimaCompra: fechaUltimaCompra,
	}, nil
}

// ─── CohorteCliente row mapper ─────────────────────────────────────────────────

// cohorteRowRaw is the intermediate scan target for one MSP_RX_COHORTE row.
// The table is CHARACTER SET UTF8 — plain string / sql.NullString is the correct
// scan target. Column order must match cohorteCols exactly.
type cohorteRowRaw struct {
	idRaw                string
	clienteID            int
	nombre               sql.NullString
	telefono             sql.NullString
	segmento             string
	enControl            int16 // SMALLINT; 0=false 1=true
	fueContactado        int16 // SMALLINT; 0=false 1=true
	cohorteFechaRaw      any   // TIMESTAMP NOT NULL
	fechaUltimaCompraRaw any   // TIMESTAMP nullable
	saldoRaw             any   // NUMERIC(18,2) NOT NULL
	porLiquidarPctRaw    any   // NUMERIC(5,2) nullable
	createdAtRaw         any   // TIMESTAMP NOT NULL
	updatedAtRaw         any   // TIMESTAMP NOT NULL
}

func (r *cohorteRowRaw) scanFrom(s rowScanner) error {
	return s.Scan(
		&r.idRaw,
		&r.clienteID,
		&r.nombre,
		&r.telefono,
		&r.segmento,
		&r.enControl,
		&r.fueContactado,
		&r.cohorteFechaRaw,
		&r.fechaUltimaCompraRaw,
		&r.saldoRaw,
		&r.porLiquidarPctRaw,
		&r.createdAtRaw,
		&r.updatedAtRaw,
	)
}

func assembleCohorte(r *cohorteRowRaw) (*domain.CohorteCliente, error) {
	id, err := parseUUIDColumn("ID", r.idRaw)
	if err != nil {
		return nil, err
	}
	seg, err := domain.ParseSegmento(r.segmento)
	if err != nil {
		return nil, err
	}
	saldo, err := firebird.ScanDecimal(r.saldoRaw, 2)
	if err != nil {
		return nil, err
	}
	porLiquidarPct, err := scanNullableDecimal(r.porLiquidarPctRaw)
	if err != nil {
		return nil, err
	}
	cohorteFecha, err := firebird.ScanUTCTime(r.cohorteFechaRaw)
	if err != nil {
		return nil, err
	}
	fechaUltimaCompra, err := scanNullableTime(r.fechaUltimaCompraRaw)
	if err != nil {
		return nil, err
	}
	createdAt, err := firebird.ScanUTCTime(r.createdAtRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := firebird.ScanUTCTime(r.updatedAtRaw)
	if err != nil {
		return nil, err
	}
	return domain.HydrateCohorteCliente(domain.HydrateCohorteClienteParams{
		ID:                    id,
		ClienteID:             r.clienteID,
		Nombre:                nullStringVal(r.nombre),
		Telefono:              nullStringVal(r.telefono),
		Segmento:              seg,
		EnControl:             r.enControl != 0,
		FueContactado:         r.fueContactado != 0,
		CohorteFecha:          cohorteFecha,
		FechaUltimaCompraBase: fechaUltimaCompra,
		Saldo:                 saldo,
		PorLiquidarPct:        porLiquidarPct,
		CreatedAt:             createdAt,
		UpdatedAt:             updatedAt,
	}), nil
}

// ─── Mensaje row mapper ────────────────────────────────────────────────────────

// mensajeRowRaw is the intermediate scan target for one MSP_RX_MENSAJES row.
// The table is CHARACTER SET UTF8 (CUERPO is BLOB SUB_TYPE TEXT UTF8) — plain
// string / sql.NullString is the correct scan target. Column order must match
// mensajeCols exactly.
type mensajeRowRaw struct {
	idRaw         string
	clienteID     int
	segmento      string
	telefono      sql.NullString
	cuerpo        sql.NullString // BLOB SUB_TYPE TEXT
	estado        string
	senderKind    sql.NullString
	encoladoEnRaw any // TIMESTAMP NOT NULL
	enviadoEnRaw  any // TIMESTAMP nullable
	errorMotivo   sql.NullString
	createdAtRaw  any // TIMESTAMP NOT NULL
	updatedAtRaw  any // TIMESTAMP NOT NULL
}

func (r *mensajeRowRaw) scanFrom(s rowScanner) error {
	return s.Scan(
		&r.idRaw,
		&r.clienteID,
		&r.segmento,
		&r.telefono,
		&r.cuerpo,
		&r.estado,
		&r.senderKind,
		&r.encoladoEnRaw,
		&r.enviadoEnRaw,
		&r.errorMotivo,
		&r.createdAtRaw,
		&r.updatedAtRaw,
	)
}

func assembleMensaje(r *mensajeRowRaw) (*domain.Mensaje, error) {
	id, err := parseUUIDColumn("ID", r.idRaw)
	if err != nil {
		return nil, err
	}
	seg, err := domain.ParseSegmento(r.segmento)
	if err != nil {
		return nil, err
	}
	estado, err := domain.ParseEstadoMensaje(r.estado)
	if err != nil {
		return nil, err
	}
	var senderKind domain.SenderKind
	if sk := nullStringVal(r.senderKind); sk != "" {
		senderKind, err = domain.ParseSenderKind(sk)
		if err != nil {
			return nil, err
		}
	}
	encoladoEn, err := firebird.ScanUTCTime(r.encoladoEnRaw)
	if err != nil {
		return nil, err
	}
	enviadoEn, err := scanNullableTime(r.enviadoEnRaw)
	if err != nil {
		return nil, err
	}
	createdAt, err := firebird.ScanUTCTime(r.createdAtRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := firebird.ScanUTCTime(r.updatedAtRaw)
	if err != nil {
		return nil, err
	}
	return domain.HydrateMensaje(domain.HydrateMensajeParams{
		ID:         id,
		ClienteID:  r.clienteID,
		Segmento:   seg,
		Telefono:   nullStringVal(r.telefono),
		Cuerpo:     nullStringVal(r.cuerpo),
		Estado:     estado,
		SenderKind: senderKind,
		EncoladoEn: encoladoEn,
		EnviadoEn:  enviadoEn,
		Error:      nullStringVal(r.errorMotivo),
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	}), nil
}

// ─── Shared helpers ────────────────────────────────────────────────────────────

// parseUUIDColumn converts a CHAR(36) column string to a uuid.UUID.
func parseUUIDColumn(column, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.NewInternal(
			"firebird_uuid_invalid",
			"uuid inválido en columna de base de datos",
		).
			WithSource("reactivacionfb").
			WithError(err).
			WithField("column", column).
			WithField("raw_value", raw)
	}
	return id, nil
}

// nullStringVal returns the string value of a sql.NullString, or "" if NULL.
func nullStringVal(ns sql.NullString) string {
	if !ns.Valid {
		return ""
	}
	return ns.String
}

// scanNullableTime scans a nullable TIMESTAMP column. Returns zero time.Time when
// the column is NULL (the driver may deliver nil for NULL columns declared as any).
func scanNullableTime(src any) (time.Time, error) {
	if src == nil {
		return time.Time{}, nil
	}
	return firebird.ScanUTCTime(src)
}

// scanNullableDecimal scans a nullable NUMERIC(N,2) column. Returns decimal.Zero
// when the column is NULL.
func scanNullableDecimal(src any) (decimal.Decimal, error) {
	if src == nil {
		return decimal.Zero, nil
	}
	return firebird.ScanDecimal(src, 2)
}
