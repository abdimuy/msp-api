//nolint:misspell // vocabulario español por convención del proyecto.
package desempenofb

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

var (
	_ outbound.ActividadesRepo     = (*ActividadesRepo)(nil)
	_ outbound.ActividadesLoteRepo = (*ActividadesRepo)(nil)
)

// ActividadesRepo lee pagos aplicados (MSP_PAGOS_VENTAS), pagos pendientes
// (MSP_PAGOS_RECIBIDOS en 'P') y visitas (MSP_VISITAS) de una zona.
type ActividadesRepo struct {
	pool *firebird.Pool
}

// NewActividadesRepo arma el repositorio.
func NewActividadesRepo(pool *firebird.Pool) *ActividadesRepo { return &ActividadesRepo{pool: pool} }

// doctoPVDe es el DOCTO_PV_ID de la venta (contrato v2 `docto_pv_id`).
func doctoPVDe(columnaCargo string) string {
	return `(SELECT des.DOCTO_FTE_ID FROM DOCTOS_ENTRE_SIS des
      WHERE des.CLAVE_SIS_FTE = 'PV' AND des.CLAVE_SIS_DEST = 'CC'
        AND des.DOCTO_DEST_ID = ` + columnaCargo + ` ROWS 1)`
}

// folioVentaDe es el folio de punto de venta del cargo, por el puente
// DOCTOS_ENTRE_SIS (mismo patrón que rutas). Subconsultas escalares con
// ROWS 1 y SIN parámetros: firebirdsql v0.9.19 no enlaza ? dentro de tablas
// derivadas. DOCTOS_PV.FOLIO es CHARACTER SET NONE → firebird.Win1252.
func folioVentaDe(columnaCargo string) string {
	return `(SELECT pv.FOLIO FROM DOCTOS_PV pv WHERE pv.DOCTO_PV_ID = (
      SELECT des.DOCTO_FTE_ID FROM DOCTOS_ENTRE_SIS des
      WHERE des.CLAVE_SIS_FTE = 'PV' AND des.CLAVE_SIS_DEST = 'CC'
        AND des.DOCTO_DEST_ID = ` + columnaCargo + ` ROWS 1) ROWS 1)`
}

