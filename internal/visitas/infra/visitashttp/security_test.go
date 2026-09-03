// Security sweep for the visitas HTTP module: asserts that POST /v2/visitas
// enforces the authn/authz preamble (currentUserOrError → requirePerm) and
// that malformed input is rejected cleanly (4xx, never 500/panic) before
// ever reaching the app service. No Firebird is required: the router is
// built with a nil *app.Service — the handler never dereferences it because
// either authorization fails first, or the transport-layer parsing
// (uuid.Parse / time.Parse / domain validation) rejects the malformed input
// before RegistrarVisita is called.
//
// Mirrors internal/cobranza/infra/cobranzahttp/security_test.go's shape.
//
//nolint:misspell // visitas vocabulary is Spanish per project convention.
package visitashttp_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	visitasapp "github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/infra/visitashttp"
)

// secVisitasNoPermUser is an authenticated principal holding no permissions
// at all — proves the 403 path is authorization, not authentication.
func secVisitasNoPermUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-sec-visitas-sin-permisos",
		Email:       "monica.pineda@muebleriamsp.mx",
		Nombre:      "Monica Pineda",
		Permisos:    []string{},
	}
}

// secVisitasRouter mounts the visitas router with a nil service. cu == nil
// skips the planter middleware entirely, simulating an unauthenticated
// caller.
func secVisitasRouter(cu *auth.CurrentUser) http.Handler {
	r := chi.NewRouter()
	if cu != nil {
		r.Use(planter(*cu))
	}
	visitashttp.MountRouter(r, nil)
	return r
}

// secVisitasRealRouter mounts the visitas router with a REAL service backed
// by an in-memory repo.
//
// It exists because secVisitasRouter's nil *app.Service is only safe for
// requests that are rejected BEFORE the service is touched — i.e. by authn,
// authz, or transport-level parsing. Anything that gets past those reaches
// app.RegistrarVisita, which dereferences s.clock on its first line, and the
// process panics instead of returning a status. Domain-level rejections
// (an unknown tipo_visita, a missing cliente_id) are exactly that case, so
// they need a real service to be observable as a status code at all.
func secVisitasRealRouter(cu auth.CurrentUser) http.Handler {
	repo := newFakeVisitasRepo()
	svc := visitasapp.NewService(repo, &fakeClock{t: time.Now().UTC()})
	r := chi.NewRouter()
	r.Use(planter(cu))
	visitashttp.MountRouter(r, svc)
	return r
}

// secVisitasValidBody is a well-formed POST /v2/visitas body — used by the
// 401/403 subtests, where auth is expected to reject the request before the
// (nil) service is ever reached.
func secVisitasValidBody() []byte {
	visitaID := uuid.New()
	fecha := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	return []byte(`{` +
		`"id":"` + visitaID.String() + `",` +
		`"cliente_id":11486,` +
		`"cobrador_id":42,` +
		`"cobrador":"Ramírez García, Jorge",` +
		`"forma_cobro_id":87327,` +
		`"lat":19.432608,` +
		`"lng":-99.133209,` +
		`"nota":"cliente no estaba",` +
		`"tipo_visita":"No se encontraba",` +
		`"zona_cliente_id":7,` +
		`"fecha":"` + fecha + `"` +
		`}`)
}

// secVisitasRequest builds a request for /visitas with the given body and
// (optional) Idempotency-Key header matching body.id, so the idempotency
// cross-check never fires as a confound in the auth subtests.
func secVisitasRequest(method string, body []byte, idempotencyKey string) *http.Request {
	req := httptest.NewRequest(method, "/visitas", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return req
}

// TestSecurity_Visitas_NoAuth_Returns401 verifies POST /v2/visitas rejects an
// unauthenticated caller with 401 — the nil service is never dereferenced.
func TestSecurity_Visitas_NoAuth_Returns401(t *testing.T) {
	t.Parallel()

	req := secVisitasRequest(http.MethodPost, secVisitasValidBody(), "")
	rec := httptest.NewRecorder()

	secVisitasRouter(nil).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())
}

