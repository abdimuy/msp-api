//nolint:misspell,paralleltest // vocabulario español (comando, Comandos, firma) por convención del proyecto; las pruebas de Firebird comparten una transacción con rollback.
package garhttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garfb"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garhttp"
	"github.com/abdimuy/msp-api/internal/garantias/infra/storage"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Los tres e2e por HTTP del §8: de httptest al app real y de ahí a garfb real,
// dentro de WithTestTransaction, que revierte todo al terminar. El router es el
// de producción (garhttp.MountRouter) y el almacenamiento es el de producción
// sobre un directorio temporal; lo único doble es la identidad.
//
// La tarea 10 añade la huella a MSP_GA_EVENTO, la auditoría a MSP_GA_EVENTO y
// TIPO a MSP_GA_IMAGEN, y hace que Guardar inserte las imágenes. Mientras no
// esté integrada, las aserciones que dependen de esas columnas se omiten con
// aviso y el resto de la historia corre igual; el reporte declara el hueco.

// txRunnerHTTP reutiliza firebird.RunInTx, que es re-entrante: la transacción
// de WithTestTransaction ya viaja en el contexto, así que las peticiones HTTP
// escriben en ella y el rollback final las borra.
type txRunnerHTTP struct {
	pool *firebird.Pool
}

func (r txRunnerHTTP) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	return firebird.RunInTx(ctx, r.pool.DB, fn)
}

// relojHTTP congela la hora con la que app sella los eventos.
type relojHTTP struct {
	ahora time.Time
}

func (r *relojHTTP) Now() time.Time { return r.ahora }

// entornoE2EHTTP reúne el router real y los repositorios que la prueba usa
// para leer de la base lo que el HTTP devolvió escrito.
type entornoE2EHTTP struct {
	router    http.Handler
	garantias *garfb.GarantiaRepo
	eventos   *garfb.EventoRepo
	pool      *firebird.Pool
	storage   string
	clk       *relojHTTP
}

// nuevoEntornoE2EHTTP monta el módulo completo sobre Firebird real y un
// almacenamiento de archivos en un directorio temporal.
func nuevoEntornoE2EHTTP(t *testing.T, pool *firebird.Pool) *entornoE2EHTTP {
	t.Helper()

	garantias := garfb.NewGarantiaRepo(pool)
	bandeja := garfb.NewBandejaRepo(pool)
	eventos := garfb.NewEventoRepo(pool)
	imagenes := garfb.NewImagenRepo(pool)
	folios := garfb.NewFolioGenerator(pool)

	base := t.TempDir()

	almacen, err := storage.NewFilesystemProvider(base)
	require.NoError(t, err)

	identity := &fakeHTTPIdentity{
		usuario:   outbound.Usuario{ID: "usuario-1", Nombre: "Ruben"},
		permitido: true,
	}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: garantias,
		Bandeja:   bandeja,
		Eventos:   eventos,
		Identity:  identity,
	})

	clk := &relojHTTP{ahora: time.Date(2026, time.October, 20, 15, 0, 0, 0, time.UTC)}

	servicio := app.NewService(app.Deps{
		GarantiaRepo:   garantias,
		EventoRepo:     eventos,
		ImagenRepo:     imagenes,
		FolioGenerator: folios,
		TxRunner:       txRunnerHTTP{pool: pool},
		Identity:       identity,
		Clock:          clk,
		Storage:        almacen,
	})

	r := chi.NewRouter()
	garhttp.MountRouter(r, garhttp.Deps{Consultas: consultas, Comandos: servicio})

	return &entornoE2EHTTP{
		router:    r,
		garantias: garantias,
		eventos:   eventos,
		pool:      pool,
		storage:   base,
		clk:       clk,
	}
}

// cuerpo arma el cuerpo JSON de un comando con el sello del dispositivo que
// usa toda la historia.
func (e *entornoE2EHTTP) cuerpo(extras map[string]any) map[string]any {
	cuerpo := map[string]any{
		"device_created_at": e.clk.Now().Format(time.RFC3339),
	}

	for campo, valor := range extras {
		cuerpo[campo] = valor
	}

	return cuerpo
}