// queryPagosAplicados: pagos y condonaciones en Microsip. Conceptos 87327
// (Cobranza en ruta) y 27969 (Condonaciones): los que el teléfono ve
// (pagoConceptoFilter), declarados aquí por separado a propósito.
//
// Sin fan-out: la forma de cobro y el pago de la app se resuelven con
// subconsultas escalares MIN(...) (FORMAS_COBRO_DOCTOS y MSP_PAGOS_RECIBIDOS
// no garantizan 1:1). Charsets: CLIENTES.NOMBRE ISO8859_1 (LEFT JOIN →
// sql.NullString); DOCTOS_CC.LAT/LON NONE (Win1252); MSP_PAGOS_RECIBIDOS.ID
// NONE con UUID ASCII y COBRADOR UTF8 (sql.NullString); MSP_USUARIOS.
// FIREBASE_UID ASCII. Usa IDX_MSP_PAGOS_ZONA_FECHA (ZONA_CLIENTE_ID, FECHA).
//
// Microsip manda sobre la caché: cargo, cliente, concepto, fecha, importe,
// cancelado y aplicado salen con la MISMA regla que selectPagoColsP del sync
// (COALESCE(i.*, dc.*, p.*)), escrita aquí aparte porque esa constante no se
// toca (COBRANZA-SYNC.md). El filtro de concepto y el de cancelado se aplican
// sobre esos mismos valores. El rango de fechas sigue sobre p.FECHA (índice).
//
// Liga con la captura de la app (MSP_PAGOS_RECIBIDOS): por IMPTE_DOCTO_CC_ID
// y, si no hay ninguna así, por DOCTO_CC_ID con IMPTE_DOCTO_CC_ID nulo
// (capturas viejas; IDX_PAGOS_REC_DOCTO_CC). MIN(ID) en cada rama: una fila
// de salida por abono, sin fan-out.
func queryPagosAplicados(filtro string) string {
	return `
SELECT
  p.ZONA_CLIENTE_ID,
  p.FECHA,
  p.IMPTE_DOCTO_CC_ID,
  COALESCE(dc.DOCTO_CC_ID, p.DOCTO_CC_ID),
  ` + cargoAplicado + `,
  COALESCE(dc.CLIENTE_ID, p.CLIENTE_ID),
  c.NOMBRE,
  COALESCE(dc.CONCEPTO_CC_ID, p.CONCEPTO_CC_ID),
  COALESCE(p.FECHA, CAST(dc.FECHA AS TIMESTAMP)),
  CAST(COALESCE(i.IMPORTE + i.IMPUESTO, p.IMPORTE + p.IMPUESTO) AS NUMERIC(18,2)),
  COALESCE(i.APLICADO, p.APLICADO),
  dc.LAT,
  dc.LON,
  dc.COBRADOR_ID,
  (SELECT MIN(fcd.FORMA_COBRO_ID) FROM FORMAS_COBRO_DOCTOS fcd
    WHERE fcd.NOM_TABLA_DOCTOS = 'DOCTOS_CC' AND fcd.DOCTO_ID = p.DOCTO_CC_ID),
  ` + folioVentaDe(cargoAplicado) + `,
  ` + doctoPVDe(cargoAplicado) + `,
  pr.ID,
  pr.COBRADOR_ID,
  pr.COBRADOR,
  pr.FORMA_COBRO_ID,
  pr.LAT,
  pr.LON,
  pr.RECEIVED_AT,
  u.FIREBASE_UID,
  (SELECT COUNT(*) FROM MSP_PAGOS_IMAGENES im WHERE im.PAGO_ID = pr.ID)
FROM MSP_PAGOS_VENTAS p
LEFT JOIN IMPORTES_DOCTOS_CC i ON i.IMPTE_DOCTO_CC_ID = p.IMPTE_DOCTO_CC_ID
LEFT JOIN DOCTOS_CC dc ON dc.DOCTO_CC_ID = p.DOCTO_CC_ID
LEFT JOIN CLIENTES c   ON c.CLIENTE_ID  = COALESCE(dc.CLIENTE_ID, p.CLIENTE_ID)
LEFT JOIN MSP_PAGOS_RECIBIDOS pr ON pr.ID = COALESCE(
  (SELECT MIN(pr2.ID) FROM MSP_PAGOS_RECIBIDOS pr2 WHERE pr2.IMPTE_DOCTO_CC_ID = p.IMPTE_DOCTO_CC_ID),
  (SELECT MIN(pr3.ID) FROM MSP_PAGOS_RECIBIDOS pr3
    WHERE pr3.DOCTO_CC_ID = p.DOCTO_CC_ID AND pr3.IMPTE_DOCTO_CC_ID IS NULL))
LEFT JOIN MSP_USUARIOS u ON u.ID = pr.CREATED_BY
WHERE ` + filtro + `
  AND COALESCE(i.CANCELADO, p.CANCELADO) = 'N'
  AND COALESCE(dc.CONCEPTO_CC_ID, p.CONCEPTO_CC_ID) IN (87327, 27969)`
}

// cargoAplicado es el cargo (venta) del abono, con Microsip sobre la caché.
const cargoAplicado = `COALESCE(i.DOCTO_CC_ACR_ID, p.DOCTO_CC_ACR_ID)`

