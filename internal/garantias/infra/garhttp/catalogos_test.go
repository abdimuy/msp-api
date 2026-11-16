package garhttp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

type respuestaCatalogos struct {
	Etapas       []string `json:"etapas"`
	Ubicaciones  []string `json:"ubicaciones"`
	Estados      []string `json:"estados"`
	TiposEvento  []string `json:"tipos_evento"`
	Transiciones []struct {
		Origen     string   `json:"origen"`
		Etapa      string   `json:"etapa"`
		Siguientes []string `json:"siguientes"`
	} `json:"transiciones"`
}

func solicitarCatalogos(t *testing.T, identity *seguridadIdentity) *httptest.ResponseRecorder {
	t.Helper()

	router, _, _, _ := nuevoRouterSeguridad(identity)
	req := httptest.NewRequest(http.MethodGet, "/garantias/catalogos", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	return rec
}

func TestCatalogos_RespuestaCompleta(t *testing.T) {
	t.Parallel()
	identity := &seguridadIdentity{
		permitidos: map[domain.Permiso]bool{
			domain.PermisoLeer:       true,
			domain.PermisoCrear:      false,
			domain.PermisoActualizar: false,
			domain.PermisoAutorizar:  false,
			domain.PermisoCerrar:     false,
		},
	}

	rec := solicitarCatalogos(t, identity)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got respuestaCatalogos
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

	etapas := make([]string, 0, len(domain.Etapas()))
	for _, etapa := range domain.Etapas() {
		etapas = append(etapas, etapa.String())
	}
	require.Equal(t, etapas, got.Etapas)

	ubicaciones := make([]string, 0, len(domain.Ubicaciones()))
	for _, ubicacion := range domain.Ubicaciones() {
		ubicaciones = append(ubicaciones, ubicacion.String())
	}
	require.Equal(t, ubicaciones, got.Ubicaciones)

	require.Len(t, got.Estados, 6)
	for _, estado := range got.Estados {
		_, err := domain.ParseEstadoFolio(estado)
		require.NoError(t, err)
	}

	require.Len(t, got.TiposEvento, 17)
	for _, tipo := range got.TiposEvento {
		_, err := domain.ParseTipoEvento(tipo)
		require.NoError(t, err)
	}

	origenes := []domain.OrigenFolio{
		domain.OrigenFolioCliente,
		domain.OrigenFolioPiso,
	}

	require.Len(t, got.Transiciones, len(origenes)*len(domain.Etapas()))

	i := 0
	for _, origen := range origenes {
		for _, etapa := range domain.Etapas() {
			transicion := got.Transiciones[i]
			require.Equal(t, origen.String(), transicion.Origen)
			require.Equal(t, etapa.String(), transicion.Etapa)

			esperadas := make([]string, 0)
			for _, siguiente := range domain.SiguientesPorAvanzar(etapa, origen) {
				esperadas = append(esperadas, siguiente.String())
			}
			require.Equal(t, esperadas, transicion.Siguientes)
			i++
		}
	}
}

func TestCatalogos_SinUsuario_401(t *testing.T) {
	t.Parallel()
	identity := &seguridadIdentity{
		sinUsuario: true,
		permitidos: map[domain.Permiso]bool{},
	}

	rec := solicitarCatalogos(t, identity)
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	require.Empty(t, identity.consultados)
}

func TestCatalogos_SinPermisoLeer_403(t *testing.T) {
	t.Parallel()
	identity := &seguridadIdentity{
		permitidos: map[domain.Permiso]bool{
			domain.PermisoLeer:       false,
			domain.PermisoCrear:      false,
			domain.PermisoActualizar: true,
			domain.PermisoAutorizar:  false,
			domain.PermisoCerrar:     false,
		},
	}

	rec := solicitarCatalogos(t, identity)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Equal(t, []domain.Permiso{domain.PermisoLeer}, identity.consultados)
}
