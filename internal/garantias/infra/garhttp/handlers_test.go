package garhttp_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

func TestListarGarantias_CaminoFeliz_JSONCompleto(t *testing.T) {
	t.Parallel()

	garantia, _ := fixtureHTTPGarantia()

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	bandeja := &fakeHTTPBandejaRepo{
		pagina: outbound.Pagina[*domain.Garantia]{
			Items:           []*domain.Garantia{garantia},
			SiguienteCursor: "cursor-siguiente",
		},
	}

	r := buildHTTPRouter(identity, nil, bandeja, nil)

	req := httptest.NewRequest(http.MethodGet, "/garantias", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	esperado := `{
		"$schema": "https://example.com/v2/schemas/ListarGarantiasResponseBody.json",
		"items": [
			{
				"id": "11111111-1111-1111-1111-111111111111",
				"folio": "GA-000123",
				"origen": "cliente",
				"cliente_id": 25,
				"venta_id": 80,
				"estado_cuenta": "liquidada",
				"estado": "en_proceso",
				"description": "Garantía de sala",
				"vigencia_hasta": "2027-01-31",
				"calle": "Reforma",
				"numero_exterior": "123",
				"colonia": "Centro",
				"localidad": "Centro",
				"ciudad": "Ciudad de México",
				"codigo_postal": "06000",
				"gps_lat": 19.4326,
				"gps_lon": -99.1332,
				"abierto_por": "ruben",
				"created_at": "2026-10-13T15:00:00Z",
				"updated_at": "2026-10-13T15:30:00Z",
				"articulos": [
					{
						"id": "22222222-2222-2222-2222-222222222222",
						"garantia_id": "11111111-1111-1111-1111-111111111111",
						"rol": "original",
						"articulo_id": 150,
						"clave": "SALA-001",
						"description": "Respaldo dañado",
						"ruta": "taller",
						"etapa": "en_taller",
						"ubicacion": "taller",
						"created_at": "2026-10-13T15:00:00Z",
						"updated_at": "2026-10-13T15:30:00Z"
					}
				]
			}
		],
		"next_cursor": "cursor-siguiente",
		"has_more": true
	}`

	require.JSONEq(t, esperado, rec.Body.String())
	require.Equal(t, 1, bandeja.calls)
}

func TestObtenerGarantia_CaminoFeliz_JSONCompleto(t *testing.T) {
	t.Parallel()

	garantia, evento := fixtureHTTPGarantia()

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

	r := buildHTTPRouter(identity, garantias, nil, eventos)

	req := httptest.NewRequest(
		http.MethodGet,
		"/garantias/11111111-1111-1111-1111-111111111111",
		nil,
	)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	esperado := `{
	"$schema": "https://example.com/v2/schemas/ObtenerGarantiaResponseBody.json",
		"garantia": {
			"id": "11111111-1111-1111-1111-111111111111",
			"folio": "GA-000123",
			"origen": "cliente",
			"cliente_id": 25,
			"venta_id": 80,
			"estado_cuenta": "liquidada",
			"estado": "en_proceso",
			"description": "Garantía de sala",
			"vigencia_hasta": "2027-01-31",
			"calle": "Reforma",
			"numero_exterior": "123",
			"colonia": "Centro",
			"localidad": "Centro",
			"ciudad": "Ciudad de México",
			"codigo_postal": "06000",
			"gps_lat": 19.4326,
			"gps_lon": -99.1332,
			"abierto_por": "ruben",
			"created_at": "2026-10-13T15:00:00Z",
			"updated_at": "2026-10-13T15:30:00Z",
			"articulos": [
				{
					"id": "22222222-2222-2222-2222-222222222222",
					"garantia_id": "11111111-1111-1111-1111-111111111111",
					"rol": "original",
					"articulo_id": 150,
					"clave": "SALA-001",
					"description": "Respaldo dañado",
					"ruta": "taller",
					"etapa": "en_taller",
					"ubicacion": "taller",
					"created_at": "2026-10-13T15:00:00Z",
					"updated_at": "2026-10-13T15:30:00Z"
				}
			]
		},
		"eventos": [
			{
				"id": "33333333-3333-3333-3333-333333333333",
				"garantia_id": "11111111-1111-1111-1111-111111111111",
				"articulo_ref": "22222222-2222-2222-2222-222222222222",
				"tipo": "diagnostico_registrado",
				"description": "Se envía al taller",
				"etapa_desde": "en_revision",
				"etapa_hasta": "en_taller",
				"usuario": "ruben",
				"rol_decisor": "tecnica",
				"gps_lat": 19.4326,
				"gps_lon": -99.1332,
				"created_at": "2026-10-13T15:30:00Z",
				"device_created_at": "2026-10-13T15:30:00Z",
				"clave_idempotencia": "44444444-4444-4444-4444-444444444444"
			}
		]
	}`

	require.JSONEq(t, esperado, rec.Body.String())
	require.Equal(t, 1, garantias.calls)
	require.Zero(t, eventos.calls)
	require.Equal(t, garantia.ID(), garantias.ultimoID)
}

func TestListarGarantias_QueryParamsInvalidos_422(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nombre string
		param  string
		valor  string
		code   string
	}{
		{
			nombre: "limit",
			param:  "limit",
			valor:  "abc",
			code:   "invalid_limit",
		},
		{
			nombre: "limit_cero",
			param:  "limit",
			valor:  "0",
			code:   "invalid_limit",
		},
		{
			nombre: "estado",
			param:  "estado",
			valor:  "estado_invalido",
			code:   "invalid_estado",
		},
		{
			nombre: "origen",
			param:  "origen",
			valor:  "origen_invalido",
			code:   "invalid_origen",
		},
		{
			nombre: "cliente_id",
			param:  "cliente_id",
			valor:  "abc",
			code:   "invalid_cliente_id",
		},
		{
			nombre: "cliente_id_cero",
			param:  "cliente_id",
			valor:  "0",
			code:   "invalid_cliente_id",
		},
		{
			nombre: "cliente_id_fuera_de_int32",
			param:  "cliente_id",
			valor:  "2147483648",
			code:   "invalid_cliente_id",
		},
		{
			nombre: "etapa",
			param:  "etapa",
			valor:  "etapa_invalida",
			code:   "invalid_etapa",
		},
		{
			nombre: "ubicacion",
			param:  "ubicacion",
			valor:  "ubicacion_invalida",
			code:   "invalid_ubicacion",
		},
		{
			nombre: "desde",
			param:  "desde",
			valor:  "13-10-2026",
			code:   "invalid_desde",
		},
		{
			nombre: "hasta",
			param:  "hasta",
			valor:  "13-10-2026",
			code:   "invalid_hasta",
		},
	}

	for _, caso := range casos {
		caso := caso

		t.Run(caso.nombre, func(t *testing.T) {
			t.Parallel()

			identity := &fakeHTTPIdentity{
				usuario: outbound.Usuario{
					ID:     "usuario-1",
					Nombre: "Ruben",
				},
				permitido: true,
			}

			bandeja := &fakeHTTPBandejaRepo{}

			r := buildHTTPRouter(identity, nil, bandeja, nil)

			req := httptest.NewRequest(
				http.MethodGet,
				"/garantias?"+caso.param+"="+url.QueryEscape(caso.valor),
				nil,
			)
			rec := httptest.NewRecorder()

			r.ServeHTTP(rec, req)

			require.Equal(
				t,
				http.StatusUnprocessableEntity,
				rec.Code,
				rec.Body.String(),
			)

			if caso.code != "" {
				require.Contains(t, rec.Body.String(), caso.code)
			}

			require.Equal(t, 0, bandeja.calls)
		})
	}
}