// TestSecurity_Visitas_MissingPermission_Returns403 verifies an authenticated
// caller lacking PermCobranzaVerPagos is rejected with 403 before the nil
// service is dereferenced.
func TestSecurity_Visitas_MissingPermission_Returns403(t *testing.T) {
	t.Parallel()

	req := secVisitasRequest(http.MethodPost, secVisitasValidBody(), "")
	rec := httptest.NewRecorder()

	cu := secVisitasNoPermUser()
	secVisitasRouter(&cu).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
}

// TestSecurity_Visitas_MalformedOrOversizedInput asserts a table of
// malformed/oversized/path-injection-flavored requests all resolve to a
// clean 4xx (never 500, never a panic) for an authenticated, fully-permitted
// caller.
//
// It runs against the REAL service router, not the nil-service one. It used
// to run against the nil service and pass, but only by accident: Huma treats
// every field of a typed Body as required unless the json tag says
// omitempty, so each of these bodies was rejected at the schema level for
// omitting cobrador_id / lat / lng, never for the flaw the case is named
// after. Now that the route reads its own body (both Content-Types share one
// handler), those flaws are caught where they belong — uuid.Parse,
// time.Parse and the domain bounds — which needs a service that exists.
func TestSecurity_Visitas_MalformedOrOversizedInput(t *testing.T) {
	t.Parallel()

	cu := visitaUser() // holds PermCobranzaVerPagos (defined in handlers_test.go)

	cases := []struct {
		name string
		body []byte
	}{
		{
			name: "junk (non-UUID) body id",
			body: []byte(`{"id":"'; DROP TABLE MSP_VISITAS; --","cliente_id":11486,"cobrador":"x","tipo_visita":"No se encontraba","fecha":"` + time.Now().UTC().Format(time.RFC3339) + `"}`),
		},
		{
			name: "path-traversal-flavored body id",
			body: []byte(`{"id":"../../../etc/passwd","cliente_id":11486,"cobrador":"x","tipo_visita":"No se encontraba","fecha":"` + time.Now().UTC().Format(time.RFC3339) + `"}`),
		},
		{
			name: "control characters embedded in cobrador",
			body: []byte(`{"id":"` + uuid.New().String() + `","cliente_id":11486,"cobrador":"Juan` + "\x01" + `Perez","tipo_visita":"No se encontraba","fecha":"` + time.Now().UTC().Format(time.RFC3339) + `"}`),
		},
		{
			name: "oversized cobrador field",
			body: []byte(`{"id":"` + uuid.New().String() + `","cliente_id":11486,"cobrador":"` + string(bytes.Repeat([]byte("a"), 5000)) + `","tipo_visita":"No se encontraba","fecha":"` + time.Now().UTC().Format(time.RFC3339) + `"}`),
		},
		{
			name: "malformed JSON (truncated body)",
			body: []byte(`{"id":"` + uuid.New().String() + `","cliente_id":114`),
		},
		{
			name: "empty body",
			body: []byte(``),
		},
		{
			name: "missing tipo_visita",
			body: []byte(`{"id":"` + uuid.New().String() + `","cliente_id":11486,"cobrador":"x","fecha":"` + time.Now().UTC().Format(time.RFC3339) + `"}`),
		},
		{
			name: "negative cliente_id",
			body: []byte(`{"id":"` + uuid.New().String() + `","cliente_id":-1,"cobrador":"x","tipo_visita":"No se encontraba","fecha":"` + time.Now().UTC().Format(time.RFC3339) + `"}`),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := secVisitasRequest(http.MethodPost, tc.body, "")
			rec := httptest.NewRecorder()

			secVisitasRealRouter(cu).ServeHTTP(rec, req)

			assert.GreaterOrEqual(t, rec.Code, 400, "body=%s", rec.Body.String())
			assert.Less(t, rec.Code, 500, "must not be a 5xx (no panic/internal error); body=%s", rec.Body.String())
		})
	}
}

// ─── GET /v2/visitas/{id} ───────────────────────────────────────────────────

