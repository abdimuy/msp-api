package garhttp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

func TestObtenerGarantia_DetalleIncluyeImagenesYAuditoria(t *testing.T) {
	t.Parallel()

	garantia, _ := fixtureHTTPGarantia()

	eventoID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	imagenID := uuid.MustParse("66666666-6666-6666-6666-666666666666")

	now := time.Date(2026, 10, 31, 18, 0, 0, 0, time.UTC)
	distancia := 128.75
	auditoria := domain.AuditoriaGPSConDesvio

	evento := domain.HydrateEvento(domain.HydrateEventoParams{
		ID:                eventoID,
		GarantiaID:        garantia.ID(),
		Tipo:              domain.TipoEventoFolioCerrado,
		Description:       "Cierre con desviación justificada",
		Usuario:           "ruben",
		CreatedAt:         now,
		DeviceCreatedAt:   now,
		ClaveIdempotencia: "77777777-7777-7777-7777-777777777777",
		DistanciaEntregaM: &distancia,
		AuditoriaGPS:      &auditoria,
	})

	imagen := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:          imagenID,
		EventoID:    eventoID,
		Tipo:        domain.TipoImagenEvidencia,
		Ruta:        "garantias/11111111/evidencia.png",
		Description: "Foto de entrega",
		SubidaPor:   "ruben",
		Huella:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		CreatedAt:   now,
	})

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	garantias := &fakeHTTPGarantiaRepo{
		garantia: garantia,
	}

	eventos := &fakeHTTPEventoRepo{
		eventos: []*domain.Evento{evento},
	}

	imagenes := &fakeHTTPImagenRepo{
		imagenes: []*domain.Imagen{imagen},
	}

	r := buildHTTPRouterConImagenes(
		identity,
		garantias,
		nil,
		eventos,
		imagenes,
		&fakeHTTPStorage{},
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/garantias/"+garantia.ID().String(),
		nil,
	)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	body := rec.Body.String()

	require.Contains(t, body, `"tipo":"folio_cerrado"`)
	require.Contains(t, body, `"distancia_entrega_m":128.75`)
	require.Contains(t, body, `"auditoria_gps":"con_desvio"`)

	require.Contains(t, body, `"id":"66666666-6666-6666-6666-666666666666"`)
	require.Contains(t, body, `"tipo":"evidencia"`)
	require.Contains(t, body, `"descripcion":"Foto de entrega"`)
	require.Contains(t, body, `"subida_por":"ruben"`)

	require.Contains(
		t,
		body,
		`"url":"/garantias/`+
			garantia.ID().String()+
			`/eventos/55555555-5555-5555-5555-555555555555`+
			`/imagenes/66666666-6666-6666-6666-666666666666"`,
	)

	require.NotContains(t, body, `"huella"`)

	require.Equal(t, 1, imagenes.listarCalls)
	require.Equal(t, []uuid.UUID{eventoID}, imagenes.ultimosEventos)
}

func TestObtenerImagen_DevuelveBytesYContentType(t *testing.T) {
	t.Parallel()

	garantia, evento := fixtureHTTPGarantia()

	imagenID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	ruta := "garantias/test/evidencia.png"
	contenido := []byte{
		0x89, 0x50, 0x4E, 0x47,
		0x0D, 0x0A, 0x1A, 0x0A,
		0x01, 0x02, 0x03,
	}

	imagen := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:          imagenID,
		EventoID:    evento.ID(),
		Tipo:        domain.TipoImagenEvidencia,
		Ruta:        ruta,
		Description: "Evidencia",
		SubidaPor:   "ruben",
		CreatedAt:   time.Now().UTC(),
	})

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	garantias := &fakeHTTPGarantiaRepo{
		garantia: garantia,
	}

	eventos := &fakeHTTPEventoRepo{
		eventos: []*domain.Evento{evento},
	}

	imagenes := &fakeHTTPImagenRepo{
		imagen: imagen,
	}

	storage := &fakeHTTPStorage{
		objetos: map[string][]byte{
			ruta: contenido,
		},
		tipos: map[string]string{
			ruta: "image/png",
		},
	}

	r := buildHTTPRouterConImagenes(
		identity,
		garantias,
		nil,
		eventos,
		imagenes,
		storage,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/garantias/"+garantia.ID().String()+
			"/eventos/"+evento.ID().String()+
			"/imagenes/"+imagenID.String(),
		nil,
	)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.Equal(t, contenido, rec.Body.Bytes())

	require.Equal(t, 1, imagenes.obtenerCalls)
	require.Equal(t, imagenID, imagenes.ultimoID)

	require.Equal(t, 1, storage.getCalls)
	require.Equal(t, ruta, storage.ultimaRuta)
}

