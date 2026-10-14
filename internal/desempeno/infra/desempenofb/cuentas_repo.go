//nolint:misspell // vocabulario español por convención del proyecto.
package desempenofb

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

var _ outbound.CuentasRepo = (*CuentasRepo)(nil)

// CuentasRepo lee el universo de ventas de la semana de una zona.
type CuentasRepo struct {
	pool *firebird.Pool
}

// NewCuentasRepo arma el repositorio.
func NewCuentasRepo(pool *firebird.Pool) *CuentasRepo { return &CuentasRepo{pool: pool} }

// puentePV resuelve el DOCTO_PV_ID del cargo (sin parámetros: ver folioVentaDe).
const puentePV = `(SELECT des.DOCTO_FTE_ID FROM DOCTOS_ENTRE_SIS des
      WHERE des.CLAVE_SIS_FTE = 'PV' AND des.CLAVE_SIS_DEST = 'CC'
        AND des.DOCTO_DEST_ID = s.DOCTO_CC_ID ROWS 1)`

// queryUniverso: ventas a crédito (no CONTADO, como rutas) de la zona, no
// canceladas, con saldo > 0 o con FECHA_ULT_PAGO dentro de la semana: el mismo
// predicado de ventana que el sync del teléfono (COBRANZA-SYNC.md §5) con
// desde = inicio de semana. Usa IDX_MSP_SALDOS_ZONA_FUP.
//
// PRECIO_TOTAL y ENGANCHE alimentan CuentaUniverso.EsNueva (saldo == total −
// enganche, OrdenDeCobranza.kt:103 de msp-app-kt). Salen de las MISMAS columnas
// que el sync de ventas manda al teléfono (cobranza/infra/ventfb/ventas_repo.go
// selectVentaCols: s.PRECIO_TOTAL → `precio_total`, l.ENGANCHE → `enganche`),
// que el teléfono guarda como PRECIO_TOTAL/ENGANCHE (VentaDto.kt toEntity,
// NULL → 0 por parseAmount) y lee como totalVenta/enganche
// (RoomVentasAdapter.kt:99,101). Por eso ENGANCHE nulo (sin contrato) es 0.
//
// producto = descripción (ARTICULOS.NOMBRE) del primer artículo de la venta
// por POSICION, resolución del arquitecto sobre D-API-7.
//
// CAST a NUMERIC(18,2): firebirdsql v0.9.x devuelve sin escala si no.
// Charsets: CLIENTES.NOMBRE ISO8859_1 y ARTICULOS.NOMBRE ISO8859_1 →
// sql.NullString; DOCTOS_PV.FOLIO NONE → firebird.Win1252 (sin COALESCE con
// literal: revienta con acentos, ENCODING_HANDLING.md).
var queryUniverso = `
SELECT
  s.DOCTO_CC_ID,
  s.CLIENTE_ID,
  c.NOMBRE,
  CAST(COALESCE(l.PARCIALIDAD, 0) AS NUMERIC(18,2)),
  CAST(s.SALDO AS NUMERIC(18,2)),
  CAST(s.PRECIO_TOTAL AS NUMERIC(18,2)),
  CAST(COALESCE(l.ENGANCHE, 0) AS NUMERIC(18,2)),
  s.FECHA_CARGO,
  COALESCE(` + puentePV + `, 0),
  (SELECT pv.FOLIO FROM DOCTOS_PV pv WHERE pv.DOCTO_PV_ID = ` + puentePV + ` ROWS 1),
  (SELECT FIRST 1 a.NOMBRE FROM DOCTOS_PV_DET det JOIN ARTICULOS a ON a.ARTICULO_ID = det.ARTICULO_ID
    WHERE det.DOCTO_PV_ID = ` + puentePV + ` AND det.ROL IN ('N', 'J') ORDER BY det.POSICION)
FROM MSP_SALDOS_VENTAS s
LEFT JOIN LIBRES_CARGOS_CC l   ON l.DOCTO_CC_ID      = s.DOCTO_CC_ID
LEFT JOIN LISTAS_ATRIBUTOS lfp ON lfp.LISTA_ATRIB_ID = l.FORMA_DE_PAGO
LEFT JOIN CLIENTES c           ON c.CLIENTE_ID       = s.CLIENTE_ID
WHERE s.ZONA_CLIENTE_ID = ?
  AND s.CARGO_CANCELADO <> 'S'
  AND COALESCE(UPPER(lfp.VALOR_DESPLEGADO), 'SEMANAL') <> 'CONTADO'
  AND (s.SALDO > 0 OR s.FECHA_ULT_PAGO >= ?)
ORDER BY s.DOCTO_CC_ID`

// UniversoSemana implementa outbound.CuentasRepo.
func (r *CuentasRepo) UniversoSemana(ctx context.Context, zonaID int, inicioSemana time.Time) ([]domain.CuentaUniverso, error) {
	var out []domain.CuentaUniverso
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, err := q.QueryContext(ctx, queryUniverso, zonaID, firebird.ToWallClock(inicioSemana))
		if err != nil {
			return firebird.MapError(err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			c, err := scanCuenta(rows)
			if err != nil {
				return firebird.MapError(err)
			}
			out = append(out, c)
		}
		return firebird.MapError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func scanCuenta(rows *sql.Rows) (domain.CuentaUniverso, error) {
	var (
		c                                  domain.CuentaUniverso
		clienteNombre, producto            sql.NullString
		parcialidadRaw, saldoRaw, cargoRaw any
		precioTotalRaw, engancheRaw        any
		folio                              firebird.Win1252 // DOCTOS_PV.FOLIO — NONE
	)
	if err := rows.Scan(&c.VentaID, &c.ClienteID, &clienteNombre, &parcialidadRaw, &saldoRaw,
		&precioTotalRaw, &engancheRaw, &cargoRaw, &c.DoctoPVID, &folio, &producto); err != nil {
		return c, err
	}
	var err error
	if c.Parcialidad, err = firebird.ScanDecimal(parcialidadRaw, 2); err != nil {
		return c, err
	}
	if c.Saldo, err = firebird.ScanDecimal(saldoRaw, 2); err != nil {
		return c, err
	}
	if c.PrecioTotal, err = firebird.ScanDecimal(precioTotalRaw, 2); err != nil {
		return c, err
	}
	if c.Enganche, err = firebird.ScanDecimal(engancheRaw, 2); err != nil {
		return c, err
	}
	if c.FechaCargo, err = firebird.ScanUTCTime(cargoRaw); err != nil {
		return c, err
	}
	c.ClienteNombre = clienteNombre.String
	c.Producto = producto.String
	c.Folio = strings.TrimSpace(string(folio))
	return c, nil
}
