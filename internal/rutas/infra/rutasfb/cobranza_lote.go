//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutasfb

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

// columnasVentaLote son las columnas de queryVentasPorZona en el mismo orden
// (las lee scanVentaCobranza), con ABONO_SEMANA en cero: el abono depende de
// la ventana de CADA consulta y se suma en Go con las filas de
// queryAbonosLote.
const columnasVentaLote = `
  s.DOCTO_CC_ID,
  s.CLIENTE_ID,
  s.ZONA_CLIENTE_ID,
  CAST(COALESCE(l.PARCIALIDAD, 0) AS NUMERIC(18,2)),
  COALESCE(UPPER(lfp.VALOR_DESPLEGADO), 'SEMANAL'),
  CAST(0 AS NUMERIC(18,2)),
  CAST(s.SALDO        AS NUMERIC(18,2)),
  CAST(s.PRECIO_TOTAL AS NUMERIC(18,2)),
  s.FECHA_CARGO,
  s.FECHA_ULT_PAGO,
  c.NOMBRE,
  COALESCE((
    SELECT des.DOCTO_FTE_ID
    FROM DOCTOS_ENTRE_SIS des
    WHERE des.CLAVE_SIS_FTE  = 'PV'
      AND des.CLAVE_SIS_DEST = 'CC'
      AND des.DOCTO_DEST_ID  = s.DOCTO_CC_ID
    ROWS 1
  ), 0),
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
  ), '')`

const unionesVentaLote = `
LEFT JOIN LIBRES_CARGOS_CC l    ON l.DOCTO_CC_ID      = s.DOCTO_CC_ID
LEFT JOIN LISTAS_ATRIBUTOS lfp  ON lfp.LISTA_ATRIB_ID = l.FORMA_DE_PAGO
LEFT JOIN CLIENTES c            ON c.CLIENTE_ID        = s.CLIENTE_ID`

// filtroCreditoVivo: crédito no cancelado y no de contado, como queryVentasPorZona.
//
//nolint:gosec // G101 falso positivo: es SQL, no una credencial.
const filtroCreditoVivo = `
  AND s.CARGO_CANCELADO <> 'S'
  AND COALESCE(UPPER(lfp.VALOR_DESPLEGADO), 'SEMANAL') <> 'CONTADO'`

// filtroAbonoRuta es el mismo filtro de pagos que la subconsulta p de
// queryVentasPorZona: cobranza en ruta (87327) no cancelada. La zona y la
// ventana de p.FECHA las pone filtroZonasFechas.
const filtroAbonoRuta = `p.CANCELADO = 'N'
  AND p.CONCEPTO_CC_ID = 87327`

// queryVentasLote es el universo de VentasPorZona para un CONJUNTO de zonas,
// sin agregado correlacionado ni recorrido del historial:
//
//   - rama 1: ventas con saldo (IDX_MSP_SALDOS_ZONA_SALDO, rango SALDO > 0);
//   - rama 2: ventas SALDADAS con al menos un abono de ruta desde el inicio
//     más antiguo que se pide para SU zona, partiendo de los pagos (índice
//     por zona y fecha)
//     hacia la venta por PK. El abono debe ser de la MISMA zona que la venta,
//     como en VentasPorZona (subconsulta y filtro externo con la misma zona).
//     Una venta con varios abonos sale varias veces: Go la deja una vez.
//
// La regla final de VentasPorZona —SALDO > 0 OR abono de la ventana > 0— se
// aplica en Go por consulta. SALDO es NUMERIC(14,2): el CAST a (18,2) no
// redondea y Saldo.IsPositive() en Go es exactamente SALDO > 0.
// Contextos por rama (ver ramasPorSentencia): ventas con saldo s, l, lfp, c +
// des, pv, des = 7; saldadas con abono p + esos 7 = 8; abonos p = 1.
const (
	contextosVentas = 8
	contextosAbonos = 1
)

