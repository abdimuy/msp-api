//nolint:misspell // vocabulario español (comando, Comandos) por convención del proyecto.
package garhttp_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
)

// ejemploOperacion is one syntactically valid request for an operation. The
// security sweep and the per-route tests reuse it, so a required field or a
// path shape can only be fixed in one place.
type ejemploOperacion struct {
	OperationID string
	Method      string
	Path        string
	ContentType string
	Body        []byte
}

// identificadores de ejemplo: válidos y estables para que la misma petición
// sirva en todas las pruebas.
const (
	idGarantiaEjemplo = "11111111-1111-1111-1111-111111111111"
	idArticuloEjemplo = "22222222-2222-2222-2222-222222222222"
	claveEjemplo      = "44444444-4444-4444-4444-444444444444"
)

// deviceCreatedAtEjemplo es el instante RFC3339 que llevan todos los cuerpos.
const deviceCreatedAtEjemplo = "2026-10-13T15:00:00Z"

// firmaJPEGDePrueba es un JPEG mínimo (sólo cabecera SOI/APP0) para la
// petición multipart de entrega. La valida el handler de la parte 2.
var firmaJPEGDePrueba = []byte{
	0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10,
	0x4A, 0x46, 0x49, 0x46, 0x00, 0x01,
	0x01, 0x00, 0x00, 0x01, 0x00, 0x01,
	0x00, 0x00, 0xFF, 0xD9,
}

// ejemplosComandos returns one valid request per command operation: the
// thirteen JSON routes of this task plus the multipart delivery. The multipart
// one belongs to Ruben's part; it lives here because the shared contract asks
// for all fourteen in this file.
func ejemplosComandos() []ejemploOperacion {
	json := func(operationID, path, body string) ejemploOperacion {
		return ejemploOperacion{
			OperationID: operationID,
			Method:      http.MethodPost,
			Path:        path,
			ContentType: "application/json",
			Body:        []byte(body),
		}
	}

	articulo := "/garantias/" + idGarantiaEjemplo + "/articulos/" + idArticuloEjemplo

	return []ejemploOperacion{
		json("abrir-garantia", "/garantias", `{
			"origen": "cliente",
			"cliente_id": 25,
			"venta_id": 80,
			"estado_cuenta": "liquidada",
			"description": "Garantía de sala",
			"vigencia_hasta": "2027-01-31T00:00:00Z",
			"calle": "Reforma",
			"numero_exterior": "123",
			"colonia": "Centro",
			"localidad": "Centro",
			"ciudad": "Ciudad de México",
			"codigo_postal": "06000",
			"gps_lat": 19.4326,
			"gps_lon": -99.1332,
			"device_created_at": "`+deviceCreatedAtEjemplo+`"
		}`),

		json("iniciar-proceso", "/garantias/"+idGarantiaEjemplo+"/iniciar-proceso", `{
			"gps_lat": 19.4326,
			"gps_lon": -99.1332,
			"device_created_at": "`+deviceCreatedAtEjemplo+`"
		}`),

		json("marcar-listo-entrega", "/garantias/"+idGarantiaEjemplo+"/listo-entrega", `{
			"gps_lat": 19.4326,
			"gps_lon": -99.1332,
			"device_created_at": "`+deviceCreatedAtEjemplo+`"
		}`),

		json("cerrar-garantia", "/garantias/"+idGarantiaEjemplo+"/cerrar", `{
			"justificacion_desvio": "el cliente cambió de domicilio",
			"gps_lat": 19.4326,
			"gps_lon": -99.1332,
			"device_created_at": "`+deviceCreatedAtEjemplo+`"
		}`),

		json("cancelar-garantia", "/garantias/"+idGarantiaEjemplo+"/cancelar", `{
			"motivo": "el cliente desistió",
			"gps_lat": 19.4326,
			"gps_lon": -99.1332,
			"device_created_at": "`+deviceCreatedAtEjemplo+`"
		}`),

		json("agregar-articulo", "/garantias/"+idGarantiaEjemplo+"/articulos", `{
			"articulo_id": 151,
			"clave": "SALA-002",
			"description": "Otra pieza de la sala",
			"gps_lat": 19.4326,
			"gps_lon": -99.1332,
			"device_created_at": "`+deviceCreatedAtEjemplo+`"
		}`),

		json("avanzar-articulo", articulo+"/avanzar", `{
			"etapa_destino": "reparado_taller",
			"gps_lat": 19.4326,
			"gps_lon": -99.1332,
			"device_created_at": "`+deviceCreatedAtEjemplo+`"
		}`),

		json("registrar-diagnostico", articulo+"/diagnostico", `{
			"ruta": "taller",
			"rol_decisor": "tecnica",
			"gps_lat": 19.4326,
			"gps_lon": -99.1332,
			"device_created_at": "`+deviceCreatedAtEjemplo+`"
		}`),

		json("registrar-dictamen", articulo+"/dictamen", `{
			"dictamen": "aceptada",
			"gps_lat": 19.4326,
			"gps_lon": -99.1332,
			"device_created_at": "`+deviceCreatedAtEjemplo+`"
		}`),

		json("mover-articulo", articulo+"/mover", `{
			"ubicacion": "en_transito",
			"gps_lat": 19.4326,
			"gps_lon": -99.1332,
			"device_created_at": "`+deviceCreatedAtEjemplo+`"
		}`),

		json("autorizar-cambio-fisico", articulo+"/cambio-fisico", `{
			"rol_decisor": "tecnica",
			"gps_lat": 19.4326,
			"gps_lon": -99.1332,
			"device_created_at": "`+deviceCreatedAtEjemplo+`"
		}`),

		json("marcar-no-devuelto", articulo+"/no-devuelto", `{
			"rol_decisor": "tecnica",
			"gps_lat": 19.4326,
			"gps_lon": -99.1332,
			"device_created_at": "`+deviceCreatedAtEjemplo+`"
		}`),

		json("registrar-desenlace", articulo+"/desenlace", `{
			"desenlace": "segunda_mano",
			"rol_decisor": "tecnica",
			"gps_lat": 19.4326,
			"gps_lon": -99.1332,
			"device_created_at": "`+deviceCreatedAtEjemplo+`"
		}`),

		ejemploEntregaMultipart(),
	}
}

// ejemploEntregaMultipart builds the multipart request of entregar-garantia
// with the signature file and the three fields the handler reads.
func ejemploEntregaMultipart() ejemploOperacion {
	var cuerpo bytes.Buffer
	writer := multipart.NewWriter(&cuerpo)

	_ = writer.WriteField("device_created_at", deviceCreatedAtEjemplo)
	_ = writer.WriteField("gps_lat", "19.4326")
	_ = writer.WriteField("gps_lon", "-99.1332")

	parte, err := writer.CreateFormFile("firma", "firma.jpg")
	if err != nil {
		panic(err)
	}

	_, _ = parte.Write(firmaJPEGDePrueba)
	_ = writer.Close()

	return ejemploOperacion{
		OperationID: "entregar-garantia",
		Method:      http.MethodPost,
		Path:        "/garantias/" + idGarantiaEjemplo + "/entregar",
		ContentType: writer.FormDataContentType(),
		Body:        cuerpo.Bytes(),
	}
}

// ejemplosJSONComandos are the thirteen JSON operations registered in this
// part; the multipart delivery is registered by the next one.
func ejemplosJSONComandos() []ejemploOperacion {
	out := make([]ejemploOperacion, 0, len(ejemplosComandos()))
	for _, ejemplo := range ejemplosComandos() {
		if ejemplo.ContentType == "application/json" {
			out = append(out, ejemplo)
		}
	}
	return out
}