// json envía un comando JSON y devuelve la respuesta cruda, para las pruebas
// que esperan un error.
func (e *entornoE2EHTTP) json(
	ctx context.Context,
	t *testing.T,
	ruta, clave string,
	cuerpo map[string]any,
) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer

	require.NoError(t, json.NewEncoder(&buf).Encode(cuerpo))

	req := httptest.NewRequest(http.MethodPost, ruta, &buf).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", clave)

	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)

	return rec
}

// jsonOK envía un comando JSON, exige 200 y devuelve el folio de la respuesta.
func (e *entornoE2EHTTP) jsonOK(
	ctx context.Context,
	t *testing.T,
	ruta, clave string,
	cuerpo map[string]any,
) garhttp.GarantiaDTO {
	t.Helper()

	return folioDeRespuesta(t, e.json(ctx, t, ruta, clave, cuerpo))
}

// entrega envía el multipart de entregar-garantia.
func (e *entornoE2EHTTP) entrega(
	ctx context.Context,
	t *testing.T,
	garantiaID, clave string,
	lat, lon float64,
	firma []byte,
) *httptest.ResponseRecorder {
	t.Helper()

	var cuerpo bytes.Buffer

	writer := multipart.NewWriter(&cuerpo)

	require.NoError(t, writer.WriteField("device_created_at", e.clk.Now().Format(time.RFC3339)))
	require.NoError(t, writer.WriteField("gps_lat", strconv.FormatFloat(lat, 'f', 6, 64)))
	require.NoError(t, writer.WriteField("gps_lon", strconv.FormatFloat(lon, 'f', 6, 64)))

	parte, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="firma"; filename="firma.jpg"`},
		"Content-Type":        {"image/jpeg"},
	})
	require.NoError(t, err)

	_, err = parte.Write(firma)
	require.NoError(t, err)

	require.NoError(t, writer.Close())

	req := httptest.NewRequest(
		http.MethodPost,
		"/garantias/"+garantiaID+"/entregar",
		&cuerpo,
	).WithContext(ctx)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Idempotency-Key", clave)

	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)

	return rec
}

// avanzar mueve un artículo, con GPS opcional para la recolección.
func (e *entornoE2EHTTP) avanzar(
	ctx context.Context,
	t *testing.T,
	garantiaID, articuloID string,
	etapa domain.Etapa,
	clave string,
	lat, lon *float64,
) garhttp.GarantiaDTO {
	t.Helper()

	extras := map[string]any{"etapa_destino": string(etapa)}

	if lat != nil && lon != nil {
		extras["gps_lat"] = *lat
		extras["gps_lon"] = *lon
	}

	return e.jsonOK(ctx, t,
		"/garantias/"+garantiaID+"/articulos/"+articuloID+"/avanzar",
		clave, e.cuerpo(extras),
	)
}

// folioDeRespuesta decodifica la respuesta común de los comandos.
func folioDeRespuesta(t *testing.T, rec *httptest.ResponseRecorder) garhttp.GarantiaDTO {
	t.Helper()

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var dto garhttp.GarantiaDTO

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto), rec.Body.String())

	return dto
}

// tarea10AplicadaHTTP comprueba las cuatro columnas que la tarea 10 añade y
// que habilitan las aserciones de huella, auditoría e imagen. Sin ellas, esas
// aserciones se omiten; el resto del e2e corre contra la base tal como está.
func tarea10AplicadaHTTP(ctx context.Context, t *testing.T, pool *firebird.Pool) bool {
	t.Helper()

	var columnas int

	err := firebird.GetQuerier(ctx, pool.DB).QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM RDB$RELATION_FIELDS
		  WHERE (RDB$RELATION_NAME = 'MSP_GA_EVENTO'
		         AND RDB$FIELD_NAME IN ('HUELLA', 'AUDITORIA_GPS', 'DISTANCIA_ENTREGA_M'))
		     OR (RDB$RELATION_NAME = 'MSP_GA_IMAGEN'
		         AND RDB$FIELD_NAME = 'TIPO')`,
	).Scan(&columnas)
	require.NoError(t, err)

	return columnas == 4
}

