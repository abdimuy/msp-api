//nolint:misspell // vocabulario español (comando, Comandos, firma) por convención del proyecto.
package garhttp_test

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// campoFormulario is one text field of the multipart delivery.
type campoFormulario struct {
	nombre string
	valor  string
}

// entregaMultipart describes one delivery request. The zero value is the valid
// baseline: the sample JPEG and both coordinates. A nil firma means the sample
// (an empty non-nil slice sends a zero-byte file); sinFirma drops the file part
// and omitir drops a form field.
type entregaMultipart struct {
	firma            []byte
	sinFirma         bool
	firmaContentType string
	campos           []campoFormulario
	omitir           []string
}

// ejemploEntregaCon builds the multipart request of entregar-garantia. Huma
// reads the file type from the part's headers, so CreatePart is used instead of
// CreateFormFile, which leaves the type at application/octet-stream.
func ejemploEntregaCon(t *testing.T, p entregaMultipart) ejemploOperacion {
	t.Helper()

	campos := map[string]string{
		"device_created_at": deviceCreatedAtEjemplo,
		"gps_lat":           "19.4326",
		"gps_lon":           "-99.1332",
	}
	orden := []string{"device_created_at", "gps_lat", "gps_lon"}

	for _, campo := range p.campos {
		campos[campo.nombre] = campo.valor
	}

	for _, nombre := range p.omitir {
		delete(campos, nombre)
	}

	contentTypeFirma := p.firmaContentType
	if contentTypeFirma == "" {
		contentTypeFirma = "image/jpeg"
	}

	firma := p.firma
	if firma == nil {
		firma = firmaJPEGDePrueba
	}

	var cuerpo bytes.Buffer

	writer := multipart.NewWriter(&cuerpo)
	for _, nombre := range orden {
		if valor, ok := campos[nombre]; ok {
			require.NoError(t, writer.WriteField(nombre, valor))
		}
	}

	if !p.sinFirma {
		parte, err := writer.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": {`form-data; name="firma"; filename="firma.jpg"`},
			"Content-Type":        {contentTypeFirma},
		})
		require.NoError(t, err)

		_, err = parte.Write(firma)
		require.NoError(t, err)
	}

	require.NoError(t, writer.Close())

	return ejemploOperacion{
		OperationID: "entregar-garantia",
		Method:      http.MethodPost,
		Path:        "/garantias/" + idGarantiaEjemplo + "/entregar",
		ContentType: writer.FormDataContentType(),
		Body:        cuerpo.Bytes(),
	}
}

// firmaEvento returns the folio_entregado event of the staged timeline.
func firmaEvento(t *testing.T, eventos []*domain.Evento) *domain.Evento {
	t.Helper()

	var encontrados []*domain.Evento

	for _, evento := range eventos {
		if evento.Tipo() == domain.TipoEventoFolioEntregado {
			encontrados = append(encontrados, evento)
		}
	}

	require.Len(t, encontrados, 1)

	return encontrados[0]
}

func TestEntregar_SinFirma_422(t *testing.T) {
	t.Parallel()

	fakes := nuevoRouterCompleto("entregar-garantia")
	ejemplo := ejemploEntregaCon(t, entregaMultipart{sinFirma: true})

	rec := enviarEjemplo(fakes.router, ejemplo, claveEjemplo)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), domain.ErrFirmaRequerida.Code)
	require.Zero(t, fakes.repositorio.guardados)
	require.Empty(t, fakes.storage.guardados)
	require.Empty(t, fakes.storage.borrados)
}

func TestEntregar_FirmaInvalida_422(t *testing.T) {
	t.Parallel()

	casos := map[string]struct {
		peticion entregaMultipart
		code     string
	}{
		"vacia": {
			// app treats zero bytes as no signature: the bytes never reach
			// guardarBlob, so the sentinel is the same as an absent part.
			peticion: entregaMultipart{firma: []byte{}},
			code:     domain.ErrFirmaRequerida.Code,
		},
		"muy_grande": {
			peticion: entregaMultipart{
				firma: append(append([]byte{}, firmaJPEGDePrueba...), make([]byte, 1<<20)...),
			},
			code: domain.ErrImagenMuyGrande.Code,
		},
		"no_es_imagen": {
			peticion: entregaMultipart{firma: []byte("texto que no es una imagen")},
			code:     domain.ErrImagenFormatoInvalido.Code,
		},
	}

	for nombre, caso := range casos {
		t.Run(nombre, func(t *testing.T) {
			t.Parallel()

			fakes := nuevoRouterCompleto("entregar-garantia")
			ejemplo := ejemploEntregaCon(t, caso.peticion)

			rec := enviarEjemplo(fakes.router, ejemplo, claveEjemplo)

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), caso.code)
			require.Zero(t, fakes.repositorio.guardados)
			require.Empty(t, fakes.storage.guardados)
		})
	}
}

func TestEntregar_GPSInvalido_422(t *testing.T) {
	t.Parallel()

	casos := map[string]struct {
		peticion entregaMultipart
		code     string
	}{
		"lat_no_numerica": {
			peticion: entregaMultipart{campos: []campoFormulario{{nombre: "gps_lat", valor: "norte"}}},
			code:     domain.ErrEventoGPSInvalido.Code,
		},
		"lat_fuera_de_rango": {
			peticion: entregaMultipart{campos: []campoFormulario{{nombre: "gps_lat", valor: "91"}}},
			code:     domain.ErrEventoGPSInvalido.Code,
		},
		"lon_fuera_de_rango": {
			peticion: entregaMultipart{campos: []campoFormulario{{nombre: "gps_lon", valor: "-181"}}},
			code:     domain.ErrEventoGPSInvalido.Code,
		},
		"sin_lon": {
			peticion: entregaMultipart{omitir: []string{"gps_lon"}},
			code:     domain.ErrEntregaGPSRequerido.Code,
		},
	}

	for nombre, caso := range casos {
		t.Run(nombre, func(t *testing.T) {
			t.Parallel()

			fakes := nuevoRouterCompleto("entregar-garantia")
			ejemplo := ejemploEntregaCon(t, caso.peticion)

			rec := enviarEjemplo(fakes.router, ejemplo, claveEjemplo)

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), caso.code)
			require.Zero(t, fakes.repositorio.guardados)
			require.Empty(t, fakes.storage.guardados)
		})
	}
}

