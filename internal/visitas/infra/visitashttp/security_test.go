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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/auth"
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
		`"tipo_visita":"cobro",` +
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
// caller — proving Huma's schema validation and the handler's own parsing
// (uuid.Parse / time.Parse / domain bounds) reject bad input before ever
// reaching the (nil) service.
func TestSecurity_Visitas_MalformedOrOversizedInput(t *testing.T) {
	t.Parallel()

	cu := visitaUser() // holds PermCobranzaVerPagos (defined in handlers_test.go)

	cases := []struct {
		name string
		body []byte
	}{
		{
			name: "junk (non-UUID) body id",
			body: []byte(`{"id":"'; DROP TABLE MSP_VISITAS; --","cliente_id":11486,"cobrador":"x","tipo_visita":"cobro","fecha":"` + time.Now().UTC().Format(time.RFC3339) + `"}`),
		},
		{
			name: "path-traversal-flavored body id",
			body: []byte(`{"id":"../../../etc/passwd","cliente_id":11486,"cobrador":"x","tipo_visita":"cobro","fecha":"` + time.Now().UTC().Format(time.RFC3339) + `"}`),
		},
		{
			name: "control characters embedded in cobrador",
			body: []byte(`{"id":"` + uuid.New().String() + `","cliente_id":11486,"cobrador":"Juan` + "\x01" + `Perez","tipo_visita":"cobro","fecha":"` + time.Now().UTC().Format(time.RFC3339) + `"}`),
		},
		{
			name: "oversized cobrador field",
			body: []byte(`{"id":"` + uuid.New().String() + `","cliente_id":11486,"cobrador":"` + string(bytes.Repeat([]byte("a"), 5000)) + `","tipo_visita":"cobro","fecha":"` + time.Now().UTC().Format(time.RFC3339) + `"}`),
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
			body: []byte(`{"id":"` + uuid.New().String() + `","cliente_id":-1,"cobrador":"x","tipo_visita":"cobro","fecha":"` + time.Now().UTC().Format(time.RFC3339) + `"}`),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := secVisitasRequest(http.MethodPost, tc.body, "")
			rec := httptest.NewRecorder()

			secVisitasRouter(&cu).ServeHTTP(rec, req)

			assert.GreaterOrEqual(t, rec.Code, 400, "body=%s", rec.Body.String())
			assert.Less(t, rec.Code, 500, "must not be a 5xx (no panic/internal error); body=%s", rec.Body.String())
		})
	}
}