// eventoFolio devuelve el único evento del tipo pedido dentro de la línea de
// tiempo del folio.
func eventoFolio(t *testing.T, evs []*domain.Evento, tipo domain.TipoEvento) *domain.Evento {
	t.Helper()

	var encontrados []*domain.Evento

	for _, evento := range evs {
		if evento.Tipo() == tipo {
			encontrados = append(encontrados, evento)
		}
	}

	require.Len(t, encontrados, 1, "evento %s", tipo)

	return encontrados[0]
}

// eventosConClave cuenta los eventos escritos con una clave de idempotencia.
func eventosConClave(evs []*domain.Evento, clave string) int {
	total := 0

	for _, evento := range evs {
		if evento.ClaveIdempotencia() == clave {
			total++
		}
	}

	return total
}

// imagenesFirmaDelFolio lee de la base las imágenes de tipo firma colgadas de
// eventos del folio. Es la comprobación literal del brief: TIPO='firma' y
// EVENTO_ID apuntando al folio_entregado.
func imagenesFirmaDelFolio(
	ctx context.Context,
	t *testing.T,
	pool *firebird.Pool,
	garantiaID string,
) []string {
	t.Helper()

	rows, err := firebird.GetQuerier(ctx, pool.DB).QueryContext(
		ctx,
		`SELECT i.RUTA
		   FROM MSP_GA_IMAGEN i
		   JOIN MSP_GA_EVENTO e ON e.ID = i.EVENTO_ID
		  WHERE e.GARANTIA_ID = ?
		    AND e.TIPO = ?
		    AND i.TIPO = ?`,
		garantiaID,
		string(domain.TipoEventoFolioEntregado),
		string(domain.TipoImagenFirma),
	)
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	var rutas []string

	for rows.Next() {
		var ruta string

		require.NoError(t, rows.Scan(&ruta))

		rutas = append(rutas, ruta)
	}

	require.NoError(t, rows.Err())

	return rutas
}

// blobsFirma lista los archivos de firma que quedaron en el almacenamiento
// temporal, sin los sidecars de metadatos.
func blobsFirma(t *testing.T, baseDir string) []string {
	t.Helper()

	dir := filepath.Join(baseDir, "garantias", "firmas")

	entradas, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	require.NoError(t, err)

	var nombres []string

	for _, entrada := range entradas {
		if entrada.IsDir() || strings.HasSuffix(entrada.Name(), ".meta") {
			continue
		}

		nombres = append(nombres, entrada.Name())
	}

	return nombres
}