// queryPagosPendientes: capturas de la app que todavía no llegan a Microsip
// (ESTADO 'P'). La zona sale del cargo (MSP_SALDOS_VENTAS), porque
// MSP_PAGOS_RECIBIDOS no la guarda. INTENTOS > 0 = Microsip ya lo rechazó.
//
// Una captura en 'P' cuyo abono YA está vivo en Microsip (el worker no
// alcanzó a marcarla) no es pendiente: sale como aplicada en
// queryPagosAplicados y aquí se excluye, por IMPTE_DOCTO_CC_ID (PK de la
// caché) y por DOCTO_CC_ID vía IMPORTES_DOCTOS_CC (MSP_PAGOS_VENTAS no tiene
// índice por DOCTO_CC_ID). "Vivo" = la misma regla de cancelado que arriba.
//
// CLIENTE_ID e IMPORTE son nullables (migración 000016): una fila sin ellos
// se descarta con advertencia en vez de tumbar la respuesta.
func queryPagosPendientes(filtro string) string {
	return `
SELECT
  s.ZONA_CLIENTE_ID,
  pr.FECHA,
  pr.ID,
  pr.CARGO_DOCTO_CC_ID,
  pr.CLIENTE_ID,
  c.NOMBRE,
  pr.CONCEPTO_CC_ID,
  pr.FECHA,
  CAST(pr.IMPORTE AS NUMERIC(18,2)),
  pr.FORMA_COBRO_ID,
  pr.LAT,
  pr.LON,
  pr.COBRADOR_ID,
  pr.COBRADOR,
  pr.INTENTOS,
  pr.RECEIVED_AT,
  u.FIREBASE_UID,
  (SELECT COUNT(*) FROM MSP_PAGOS_IMAGENES im WHERE im.PAGO_ID = pr.ID),
  ` + folioVentaDe("pr.CARGO_DOCTO_CC_ID") + `,
  ` + doctoPVDe("pr.CARGO_DOCTO_CC_ID") + `
FROM MSP_PAGOS_RECIBIDOS pr
JOIN MSP_SALDOS_VENTAS s ON s.DOCTO_CC_ID = pr.CARGO_DOCTO_CC_ID
LEFT JOIN CLIENTES c     ON c.CLIENTE_ID  = pr.CLIENTE_ID
LEFT JOIN MSP_USUARIOS u ON u.ID = pr.CREATED_BY
WHERE pr.ESTADO = 'P'
  AND ` + filtro + `
  AND NOT EXISTS (SELECT 1 FROM MSP_PAGOS_VENTAS p2
    LEFT JOIN IMPORTES_DOCTOS_CC i2 ON i2.IMPTE_DOCTO_CC_ID = p2.IMPTE_DOCTO_CC_ID
    WHERE p2.IMPTE_DOCTO_CC_ID = pr.IMPTE_DOCTO_CC_ID
      AND COALESCE(i2.CANCELADO, p2.CANCELADO) = 'N')
  AND NOT EXISTS (SELECT 1 FROM IMPORTES_DOCTOS_CC i3
    JOIN MSP_PAGOS_VENTAS p3 ON p3.IMPTE_DOCTO_CC_ID = i3.IMPTE_DOCTO_CC_ID
    WHERE i3.DOCTO_CC_ID = pr.DOCTO_CC_ID
      AND COALESCE(i3.CANCELADO, p3.CANCELADO) = 'N')`
}

// queryVisitas: MSP_VISITAS es legacy (Node). Sus columnas de texto son
// CHARACTER SET NONE con bytes UTF-8: se escanean como string plano, nunca
// Win1252 (visitasfb/repo.go). IMPTE_DOCTO_CC_ID es la VENTA (cargo) a la que
// se ligó la visita, como en el teléfono (DerivacionModelos.kt): el nombre
// miente y C0 lo mide (consulta 5).
func queryVisitas(filtro string) string {
	return `
SELECT
  v.ZONA_CLIENTE_ID,
  v.FECHA,
  v.ID,
  v.CLIENTE_ID,
  c.NOMBRE,
  v.FECHA,
  v.TIPO_VISITA,
  v.NOTA,
  v.LAT,
  v.LNG,
  v.COBRADOR_ID,
  v.COBRADOR,
  v.IMPTE_DOCTO_CC_ID,
  (SELECT COUNT(*) FROM MSP_VISITAS_IMAGENES im WHERE im.VISITA_ID = v.ID),
  ` + folioVentaDe("v.IMPTE_DOCTO_CC_ID") + `,
  ` + doctoPVDe("v.IMPTE_DOCTO_CC_ID") + `
FROM MSP_VISITAS v
LEFT JOIN CLIENTES c ON c.CLIENTE_ID = v.CLIENTE_ID
WHERE ` + filtro
}

// ActividadesZona implementa outbound.ActividadesRepo: es la lectura por lote
// con una sola consulta.
func (r *ActividadesRepo) ActividadesZona(ctx context.Context, zonaID int, desde, hasta time.Time) ([]domain.Actividad, error) {
	out, err := r.ActividadesZonas(ctx, []outbound.ConsultaActividades{{ZonaID: zonaID, Desde: desde, Hasta: hasta}})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}

// claveFila son las dos primeras columnas de cada lectura por lote: la zona
// por la que se reparte y la columna EXACTA del filtro de fechas del WHERE
// (p.FECHA, pr.FECHA, v.FECHA) tal como la entrega el driver, en hora de pared.
type claveFila struct {
	zona  int
	fecha time.Time
}

// filaZona es una actividad con la clave de la fila que la trajo.
type filaZona struct {
	clave claveFila
	act   domain.Actividad
}

