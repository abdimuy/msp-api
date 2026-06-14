//nolint:misspell // Spanish domain vocabulary (candidato, cohorte, zona, etc.) by project convention.
package analyticsfb

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// rowScanner is the minimal surface satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// ─── WinbackCandidato row mapper ──────────────────────────────────────────────

// candidatoRowRaw is the intermediate scan target for one MSP_AN_WINBACK_CANDIDATOS
// row. Timestamps and NUMERIC columns are declared as any so the driver delivers
// its native type; they are normalized after scanning.
//
// MSP_AN_WINBACK_CANDIDATOS columns are CHARACTER SET UTF8 — plain string /
// sql.NullString is the correct scan target (no Win1252 decoding needed).
type candidatoRowRaw struct {
	idRaw             string
	clienteID         int
	nombre            sql.NullString
	zona              sql.NullString
	telefono          sql.NullString
	fechaUltimaCompra any // TIMESTAMP nullable
	frecuencia        int
	monetaryRaw       any // NUMERIC(18,2)
	saldoRaw          any // NUMERIC(18,2)
	porLiquidarPctRaw any // NUMERIC(5,2) nullable
	nextBestProduct   sql.NullString
	enControl         int16 // SMALLINT; 0=false 1=true
	cohorteFechaRaw   any   // TIMESTAMP NOT NULL
	createdAtRaw      any   // TIMESTAMP NOT NULL
	updatedAtRaw      any   // TIMESTAMP NOT NULL
}

func (r *candidatoRowRaw) scanFrom(s rowScanner) error {
	return s.Scan(
		&r.idRaw,
		&r.clienteID,
		&r.nombre,
		&r.zona,
		&r.telefono,
		&r.fechaUltimaCompra,
		&r.frecuencia,
		&r.monetaryRaw,
		&r.saldoRaw,
		&r.porLiquidarPctRaw,
		&r.nextBestProduct,
		&r.enControl,
		&r.cohorteFechaRaw,
		&r.createdAtRaw,
		&r.updatedAtRaw,
	)
}

func assembleCandidato(r *candidatoRowRaw) (*domain.WinbackCandidato, error) {
	id, err := parseUUIDColumn("ID", r.idRaw)
	if err != nil {
		return nil, err
	}
	monetary, err := firebird.ScanDecimal(r.monetaryRaw, 2)
	if err != nil {
		return nil, err
	}
	saldo, err := firebird.ScanDecimal(r.saldoRaw, 2)
	if err != nil {
		return nil, err
	}
	porLiquidarPct, err := scanNullableDecimal(r.porLiquidarPctRaw, 2)
	if err != nil {
		return nil, err
	}
	fechaUltimaCompra, err := scanNullableTime(r.fechaUltimaCompra)
	if err != nil {
		return nil, err
	}
	cohorteFecha, err := firebird.ScanUTCTime(r.cohorteFechaRaw)
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
	return domain.HydrateWinbackCandidato(domain.HydrateWinbackCandidatoParams{
		ID:                id,
		ClienteID:         r.clienteID,
		Nombre:            nullStringVal(r.nombre),
		Zona:              nullStringVal(r.zona),
		Telefono:          nullStringVal(r.telefono),
		FechaUltimaCompra: fechaUltimaCompra,
		Frecuencia:        r.frecuencia,
		Monetary:          monetary,
		Saldo:             saldo,
		PorLiquidarPct:    porLiquidarPct,
		NextBestProduct:   nullStringVal(r.nextBestProduct),
		EnControl:         r.enControl != 0,
		CohorteFecha:      cohorteFecha,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
	}), nil
}