// secVisitasTodosMenosVerPagos builds an authenticated principal holding
// EVERY permission the auth module knows EXCEPT cobranza:ver_pagos — the one
// the visitas routes actually require.
//
// This is stronger than the empty-permission principal above. A handler that
// checked the wrong permission code, or checked "holds at least one
// permission", would pass the empty-set test and fail this one. Because it is
// derived from authdomain.AllPermissions(), a permission added later is
// automatically included without touching this test.
func secVisitasTodosMenosVerPagos(t *testing.T) auth.CurrentUser {
	t.Helper()

	todas := authdomain.AllPermissions()
	require.NotEmpty(t, todas, "positive control: the permission catalog must not be empty")

	perms := make([]string, 0, len(todas))
	for _, p := range todas {
		if p.Code == authdomain.PermCobranzaVerPagos {
			continue
		}
		perms = append(perms, string(p.Code))
	}
	require.Len(t, perms, len(todas)-1, "exactly one permission must have been withheld")
	require.NotContains(t, perms, string(authdomain.PermCobranzaVerPagos))

	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-sec-visitas-todos-menos-ver-pagos",
		Email:       "raul.zavaleta@muebleriamsp.mx",
		Nombre:      "Raúl Zavaleta",
		Permisos:    perms,
	}
}

// secObtenerVisitaRequest builds a GET /visitas/{id}. The segment is
// percent-escaped so the request itself is always well-formed — an
// unparseable target would panic in httptest.NewRequest and never reach the
// server, which is the opposite of what this sweep is testing.
func secObtenerVisitaRequest(rawID string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "/visitas/"+url.PathEscape(rawID), nil)
}

// TestSecurity_ObtenerVisita_NoAuth_Returns401 — axis 1: no credentials.
func TestSecurity_ObtenerVisita_NoAuth_Returns401(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	secVisitasRouter(nil).ServeHTTP(rec, secObtenerVisitaRequest(uuid.New().String()))

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())
}

// TestSecurity_ObtenerVisita_MissingPermission_Returns403 — axis 2, in both
// its forms: a principal with no permissions at all, and a principal holding
// every permission except the one that matters.
func TestSecurity_ObtenerVisita_MissingPermission_Returns403(t *testing.T) {
	t.Parallel()

	todosMenos := secVisitasTodosMenosVerPagos(t)

	cases := []struct {
		name string
		cu   auth.CurrentUser
	}{
		{"no permissions at all", secVisitasNoPermUser()},
		{"every permission except cobranza:ver_pagos", todosMenos},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			secVisitasRouter(&tc.cu).ServeHTTP(rec, secObtenerVisitaRequest(uuid.New().String()))

			assert.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
		})
	}
}

// TestSecurity_CrearVisita_TodosLosPermisosMenosElQueImporta extends axis 2
// to the write route, which previously only had the empty-permission case.
func TestSecurity_CrearVisita_TodosLosPermisosMenosElQueImporta(t *testing.T) {
	t.Parallel()

	cu := secVisitasTodosMenosVerPagos(t)
	req := secVisitasRequest(http.MethodPost, secVisitasValidBody(), "")
	rec := httptest.NewRecorder()

	secVisitasRouter(&cu).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
}

// TestSecurity_ObtenerVisita_MalformedPathParam — axis 3: a malformed path
// param must resolve to a clean 4xx, never a 500 or a panic.
//
// The router carries a REAL service over an empty in-memory repo, because a
// well-formed-but-unknown UUID (the last case) legitimately reaches the
// service and must answer 404. A nil service would panic there instead of
// returning a status, hiding the very behaviour under test.
func TestSecurity_ObtenerVisita_MalformedPathParam(t *testing.T) {
	t.Parallel()

	cu := visitaUser() // holds PermCobranzaVerPagos, so authz cannot mask the result
	router := secVisitasRealRouter(cu)

	cases := []struct {
		name string
		raw  string
	}{
		{"not a uuid", "no-soy-un-uuid"},
		{"numeric", "123"},
		{"empty segment", " "},
		{"sql injection", "'; DROP TABLE MSP_VISITAS; --"},
		{"path traversal", "../../../etc/passwd"},
		{"uuid with trailing junk", uuid.New().String() + "junk"},
		{"uuid missing a character", uuid.New().String()[:35]},
		{"nul byte", "abc\x00def"},
		{"very long segment", strings.Repeat("a", 5000)},
		{"unicode", "María-García-ñ"},
		{"nil uuid is well-formed but must not 5xx", uuid.Nil.String()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, secObtenerVisitaRequest(tc.raw))

			assert.GreaterOrEqual(t, rec.Code, 400, "body=%s", rec.Body.String())
			assert.Less(t, rec.Code, 500, "must not be a 5xx (no panic/internal error); body=%s", rec.Body.String())
		})
	}
}