func TestListarGarantias_CursorInvalido_422(t *testing.T) {
	t.Parallel()

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	bandeja := &fakeHTTPBandejaRepo{
		err: apperror.NewValidation(
			"warranty_cursor_invalid",
			"el cursor de garantías no es válido",
		),
	}

	r := buildHTTPRouter(identity, nil, bandeja, nil)

	req := httptest.NewRequest(
		http.MethodGet,
		"/garantias?cursor=cursor-invalido",
		nil,
	)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(
		t,
		http.StatusUnprocessableEntity,
		rec.Code,
		rec.Body.String(),
	)
	require.Equal(t, 1, bandeja.calls)
}

func TestObtenerGarantia_UUIDInvalido_422(t *testing.T) {
	t.Parallel()

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	garantias := &fakeHTTPGarantiaRepo{}
	eventos := &fakeHTTPEventoRepo{}

	r := buildHTTPRouter(identity, garantias, nil, eventos)

	req := httptest.NewRequest(
		http.MethodGet,
		"/garantias/no-es-un-uuid",
		nil,
	)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(
		t,
		http.StatusUnprocessableEntity,
		rec.Code,
		rec.Body.String(),
	)

	require.Contains(t, rec.Body.String(), "invalid_uuid")
	require.Contains(
		t,
		rec.Body.String(),
		"el identificador de la garantía no es un UUID válido",
	)

	require.Equal(t, 0, garantias.calls)
	require.Equal(t, 0, eventos.calls)
}

