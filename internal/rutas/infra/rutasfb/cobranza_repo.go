//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutasfb

import (
	"context"
	"database/sql"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

// Compile-time assertion.
var _ outbound.CobranzaRepo = (*CobranzaRepo)(nil)

// CobranzaRepo is the Firebird-backed implementation of outbound.CobranzaRepo.
// Reads MSP_SALDOS_VENTAS, LIBRES_CARGOS_CC, LISTAS_ATRIBUTOS, and
// MSP_PAGOS_VENTAS — no tables are written.
type CobranzaRepo struct {
	pool *firebird.Pool
}

// NewCobranzaRepo builds a CobranzaRepo wired to the given pool.
func NewCobranzaRepo(pool *firebird.Pool) *CobranzaRepo {
	return &CobranzaRepo{pool: pool}
}

// queryVentasPorZona returns one row per active venta for the given zona,
// including the sum of valid payments in [desde, hasta] and the frecuencia
// string resolved from LISTAS_ATRIBUTOS.
//
// CAST(SUM(...) AS NUMERIC(18,2)) is mandatory — firebirdsql v0.9.x returns
// NUMERIC aggregates unscaled without the explicit cast.
//
// DOCTO_PV_ID and FOLIO use correlated scalar subqueries with ROWS 1 to avoid
// the firebirdsql v0.9.19 parameter-binding bug that rejects ? inside FROM-clause
// derived tables.
//
// The credit total is PRECIO_TOTAL, NOT TOTAL_IMPORTE. Per migration 000010,
// MSP_SALDOS_VENTAS.TOTAL_IMPORTE is the sum of active PAYMENTS (conceptos
// 87327/155/11) and SALDO = PRECIO_TOTAL − TOTAL_IMPORTE − IMPTE_REST. Using
// TOTAL_IMPORTE as the credit total made pagadoAntes go negative whenever a
// client had paid little (SALDO > TOTAL_IMPORTE, ~6% of the cartera), inflating
// "atraso" to the full balance. CalcAporte/enrichVentas consume this as the
// credit total via VentaCobranza.TotalImporte.
//
// ABONO_SEMANA sums ONLY CONCEPTO_CC_ID = 87327 (Cobranza en ruta) — the actual
// money the cobrador collected on the route. Every other concept in
// MSP_PAGOS_VENTAS is NOT route collection and must not count toward the
// cobrador's cobertura/ponderado. Per CONCEPTOS_CC.NOMBRE in Microsip:
// 87327 = Cobranza en ruta, 27969 = Condonaciones (debt forgiveness, NOT a
// payment), 155 = Cobro en mostrador, 11 = Cobro, 24533 = Enganche,
// 27966 = Cancelaciones, 27967 = Fugas, 27968 = Mal Cliente, 25116 = Condonación
// por pronto pago, 12/13 = Devoluciones, 15 = Ajuste de saldo.
//
// NOTE: cobranza/infra/ventfb/pagos_repo.go `pagoConceptoFilter` uses
// IN (87327, 27969) with a comment calling 27969 "abono mostrador" — that label
// is WRONG (27969 is Condonaciones). That filter drives the Android sync; do not
// assume it is correct.
//
// Parameters: $1=zonaID, $2=desde, $3=hasta, $4=zonaID (outer filter).
const queryVentasPorZona = `
SELECT
  s.DOCTO_CC_ID,
  s.CLIENTE_ID,
  s.ZONA_CLIENTE_ID,
  CAST(COALESCE(l.PARCIALIDAD, 0) AS NUMERIC(18,2))        AS PARCIALIDAD,
  COALESCE(UPPER(lfp.VALOR_DESPLEGADO), 'SEMANAL')         AS FRECUENCIA,
  CAST(COALESCE(p.ABONO_SEMANA, 0) AS NUMERIC(18,2))       AS ABONO_SEMANA,
  CAST(s.SALDO        AS NUMERIC(18,2))                    AS SALDO,
  CAST(s.PRECIO_TOTAL AS NUMERIC(18,2))                    AS PRECIO_TOTAL,
  s.FECHA_CARGO,
  s.FECHA_ULT_PAGO,
  c.NOMBRE                                                  AS CLIENTE_NOMBRE,
  COALESCE((
    SELECT des.DOCTO_FTE_ID
    FROM DOCTOS_ENTRE_SIS des
    WHERE des.CLAVE_SIS_FTE  = 'PV'
      AND des.CLAVE_SIS_DEST = 'CC'
      AND des.DOCTO_DEST_ID  = s.DOCTO_CC_ID
    ROWS 1
  ), 0)                                                     AS DOCTO_PV_ID,
  COALESCE((
    SELECT pv.FOLIO
    FROM DOCTOS_PV pv
    WHERE pv.DOCTO_PV_ID = (
      SELECT des.DOCTO_FTE_ID
      FROM DOCTOS_ENTRE_SIS des
      WHERE des.CLAVE_SIS_FTE  = 'PV'
        AND des.CLAVE_SIS_DEST = 'CC'
        AND des.DOCTO_DEST_ID  = s.DOCTO_CC_ID
      ROWS 1
    )
    ROWS 1
  ), '')                                                    AS FOLIO
FROM MSP_SALDOS_VENTAS s
LEFT JOIN LIBRES_CARGOS_CC l    ON l.DOCTO_CC_ID      = s.DOCTO_CC_ID
LEFT JOIN LISTAS_ATRIBUTOS lfp  ON lfp.LISTA_ATRIB_ID = l.FORMA_DE_PAGO
LEFT JOIN CLIENTES c            ON c.CLIENTE_ID        = s.CLIENTE_ID
LEFT JOIN (
  SELECT DOCTO_CC_ACR_ID,
         CAST(SUM(IMPORTE) AS NUMERIC(18,2)) AS ABONO_SEMANA
  FROM MSP_PAGOS_VENTAS
  WHERE ZONA_CLIENTE_ID = ?
    AND CANCELADO = 'N'
    AND CONCEPTO_CC_ID = 87327
    AND FECHA >= ?
    AND FECHA <= ?
  GROUP BY DOCTO_CC_ACR_ID
) p ON p.DOCTO_CC_ACR_ID = s.DOCTO_CC_ID
WHERE s.ZONA_CLIENTE_ID = ?
  AND s.CARGO_CANCELADO <> 'S'
  AND COALESCE(UPPER(lfp.VALOR_DESPLEGADO), 'SEMANAL') <> 'CONTADO'
  AND (s.SALDO > 0 OR COALESCE(p.ABONO_SEMANA, 0) > 0)
ORDER BY s.DOCTO_CC_ID`

// VentasPorZona implements outbound.CobranzaRepo.
func (r *CobranzaRepo) VentasPorZona(
	ctx context.Context, zonaID int, desde, hasta time.Time,
) ([]rutasdomain.VentaCobranza, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(
		ctx, queryVentasPorZona,
		zonaID, // subquery param
		firebird.ToWallClock(desde),
		firebird.ToWallClock(hasta),
		zonaID, // outer WHERE
	)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var result []rutasdomain.VentaCobranza
	for rows.Next() {
		v, serr := scanVentaCobranza(rows)
		if serr != nil {
			return nil, firebird.MapError(serr)
		}
		result = append(result, v)
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, firebird.MapError(rerr)
	}
	return result, nil
}

// ventaCobranzaRaw holds raw scan targets for one cobranza row.
type ventaCobranzaRaw struct {
	ventaID          int
	clienteID        int
	zonaID           int
	parcialidadRaw   any
	frecuencia       string
	abonoRaw         any
	saldoRaw         any
	totalRaw         any
	fechaCargo       time.Time
	fechaUltPago     sql.NullTime
	clienteNombreRaw firebird.Win1252 // CLIENTES.NOMBRE — CHARACTER SET NONE (Win1252)
	doctoPVID        int
	folioRaw         firebird.Win1252 // DOCTOS_PV.FOLIO — CHARACTER SET NONE (Win1252)
}

func scanVentaCobranza(s scannable) (rutasdomain.VentaCobranza, error) {
	var raw ventaCobranzaRaw
	if err := s.Scan(
		&raw.ventaID,
		&raw.clienteID,
		&raw.zonaID,
		&raw.parcialidadRaw,
		&raw.frecuencia,
		&raw.abonoRaw,
		&raw.saldoRaw,
		&raw.totalRaw,
		&raw.fechaCargo,
		&raw.fechaUltPago,
		&raw.clienteNombreRaw,
		&raw.doctoPVID,
		&raw.folioRaw,
	); err != nil {
		return rutasdomain.VentaCobranza{}, err
	}

	parcialidad, err := firebird.ScanDecimal(raw.parcialidadRaw, 2)
	if err != nil {
		return rutasdomain.VentaCobranza{}, err
	}
	abono, err := firebird.ScanDecimal(raw.abonoRaw, 2)
	if err != nil {
		return rutasdomain.VentaCobranza{}, err
	}
	saldo, err := firebird.ScanDecimal(raw.saldoRaw, 2)
	if err != nil {
		return rutasdomain.VentaCobranza{}, err
	}
	total, err := firebird.ScanDecimal(raw.totalRaw, 2)
	if err != nil {
		return rutasdomain.VentaCobranza{}, err
	}

	fechaCargo, err := firebird.ScanUTCTime(raw.fechaCargo)
	if err != nil {
		return rutasdomain.VentaCobranza{}, err
	}

	var fechaUltPago *time.Time
	if raw.fechaUltPago.Valid {
		t, terr := firebird.ScanUTCTime(raw.fechaUltPago.Time)
		if terr != nil {
			return rutasdomain.VentaCobranza{}, terr
		}
		fechaUltPago = &t
	}

	// Aporte and Vencidas are computed in the app layer once fechaInicio is
	// known (from the Firestore cobrador calendar). The repo returns raw fields
	// only; the service calls enrichVentas after fetching.
	return rutasdomain.VentaCobranza{
		VentaID:       raw.ventaID,
		ClienteID:     raw.clienteID,
		ZonaID:        raw.zonaID,
		ClienteNombre: string(raw.clienteNombreRaw),
		Folio:         string(raw.folioRaw),
		DoctoPVID:     raw.doctoPVID,
		Parcialidad:   parcialidad,
		Frecuencia:    rutasdomain.Frecuencia(raw.frecuencia),
		AbonoSemana:   abono,
		Saldo:         saldo,
		TotalImporte:  total,
		FechaCargo:    fechaCargo,
		FechaUltPago:  fechaUltPago,
		// Aporte and Vencidas are computed in the app layer after Plazos is known.
		Aporte:   decimal.Zero,
		Vencidas: decimal.Zero,
	}, nil
}
