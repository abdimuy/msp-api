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
	idRaw              string
	clienteID          int
	nombre             sql.NullString
	zona               sql.NullString
	telefono           sql.NullString
	fechaUltimaCompra  any // TIMESTAMP nullable
	frecuencia         int
	monetaryRaw        any // NUMERIC(18,2)
	saldoRaw           any // NUMERIC(18,2)
	porLiquidarPctRaw  any // NUMERIC(5,2) nullable
	nextBestProduct    sql.NullString
	enControl          int16 // SMALLINT; 0=false 1=true
	cohorteFechaRaw    any   // TIMESTAMP NOT NULL
	createdAtRaw       any   // TIMESTAMP NOT NULL
	updatedAtRaw       any   // TIMESTAMP NOT NULL
	fechaUltimoPagoRaw any   // TIMESTAMP nullable
	numPagosRaw        any   // INTEGER nullable
	cadenciaDiasRaw    any   // INTEGER nullable
	diasAtrasoProm     any   // INTEGER nullable
	pctPagosATiempoRaw any   // NUMERIC(5,2) nullable
	fechaProxPagoRaw   any   // TIMESTAMP nullable
	montoProxPagoRaw   any   // NUMERIC(18,2) nullable
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
		&r.fechaUltimoPagoRaw,
		&r.numPagosRaw,
		&r.cadenciaDiasRaw,
		&r.diasAtrasoProm,
		&r.pctPagosATiempoRaw,
		&r.fechaProxPagoRaw,
		&r.montoProxPagoRaw,
	)
}

// candidatoCobranzaScanned holds decoded cobranza signal fields for assembleCandidato.
type candidatoCobranzaScanned struct {
	fechaUltimoPago time.Time
	numPagos        int
	cadenciaDias    int
	diasAtrasoProm  int
	pctPagosATiempo decimal.Decimal
	fechaProxPago   time.Time
	montoProxPago   decimal.Decimal
}

// scanCandidatoCobranza decodes the 6 nullable cobranza signal columns from r.
// Extracted to keep assembleCandidato's cyclomatic complexity within limits.
func scanCandidatoCobranza(r *candidatoRowRaw) (candidatoCobranzaScanned, error) {
	var out candidatoCobranzaScanned
	var err error
	out.fechaUltimoPago, err = scanNullableTime(r.fechaUltimoPagoRaw)
	if err != nil {
		return out, err
	}
	out.numPagos, err = scanNullableIntDecimal(r.numPagosRaw)
	if err != nil {
		return out, err
	}
	out.cadenciaDias, err = scanNullableIntDecimal(r.cadenciaDiasRaw)
	if err != nil {
		return out, err
	}
	out.diasAtrasoProm, err = scanNullableIntDecimal(r.diasAtrasoProm)
	if err != nil {
		return out, err
	}
	out.pctPagosATiempo, err = scanNullableDecimal(r.pctPagosATiempoRaw)
	if err != nil {
		return out, err
	}
	out.fechaProxPago, err = scanNullableTime(r.fechaProxPagoRaw)
	if err != nil {
		return out, err
	}
	out.montoProxPago, err = scanNullableDecimal(r.montoProxPagoRaw)
	if err != nil {
		return out, err
	}
	return out, nil
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
	porLiquidarPct, err := scanNullableDecimal(r.porLiquidarPctRaw)
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
	cob, err := scanCandidatoCobranza(r)
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
		FechaUltimoPago:   cob.fechaUltimoPago,
		NumPagos:          cob.numPagos,
		CadenciaDias:      cob.cadenciaDias,
		DiasAtrasoProm:    cob.diasAtrasoProm,
		PctPagosATiempo:   cob.pctPagosATiempo,
		FechaProxPago:     cob.fechaProxPago,
		MontoProxPago:     cob.montoProxPago,
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
	clienteID          int
	nombre             firebird.Win1252  // Win1252: CLIENTES.NOMBRE
	zona               firebird.Win1252  // Win1252: ZONAS_CLIENTES.NOMBRE (COALESCE to '')
	telefono           *firebird.Win1252 // Win1252 nullable: DIRS_CLIENTES.TELEFONO1
	fechaUltimaCompra  any               // DATE from DOCTOS_PV.FECHA MAX
	frecuencia         int               // COUNT(DISTINCT …)
	monetaryRaw        any               // CAST(SUM(IMPORTE_NETO) AS NUMERIC(18,2))
	saldoRaw           any               // CAST(… AS NUMERIC(18,2))
	porLiquidarRaw     any               // CAST(… AS NUMERIC(5,2))
	nextBestProduct    firebird.Win1252  // Win1252: ARTICULOS.NOMBRE (COALESCE to '')
	fechaUltimoPagoRaw any               // TIMESTAMP nullable: MAX(sv.FECHA_ULT_PAGO)
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
		&r.fechaUltimoPagoRaw,
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
	porLiquidarPct, err := scanNullableDecimal(r.porLiquidarRaw)
	if err != nil {
		return outbound.AnclaCliente{}, err
	}
	fechaUltimaCompra, err := scanNullableTime(r.fechaUltimaCompra)
	if err != nil {
		return outbound.AnclaCliente{}, err
	}
	fechaUltimoPago, err := scanNullableTime(r.fechaUltimoPagoRaw)
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
		FechaUltimoPago:   fechaUltimoPago,
	}, nil
}

