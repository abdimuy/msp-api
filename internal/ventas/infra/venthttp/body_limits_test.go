//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

// body_limits_test.go — DoS-shape / body-limit tests for the ventas HTTP layer.
//
// Design notes:
//
//   - The production BodyLimit middleware (middleware.BodyLimit) is NOT wired
//     inside buildRouter — it lives in provideHTTPServer. Tests that exercise
//     the 413 path apply the middleware themselves via bodyLimitRouter so they
//     mirror production topology.
//
//   - Every test asserts rec.Code != 500 as a hard floor. If a case produces
//     500 right now, the test is marked t.Skip with a BUG comment.
//
//   - Configured limit: HTTP_MAX_BODY_SIZE_MB=10 (default), so the middleware
//     cap is 10 * 1024 * 1024 = 10_485_760 bytes.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/middleware"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// bodyLimitMB is the default production body-limit. Mirror the envDefault in
// internal/platform/config: HTTP_MAX_BODY_SIZE_MB=10.
const bodyLimitMB = 10

// bodyLimitBytes is the byte cap enforced by the BodyLimit middleware.
const bodyLimitBytes = int64(bodyLimitMB) * 1024 * 1024

// buildRouterWithBodyLimit wraps buildRouter with the production BodyLimit
// middleware so oversized-body tests see the same rejection path as production.
func buildRouterWithBodyLimit(t *testing.T) http.Handler {
	t.Helper()
	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	inner := buildRouter(t, svc, cu)
	return middleware.BodyLimit(bodyLimitBytes)(inner)
}

