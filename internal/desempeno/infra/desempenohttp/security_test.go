package desempenohttp_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

var rutas = []string{
	"/desempeno/catalogos",
	"/desempeno/usuarios",
	"/desempeno/usuarios/abc123/jornada",
	"/desempeno/usuarios/abc123/actividad",
	"/desempeno/usuarios/abc123/cuentas",
}

func TestSecurity_SinUsuarioEs401(t *testing.T) {
	t.Parallel()
	for _, p := range rutas {
		rec := httptest.NewRecorder()
		router(t, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		assert.Equal(t, http.StatusUnauthorized, rec.Code, p)
	}
}

// Con los permisos vecinos pero sin desempeno:ver → 403 (control: cobranza,
// rutas y clientes no abren esta pantalla).
func TestSecurity_SinPermisoEs403(t *testing.T) {
	t.Parallel()
	cu := usuarioCon(auth.PermCobranzaVerPagos, auth.PermCobranzaVerSaldos, auth.PermRutasLeer, auth.PermClientesLeer)
	for _, p := range rutas {
		rec := httptest.NewRecorder()
		router(t, &cu).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		assert.Equal(t, http.StatusForbidden, rec.Code, p)
		assert.Contains(t, rec.Body.String(), string(auth.PermDesempenoVer), p)
	}
}

// Control positivo de las dos anteriores: con el permiso, las cinco rutas abren.
func TestSecurity_ConPermisoPasa(t *testing.T) {
	t.Parallel()
	cu := usuarioCon(auth.PermDesempenoVer)
	for _, p := range rutas {
		rec := httptest.NewRecorder()
		router(t, &cu).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		assert.Equal(t, http.StatusOK, rec.Code, "%s: %s", p, rec.Body.String())
	}
}

// codigoDe extrae "code=<código>" del cuerpo de error Huma (mismo formato que
// rutas: errors[0].message).
func codigoDe(t *testing.T, cuerpo []byte) string {
	t.Helper()
	var e struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(cuerpo, &e), string(cuerpo))
	require.NotEmpty(t, e.Errors, string(cuerpo))
	return e.Errors[0].Message
}

