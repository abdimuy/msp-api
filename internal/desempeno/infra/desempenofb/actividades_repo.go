//nolint:misspell // vocabulario español por convención del proyecto.
package desempenofb

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

var _ outbound.ActividadesRepo = (*ActividadesRepo)(nil)

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
var queryPagosAplicados = `
SELECT
  p.IMPTE_DOCTO_CC_ID,
  p.DOCTO_CC_ID,
  p.DOCTO_CC_ACR_ID,
  p.CLIENTE_ID,
  c.NOMBRE,
  p.CONCEPTO_CC_ID,
  p.FECHA,
  CAST(p.IMPORTE + p.IMPUESTO AS NUMERIC(18,2)),
  p.APLICADO,
  dc.LAT,
  dc.LON,
  dc.COBRADOR_ID,
  (SELECT MIN(fcd.FORMA_COBRO_ID) FROM FORMAS_COBRO_DOCTOS fcd
    WHERE fcd.NOM_TABLA_DOCTOS = 'DOCTOS_CC' AND fcd.DOCTO_ID = p.DOCTO_CC_ID),
  ` + folioVentaDe("p.DOCTO_CC_ACR_ID") + `,
  ` + doctoPVDe("p.DOCTO_CC_ACR_ID") + `,
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
LEFT JOIN DOCTOS_CC dc ON dc.DOCTO_CC_ID = p.DOCTO_CC_ID
LEFT JOIN CLIENTES c   ON c.CLIENTE_ID  = p.CLIENTE_ID
LEFT JOIN MSP_PAGOS_RECIBIDOS pr ON pr.ID = (
  SELECT MIN(pr2.ID) FROM MSP_PAGOS_RECIBIDOS pr2 WHERE pr2.IMPTE_DOCTO_CC_ID = p.IMPTE_DOCTO_CC_ID)
LEFT JOIN MSP_USUARIOS u ON u.ID = pr.CREATED_BY
WHERE p.ZONA_CLIENTE_ID = ?
  AND p.FECHA >= ?
  AND p.FECHA <= ?
  AND p.CANCELADO = 'N'
  AND p.CONCEPTO_CC_ID IN (87327, 27969)`

// queryPagosPendientes: capturas de la app que todavía no llegan a Microsip
// (ESTADO 'P'). La zona sale del cargo (MSP_SALDOS_VENTAS), porque
// MSP_PAGOS_RECIBIDOS no la guarda. INTENTOS > 0 = Microsip ya lo rechazó.
var queryPagosPendientes = `
SELECT
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
  AND s.ZONA_CLIENTE_ID = ?
  AND pr.FECHA >= ?
  AND pr.FECHA <= ?`

// queryVisitas: MSP_VISITAS es legacy (Node). Sus columnas de texto son
// CHARACTER SET NONE con bytes UTF-8: se escanean como string plano, nunca
// Win1252 (visitasfb/repo.go). IMPTE_DOCTO_CC_ID es la VENTA (cargo) a la que
// se ligó la visita, como en el teléfono (DerivacionModelos.kt): el nombre
// miente y C0 lo mide (consulta 5).
var queryVisitas = `
SELECT
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
WHERE v.ZONA_CLIENTE_ID = ?
  AND v.FECHA >= ?
  AND v.FECHA <= ?`

// ActividadesZona implementa outbound.ActividadesRepo. Las tres lecturas van
// en una transacción de solo lectura (una misma foto de la base).
func (r *ActividadesRepo) ActividadesZona(ctx context.Context, zonaID int, desde, hasta time.Time) ([]domain.Actividad, error) {
	var out []domain.Actividad
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		args := []any{zonaID, firebird.ToWallClock(desde), firebird.ToWallClock(hasta)}
		for _, lectura := range []struct {
			sql  string
			scan func(*sql.Rows) (domain.Actividad, error)
		}{
			{queryPagosAplicados, scanPagoAplicado},
			{queryPagosPendientes, scanPagoPendiente},
			{queryVisitas, scanVisita},
		} {
			acts, err := leer(ctx, q, lectura.sql, args, lectura.scan)
			if err != nil {
				return err
			}
			out = append(out, acts...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func leer(ctx context.Context, q firebird.Querier, query string, args []any,
	scan func(*sql.Rows) (domain.Actividad, error),
) ([]domain.Actividad, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Actividad
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, firebird.MapError(err)
		}
		out = append(out, a)
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

func scanPagoAplicado(rows *sql.Rows) (domain.Actividad, error) {
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
	if err := rows.Scan(&impteID, &doctoCCID, &cargoID, &clienteID, &clienteNombre, &conceptoID,
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

func scanPagoPendiente(rows *sql.Rows) (domain.Actividad, error) {
	var (
		id                                     string
		cargoID, clienteID                     int
		clienteNombre, cobrador, uid           sql.NullString
		conceptoID, forma, cobradorID, doctoPV sql.NullInt64
		fechaRaw, importeRaw, recibido         any
		lat                                    firebird.Win1252 // MSP_PAGOS_RECIBIDOS.LAT — NONE
		lon                                    firebird.Win1252 // MSP_PAGOS_RECIBIDOS.LON — NONE
		folio                                  firebird.Win1252 // DOCTOS_PV.FOLIO — NONE
		intentos, numFotos                     int
	)
	if err := rows.Scan(&id, &cargoID, &clienteID, &clienteNombre, &conceptoID, &fechaRaw, &importeRaw,
		&forma, &lat, &lon, &cobradorID, &cobrador, &intentos, &recibido, &uid, &numFotos, &folio, &doctoPV); err != nil {
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
	id = strings.TrimSpace(id)
	a := domain.Actividad{
		ID: domain.IDPagoPendiente(id), Tipo: domain.ClasificarTipoPago(int(forma.Int64), int(conceptoID.Int64)),
		Hora: hora, ClienteID: clienteID, ClienteNombre: clienteNombre.String,
		VentaID: cargoID, DoctoPVID: int(doctoPV.Int64), Folio: strings.TrimSpace(string(folio)), NumFotos: numFotos,
		Lat: coordenada(string(lat)), Lng: coordenada(string(lon)),
		RegistradoPor: domain.RegistradoPor{
			UsuarioUID: uid.String, CobradorID: int(cobradorID.Int64), Nombre: cobrador.String, Origen: domain.OrigenApp,
		},
		Pago: &domain.DatosPago{
			Importe: importe, FormaCobroID: int(forma.Int64), Aplicado: false,
			Rechazado: intentos > 0, PagoRecibidoID: id,
		},
	}
	if a.RecibidoAt, err = instanteNulo(recibido); err != nil {
		return domain.Actividad{}, err
	}
	return a, nil
}

func scanVisita(rows *sql.Rows) (domain.Actividad, error) {
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
	if err := rows.Scan(&id, &clienteID, &clienteNombre, &fechaRaw, &tipoVisita, &nota, &lat, &lng,
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