// assertNotInternalServerError is the hard floor every body-limit test must
// pass. A 500 means the handler panicked or returned an unrecovered error on
// a DoS-shaped input — always a real bug.
func assertNotInternalServerError(t *testing.T, rec *httptest.ResponseRecorder, label string) {
	t.Helper()
	if rec.Code == http.StatusInternalServerError {
		t.Errorf("BUG: %s returned 500 Internal Server Error — "+
			"handler must never 5xx on a malformed/oversized payload. body=%s",
			label, rec.Body.String())
	}
}

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestVentas_BodyLimits_OversizedJSONIsRejected verifies that a POST /ventas
// body just above the 10 MB BodyLimit is rejected with 413 or 400 (not 500).
// The middleware wraps r.Body with MaxBytesReader; Huma sees the read error
// when it tries to decode and must surface a 4xx.
func TestVentas_BodyLimits_OversizedJSONIsRejected(t *testing.T) {
	t.Parallel()

	r := buildRouterWithBodyLimit(t)

	// Build a body slightly larger than 10 MB: start with a valid skeleton and
	// inflate the nota field well past the limit.
	overLimit := bodyLimitBytes + 1024 // 1 KB over
	// The JSON envelope itself is small; fill nota with enough 'A's to push
	// the total body over the limit.
	notaPadding := strings.Repeat("A", int(overLimit))
	body := validCreateBody()
	body.Nota = &notaPadding

	b, err := json.Marshal(body)
	require.NoError(t, err)
	// Sanity: confirm we actually exceeded the limit.
	require.Greater(t, int64(len(b)), bodyLimitBytes, "test body must exceed the limit to be meaningful")

	req := httptest.NewRequest(http.MethodPost, "/ventas", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertNotInternalServerError(t, rec, "oversized JSON body")
	assert.GreaterOrEqual(t, rec.Code, 400, "oversized body must be rejected with 4xx, got %d", rec.Code)
	assert.Less(t, rec.Code, 500, "oversized body must not produce 5xx, got %d body=%s", rec.Code, rec.Body.String())
	// The middleware emits 413 when MaxBytesReader fires before the handler
	// reads the body; Huma may also surface 400/422 on parse failure. Both
	// are acceptable; 500 is not.
	t.Logf("oversized body → HTTP %d (expected 413 or 400)", rec.Code)
}

// TestVentas_BodyLimits_DeeplyNestedJSON verifies that a POST /ventas with
// deeply nested JSON (10 000 levels of `{"id":...}` nesting) is rejected with
// a 4xx, not a 5xx. A stack-overflow inside the JSON parser would produce a
// panic → 500 or a goroutine crash, both unacceptable.
func TestVentas_BodyLimits_DeeplyNestedJSON(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	const depth = 10_000
	// Build {"id":{"id":{"id":...}}} to depth levels.
	// strings.Builder.WriteString never returns an error (the signature is for
	// io.Writer compatibility); suppress the linter warning with a blank
	// assignment rather than a noisy _ = ... on every call.
	var sb strings.Builder
	open := strings.Repeat(`{"id":`, depth)
	_, _ = sb.WriteString(open)
	_, _ = sb.WriteString(`"` + uuid.NewString() + `"`)
	closeBraces := strings.Repeat(`}`, depth)
	_, _ = sb.WriteString(closeBraces)
	deeplyNested := sb.String()

	req := httptest.NewRequest(http.MethodPost, "/ventas", bytes.NewReader([]byte(deeplyNested)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertNotInternalServerError(t, rec, "deeply nested JSON")
	assert.GreaterOrEqual(t, rec.Code, 400, "deeply nested JSON must be 4xx, got %d", rec.Code)
	assert.Less(t, rec.Code, 500, "deeply nested JSON must NOT be 5xx, got %d body=%s", rec.Code, rec.Body.String())
}

// TestVentas_BodyLimits_HugeProductosList verifies that a POST /ventas with
// 10 000 minimal productos is rejected cleanly (≤ 422 or 413), not 500.
// Either the BodyLimit middleware fires, Huma's per-field validation triggers,
// or the app layer returns a validation error — none of those paths should
// produce an unhandled 500.
func TestVentas_BodyLimits_HugeProductosList(t *testing.T) {
	t.Parallel()

	r := buildRouterWithBodyLimit(t)

	body := validCreateBody()
	body.Productos = make([]venthttp.ProductoDTO, 0, 10_000)
	for i := range 10_000 {
		body.Productos = append(body.Productos, venthttp.ProductoDTO{
			ID:               uuid.NewString(),
			ArticuloID:       i + 1,
			Articulo:         "Artículo",
			Cantidad:         "1",
			PrecioAnual:      "100",
			PrecioCorto:      "90",
			PrecioContado:    "80",
			AlmacenOrigenID:  intPtr(1),
			AlmacenDestinoID: intPtr(2),
		})
	}

	req := jsonRequest(t, http.MethodPost, "/ventas", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertNotInternalServerError(t, rec, "10 000-item productos list")
	assert.GreaterOrEqual(t, rec.Code, 400,
		"10 000-item list must be 4xx: got %d body=%s", rec.Code, rec.Body.String())
	assert.Less(t, rec.Code, 500,
		"10 000-item list must NOT be 5xx: got %d body=%s", rec.Code, rec.Body.String())
	t.Logf("10 000-item productos → HTTP %d", rec.Code)
}

// TestVentas_BodyLimits_HugeNotaField verifies that a POST /ventas with a
// nota field of 1_000_000 characters (≫ domain maxNotaLength=500) is
// rejected with 422 by the app-layer validation. The field should never
// silently truncate or produce a 500.
func TestVentas_BodyLimits_HugeNotaField(t *testing.T) {
	t.Parallel()

	// Use a plain router without BodyLimit so we can isolate the domain
	// validation layer (the 1 MB nota is under the 10 MB HTTP limit).
	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	hugeNota := strings.Repeat("A", 1_000_000) // 1 MB string, domain max is 500
	body := validCreateBody()
	body.Nota = &hugeNota

	req := jsonRequest(t, http.MethodPost, "/ventas", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertNotInternalServerError(t, rec, "huge nota field")
	// Domain enforces maxNotaLength=500; the app layer maps that to 422.
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code,
		"1 MB nota must be rejected with 422, got %d body=%s", rec.Code, rec.Body.String())
}

// TestVentas_BodyLimits_HugeNombreClienteField verifies that a POST /ventas
// with a cliente.nombre of 10 000 characters (≫ domain maxNombreClienteLength=200)
// is rejected with 422 by the app-layer validation.
func TestVentas_BodyLimits_HugeNombreClienteField(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	body.Cliente.Nombre = strings.Repeat("N", 10_000) // domain max is 200

	req := jsonRequest(t, http.MethodPost, "/ventas", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertNotInternalServerError(t, rec, "huge cliente.nombre field")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code,
		"10 000-char nombre must be rejected with 422, got %d body=%s", rec.Code, rec.Body.String())
}

// TestVentas_BodyLimits_PATCH_Header_OversizedBody verifies that a
// PATCH /ventas/{id} request with a nota field that exceeds both the domain
// max (500) and the HTTP body limit when combined with padding is properly
// rejected. Tests the edit path in addition to the create path.
func TestVentas_BodyLimits_PATCH_Header_OversizedBody(t *testing.T) {
	t.Parallel()

	r := buildRouterWithBodyLimit(t)

	// First seed a venta through the same router (which has BodyLimit).
	// The seed body is small enough to pass.
	seedBody := validCreateBody()
	seedBytes, err := json.Marshal(seedBody)
	require.NoError(t, err)
	seedReq := httptest.NewRequest(http.MethodPost, "/ventas", bytes.NewReader(seedBytes))
	seedReq.Header.Set("Content-Type", "application/json")
	seedRec := httptest.NewRecorder()
	r.ServeHTTP(seedRec, seedReq)
	require.Equal(t, http.StatusCreated, seedRec.Code, "seed venta: %s", seedRec.Body.String())

	// Now send a PATCH with an oversized nota.
	overLimit := bodyLimitBytes + 1024
	notaPadding := strings.Repeat("B", int(overLimit))
	editBody := validHeaderBody()
	editBody.Nota = &notaPadding

	b, err := json.Marshal(editBody)
	require.NoError(t, err)
	require.Greater(t, int64(len(b)), bodyLimitBytes, "PATCH body must exceed limit")

	req := httptest.NewRequest(http.MethodPatch, "/ventas/"+seedBody.ID, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertNotInternalServerError(t, rec, "PATCH with oversized body")
	assert.GreaterOrEqual(t, rec.Code, 400, "oversized PATCH must be 4xx, got %d", rec.Code)
	assert.Less(t, rec.Code, 500, "oversized PATCH must NOT be 5xx, got %d body=%s", rec.Code, rec.Body.String())
	t.Logf("PATCH oversized nota → HTTP %d", rec.Code)
}

// TestVentas_BodyLimits_EmptyBodyRejected verifies that POST /ventas with a
// genuinely empty body (Content-Length: 0) is rejected with 400 or 422, not
// 500. An empty body cannot satisfy the required fields, so Huma must surface
// a parse / validation error.
func TestVentas_BodyLimits_EmptyBodyRejected(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	req := httptest.NewRequest(http.MethodPost, "/ventas", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = 0
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertNotInternalServerError(t, rec, "empty body")
	assert.GreaterOrEqual(t, rec.Code, 400, "empty body must be 4xx, got %d body=%s", rec.Code, rec.Body.String())
	assert.Less(t, rec.Code, 500, "empty body must NOT be 5xx, got %d body=%s", rec.Code, rec.Body.String())
}

// TestVentas_BodyLimits_MalformedJSON verifies that POST /ventas with raw
// malformed JSON is rejected with 400 or 422, not 500. Huma's body parser
// must recover the parse error and return a typed 4xx response.
func TestVentas_BodyLimits_MalformedJSON(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	cases := []struct {
		name string
		body string
	}{
		{"open brace only", `{not valid json`},
		{"truncated unicode", `{"id":"\u00`},
		{"array instead of object", `[1,2,3]`},
		{"bare value", `42`},
		{"control chars", fmt.Sprintf(`{"id":"%c%c%c"}`, 0x00, 0x01, 0x02)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/ventas", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			assertNotInternalServerError(t, rec, "malformed JSON: "+tc.name)
			assert.GreaterOrEqual(t, rec.Code, 400,
				"malformed JSON (%s) must be 4xx, got %d body=%s", tc.name, rec.Code, rec.Body.String())
			assert.Less(t, rec.Code, 500,
				"malformed JSON (%s) must NOT be 5xx, got %d body=%s", tc.name, rec.Code, rec.Body.String())
		})
	}
}