func TestGarHTTP_Seguridad_401_403(t *testing.T) {
	t.Parallel()

	const garantiaID = "11111111-1111-1111-1111-111111111111"

	casos := []struct {
		nombre   string
		ruta     string
		identity *fakeHTTPIdentity
		status   int
	}{
		{
			nombre: "bandeja_sin_usuario",
			ruta:   "/garantias?limit=20",
			identity: &fakeHTTPIdentity{
				usuarioErr: domain.ErrUsuarioNoAutenticado,
			},
			status: http.StatusUnauthorized,
		},
		{
			nombre: "bandeja_sin_permiso",
			ruta:   "/garantias?limit=20",
			identity: &fakeHTTPIdentity{
				usuario: outbound.Usuario{
					ID:     "usuario-1",
					Nombre: "Ruben",
				},
				permitido: false,
			},
			status: http.StatusForbidden,
		},
		{
			nombre: "detalle_sin_usuario",
			ruta:   "/garantias/" + garantiaID,
			identity: &fakeHTTPIdentity{
				usuarioErr: domain.ErrUsuarioNoAutenticado,
			},
			status: http.StatusUnauthorized,
		},
		{
			nombre: "detalle_sin_permiso",
			ruta:   "/garantias/" + garantiaID,
			identity: &fakeHTTPIdentity{
				usuario: outbound.Usuario{
					ID:     "usuario-1",
					Nombre: "Ruben",
				},
				permitido: false,
			},
			status: http.StatusForbidden,
		},
	}

	for _, caso := range casos {
		caso := caso

		t.Run(caso.nombre, func(t *testing.T) {
			t.Parallel()

			garantias := &fakeHTTPGarantiaRepo{}
			bandeja := &fakeHTTPBandejaRepo{}
			eventos := &fakeHTTPEventoRepo{}

			r := buildHTTPRouter(
				caso.identity,
				garantias,
				bandeja,
				eventos,
			)

			req := httptest.NewRequest(
				http.MethodGet,
				caso.ruta,
				nil,
			)
			rec := httptest.NewRecorder()

			r.ServeHTTP(rec, req)

			require.Equal(
				t,
				caso.status,
				rec.Code,
				rec.Body.String(),
			)

			require.Equal(t, 0, bandeja.calls)
			require.Equal(t, 0, garantias.calls)
			require.Equal(t, 0, eventos.calls)
		})
	}
}

func TestObtenerGarantia_NoEncontrada_404(t *testing.T) {
	t.Parallel()

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	garantias := &fakeHTTPGarantiaRepo{
		err: domain.ErrGarantiaNoEncontrada,
	}

	eventos := &fakeHTTPEventoRepo{}

	r := buildHTTPRouter(identity, garantias, nil, eventos)

	req := httptest.NewRequest(
		http.MethodGet,
		"/garantias/11111111-1111-1111-1111-111111111111",
		nil,
	)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(
		t,
		http.StatusNotFound,
		rec.Code,
		rec.Body.String(),
	)

	require.Equal(t, 1, garantias.calls)
	require.Equal(t, 0, eventos.calls)
}