// partirVentas: tramos de ramas de saldadas; el primero deja lugar a la rama
// de saldo > 0.
func partirVentas(ramas []ramaGrupo) [][]ramaGrupo {
	n := ramasPorSentencia(contextosVentas)
	primero := ramas[:min(len(ramas), n-1)]
	return append([][]ramaGrupo{primero}, partir(ramas[len(primero):], n)...)
}

// queryVentasLote: la rama de saldo > 0 de las zonas (si zonas no está vacío)
// y una rama de saldadas por grupo.
func queryVentasLote(zonas []int, ramas []ramaGrupo) (string, []any) {
	args := make([]any, 0, len(zonas))
	for _, z := range zonas {
		args = append(args, z)
	}
	var partes []string
	if len(zonas) > 0 {
		partes = append(partes, `SELECT`+columnasVentaLote+`
FROM MSP_SALDOS_VENTAS s`+unionesVentaLote+`
WHERE s.ZONA_CLIENTE_ID IN (`+marcadores(len(zonas))+`)
  AND s.SALDO > 0`+filtroCreditoVivo)
	}
	saldadas, argsSaldadas := unirRamas(ramas, func(filtro string) string {
		return `SELECT` + columnasVentaLote + `
FROM MSP_PAGOS_VENTAS p
JOIN MSP_SALDOS_VENTAS s ON s.DOCTO_CC_ID = p.DOCTO_CC_ACR_ID
                        AND s.ZONA_CLIENTE_ID = p.ZONA_CLIENTE_ID` + unionesVentaLote + `
WHERE ` + filtro + `
  AND ` + filtroAbonoRuta + `
  AND NOT (s.SALDO > 0)` + filtroCreditoVivo
	})
	if saldadas != "" {
		partes = append(partes, saldadas)
	}
	return strings.Join(partes, "\nUNION ALL\n"), append(args, argsSaldadas...)
}

// queryAbonosLote trae cada abono de ruta desde el inicio más antiguo de su
// zona (no la suma): cada consulta suma en Go los de SU ventana. IMPORTE es NUMERIC(14,2)
// y la suma decimal en Go es exacta, igual que CAST(SUM(IMPORTE) AS
// NUMERIC(18,2)).
func queryAbonosLote(ramas []ramaGrupo) (string, []any) {
	return unirRamas(ramas, func(filtro string) string {
		return `SELECT p.ZONA_CLIENTE_ID, p.DOCTO_CC_ACR_ID, p.FECHA, CAST(p.IMPORTE AS NUMERIC(18,2))
FROM MSP_PAGOS_VENTAS p
WHERE ` + filtro + `
  AND ` + filtroAbonoRuta
	})
}

// abonoRuta: FECHA tal como la entrega el driver (hora de pared), la columna
// exacta del `p.FECHA >= ? AND p.FECHA <= ?` de VentasPorZona.
type abonoRuta struct {
	fecha   time.Time
	importe decimal.Decimal
}

type claveVentaZona struct{ zona, venta int }

