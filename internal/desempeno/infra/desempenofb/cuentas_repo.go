//nolint:misspell // vocabulario español por convención del proyecto.
package desempenofb

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

var (
	_ outbound.CuentasRepo     = (*CuentasRepo)(nil)
	_ outbound.CuentasLoteRepo = (*CuentasRepo)(nil)
)

// frecuenciaPorOmision es la que el teléfono asume si frec_pago llega vacío.
const frecuenciaPorOmision = "SEMANAL"

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
// FrecPago, FechaVenta y FechaUltPago alimentan NumAtrasos y salen de las
// MISMAS fuentes que el sync de ventas (ventfb/ventas_repo.go selectVentaCols y
// ventaFromClause): UPPER(lfp.VALOR_DESPLEGADO) → `frec_pago`, dc.FECHA del
// DOCTOS_PV unido por s.DOCTO_PV_ID → `fecha_venta`, s.FECHA_ULT_PAGO →
// `fecha_ult_pago`. LISTAS_ATRIBUTOS.VALOR_DESPLEGADO es NONE: se lee desnuda
// (firebird.Win1252) y se pasa a mayúsculas en Go; vacía = "SEMANAL", como el
// teléfono (VentaDto.kt: frec_pago.ifBlank { SEMANAL }).
//
// CAST a NUMERIC(18,2): firebirdsql v0.9.x devuelve sin escala si no.
// Charsets: CLIENTES.NOMBRE ISO8859_1 y ARTICULOS.NOMBRE ISO8859_1 →
// sql.NullString; DOCTOS_PV.FOLIO NONE → firebird.Win1252 (sin COALESCE con
// literal: revienta con acentos, ENCODING_HANDLING.md).
//
// Por lote (UniversoZonas) la consulta cubre un CONJUNTO de zonas y el inicio
// de semana más antiguo, en dos ramas que usan cada una su índice en vez de
// recorrer el historial de la zona (antes, el ORDER BY s.DOCTO_CC_ID obligaba
// a recorrer la PK):
//
//   - rama 1: SALDO > 0 (IDX_MSP_SALDOS_ZONA_SALDO);
//   - rama 2: saldadas con FECHA_ULT_PAGO >= inicio (IDX_MSP_SALDOS_ZONA_FUP).
//
// El predicado de cada consulta —SALDO > 0 OR FECHA_ULT_PAGO >= su inicio—
// y el orden por DOCTO_CC_ID se aplican en Go. SALDO es NUMERIC(14,2): el CAST
// a (18,2) no redondea y Saldo.IsPositive() es exactamente SALDO > 0.
const columnasUniverso = `
  s.ZONA_CLIENTE_ID,
  s.FECHA_ULT_PAGO,
  s.DOCTO_CC_ID,
  s.CLIENTE_ID,
  c.NOMBRE,
  CAST(COALESCE(l.PARCIALIDAD, 0) AS NUMERIC(18,2)),
  CAST(s.SALDO AS NUMERIC(18,2)),
  CAST(s.PRECIO_TOTAL AS NUMERIC(18,2)),
  CAST(COALESCE(l.ENGANCHE, 0) AS NUMERIC(18,2)),
  s.FECHA_CARGO,
  lfp.VALOR_DESPLEGADO,
  dc.FECHA,
  s.FECHA_ULT_PAGO,
  COALESCE(` + puentePV + `, 0),
  (SELECT pv.FOLIO FROM DOCTOS_PV pv WHERE pv.DOCTO_PV_ID = ` + puentePV + ` ROWS 1),
  (SELECT FIRST 1 a.NOMBRE FROM DOCTOS_PV_DET det JOIN ARTICULOS a ON a.ARTICULO_ID = det.ARTICULO_ID
    WHERE det.DOCTO_PV_ID = ` + puentePV + ` AND det.ROL IN ('N', 'J') ORDER BY det.POSICION)
FROM MSP_SALDOS_VENTAS s
LEFT JOIN LIBRES_CARGOS_CC l   ON l.DOCTO_CC_ID      = s.DOCTO_CC_ID
LEFT JOIN LISTAS_ATRIBUTOS lfp ON lfp.LISTA_ATRIB_ID = l.FORMA_DE_PAGO
LEFT JOIN CLIENTES c           ON c.CLIENTE_ID       = s.CLIENTE_ID
LEFT JOIN DOCTOS_PV dc         ON dc.DOCTO_PV_ID     = s.DOCTO_PV_ID`

const filtroUniverso = `
  AND s.CARGO_CANCELADO <> 'S'
  AND COALESCE(UPPER(lfp.VALOR_DESPLEGADO), 'SEMANAL') <> 'CONTADO'`