// TestE2EHTTP_PisoEntregaCierraYReintenta recorre la historia 1 del §8 por
// HTTP. El folio de piso se entrega en mostrador con firma y cierra con
// auditoría no_aplica. Además, sobre la entrega, el mismo POST se manda dos
// veces con la misma clave y una tercera con otro cuerpo.
func TestE2EHTTP_PisoEntregaCierraYReintenta(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		ent := nuevoEntornoE2EHTTP(t, pool)
		clave := func() string { return uuid.NewString() }

		abierta := ent.jsonOK(ctx, t, "/garantias", clave(), ent.cuerpo(map[string]any{
			"origen":      "piso",
			"description": "Sillón de piso con costura abierta",
		}))
		id := abierta.ID

		agregada := ent.jsonOK(ctx, t, "/garantias/"+id+"/articulos", clave(), ent.cuerpo(map[string]any{
			"articulo_id": 981003,
			"clave":       "PISO-001",
			"description": "Sillón de exhibición",
		}))
		require.Len(t, agregada.Articulos, 1)
		articuloID := agregada.Articulos[0].ID

		ent.jsonOK(ctx, t, "/garantias/"+id+"/iniciar-proceso", clave(), ent.cuerpo(nil))

		ent.avanzar(ctx, t, id, articuloID, domain.EtapaEnRevision, clave(), nil, nil)

		ent.jsonOK(ctx, t, "/garantias/"+id+"/articulos/"+articuloID+"/diagnostico", clave(), ent.cuerpo(map[string]any{
			"ruta":        "taller",
			"rol_decisor": "tecnica",
		}))

		ent.avanzar(ctx, t, id, articuloID, domain.EtapaReparadoTaller, clave(), nil, nil)
		ent.avanzar(ctx, t, id, articuloID, domain.EtapaListoEntrega, clave(), nil, nil)
		ent.avanzar(ctx, t, id, articuloID, domain.EtapaReingresadoInventario, clave(), nil, nil)

		ent.jsonOK(ctx, t, "/garantias/"+id+"/listo-entrega", clave(), ent.cuerpo(nil))

		// La entrega de piso es en mostrador y con GPS: es la que habilita el
		// cierre. La firma viaja en el multipart.
		claveEntrega := clave()
		lat, lon := 19.4326, -99.1332

		primera := ent.entrega(ctx, t, id, claveEntrega, lat, lon, firmaJPEGDePrueba)
		folioDeRespuesta(t, primera)

		if tarea10AplicadaHTTP(ctx, t, ent.pool) {
			// Misma clave y mismo cuerpo: repetición, 200 y una sola fila.
			segunda := ent.entrega(ctx, t, id, claveEntrega, lat, lon, firmaJPEGDePrueba)
			folioDeRespuesta(t, segunda)

			// Misma clave y otra firma: la clave es la misma, el contenido no.
			otraFirma := append(append([]byte{}, firmaJPEGDePrueba...), 0x00)

			tercera := ent.entrega(ctx, t, id, claveEntrega, lat, lon, otraFirma)
			require.Equal(t, http.StatusConflict, tercera.Code, tercera.Body.String())
			require.Contains(t, tercera.Body.String(), domain.ErrClaveIdempotenciaMismatch.Code)

			evs, err := ent.eventos.ListarPorGarantia(ctx, uuidDe(id))
			require.NoError(t, err)
			require.Equal(t, 1, eventosConClave(evs, claveEntrega),
				"la repetición y el conflicto no escriben otra fila")

			// De los tres intentos, el que ganó fue el primero: sólo queda su
			// blob y las dos rutas del reintento y del conflicto se borraron.
			require.Len(t, blobsFirma(t, ent.storage), 1)
		} else {
			t.Log("reintento y huella no verificados: falta la tarea 10 (migración 000071)")
		}

		ent.jsonOK(ctx, t, "/garantias/"+id+"/cerrar", clave(), ent.cuerpo(map[string]any{
			"gps_lat": lat,
			"gps_lon": lon,
		}))

		final, err := ent.garantias.Obtener(ctx, uuidDe(id))
		require.NoError(t, err)
		require.Equal(t, domain.EstadoFolioCerrado, final.Estado())

		articulo := final.ArticulosForRepo()[0]
		require.Equal(t, domain.EtapaReingresadoInventario, articulo.Etapa())
		require.Equal(t, domain.UbicacionAlmacenRevision, articulo.Ubicacion())

		evs, err := ent.eventos.ListarPorGarantia(ctx, final.ID())
		require.NoError(t, err)
		require.NotEmpty(t, evs)
		require.Equal(t, domain.TipoEventoFolioCerrado, evs[len(evs)-1].Tipo())

		if !tarea10AplicadaHTTP(ctx, t, ent.pool) {
			t.Log("auditoría GPS e imagen de firma no verificadas: falta la tarea 10")
			return
		}

		cerrado := eventoFolio(t, evs, domain.TipoEventoFolioCerrado)
		require.NotNil(t, cerrado.AuditoriaGPS())
		require.Equal(t, domain.AuditoriaGPSNoAplica, *cerrado.AuditoriaGPS())
		require.Nil(t, cerrado.DistanciaEntregaM())

		rutas := imagenesFirmaDelFolio(ctx, t, ent.pool, id)
		require.Len(t, rutas, 1, "la firma queda colgada del folio_entregado")

		contenido, err := os.ReadFile(filepath.Join(ent.storage, filepath.FromSlash(rutas[0])))
		require.NoError(t, err)
		require.Equal(t, firmaJPEGDePrueba, contenido)
	})
}

