package authhttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// errFuzzVerify is the deterministic error the fake firebase client returns
// during fuzzing so SyncFromFirebase short-circuits cleanly instead of
// nil-dereferencing on the (Token == nil, Err == nil) path.
var errFuzzVerify = errors.New("fuzz: verify rejected")

// FuzzLoginBody feeds arbitrary JSON-ish bodies into POST /v2/auth/login and
// asserts the handler never panics and always returns a valid HTTP status
// code. The handler is allowed to surface any 2xx/3xx/4xx/5xx response — the
// invariant is that the server stays alive and emits a well-formed response.
func FuzzLoginBody(f *testing.F) {
	seeds := []string{
		`{"id_token":"dev:alice"}`,
		`{}`,
		``,
		`{`,
		`{"id_token":""}`,
		`{"id_token":` + strings.Repeat("a", 100_000) + `}`,
		`{"id_token":null}`,
		`{"id_token":1}`,
		`{"id_token":true}`,
		`{"id_token":["array"]}`,
		"{\"id_token\":\"\x00nullbyte\"}",
		`{"id_token":"valid","extra":"field"}`,
		`not json at all`,
		`[]`,
		`null`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, body string) {
		rig := newTestRig(t)
		// Force firebase to fail deterministically so accepted bodies do not
		// nil-dereference on the (Token == nil, Err == nil) fake path.
		rig.firebase.Err = errFuzzVerify

		r := chi.NewRouter()
		MountRouter(r, rig.svc, rig.firebase, rig.usuarios, newNoopIdempotencyStore())

		req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		// The crucial invariant: ServeHTTP must not panic on any input.
		r.ServeHTTP(rec, req)

		if rec.Code < 100 || rec.Code >= 600 {
			t.Fatalf("invalid HTTP status %d for body %q", rec.Code, body)
		}
	})
}