func TestListarGarantias_ErrorInterno_500NoFiltraMensaje(t *testing.T) {
	t.Parallel()

	const mensajeInterno = "firebird: password=super-secreto tabla MSP_GA_GARANTIA"

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	bandeja := &fakeHTTPBandejaRepo{
		err: errors.New(mensajeInterno),
	}

	r := buildHTTPRouter(identity, nil, bandeja, nil)

	req := httptest.NewRequest(
		http.MethodGet,
		"/garantias",
		nil,
	)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(
		t,
		http.StatusInternalServerError,
		rec.Code,
		rec.Body.String(),
	)

	require.NotContains(t, rec.Body.String(), mensajeInterno)
	require.Contains(t, rec.Body.String(), "ocurrió un error interno")
	require.Equal(t, 1, bandeja.calls)
}

func TestOpenAPI_PathsRegistered(t *testing.T) {
	t.Parallel()

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	r := buildHTTPRouter(identity, nil, nil, nil)

	req := httptest.NewRequest(
		http.MethodGet,
		"/openapi.json",
		nil,
	)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(
		t,
		http.StatusOK,
		rec.Code,
		rec.Body.String(),
	)

	doc := rec.Body.String()

	for _, esperado := range []string{
		`"/garantias"`,
		`"/garantias/{id}"`,
		`"operationId":"listar-garantias"`,
		`"operationId":"obtener-garantia"`,
		`"bearerAuth"`,
		`"format":"uuid"`,
		`"format":"date-time"`,
		`"format":"date"`,
		`"401":`,
		`"403":`,
		`"404":`,
		`"422":`,
	} {
		require.Contains(t, doc, esperado)
	}
}

func TestListarGarantias_InyeccionSQLFiltros_422(t *testing.T) {
	t.Parallel()

	const inyeccion = "' OR 1=1 --"

	filtros := []string{
		"estado",
		"origen",
		"cliente_id",
		"etapa",
		"ubicacion",
		"desde",
		"hasta",
	}

	for _, filtro := range filtros {
		filtro := filtro

		t.Run(filtro, func(t *testing.T) {
			t.Parallel()

			identity := &fakeHTTPIdentity{
				usuario: outbound.Usuario{
					ID:     "usuario-1",
					Nombre: "Ruben",
				},
				permitido: true,
			}

			bandeja := &fakeHTTPBandejaRepo{}

			r := buildHTTPRouter(
				identity,
				nil,
				bandeja,
				nil,
			)

			req := httptest.NewRequest(
				http.MethodGet,
				"/garantias?"+filtro+"="+url.QueryEscape(inyeccion),
				nil,
			)
			rec := httptest.NewRecorder()

			r.ServeHTTP(rec, req)

			require.Equal(
				t,
				http.StatusUnprocessableEntity,
				rec.Code,
				rec.Body.String(),
			)

			require.Equal(
				t,
				0,
				bandeja.calls,
				"el filtro %s no debe llegar al repositorio",
				filtro,
			)
		})
	}
}

func TestListarGarantias_FiltrosValidos_LleganTipadosAlRepo(t *testing.T) {
	t.Parallel()

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	bandeja := &fakeHTTPBandejaRepo{
		pagina: outbound.Pagina[*domain.Garantia]{
			Items: []*domain.Garantia{},
		},
	}

	r := buildHTTPRouter(identity, nil, bandeja, nil)

	query := url.Values{}
	query.Set("cursor", "cursor-ok")
	query.Set("limit", "50")
	query.Set("estado", "en_proceso")
	query.Set("origen", "cliente")
	query.Set("cliente_id", "25")
	query.Set("etapa", "en_taller")
	query.Set("ubicacion", "taller")
	query.Set("desde", "2026-10-01T00:00:00-06:00")
	query.Set("hasta", "2026-11-01T00:00:00Z")

	req := httptest.NewRequest(
		http.MethodGet,
		"/garantias?"+query.Encode(),
		nil,
	)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, bandeja.calls)

	require.Equal(t, "cursor-ok", bandeja.paginacion.Cursor)
	require.Equal(t, 50, bandeja.paginacion.Limite)

	require.NotNil(t, bandeja.filtros.Estado)
	require.Equal(t, domain.EstadoFolioEnProceso, *bandeja.filtros.Estado)

	require.NotNil(t, bandeja.filtros.Origen)
	require.Equal(t, domain.OrigenFolioCliente, *bandeja.filtros.Origen)

	require.NotNil(t, bandeja.filtros.ClienteID)
	require.Equal(t, 25, *bandeja.filtros.ClienteID)

	require.NotNil(t, bandeja.filtros.Etapa)
	require.Equal(t, domain.EtapaEnTaller, *bandeja.filtros.Etapa)

	require.NotNil(t, bandeja.filtros.Ubicacion)
	require.Equal(t, domain.UbicacionTaller, *bandeja.filtros.Ubicacion)

	require.NotNil(t, bandeja.filtros.Desde)
	require.Equal(
		t,
		time.Date(2026, time.October, 1, 6, 0, 0, 0, time.UTC),
		*bandeja.filtros.Desde,
	)

	require.NotNil(t, bandeja.filtros.Hasta)
	require.Equal(
		t,
		time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC),
		*bandeja.filtros.Hasta,
	)
}