// ActividadesZonas implementa outbound.ActividadesLoteRepo: TRES sentencias
// para todas las consultas (aplicados, pendientes, visitas), sobre el
// conjunto de zonas y la ventana mínima que las cubre, en una transacción de
// solo lectura (una misma foto de la base). El reparto por consulta se hace
// en Go con la misma regla de ventana inclusiva; cada consulta conserva el
// orden de siempre: aplicados, luego pendientes, luego visitas.
func (r *ActividadesRepo) ActividadesZonas(
	ctx context.Context, consultas []outbound.ConsultaActividades,
) ([][]domain.Actividad, error) {
	out := make([][]domain.Actividad, len(consultas))
	if len(consultas) == 0 {
		return out, nil
	}
	grupos, hasta := cubrirConsultas(consultas)
	var porTipo [3][]filaZona
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		for i, lectura := range lecturasActividades(grupos, hasta) {
			filas, err := leer(ctx, q, lectura.sql, lectura.args, lectura.scan)
			if err != nil {
				return err
			}
			porTipo[i] = filas
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, filas := range porTipo {
		ordenarEmpates(filas)
	}
	for i, c := range consultas {
		v := nuevaVentanaPared(c.Desde, c.Hasta)
		var acts []domain.Actividad
		for _, filas := range porTipo {
			for _, f := range filas {
				if f.clave.zona == c.ZonaID && v.contiene(f.clave.fecha) {
					acts = append(acts, f.act)
				}
			}
		}
		out[i] = acts
	}
	return out, nil
}

// ordenarEmpates fija el orden de cada tipo por (hora, id). La derivación
// resuelve los empates por el orden de entrada (deriver.go: masReciente se
// queda con el PRIMERO de dos pagos de la misma venta a la misma hora, y entre
// visitas iguales gana la primera), y ninguna de las tres consultas tiene
// ORDER BY: sin este orden, quién "determina" una venta en un empate exacto
// dependía del plan de Firebird. Desempate documentado: gana el id menor
// (E-INF-013: en dev cambia el DeterminadoPor de UNA venta respecto al orden
// que daba el plan de antes).
// Ordenar en Go y no en SQL: un ORDER BY sobre la fecha haría que Firebird
// recorriera el índice en orden en vez de usar el de zona y fecha.
func ordenarEmpates(filas []filaZona) {
	sort.SliceStable(filas, func(i, j int) bool {
		a, b := filas[i].act, filas[j].act
		if !a.Hora.Equal(b.Hora) {
			return a.Hora.Before(b.Hora)
		}
		return a.ID < b.ID
	})
}

// cubrirConsultas: por zona, el desde más antiguo que se le pide (agrupado
// por desde) y el hasta más reciente de todas.
func cubrirConsultas(consultas []outbound.ConsultaActividades) ([]grupoDesde, time.Time) {
	desdePorZona := map[int]time.Time{}
	hasta := consultas[0].Hasta
	for _, c := range consultas {
		if d, ok := desdePorZona[c.ZonaID]; !ok || c.Desde.Before(d) {
			desdePorZona[c.ZonaID] = c.Desde
		}
		if c.Hasta.After(hasta) {
			hasta = c.Hasta
		}
	}
	return agruparZonas(desdePorZona), hasta
}

type lecturaActividades struct {
	sql  string
	args []any
	scan func(*sql.Rows, *claveFila) (domain.Actividad, error)
}

// lecturasActividades: las tres sentencias por lote, cada una filtrada por la
// columna de zona y la de fecha de SU tabla (las mismas del WHERE por zona),
// una rama UNION ALL por grupo de zonas, en el orden de siempre: aplicados,
// pendientes, visitas.
func lecturasActividades(grupos []grupoDesde, hasta time.Time) [3]lecturaActividades {
	fa, aa := unirRamas(ramasPorGrupo("p.ZONA_CLIENTE_ID", "p.FECHA", grupos, hasta), queryPagosAplicados)
	fp, ap := unirRamas(ramasPorGrupo("s.ZONA_CLIENTE_ID", "pr.FECHA", grupos, hasta), queryPagosPendientes)
	fv, av := unirRamas(ramasPorGrupo("v.ZONA_CLIENTE_ID", "v.FECHA", grupos, hasta), queryVisitas)
	return [3]lecturaActividades{
		{fa, aa, scanPagoAplicado},
		{fp, ap, scanPagoPendiente},
		{fv, av, scanVisita},
	}
}

// errFilaDescartada lo devuelve un scan que decidió omitir la fila (ya dejó
// su advertencia en el log); leer sigue con la siguiente.
var errFilaDescartada = errors.New("desempenofb: fila descartada")

func leer(ctx context.Context, q firebird.Querier, query string, args []any,
	scan func(*sql.Rows, *claveFila) (domain.Actividad, error),
) ([]filaZona, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []filaZona
	for rows.Next() {
		var k claveFila
		a, err := scan(rows, &k)
		if errors.Is(err, errFilaDescartada) {
			continue
		}
		if err != nil {
			return nil, firebird.MapError(err)
		}
		out = append(out, filaZona{clave: k, act: a})
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return out, nil
}

// coordenada lee un texto decimal ("18.4617"); vacío o ilegible = sin dato.
func coordenada(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

func primeraCoordenada(a, b string) *float64 {
	if c := coordenada(a); c != nil {
		return c
	}
	return coordenada(b)
}

func instanteNulo(raw any) (*time.Time, error) {
	if raw == nil {
		return nil, nil //nolint:nilnil // NULL es "sin dato"
	}
	t, err := firebird.ScanUTCTime(raw)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func scanPagoAplicado(rows *sql.Rows, k *claveFila) (domain.Actividad, error) {
	var (
		impteID, doctoCCID, cargoID, clienteID, conceptoID int
		clienteNombre                                      sql.NullString
		fechaRaw, importeRaw, recibidoRaw                  any
		aplicado                                           string
		dcLat                                              firebird.Win1252 // DOCTOS_CC.LAT — NONE
		dcLon                                              firebird.Win1252 // DOCTOS_CC.LON — NONE
		folio                                              firebird.Win1252 // DOCTOS_PV.FOLIO — NONE
		prLat                                              firebird.Win1252 // MSP_PAGOS_RECIBIDOS.LAT — NONE
		prLon                                              firebird.Win1252 // MSP_PAGOS_RECIBIDOS.LON — NONE
		dcCobradorID, fcdForma, prCobradorID, prForma      sql.NullInt64
		doctoPV                                            sql.NullInt64
		prID, prCobrador, uid                              sql.NullString
		numFotos                                           int
	)
	if err := rows.Scan(&k.zona, &k.fecha, &impteID, &doctoCCID, &cargoID, &clienteID, &clienteNombre, &conceptoID,
		&fechaRaw, &importeRaw, &aplicado, &dcLat, &dcLon, &dcCobradorID, &fcdForma, &folio,
		&doctoPV, &prID, &prCobradorID, &prCobrador, &prForma, &prLat, &prLon, &recibidoRaw, &uid, &numFotos); err != nil {
		return domain.Actividad{}, err
	}
	hora, err := firebird.ScanUTCTime(fechaRaw)
	if err != nil {
		return domain.Actividad{}, err
	}
	importe, err := firebird.ScanDecimal(importeRaw, 2)
	if err != nil {
		return domain.Actividad{}, err
	}
	forma := int(fcdForma.Int64)
	if !fcdForma.Valid {
		forma = int(prForma.Int64)
	}
	a := domain.Actividad{
		ID: domain.IDPagoAplicado(impteID), Tipo: domain.ClasificarTipoPago(forma, conceptoID),
		Hora: hora, ClienteID: clienteID, ClienteNombre: clienteNombre.String,
		VentaID: cargoID, DoctoPVID: int(doctoPV.Int64), Folio: strings.TrimSpace(string(folio)), NumFotos: numFotos,
		Lat: primeraCoordenada(string(dcLat), string(prLat)), Lng: primeraCoordenada(string(dcLon), string(prLon)),
		Pago: &domain.DatosPago{
			Importe: importe, FormaCobroID: forma, Aplicado: aplicado == "S", DoctoCCID: doctoCCID,
		},
		RegistradoPor: domain.RegistradoPor{CobradorID: int(dcCobradorID.Int64), Origen: domain.OrigenOficina},
	}
	if prID.Valid {
		a.Pago.PagoRecibidoID = strings.TrimSpace(prID.String)
		a.RegistradoPor = domain.RegistradoPor{
			UsuarioUID: uid.String, CobradorID: int(prCobradorID.Int64),
			Nombre: prCobrador.String, Origen: domain.OrigenApp,
		}
		if a.RecibidoAt, err = instanteNulo(recibidoRaw); err != nil {
			return domain.Actividad{}, err
		}
	}
	return a, nil
}

func scanPagoPendiente(rows *sql.Rows, k *claveFila) (domain.Actividad, error) {
	var (
		id                                     string
		cargoID                                int
		clienteNombre, cobrador, uid           sql.NullString
		conceptoID, forma, cobradorID, doctoPV sql.NullInt64
		clienteID                              sql.NullInt64
		fechaRaw, importeRaw, recibido         any
		lat                                    firebird.Win1252 // MSP_PAGOS_RECIBIDOS.LAT — NONE
		lon                                    firebird.Win1252 // MSP_PAGOS_RECIBIDOS.LON — NONE
		folio                                  firebird.Win1252 // DOCTOS_PV.FOLIO — NONE
		numFotos                               int
		intentos                               sql.NullInt64 // nullable (migración 000016)
	)
	if err := rows.Scan(&k.zona, &k.fecha, &id, &cargoID, &clienteID, &clienteNombre, &conceptoID, &fechaRaw, &importeRaw,
		&forma, &lat, &lon, &cobradorID, &cobrador, &intentos, &recibido, &uid, &numFotos, &folio, &doctoPV); err != nil {
		return domain.Actividad{}, err
	}
	id = strings.TrimSpace(id)
	if !clienteID.Valid || importeRaw == nil {
		slog.Warn("desempenofb: pago pendiente sin cliente o sin importe; se omite",
			"pago_recibido_id", id, "cliente_nulo", !clienteID.Valid, "importe_nulo", importeRaw == nil)
		return domain.Actividad{}, errFilaDescartada
	}
	hora, err := firebird.ScanUTCTime(fechaRaw)
	if err != nil {
		return domain.Actividad{}, err
	}
	importe, err := firebird.ScanDecimal(importeRaw, 2)
	if err != nil {
		return domain.Actividad{}, err
	}
	a := domain.Actividad{
		ID: domain.IDPagoPendiente(id), Tipo: domain.ClasificarTipoPago(int(forma.Int64), int(conceptoID.Int64)),
		Hora: hora, ClienteID: int(clienteID.Int64), ClienteNombre: clienteNombre.String,
		VentaID: cargoID, DoctoPVID: int(doctoPV.Int64), Folio: strings.TrimSpace(string(folio)), NumFotos: numFotos,
		Lat: coordenada(string(lat)), Lng: coordenada(string(lon)),
		RegistradoPor: domain.RegistradoPor{
			UsuarioUID: uid.String, CobradorID: int(cobradorID.Int64), Nombre: cobrador.String, Origen: domain.OrigenApp,
		},
		Pago: &domain.DatosPago{
			Importe: importe, FormaCobroID: int(forma.Int64), Aplicado: false,
			Rechazado: intentos.Int64 > 0, PagoRecibidoID: id,
		},
	}
	if a.RecibidoAt, err = instanteNulo(recibido); err != nil {
		return domain.Actividad{}, err
	}
	return a, nil
}

func scanVisita(rows *sql.Rows, k *claveFila) (domain.Actividad, error) {
	var (
		id, tipoVisita, cobrador string
		clienteID, cobradorID    int
		clienteNombre, nota      sql.NullString
		fechaRaw                 any
		lat, lng                 float64
		ventaID, doctoPV         sql.NullInt64
		numFotos                 int
		folio                    firebird.Win1252 // DOCTOS_PV.FOLIO — NONE
	)
	if err := rows.Scan(&k.zona, &k.fecha, &id, &clienteID, &clienteNombre, &fechaRaw, &tipoVisita, &nota, &lat, &lng,
		&cobradorID, &cobrador, &ventaID, &numFotos, &folio, &doctoPV); err != nil {
		return domain.Actividad{}, err
	}
	hora, err := firebird.ScanUTCTime(fechaRaw)
	if err != nil {
		return domain.Actividad{}, err
	}
	id = strings.TrimSpace(id)
	return domain.Actividad{
		ID: domain.IDVisita(id), Tipo: domain.TipoVisita, Hora: hora,
		ClienteID: clienteID, ClienteNombre: clienteNombre.String,
		VentaID: int(ventaID.Int64), DoctoPVID: int(doctoPV.Int64), Folio: strings.TrimSpace(string(folio)), NumFotos: numFotos,
		Lat: &lat, Lng: &lng,
		RegistradoPor: domain.RegistradoPor{CobradorID: cobradorID, Nombre: cobrador, Origen: domain.OrigenApp},
		Visita:        &domain.DatosVisita{VisitaID: id, TipoVisita: tipoVisita, Nota: nota.String},
	}, nil
}