func scanCandidatoRows(rows *sql.Rows) ([]*domain.WinbackCandidato, error) {
	var result []*domain.WinbackCandidato
	for rows.Next() {
		var r candidatoRowRaw
		if err := r.scanFrom(rows); err != nil {
			return nil, firebird.MapError(err)
		}
		c, err := assembleCandidato(&r)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return result, nil
}

// ─── AnclaCliente row mapper ──────────────────────────────────────────────────

// anclaRowRaw is the intermediate scan target for one row from leerAnclasBase.
// Microsip text columns (CLIENTES.NOMBRE, ZONAS_CLIENTES.NOMBRE,
// DIRS_CLIENTES.TELEFONO1, ARTICULOS.NOMBRE) are Win1252-encoded. We use
// firebird.Win1252 as the scan destination so the driver bytes are decoded
// to UTF-8 before the domain sees them.
//
// Column order must match leerAnclasBase SELECT list exactly.
type anclaRowRaw struct {
	clienteID         int
	nombre            firebird.Win1252  // Win1252: CLIENTES.NOMBRE
	zona              firebird.Win1252  // Win1252: ZONAS_CLIENTES.NOMBRE (COALESCE to '')
	telefono          *firebird.Win1252 // Win1252 nullable: DIRS_CLIENTES.TELEFONO1
	fechaUltimaCompra any               // DATE from DOCTOS_PV.FECHA MAX
	frecuencia        int               // COUNT(DISTINCT …)
	monetaryRaw       any               // CAST(SUM(IMPORTE_NETO) AS NUMERIC(18,2))
	saldoRaw          any               // CAST(… AS NUMERIC(18,2))
	porLiquidarRaw    any               // CAST(… AS NUMERIC(5,2))
	nextBestProduct   firebird.Win1252  // Win1252: ARTICULOS.NOMBRE (COALESCE to '')
}

func (r *anclaRowRaw) scanFrom(s rowScanner) error {
	return s.Scan(
		&r.clienteID,
		&r.nombre,
		&r.zona,
		&r.telefono,
		&r.fechaUltimaCompra,
		&r.frecuencia,
		&r.monetaryRaw,
		&r.saldoRaw,
		&r.porLiquidarRaw,
		&r.nextBestProduct,
	)
}

func assembleAncla(r *anclaRowRaw) (outbound.AnclaCliente, error) {
	monetary, err := firebird.ScanDecimal(r.monetaryRaw, 2)
	if err != nil {
		return outbound.AnclaCliente{}, err
	}
	saldo, err := firebird.ScanDecimal(r.saldoRaw, 2)
	if err != nil {
		return outbound.AnclaCliente{}, err
	}
	porLiquidarPct, err := scanNullableDecimal(r.porLiquidarRaw, 2)
	if err != nil {
		return outbound.AnclaCliente{}, err
	}
	fechaUltimaCompra, err := scanNullableTime(r.fechaUltimaCompra)
	if err != nil {
		return outbound.AnclaCliente{}, err
	}
	// Decode Win1252 nullable telefono.
	var telefono string
	if r.telefono != nil {
		telefono = string(*r.telefono)
	}
	return outbound.AnclaCliente{
		ClienteID:         r.clienteID,
		Nombre:            string(r.nombre),
		Zona:              string(r.zona),
		Telefono:          telefono,
		FechaUltimaCompra: fechaUltimaCompra,
		Frecuencia:        r.frecuencia,
		Monetary:          monetary,
		Saldo:             saldo,
		PorLiquidarPct:    porLiquidarPct,
		NextBestProduct:   string(r.nextBestProduct),
	}, nil
}

// ─── Shared helpers ───────────────────────────────────────────────────────────

// parseUUIDColumn converts a CHAR(36) column string to a uuid.UUID.
func parseUUIDColumn(column, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.NewInternal(
			"firebird_uuid_invalid",
			"uuid inválido en columna de base de datos",
		).
			WithSource("firebird").
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

// scanNullableTime scans a nullable TIMESTAMP column. Returns zero time.Time
// when the column is NULL (the Firebird driver may deliver nil for NULL columns
// declared as any).
func scanNullableTime(src any) (time.Time, error) {
	if src == nil {
		return time.Time{}, nil
	}
	return firebird.ScanUTCTime(src)
}

// scanNullableDecimal scans a nullable NUMERIC column. Returns
// decimal.Zero when the column is NULL.
func scanNullableDecimal(src any, scale int) (decimal.Decimal, error) {
	if src == nil {
		return decimal.Zero, nil
	}
	return firebird.ScanDecimal(src, scale)
}