// VentasPorZonas implements outbound.CobranzaRepo (E-INF-014): dos sentencias para todas
// las consultas (las ventas candidatas y los abonos), en una misma foto de la
// base (snapshot), y el reparto por consulta en Go.
func (r *CobranzaRepo) VentasPorZonas(
	ctx context.Context, consultas []outbound.ConsultaVentasZona, hasta time.Time,
) ([][]rutasdomain.VentaCobranza, error) {
	out := make([][]rutasdomain.VentaCobranza, len(consultas))
	if len(consultas) == 0 {
		return out, nil
	}
	zonas, grupos := zonasYGrupos(consultas)
	ramas := ramasPorGrupo("p.ZONA_CLIENTE_ID", "p.FECHA", grupos, hasta)
	var (
		candidatas []rutasdomain.VentaCobranza
		abonos     map[claveVentaZona][]abonoRuta
	)
	abonos = map[claveVentaZona][]abonoRuta{}
	err := firebird.RunInSnapshotTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		for _, tramo := range partir(ramas, ramasPorSentencia(contextosAbonos)) {
			qa, aa := queryAbonosLote(tramo)
			leidos, err := leerAbonos(ctx, q, qa, aa)
			if err != nil {
				return err
			}
			for k, v := range leidos { // una zona está en un solo tramo: no se pisan
				abonos[k] = append(abonos[k], v...)
			}
		}
		for i, tramo := range partirVentas(ramas) {
			zs := zonas
			if i > 0 {
				zs = nil // la rama de saldo > 0 va solo en la primera sentencia
			}
			qv, av := queryVentasLote(zs, tramo)
			leidas, err := leerVentas(ctx, q, qv, av)
			if err != nil {
				return err
			}
			candidatas = append(candidatas, leidas...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i, c := range consultas {
		out[i] = repartirVentas(candidatas, abonos, c.ZonaID, nuevaVentanaPared(c.Desde, hasta))
	}
	return out, nil
}

// zonasYGrupos: las zonas distintas (ordenadas) y, por zona, su desde más
// antiguo agrupado por desde.
func zonasYGrupos(consultas []outbound.ConsultaVentasZona) ([]int, []grupoDesde) {
	desdePorZona := map[int]time.Time{}
	for _, c := range consultas {
		if d, ok := desdePorZona[c.ZonaID]; !ok || c.Desde.Before(d) {
			desdePorZona[c.ZonaID] = c.Desde
		}
	}
	zonas := make([]int, 0, len(desdePorZona))
	for z := range desdePorZona {
		zonas = append(zonas, z)
	}
	sort.Ints(zonas)
	return zonas, agruparZonas(desdePorZona)
}

// repartirVentas aplica la regla de VentasPorZona para UNA consulta: ventas de
// la zona con SALDO > 0 o con abono de la ventana > 0, abono = suma de los
// abonos de la misma zona con FECHA en [desde, hasta], orden DOCTO_CC_ID.
// Cada consulta recibe copias propias (el servicio las enriquece en sitio).
func repartirVentas(
	candidatas []rutasdomain.VentaCobranza, abonos map[claveVentaZona][]abonoRuta, zona int, ventana ventanaPared,
) []rutasdomain.VentaCobranza {
	var out []rutasdomain.VentaCobranza
	vistas := map[int]bool{}
	for _, v := range candidatas {
		if v.ZonaID != zona || vistas[v.VentaID] {
			continue
		}
		vistas[v.VentaID] = true
		abono := decimal.New(0, -2) // mismo exponente que CAST(... AS NUMERIC(18,2))
		for _, a := range abonos[claveVentaZona{zona: zona, venta: v.VentaID}] {
			if ventana.contiene(a.fecha) {
				abono = abono.Add(a.importe)
			}
		}
		if !v.Saldo.IsPositive() && !abono.IsPositive() {
			continue
		}
		v.AbonoSemana = abono
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].VentaID < out[j].VentaID })
	return out
}

func leerVentas(ctx context.Context, q firebird.Querier, query string, args []any) ([]rutasdomain.VentaCobranza, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []rutasdomain.VentaCobranza
	for rows.Next() {
		v, serr := scanVentaCobranza(rows)
		if serr != nil {
			return nil, firebird.MapError(serr)
		}
		out = append(out, v)
	}
	return out, firebird.MapError(rows.Err())
}

func leerAbonos(ctx context.Context, q firebird.Querier, query string, args []any) (map[claveVentaZona][]abonoRuta, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[claveVentaZona][]abonoRuta{}
	for rows.Next() {
		var (
			zona, venta int
			fecha       time.Time
			importeRaw  any
		)
		if err := rows.Scan(&zona, &venta, &fecha, &importeRaw); err != nil {
			return nil, firebird.MapError(err)
		}
		importe, err := firebird.ScanDecimal(importeRaw, 2)
		if err != nil {
			return nil, firebird.MapError(err)
		}
		k := claveVentaZona{zona: zona, venta: venta}
		out[k] = append(out[k], abonoRuta{fecha: fecha, importe: importe})
	}
	return out, firebird.MapError(rows.Err())
}