// TestE2EHTTP_ClientePorProveedorConDesvio recorre la historia 2 del §8 por
// HTTP. La recolección deja su GPS, la entrega cae lejos y el cierre exige
// justificación: la auditoría queda con_desvio.
func TestE2EHTTP_ClientePorProveedorConDesvio(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		ent := nuevoEntornoE2EHTTP(t, pool)
		clave := func() string { return uuid.NewString() }

		abierta := ent.jsonOK(ctx, t, "/garantias", clave(), ent.cuerpo(map[string]any{
			"origen":          "cliente",
			"cliente_id":      982001,
			"venta_id":        982002,
			"estado_cuenta":   "liquidada",
			"description":     "Sala con resortes vencidos",
			"calle":           "Avenida Juárez",
			"numero_exterior": "125",
			"colonia":         "Centro",
			"localidad":       "Puebla",
			"ciudad":          "Puebla",
			"codigo_postal":   "72000",
			"gps_lat":         19.4326,
			"gps_lon":         -99.1332,
		}))
		id := abierta.ID

		agregada := ent.jsonOK(ctx, t, "/garantias/"+id+"/articulos", clave(), ent.cuerpo(map[string]any{
			"articulo_id": 982003,
			"clave":       "CLI-001",
			"description": "Sala de tres piezas",
		}))
		articuloID := agregada.Articulos[0].ID

		ent.jsonOK(ctx, t, "/garantias/"+id+"/iniciar-proceso", clave(), ent.cuerpo(nil))

		latRecoleccion, lonRecoleccion := 19.4326, -99.1332

		ent.avanzar(ctx, t, id, articuloID, domain.EtapaPendienteRecoleccion, clave(), nil, nil)
		ent.avanzar(ctx, t, id, articuloID, domain.EtapaRecolectado, clave(), &latRecoleccion, &lonRecoleccion)
		ent.avanzar(ctx, t, id, articuloID, domain.EtapaEnRevision, clave(), nil, nil)

		ent.jsonOK(ctx, t, "/garantias/"+id+"/articulos/"+articuloID+"/diagnostico", clave(), ent.cuerpo(map[string]any{
			"ruta":        "proveedor",
			"rol_decisor": "tecnica",
		}))

		ent.avanzar(ctx, t, id, articuloID, domain.EtapaEnviadoProveedor, clave(), nil, nil)
		ent.avanzar(ctx, t, id, articuloID, domain.EtapaDictamenRecibido, clave(), nil, nil)

		ent.jsonOK(ctx, t, "/garantias/"+id+"/articulos/"+articuloID+"/dictamen", clave(), ent.cuerpo(map[string]any{
			"dictamen":    "aceptada",
			"rol_decisor": "tecnica",
		}))

		ent.jsonOK(ctx, t, "/garantias/"+id+"/articulos/"+articuloID+"/mover", clave(), ent.cuerpo(map[string]any{
			"ubicacion": "almacen_revision",
		}))

		ent.avanzar(ctx, t, id, articuloID, domain.EtapaListoEntrega, clave(), nil, nil)
		ent.jsonOK(ctx, t, "/garantias/"+id+"/listo-entrega", clave(), ent.cuerpo(nil))

		// La entrega cae en Puebla, lejos del punto de recolección en CDMX.
		latEntrega, lonEntrega := 19.0414, -98.2063

		folioDeRespuesta(t, ent.entrega(ctx, t, id, clave(), latEntrega, lonEntrega, firmaJPEGDePrueba))

		const justificacion = "el cliente recogió en la sucursal de Puebla"

		ent.jsonOK(ctx, t, "/garantias/"+id+"/cerrar", clave(), ent.cuerpo(map[string]any{
			"justificacion_desvio": justificacion,
			"gps_lat":              latEntrega,
			"gps_lon":              lonEntrega,
		}))

		final, err := ent.garantias.Obtener(ctx, uuidDe(id))
		require.NoError(t, err)
		require.Equal(t, domain.EstadoFolioCerrado, final.Estado())

		articulo := final.ArticulosForRepo()[0]
		require.Equal(t, domain.EtapaEntregado, articulo.Etapa())
		require.Equal(t, domain.UbicacionEntregado, articulo.Ubicacion())
		require.NotNil(t, articulo.Desenlace())
		require.Equal(t, domain.DesenlaceReparado, *articulo.Desenlace())

		evs, err := ent.eventos.ListarPorGarantia(ctx, final.ID())
		require.NoError(t, err)

		// El punto de recolección viaja en el evento que marcó recolectado.
		var recolectado *domain.Evento

		for _, evento := range evs {
			if evento.Tipo() == domain.TipoEventoEtapaAvanzada && evento.EtapaHasta() != nil &&
				*evento.EtapaHasta() == domain.EtapaRecolectado {
				recolectado = evento
			}
		}

		require.NotNil(t, recolectado, "recolectado debe dejar su evento")
		require.NotNil(t, recolectado.GPSLat())
		require.InDelta(t, latRecoleccion, *recolectado.GPSLat(), 1e-9)

		if !tarea10AplicadaHTTP(ctx, t, ent.pool) {
			t.Log("auditoría GPS no verificada: falta la tarea 10")
			return
		}

		cerrado := eventoFolio(t, evs, domain.TipoEventoFolioCerrado)
		require.NotNil(t, cerrado.AuditoriaGPS())
		require.Equal(t, domain.AuditoriaGPSConDesvio, *cerrado.AuditoriaGPS())
		require.Equal(t, justificacion, cerrado.Description())
		require.NotNil(t, cerrado.DistanciaEntregaM())
		require.Greater(t, *cerrado.DistanciaEntregaM(), domain.UmbralDesvioMetros)
	})
}