func TestEntregar_Exito_GuardaBlobYFirma(t *testing.T) {
	t.Parallel()

	fakes := nuevoRouterCompleto("entregar-garantia")

	rec := enviarEjemplo(fakes.router, ejemploEntregaCon(t, entregaMultipart{}), claveEjemplo)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assertCuerpoGarantia(t, rec)

	require.Equal(t, 1, fakes.repositorio.guardados)
	require.Len(t, fakes.storage.guardados, 1)
	require.Empty(t, fakes.storage.borrados)

	require.Len(t, fakes.imagenes.registradas, 1)

	imagen := fakes.imagenes.registradas[0]
	require.Equal(t, domain.TipoImagenFirma, imagen.Tipo())
	require.Equal(t, domain.FirmaImagenID(claveEjemplo), imagen.ID())
	require.Equal(t, fakes.storage.guardados[0], imagen.Ruta())
	require.Equal(t, "Ruben", imagen.SubidaPor())
	require.NotEmpty(t, imagen.Huella())

	evento := firmaEvento(t, fakes.eventos.eventos)
	require.Equal(t, evento.ID(), imagen.EventoID())
	require.Equal(t, claveEjemplo, evento.ClaveIdempotencia())
	require.Equal(t, imagen.Huella(), evento.Huella())

	// El GPS del agente viaja en el evento de la entrega, cada coordenada en
	// su campo: intercambiarlas es un error silencioso que la auditoría de
	// Cerrar después no podría distinguir.
	require.NotNil(t, evento.GPSLat())
	require.InDelta(t, 19.4326, *evento.GPSLat(), 1e-9)
	require.NotNil(t, evento.GPSLon())
	require.InDelta(t, -99.1332, *evento.GPSLon(), 1e-9)
}

func TestEntregar_ReintentoMismaFirma_200SinBlobHuerfano(t *testing.T) {
	t.Parallel()

	fakes := nuevoRouterCompleto("entregar-garantia")
	ejemplo := ejemploEntregaCon(t, entregaMultipart{})

	primera := enviarEjemplo(fakes.router, ejemplo, claveEjemplo)
	require.Equal(t, http.StatusOK, primera.Code, primera.Body.String())

	segunda := enviarEjemplo(fakes.router, ejemplo, claveEjemplo)
	require.Equal(t, http.StatusOK, segunda.Code, segunda.Body.String())
	assertCuerpoGarantia(t, segunda)

	// Both attempts wrote a blob because app stores before opening the
	// transaction; only the second one is garbage, because the first one's row
	// is still the one registered.
	require.Len(t, fakes.storage.guardados, 2)
	require.Len(t, fakes.storage.borrados, 1)
	require.Equal(t, fakes.storage.guardados[1], fakes.storage.borrados[0])

	require.Equal(t, 1, fakes.repositorio.guardados)
	require.Len(t, fakes.imagenes.registradas, 1)
	require.Len(t, fakes.eventos.eventosConClave(claveEjemplo), 1)
}

func TestEntregar_ReintentoOtraFirma_409YBorrarElBlob(t *testing.T) {
	t.Parallel()

	fakes := nuevoRouterCompleto("entregar-garantia")

	primera := enviarEjemplo(fakes.router, ejemploEntregaCon(t, entregaMultipart{}), claveEjemplo)
	require.Equal(t, http.StatusOK, primera.Code, primera.Body.String())

	otraFirma := append(append([]byte{}, firmaJPEGDePrueba...), 0x00)
	segunda := enviarEjemplo(fakes.router, ejemploEntregaCon(t, entregaMultipart{firma: otraFirma}), claveEjemplo)

	require.Equal(t, http.StatusConflict, segunda.Code, segunda.Body.String())
	require.Contains(t, segunda.Body.String(), domain.ErrClaveIdempotenciaMismatch.Code)

	require.Len(t, fakes.storage.guardados, 2)
	require.Len(t, fakes.storage.borrados, 1)
	require.Equal(t, fakes.storage.guardados[1], fakes.storage.borrados[0])

	require.Equal(t, 1, fakes.repositorio.guardados)
	require.Len(t, fakes.imagenes.registradas, 1)
}

func TestEntregar_ErrorDeBase_BorraElBlob(t *testing.T) {
	t.Parallel()

	fakes := nuevoRouterCompleto("entregar-garantia")
	fakes.repositorio.errGuardar = errors.New("falló la escritura")

	rec := enviarEjemplo(fakes.router, ejemploEntregaCon(t, entregaMultipart{}), claveEjemplo)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	require.Len(t, fakes.storage.guardados, 1)
	require.Equal(t, fakes.storage.guardados, fakes.storage.borrados)
	require.Empty(t, fakes.imagenes.registradas)
}

func TestEntregar_PidePermisoCerrar(t *testing.T) {
	t.Parallel()

	fakes := nuevoRouterCompleto("entregar-garantia")

	rec := enviarEjemplo(fakes.router, ejemploEntregaCon(t, entregaMultipart{}), claveEjemplo)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, []domain.Permiso{domain.PermisoCerrar}, fakes.identity.permisos)
}