// ─── CobranzaSignals row mapper ───────────────────────────────────────────────

// cobranzaRowRaw is the intermediate scan target for one row from leerCobranzaBase+leerCobranzaClose.
// Numeric aggregates are declared as any so the driver delivers its native type.
type cobranzaRowRaw struct {
	clienteID          int
	numPagos           int // NUM_GAPS+1: driver delivers int directly for INTEGER expressions
	cadenciaDias       any // NUMERIC(10,0) aggregate
	diasAtrasoProm     any // NUMERIC(10,0) aggregate
	pctPagosATiempoRaw any // NUMERIC(5,2) aggregate
	fechaProxPagoRaw   any // TIMESTAMP nullable (DATEADD may return NULL if cadencia is 0)
	montoProxPagoRaw   any // NUMERIC(18,2) aggregate
}

func (r *cobranzaRowRaw) scanFrom(s rowScanner) error {
	return s.Scan(
		&r.clienteID,
		&r.numPagos,
		&r.cadenciaDias,
		&r.diasAtrasoProm,
		&r.pctPagosATiempoRaw,
		&r.fechaProxPagoRaw,
		&r.montoProxPagoRaw,
	)
}

func assembleCobranza(r *cobranzaRowRaw) (outbound.CobranzaSignals, error) {
	cadenciaDias, err := scanNullableIntDecimal(r.cadenciaDias)
	if err != nil {
		return outbound.CobranzaSignals{}, err
	}
	diasAtrasoProm, err := scanNullableIntDecimal(r.diasAtrasoProm)
	if err != nil {
		return outbound.CobranzaSignals{}, err
	}
	pctPagosATiempo, err := scanNullableDecimal(r.pctPagosATiempoRaw)
	if err != nil {
		return outbound.CobranzaSignals{}, err
	}
	fechaProxPago, err := scanNullableTime(r.fechaProxPagoRaw)
	if err != nil {
		return outbound.CobranzaSignals{}, err
	}
	montoProxPago, err := scanNullableDecimal(r.montoProxPagoRaw)
	if err != nil {
		return outbound.CobranzaSignals{}, err
	}
	return outbound.CobranzaSignals{
		ClienteID:       r.clienteID,
		NumPagos:        r.numPagos,
		CadenciaDias:    cadenciaDias,
		DiasAtrasoProm:  diasAtrasoProm,
		PctPagosATiempo: pctPagosATiempo,
		FechaProxPago:   fechaProxPago,
		MontoProxPago:   montoProxPago,
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

// scanNullableDecimal scans a nullable NUMERIC(N,2) column. Returns
// decimal.Zero when the column is NULL.
func scanNullableDecimal(src any) (decimal.Decimal, error) {
	if src == nil {
		return decimal.Zero, nil
	}
	return firebird.ScanDecimal(src, 2)
}

// scanNullableIntDecimal scans a nullable NUMERIC(N,0) integer aggregate.
// Returns 0 when the column is NULL (insufficient data for cadence computation).
// The SQL uses CAST(AVG(CAST(x AS NUMERIC(18,4))) AS NUMERIC(10,0)) to avoid the
// nakagami driver's unscaled-aggregate bug (reference_firebirdsql_sum_scale).
func scanNullableIntDecimal(src any) (int, error) {
	if src == nil {
		return 0, nil
	}
	d, err := firebird.ScanDecimal(src, 0)
	if err != nil {
		return 0, err
	}
	return int(d.IntPart()), nil
}