func TestListarGarantias_ProyectaCamposOpcionales(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	originalID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	reemplazoID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")

	now := time.Date(
		2026, time.October, 13,
		18, 0, 0, 0,
		time.UTC,
	)

	cerrado := time.Date(
		2026, time.October, 13,
		19, 0, 0, 0,
		time.UTC,
	)

	ruta := domain.RutaReparacionProveedor
	dictamen := domain.DictamenAceptada
	desenlace := domain.DesenlaceReemplazado

	original := domain.HydrateArticulo(domain.HydrateArticuloParams{
		ID:          originalID,
		GarantiaID:  garantiaID,
		Rol:         domain.RolArticuloOriginal,
		Clave:       "ORIGINAL-001",
		Description: "Artículo original",
		Ruta:        &ruta,
		Etapa:       domain.EtapaStandby,
		Ubicacion:   domain.UbicacionAlmacenRevision,
		Dictamen:    &dictamen,
		Desenlace:   &desenlace,
		CerradoEn:   &cerrado,
		CreatedAt:   now,
		UpdatedAt:   cerrado,
	})

	reemplazo := domain.HydrateArticulo(domain.HydrateArticuloParams{
		ID:          reemplazoID,
		GarantiaID:  garantiaID,
		Rol:         domain.RolArticuloReemplazo,
		ReemplazaA:  &originalID,
		Clave:       "REEMPLAZO-001",
		Description: "Artículo de reemplazo",
		Etapa:       domain.EtapaEntregado,
		Ubicacion:   domain.UbicacionEntregado,
		CreatedAt:   now,
		UpdatedAt:   cerrado,
	})

	garantia := domain.HydrateGarantia(domain.HydrateGarantiaParams{
		ID:          garantiaID,
		Folio:       domain.Folio("GA-000999"),
		Origen:      domain.OrigenFolioPiso,
		Estado:      domain.EstadoFolioCerrado,
		Description: "Garantía cerrada",
		AbiertoPor:  "ruben",
		CerradoEn:   &cerrado,
		CreatedAt:   now,
		UpdatedAt:   cerrado,
		Articulos: []*domain.Articulo{
			original,
			reemplazo,
		},
	})

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	bandeja := &fakeHTTPBandejaRepo{
		pagina: outbound.Pagina[*domain.Garantia]{
			Items: []*domain.Garantia{garantia},
		},
	}

	r := buildHTTPRouter(identity, nil, bandeja, nil)

	req := httptest.NewRequest(http.MethodGet, "/garantias", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	require.Contains(
		t,
		rec.Body.String(),
		`"reemplaza_a":"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"`,
	)
	require.Contains(t, rec.Body.String(), `"dictamen":"aceptada"`)
	require.Contains(t, rec.Body.String(), `"desenlace":"reemplazado"`)
	require.Contains(
		t,
		rec.Body.String(),
		`"cerrado_en":"2026-10-13T19:00:00Z"`,
	)

	require.NotContains(t, rec.Body.String(), `"calle"`)
}

func TestObtenerGarantia_SinEventos_DevuelveListaVacia(t *testing.T) {
	t.Parallel()

	garantia, _ := fixtureHTTPGarantia()

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
		eventos: nil,
	}

	r := buildHTTPRouter(identity, garantias, nil, eventos)

	req := httptest.NewRequest(
		http.MethodGet,
		"/garantias/11111111-1111-1111-1111-111111111111",
		nil,
	)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"eventos":[]`)
	require.NotContains(t, rec.Body.String(), `"eventos":null`)
}

func TestListarGarantias_DesdePosteriorAHasta_422(t *testing.T) {
	t.Parallel()

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	garantias := &fakeHTTPGarantiaRepo{}
	bandeja := &fakeHTTPBandejaRepo{}
	eventos := &fakeHTTPEventoRepo{}

	r := buildHTTPRouter(identity, garantias, bandeja, eventos)

	req := httptest.NewRequest(
		http.MethodGet,
		"/garantias?desde=2026-10-14T00:00:00Z&hasta=2026-10-13T00:00:00Z",
		nil,
	)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "invalid_date_range")
	require.Equal(t, 0, bandeja.calls)
}

func TestListarGarantias_QueryParamRepetido_422(t *testing.T) {
	t.Parallel()

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	garantias := &fakeHTTPGarantiaRepo{}
	bandeja := &fakeHTTPBandejaRepo{}
	eventos := &fakeHTTPEventoRepo{}

	r := buildHTTPRouter(identity, garantias, bandeja, eventos)

	req := httptest.NewRequest(
		http.MethodGet,
		"/garantias?estado=abierto&estado=cerrado",
		nil,
	)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Equal(t, 0, bandeja.calls)
	require.Contains(t, rec.Body.String(), "estado")
	require.Contains(t, rec.Body.String(), "duplicate_query_param")
}

func TestListarGarantias_ServiceUnavailable_503(t *testing.T) {
	t.Parallel()

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	bandeja := &fakeHTTPBandejaRepo{
		err: apperror.NewServiceUnavailable(
			"warranty_service_unavailable",
			"el servicio de garantías no está disponible",
		),
	}

	r := buildHTTPRouter(
		identity,
		&fakeHTTPGarantiaRepo{},
		bandeja,
		&fakeHTTPEventoRepo{},
	)

	req := httptest.NewRequest(http.MethodGet, "/garantias", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "warranty_service_unavailable")
}

func TestListarGarantias_TooManyRequests_429(t *testing.T) {
	t.Parallel()

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	bandeja := &fakeHTTPBandejaRepo{
		err: apperror.NewTooManyRequests(
			"warranty_rate_limited",
			"demasiadas solicitudes",
		),
	}

	r := buildHTTPRouter(
		identity,
		&fakeHTTPGarantiaRepo{},
		bandeja,
		&fakeHTTPEventoRepo{},
	)

	req := httptest.NewRequest(http.MethodGet, "/garantias", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "warranty_rate_limited")
}

// KindPreconditionFailed llegó con la integración de main (versión de ventas).
// Si se quita del grupo de errores de cliente en mapAppError, este caso cae al
// default y responde 500: la prueba se pone roja.
func TestListarGarantias_PreconditionFailed_412(t *testing.T) {
	t.Parallel()

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	bandeja := &fakeHTTPBandejaRepo{
		err: apperror.NewPreconditionFailed(
			"warranty_precondition_failed",
			"la condición previa no se cumple",
		),
	}

	r := buildHTTPRouter(
		identity,
		&fakeHTTPGarantiaRepo{},
		bandeja,
		&fakeHTTPEventoRepo{},
	)

	req := httptest.NewRequest(http.MethodGet, "/garantias", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusPreconditionFailed, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "warranty_precondition_failed")
}

func TestListarGarantias_SinUsuarioQueryInvalida_401(t *testing.T) {
	t.Parallel()

	identity := &fakeHTTPIdentity{
		usuarioErr: domain.ErrUsuarioNoAutenticado,
	}

	bandeja := &fakeHTTPBandejaRepo{}

	r := buildHTTPRouter(identity, nil, bandeja, nil)

	req := httptest.NewRequest(
		http.MethodGet,
		"/garantias?estado=estado_invalido",
		nil,
	)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	require.Zero(t, bandeja.calls)
}