// TestSecurity_CrearVisita_TipoFueraDelCatalogoNoEs500 pins the new
// catalogue rejection into the sweep's third axis: an unknown tipo_visita is
// a clean 4xx, never a 500.
//
// Uses the real-service router: a domain rejection happens INSIDE the
// service, so a nil one would panic before any status was written.
func TestSecurity_CrearVisita_TipoFueraDelCatalogoNoEs500(t *testing.T) {
	t.Parallel()

	cu := visitaUser()

	for _, tipo := range []string{
		"cobro",
		"'; DROP TABLE MSP_VISITAS; --",
		strings.Repeat("a", 5000),
		"No se encontraba\x01",
	} {
		t.Run(tipo[:min(len(tipo), 32)], func(t *testing.T) {
			t.Parallel()

			visitaID := uuid.New()
			fecha := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
			body, err := json.Marshal(map[string]any{
				"id":              visitaID.String(),
				"cliente_id":      11486,
				"cobrador_id":     42,
				"cobrador":        "Ramírez García, Jorge",
				"forma_cobro_id":  87327,
				"lat":             19.432608,
				"lng":             -99.133209,
				"tipo_visita":     tipo,
				"zona_cliente_id": 7,
				"fecha":           fecha,
			})
			require.NoError(t, err)

			rec := httptest.NewRecorder()
			secVisitasRealRouter(cu).ServeHTTP(rec, secVisitasRequest(http.MethodPost, body, ""))

			assert.GreaterOrEqual(t, rec.Code, 400, "body=%s", rec.Body.String())
			assert.Less(t, rec.Code, 500, "an unknown tipo must never be a 5xx; body=%s", rec.Body.String())
		})
	}
}

// ─── GET /v2/visitas/by-ids ─────────────────────────────────────────────────

// TestSecurity_VisitasExistentesPorIDs_NoAuth_Returns401 — axis 1: no
// credentials. The nil service is never dereferenced.
func TestSecurity_VisitasExistentesPorIDs_NoAuth_Returns401(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	secVisitasRouter(nil).ServeHTTP(rec, byIDsRequest(uuid.New().String()))

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())
}

// TestSecurity_VisitasExistentesPorIDs_MissingPermission_Returns403 — axis
// 2, in both its forms: a principal with no permissions at all, and a
// principal holding every permission except the one that matters.
func TestSecurity_VisitasExistentesPorIDs_MissingPermission_Returns403(t *testing.T) {
	t.Parallel()

	todosMenos := secVisitasTodosMenosVerPagos(t)

	cases := []struct {
		name string
		cu   auth.CurrentUser
	}{
		{"no permissions at all", secVisitasNoPermUser()},
		{"every permission except cobranza:ver_pagos", todosMenos},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			secVisitasRouter(&tc.cu).ServeHTTP(rec, byIDsRequest(uuid.New().String()))

			assert.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
		})
	}
}