// TestE2EHTTP_ClientePorTallerSinDesvio recorre la historia 3 del §8 por HTTP:
// taller, cambio físico, entrega del reemplazo a metros de la recolección
// (sin_desvio) y, ya cerrado, el desenlace del original en standby.
func TestE2EHTTP_ClientePorTallerSinDesvio(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		ent := nuevoEntornoE2EHTTP(t, pool)
		clave := func() string { return uuid.NewString() }

		abierta := ent.jsonOK(ctx, t, "/garantias", clave(), ent.cuerpo(map[string]any{
			"origen":          "cliente",
			"cliente_id":      983001,
			"venta_id":        983002,
			"estado_cuenta":   "liquidada",
			"description":     "Comedor con estructura rota",
			"calle":           "Calle Reforma",
			"numero_exterior": "321",
			"colonia":         "Centro",
			"localidad":       "Puebla",
			"ciudad":          "Puebla",
			"codigo_postal":   "72000",
			"gps_lat":         19.0414,
			"gps_lon":         -98.2063,
		}))
		id := abierta.ID

		agregada := ent.jsonOK(ctx, t, "/garantias/"+id+"/articulos", clave(), ent.cuerpo(map[string]any{
			"articulo_id": 983003,
			"clave":       "TALLER-001",
			"description": "Comedor de seis sillas",
		}))
		originalID := agregada.Articulos[0].ID

		ent.jsonOK(ctx, t, "/garantias/"+id+"/iniciar-proceso", clave(), ent.cuerpo(nil))

		latRecoleccion, lonRecoleccion := 19.0414, -98.2063

		ent.avanzar(ctx, t, id, originalID, domain.EtapaPendienteRecoleccion, clave(), nil, nil)
		ent.avanzar(ctx, t, id, originalID, domain.EtapaRecolectado, clave(), &latRecoleccion, &lonRecoleccion)
		ent.avanzar(ctx, t, id, originalID, domain.EtapaEnRevision, clave(), nil, nil)

		ent.jsonOK(ctx, t, "/garantias/"+id+"/articulos/"+originalID+"/diagnostico", clave(), ent.cuerpo(map[string]any{
			"ruta":        "taller",
			"rol_decisor": "tecnica",
		}))

		cambiada := ent.jsonOK(ctx, t, "/garantias/"+id+"/articulos/"+originalID+"/cambio-fisico", clave(), ent.cuerpo(map[string]any{
			"rol_decisor": "tecnica",
		}))

		var reemplazoID string

		for _, articulo := range cambiada.Articulos {
			if articulo.Rol == string(domain.RolArticuloReemplazo) {
				reemplazoID = articulo.ID
			}
		}

		require.NotEmpty(t, reemplazoID, "el cambio físico crea el reemplazo")

		ent.jsonOK(ctx, t, "/garantias/"+id+"/listo-entrega", clave(), ent.cuerpo(nil))

		// La entrega cae a metros de la recolección: sin desvío.
		latEntrega, lonEntrega := 19.0415, -98.2064

		folioDeRespuesta(t, ent.entrega(ctx, t, id, clave(), latEntrega, lonEntrega, firmaJPEGDePrueba))

		ent.jsonOK(ctx, t, "/garantias/"+id+"/cerrar", clave(), ent.cuerpo(map[string]any{
			"gps_lat": latEntrega,
			"gps_lon": lonEntrega,
		}))

		ent.jsonOK(ctx, t, "/garantias/"+id+"/articulos/"+originalID+"/desenlace", clave(), ent.cuerpo(map[string]any{
			"desenlace":   "segunda_mano",
			"rol_decisor": "tecnica",
		}))

		final, err := ent.garantias.Obtener(ctx, uuidDe(id))
		require.NoError(t, err)
		require.Equal(t, domain.EstadoFolioCerrado, final.Estado())

		for _, articulo := range final.ArticulosForRepo() {
			switch articulo.ID().String() {
			case originalID:
				require.Equal(t, domain.EtapaSegundaMano, articulo.Etapa())
				require.Equal(t, domain.UbicacionAlmacenSegundaMano, articulo.Ubicacion())
				require.NotNil(t, articulo.Desenlace())
				require.Equal(t, domain.DesenlaceSegundaMano, *articulo.Desenlace())
			case reemplazoID:
				require.Equal(t, domain.EtapaEntregado, articulo.Etapa())
				require.NotNil(t, articulo.Desenlace())
				require.Equal(t, domain.DesenlaceReemplazado, *articulo.Desenlace())
			}
		}

		evs, err := ent.eventos.ListarPorGarantia(ctx, final.ID())
		require.NoError(t, err)

		if !tarea10AplicadaHTTP(ctx, t, ent.pool) {
			t.Log("auditoría GPS no verificada: falta la tarea 10")
			return
		}

		cerrado := eventoFolio(t, evs, domain.TipoEventoFolioCerrado)
		require.NotNil(t, cerrado.AuditoriaGPS())
		require.Equal(t, domain.AuditoriaGPSSinDesvio, *cerrado.AuditoriaGPS())
		require.NotNil(t, cerrado.DistanciaEntregaM())
		require.LessOrEqual(t, *cerrado.DistanciaEntregaM(), domain.UmbralDesvioMetros)
	})
}

// uuidDe parses a UUID string the tests got from a response.
func uuidDe(s string) uuid.UUID {
	parsed, err := uuid.Parse(s)
	if err != nil {
		panic(err)
	}

	return parsed
}