// queryUniverso: rama de saldo > 0 con todas las zonas y, por grupo de zonas
// (agruparZonas), una rama de saldadas desde el inicio más antiguo del grupo.
func queryUniverso(zonas []int, grupos []grupoDesde) (string, []any) {
	args := make([]any, 0, 2*len(zonas)+len(grupos))
	for _, z := range zonas {
		args = append(args, z)
	}
	ramas := make([]string, 0, 1+len(grupos))
	ramas = append(ramas, `
SELECT`+columnasUniverso+`
WHERE s.ZONA_CLIENTE_ID IN (`+marcadores(len(zonas))+`)
  AND s.SALDO > 0`+filtroUniverso)
	for _, g := range grupos {
		ramas = append(ramas, `
SELECT`+columnasUniverso+`
WHERE s.ZONA_CLIENTE_ID IN (`+marcadores(len(g.zonas))+`)
  AND s.FECHA_ULT_PAGO >= ?
  AND NOT (s.SALDO > 0)`+filtroUniverso)
		for _, z := range g.zonas {
			args = append(args, z)
		}
		args = append(args, firebird.ToWallClock(g.desde))
	}
	return strings.Join(ramas, "\nUNION ALL"), args
}

// UniversoSemana implementa outbound.CuentasRepo: es la lectura por lote con
// una sola consulta.
func (r *CuentasRepo) UniversoSemana(ctx context.Context, zonaID int, inicioSemana time.Time) ([]domain.CuentaUniverso, error) {
	out, err := r.UniversoZonas(ctx, []outbound.ConsultaUniverso{{ZonaID: zonaID, InicioSemana: inicioSemana}})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}

// claveCuenta son las dos primeras columnas del universo por lote: la zona por
// la que se reparte y FECHA_ULT_PAGO tal como la entrega el driver (hora de
// pared), la columna exacta del `FECHA_ULT_PAGO >= ?`.
type claveCuenta struct {
	zona int
	fup  sql.NullTime
}

type cuentaZona struct {
	clave  claveCuenta
	cuenta domain.CuentaUniverso
}

// UniversoZonas implementa outbound.CuentasLoteRepo: UNA sentencia para todas
// las consultas y el reparto en Go.
func (r *CuentasRepo) UniversoZonas(ctx context.Context, consultas []outbound.ConsultaUniverso) ([][]domain.CuentaUniverso, error) {
	out := make([][]domain.CuentaUniverso, len(consultas))
	if len(consultas) == 0 {
		return out, nil
	}
	inicioPorZona := map[int]time.Time{}
	for _, c := range consultas {
		if d, ok := inicioPorZona[c.ZonaID]; !ok || c.InicioSemana.Before(d) {
			inicioPorZona[c.ZonaID] = c.InicioSemana
		}
	}
	zonas := make([]int, 0, len(inicioPorZona))
	for z := range inicioPorZona {
		zonas = append(zonas, z)
	}
	sort.Ints(zonas)
	consulta, args := queryUniverso(zonas, agruparZonas(inicioPorZona))
	var filas []cuentaZona
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, err := q.QueryContext(ctx, consulta, args...)
		if err != nil {
			return firebird.MapError(err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var k claveCuenta
			c, err := scanCuenta(rows, &k)
			if err != nil {
				return firebird.MapError(err)
			}
			filas = append(filas, cuentaZona{clave: k, cuenta: c})
		}
		return firebird.MapError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	for i, c := range consultas {
		out[i] = universoDe(filas, c.ZonaID, c.InicioSemana)
	}
	return out, nil
}

// universoDe: las cuentas de la zona con SALDO > 0 o FECHA_ULT_PAGO >= inicio
// (el predicado de la consulta por zona, repetido por clave), por DOCTO_CC_ID
// como la consulta por zona de antes.
func universoDe(filas []cuentaZona, zona int, inicio time.Time) []domain.CuentaUniverso {
	var out []domain.CuentaUniverso
	for _, f := range filas {
		c := f.cuenta
		if f.clave.zona != zona {
			continue
		}
		if !c.Saldo.IsPositive() && (!f.clave.fup.Valid || !desdePared(f.clave.fup.Time, inicio)) {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].VentaID < out[j].VentaID })
	return out
}

func scanCuenta(rows *sql.Rows, k *claveCuenta) (domain.CuentaUniverso, error) {
	var (
		c                                  domain.CuentaUniverso
		clienteNombre, producto            sql.NullString
		parcialidadRaw, saldoRaw, cargoRaw any
		precioTotalRaw, engancheRaw        any
		folio                              firebird.Win1252 // DOCTOS_PV.FOLIO — NONE
		frecPago                           firebird.Win1252 // LISTAS_ATRIBUTOS.VALOR_DESPLEGADO — NONE
		fechaVentaRaw, fechaUltPagoRaw     any
	)
	if err := rows.Scan(&k.zona, &k.fup, &c.VentaID, &c.ClienteID, &clienteNombre, &parcialidadRaw, &saldoRaw,
		&precioTotalRaw, &engancheRaw, &cargoRaw, &frecPago, &fechaVentaRaw, &fechaUltPagoRaw, &c.DoctoPVID, &folio, &producto); err != nil {
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
	if c.FechaVenta, err = instanteNulo(fechaVentaRaw); err != nil {
		return c, err
	}
	if c.FechaUltPago, err = instanteNulo(fechaUltPagoRaw); err != nil {
		return c, err
	}
	c.FrecPago = strings.ToUpper(strings.TrimSpace(string(frecPago)))
	if c.FrecPago == "" {
		c.FrecPago = frecuenciaPorOmision
	}
	c.ClienteNombre = clienteNombre.String
	c.Producto = producto.String
	c.Folio = strings.TrimSpace(string(folio))
	return c, nil
}