// TestSecurity_VisitasExistentesPorIDs_MalformedOrOversizedInput — axis 3:
// a table of malformed/oversized ids values, all of which must resolve to a
// clean 4xx (never 500, never a panic) before the (nil) service is ever
// dereferenced — every case here is rejected by parseIDsParam itself
// (ids_required / ids_too_many / ids_invalid), which runs strictly before
// h.svc.VisitasExistentesPorIDs is called.
func TestSecurity_VisitasExistentesPorIDs_MalformedOrOversizedInput(t *testing.T) {
	t.Parallel()

	cu := visitaUser() // holds PermCobranzaVerPagos (defined in handlers_test.go)

	cases := []struct {
		name string
		req  *http.Request
	}{
		{"no ids param at all", byIDsRequestNoParam()},
		{"empty ids param", byIDsRequest("")},
		{"sql-injection-flavored id", byIDsRequest("'; DROP TABLE MSP_VISITAS; --")},
		{"path-traversal-flavored id", byIDsRequest("../../../etc/passwd")},
		{"control characters embedded in id", byIDsRequest("abc\x01def")},
		{"nul byte", byIDsRequest("abc\x00def")},
		{"unicode", byIDsRequest("María-García-ñ")},
		{"one malformed id among 99 valid ones", byIDsRequest(strings.Join(append(nIDs(99), "no-soy-un-uuid"), ","))},
		{"absurdly long single segment (5000 chars)", byIDsRequest(strings.Repeat("a", 5000))},
		{"cap + 1 well-formed uuids", byIDsRequest(strings.Join(nIDs(101), ","))},
		{"cap + 1 garbage segments", byIDsRequest(strings.Repeat("x,", 600))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			secVisitasRouter(&cu).ServeHTTP(rec, tc.req)

			assert.GreaterOrEqual(t, rec.Code, 400, "body=%s", rec.Body.String())
			assert.Less(t, rec.Code, 500, "must not be a 5xx (no panic/internal error); body=%s", rec.Body.String())
		})
	}
}

// TestSecurity_VisitasExistentesPorIDs_UnknownIDs_Never500 proves that a
// well-formed but entirely unknown id set reaches the (real) service and
// answers 200 with an empty array — never a 500 — using the same
// real-service router pattern as
// TestSecurity_ObtenerVisita_MalformedPathParam, because a nil service
// would panic on a request that legitimately reaches it.
func TestSecurity_VisitasExistentesPorIDs_UnknownIDs_Never500(t *testing.T) {
	t.Parallel()

	cu := visitaUser()
	router := secVisitasRealRouter(cu)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, byIDsRequest(strings.Join(nIDs(10), ",")))

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	var got []string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Empty(t, got, "none of these ids were ever seeded")
}

// ─── POST /v2/visitas · multipart ───────────────────────────────────────────
//
// The route now accepts a second wire format, so the security sweep is
// repeated over it. A route that is locked down in JSON and wide open in
// multipart would be no better than one that was never locked down.

// secVisitasMultipart builds a minimal, well-formed multipart body: a `datos`
// field carrying a valid visita plus one JPEG part.
// The fecha is anchored to fixedNow, the instant the multipart fixture's
// clock reports, so the positive control below is not rejected as a
// future-dated visit.
func secVisitasMultipart(t *testing.T) ([]byte, string) {
	t.Helper()
	id := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
	return buildMultipart(t, visitaBodyJSON(id.String(), fecha),
		[]testImagen{jpeg(uuid.NewString(), "puerta")})
}

// secVisitasMultipartRequest wraps a multipart body in a POST /visitas.
func secVisitasMultipartRequest(body []byte, contentType string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	return req
}

// TestSecurity_Visitas_Multipart_NoAuth_Returns401 verifies an
// unauthenticated multipart upload is refused before the (nil) service is
// touched — and before a single byte reaches storage.
func TestSecurity_Visitas_Multipart_NoAuth_Returns401(t *testing.T) {
	t.Parallel()

	body, ct := secVisitasMultipart(t)
	rec := httptest.NewRecorder()

	secVisitasRouter(nil).ServeHTTP(rec, secVisitasMultipartRequest(body, ct))

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())
}