func TestObtenerImagen_ImagenDeOtroEventoDa404(t *testing.T) {
	t.Parallel()

	garantia, evento := fixtureHTTPGarantia()

	imagenID := uuid.New()

	imagen := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:        imagenID,
		EventoID:  uuid.New(),
		Tipo:      domain.TipoImagenEvidencia,
		Ruta:      "garantias/test/ajena.jpg",
		SubidaPor: "ruben",
		CreatedAt: time.Now().UTC(),
	})

	identity := &fakeHTTPIdentity{
		usuario:   outbound.Usuario{ID: "1", Nombre: "Ruben"},
		permitido: true,
	}

	garantias := &fakeHTTPGarantiaRepo{
		garantia: garantia,
	}
	eventos := &fakeHTTPEventoRepo{
		eventos: []*domain.Evento{evento},
	}
	imagenes := &fakeHTTPImagenRepo{
		imagen: imagen,
	}
	storage := &fakeHTTPStorage{}

	r := buildHTTPRouterConImagenes(
		identity,
		garantias,
		nil,
		eventos,
		imagenes,
		storage,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/garantias/"+garantia.ID().String()+
			"/eventos/"+evento.ID().String()+
			"/imagenes/"+imagenID.String(),
		nil,
	)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "warranty_image_not_found")

	require.Equal(t, 1, imagenes.obtenerCalls)
	require.Zero(t, storage.getCalls)
}

func TestObtenerImagen_EventoDeOtraGarantiaDa404(t *testing.T) {
	t.Parallel()

	garantia, _ := fixtureHTTPGarantia()

	eventoID := uuid.New()
	otraGarantiaID := uuid.New()
	imagenID := uuid.New()

	eventoAjeno := domain.HydrateEvento(domain.HydrateEventoParams{
		ID:         eventoID,
		GarantiaID: otraGarantiaID,
	})

	ruta := "garantias/test/evento-ajeno.png"

	imagen := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:          imagenID,
		EventoID:    eventoID,
		Tipo:        domain.TipoImagenEvidencia,
		Ruta:        ruta,
		Description: "No debe descargarse",
		SubidaPor:   "ruben",
		CreatedAt:   time.Now().UTC(),
	})

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	garantias := &fakeHTTPGarantiaRepo{
		garantia: garantia,
	}
	eventos := &fakeHTTPEventoRepo{
		eventos: []*domain.Evento{eventoAjeno},
	}
	imagenes := &fakeHTTPImagenRepo{
		imagen: imagen,
	}
	storage := &fakeHTTPStorage{
		objetos: map[string][]byte{
			ruta: {0x89, 0x50, 0x4E, 0x47},
		},
		tipos: map[string]string{
			ruta: "image/png",
		},
	}

	r := buildHTTPRouterConImagenes(
		identity,
		garantias,
		nil,
		eventos,
		imagenes,
		storage,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/garantias/"+garantia.ID().String()+
			"/eventos/"+eventoID.String()+
			"/imagenes/"+imagenID.String(),
		nil,
	)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "warranty_image_not_found")

	// Debe detectar que el evento pertenece a otra garantía antes de
	// consultar siquiera la imagen o el storage.
	require.Zero(t, imagenes.obtenerCalls)
	require.Zero(t, storage.getCalls)
}