func TestErrores_CodigosDelContrato(t *testing.T) {
	t.Parallel()
	const (
		notFound  = "code=desempeno_usuario_not_found"
		sinZona   = "code=desempeno_usuario_sin_zona"
		sinInicio = "code=desempeno_usuario_sin_inicio_semana"
		invalido  = "code=desempeno_parametro_invalido"
	)
	cu := usuarioCon(auth.PermDesempenoVer)
	casos := []struct {
		path   string
		status int
		code   string
	}{
		// 404: usuario inexistente, también con basura en el uid.
		{"/desempeno/usuarios/nadie/jornada", http.StatusNotFound, notFound},
		{"/desempeno/usuarios/nadie/actividad", http.StatusNotFound, notFound},
		{"/desempeno/usuarios/nadie/cuentas", http.StatusNotFound, notFound},
		{"/desempeno/usuarios/1%20OR%201=1/cuentas", http.StatusNotFound, notFound},

		// 409: sin zona.
		{"/desempeno/usuarios/sinzona/jornada", http.StatusConflict, sinZona},
		{"/desempeno/usuarios/sinzona/actividad", http.StatusConflict, sinZona},
		{"/desempeno/usuarios/sinzona/cuentas", http.StatusConflict, sinZona},
		// 409: sin inicio de semana (la jornada solo exige zona; la actividad
		// lo exige cuando no se manda desde).
		{"/desempeno/usuarios/sininicio/actividad", http.StatusConflict, sinInicio},
		{"/desempeno/usuarios/sininicio/cuentas", http.StatusConflict, sinInicio},
		// 409: inicio de semana futuro (contrato v2.1: mismo código, nunca 422).
		{"/desempeno/usuarios/futuro/jornada", http.StatusConflict, sinInicio},
		{"/desempeno/usuarios/futuro/actividad", http.StatusConflict, sinInicio},
		{"/desempeno/usuarios/futuro/cuentas", http.StatusConflict, sinInicio},

		// 422: pausa_min fuera de 5–240 o no entero.
		{"/desempeno/usuarios?pausa_min=4", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios?pausa_min=241", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios?pausa_min=abc", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios/abc123/jornada?pausa_min=0", http.StatusUnprocessableEntity, invalido},
		// 422: horario incompleto, mal formado o invertido.
		{"/desempeno/usuarios?desde_hora=08:00", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios?hasta_hora=18:00", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios?desde_hora=18:00&hasta_hora=08:00", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios?desde_hora=08:00&hasta_hora=08:00", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios?desde_hora=8h&hasta_hora=18:00", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios/abc123/jornada?desde_hora=18:00&hasta_hora=08:00", http.StatusUnprocessableEntity, invalido},
		// 422: periodo, fecha.
		{"/desempeno/usuarios?periodo=mes", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios/abc123/jornada?fecha=14-10-2026", http.StatusUnprocessableEntity, invalido},
		// 422: rango > 92 días, invertido o mal formado.
		{"/desempeno/usuarios/abc123/actividad?desde=2026-01-01T00:00:00Z&hasta=2026-10-14T00:00:00Z", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios/abc123/actividad?desde=2026-10-14T00:00:00Z&hasta=2026-10-01T00:00:00Z", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios/abc123/actividad?desde=ayer", http.StatusUnprocessableEntity, invalido},
		// 422: limit fuera de 1–500.
		{"/desempeno/usuarios/abc123/actividad?limit=0", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios/abc123/actividad?limit=501", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios/abc123/actividad?limit=-1", http.StatusUnprocessableEntity, invalido},
		// 422: valores fuera de catálogo y cursor corrupto.
		{"/desempeno/usuarios/abc123/actividad?tipo=llamada", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios/abc123/actividad?forma_cobro=157", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios/abc123/actividad?orden=saldo", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios/abc123/actividad?dir=arriba", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios/abc123/actividad?con_fotos=si", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios/abc123/actividad?cursor=no-es-un-cursor", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios/abc123/cuentas?segmento=todos", http.StatusUnprocessableEntity, invalido},
		{"/desempeno/usuarios/abc123/cuentas?orden=hora", http.StatusUnprocessableEntity, invalido},
	}
	for _, c := range casos {
		rec := httptest.NewRecorder()
		router(t, &cu).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		assert.Equal(t, c.status, rec.Code, "%s: %s", c.path, rec.Body.String())
		assert.Equal(t, c.code, codigoDe(t, rec.Body.Bytes()), c.path)
	}
}

// Límites incluidos del contrato: 5 y 240, limit 1 y 500, rango de 92 días
// exactos. Control positivo de los 422 de arriba.
func TestErrores_LimitesValidos(t *testing.T) {
	t.Parallel()
	cu := usuarioCon(auth.PermDesempenoVer)
	for _, p := range []string{
		"/desempeno/usuarios?pausa_min=5",
		"/desempeno/usuarios?pausa_min=240",
		"/desempeno/usuarios?desde_hora=07:59&hasta_hora=08:00",
		"/desempeno/usuarios/abc123/actividad?limit=1",
		"/desempeno/usuarios/abc123/actividad?limit=500",
		"/desempeno/usuarios/abc123/actividad?desde=2026-07-14T00:00:00Z&hasta=2026-10-14T00:00:00Z",
		"/desempeno/usuarios/sininicio/jornada",
		"/desempeno/usuarios/sininicio/actividad?desde=2026-10-01T00:00:00Z",
	} {
		rec := httptest.NewRecorder()
		router(t, &cu).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		assert.Equal(t, http.StatusOK, rec.Code, "%s: %s", p, rec.Body.String())
	}
}

// Firestore caído → 503 desempeno_firestore_unavailable (Review Focus 4).
func TestErrores_FirestoreCaidoEs503(t *testing.T) {
	t.Parallel()
	cu := usuarioCon(auth.PermDesempenoVer)
	h := routerCon(&cu, nuevoServicio(t, fakeUsuarios{err: domain.ErrFirestoreNoDisponible}))
	for _, p := range rutas[1:] {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, p)
		assert.Equal(t, "code=desempeno_firestore_unavailable", codigoDe(t, rec.Body.Bytes()), p)
	}
	// Los catálogos no dependen de Firestore.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, rutas[0], nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

// Un error que no es apperror (Firebird caído) es 500 y NO filtra el texto
// interno (SQL, rutas, credenciales) al cliente.
func TestErrores_InternoNoFiltraDetalle(t *testing.T) {
	t.Parallel()
	cu := usuarioCon(auth.PermDesempenoVer)
	h := routerCon(&cu, nuevoServicio(t, fakeUsuarios{err: errors.New("op error: SELECT FIRST 1 * FROM MSP_USUARIOS password=secreto")}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/desempeno/usuarios", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "SELECT")
	assert.NotContains(t, rec.Body.String(), "secreto")
}