// TestSecurity_Visitas_Multipart_CadaPermisoMenosElCorrecto_Returns403 walks
// the whole permission catalogue and asserts that holding ANY of them —
// except cobranza:ver_pagos — is a 403 on the multipart route.
//
// The positive control is the last subtest: the same request with exactly
// cobranza:ver_pagos answers 201. Without it, a handler that returned 403
// unconditionally would pass every other row.
func TestSecurity_Visitas_Multipart_CadaPermisoMenosElCorrecto_Returns403(t *testing.T) {
	t.Parallel()

	otros := []auth.Permission{
		auth.PermUsuariosListar,
		auth.PermUsuariosVer,
		auth.PermUsuariosCrear,
		auth.PermRolesListar,
		auth.PermPermisosListar,
		auth.PermVentasListar,
		auth.PermVentasCrear,
		auth.PermVentasSubirImagenes,
		auth.PermFailedIntentsVer,
		auth.PermCobranzaVerSaldos,
		auth.PermCobranzaReconciliar,
	}

	for _, perm := range otros {
		t.Run(string(perm), func(t *testing.T) {
			t.Parallel()

			cu := secVisitasNoPermUser()
			cu.Permisos = []string{string(perm)}

			body, ct := secVisitasMultipart(t)
			rec := httptest.NewRecorder()
			secVisitasRouter(&cu).ServeHTTP(rec, secVisitasMultipartRequest(body, ct))

			assert.Equal(t, http.StatusForbidden, rec.Code,
				"holding only %s must not open the multipart route; body=%s", perm, rec.Body.String())
		})
	}

	t.Run("control positivo: cobranza:ver_pagos sí entra", func(t *testing.T) {
		t.Parallel()

		f := newMultipartFixture(t)
		body, ct := secVisitasMultipart(t)
		rec := postMultipart(t, f, body, ct, "")

		require.Equal(t, http.StatusCreated, rec.Code,
			"the sweep above only means something if the right permission DOES work; body=%s",
			rec.Body.String())
	})
}

// TestSecurity_Visitas_Multipart_MalformedNeverReturns500 asserts that no
// shape of broken multipart envelope produces a 5xx. On this route a 500
// reads to a cobrador as "the server lost my visit", so every one of these
// has to come back as a clean, actionable 4xx.
func TestSecurity_Visitas_Multipart_MalformedNeverReturns500(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
	valid := visitaBodyJSON(id.String(), fecha)

	cases := []struct {
		name        string
		body        []byte
		contentType string
	}{
		{
			name:        "boundary sin declarar",
			body:        []byte("--x\r\n\r\n--x--\r\n"),
			contentType: "multipart/form-data",
		},
		{
			name:        "boundary con caracteres de control",
			body:        []byte("--x\r\n\r\n--x--\r\n"),
			contentType: "multipart/form-data; boundary=\"a\x00b\"",
		},
		{
			name:        "cabecera de parte truncada",
			body:        []byte("--b1\r\nContent-Disposition: form-data; name=\"da"),
			contentType: "multipart/form-data; boundary=b1",
		},
		{
			name:        "parte sin terminador de cierre",
			body:        []byte("--b1\r\nContent-Disposition: form-data; name=\"datos\"\r\n\r\n" + valid + "\r\n"),
			contentType: "multipart/form-data; boundary=b1",
		},
		{
			name:        "datos con un JSON anidado hasta el absurdo",
			body:        []byte("--b1\r\nContent-Disposition: form-data; name=\"datos\"\r\n\r\n" + strings.Repeat("[", 5000) + "\r\n--b1--\r\n"),
			contentType: "multipart/form-data; boundary=b1",
		},
		{
			name:        "nombre de archivo con path traversal",
			body:        []byte("--b1\r\nContent-Disposition: form-data; name=\"imagen\"; filename=\"../../etc/passwd\"\r\nContent-Type: image/jpeg\r\n\r\nx\r\n--b1\r\nContent-Disposition: form-data; name=\"datos\"\r\n\r\n" + valid + "\r\n--b1--\r\n"),
			contentType: "multipart/form-data; boundary=b1",
		},
		{
			name:        "campo datos repetido",
			body:        []byte("--b1\r\nContent-Disposition: form-data; name=\"datos\"\r\n\r\n" + valid + "\r\n--b1\r\nContent-Disposition: form-data; name=\"datos\"\r\n\r\nbasura\r\n--b1--\r\n"),
			contentType: "multipart/form-data; boundary=b1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMultipartFixture(t)
			rec := postMultipart(t, f, tc.body, tc.contentType, "")

			assert.Less(t, rec.Code, 500,
				"must never be a 5xx (no panic, no internal error); code=%d body=%s",
				rec.Code, rec.Body.String())
		})
	}
}
