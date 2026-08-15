package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/middleware"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

// newVersionReq builds a request carrying (or omitting) X-App-Version.
func newVersionReq(version string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", nil)
	if version != "" {
		req.Header.Set(middleware.HeaderAppVersion, version)
	}
	return req
}

func TestMinAppVersion_DisabledLetsEverythingThrough(t *testing.T) {
	t.Parallel()

	mw := middleware.MinAppVersion("")

	for _, v := range []string{"", "1.0.0", "2.16.0", "no-es-version"} {
		rw := httptest.NewRecorder()
		mw(okHandler()).ServeHTTP(rw, newVersionReq(v))
		assert.Equal(t, http.StatusOK, rw.Code, "version %q must pass when the flag is off", v)
	}
}

func TestMinAppVersion_RejectsBelowMinimum(t *testing.T) {
	t.Parallel()

	mw := middleware.MinAppVersion("2.17.0")

	cases := []string{"2.16.0", "2.9.9", "1.0.0", "2.16.9"}
	for _, v := range cases {
		rw := httptest.NewRecorder()
		mw(okHandler()).ServeHTTP(rw, newVersionReq(v))

		assert.Equal(
			t, http.StatusConflict, rw.Code,
			"version %q is below the minimum and must be rejected", v,
		)
		assert.Contains(t, rw.Body.String(), "app_version_no_soportada")
	}
}

// The rejection MUST use a status the phone's decision table retries in every
// combination. A plain 4xx would be released as "permanent" the moment a
// capture confirmation is present, silently discarding the capture.
func TestMinAppVersion_RejectionIsRetryable(t *testing.T) {
	t.Parallel()

	mw := middleware.MinAppVersion("2.17.0")
	rw := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rw, newVersionReq("2.16.0"))

	retryable := map[int]bool{
		http.StatusRequestTimeout:  true,
		http.StatusConflict:        true,
		http.StatusTooEarly:        true,
		http.StatusTooManyRequests: true,
	}
	require.True(
		t, retryable[rw.Code],
		"status %d is not in the table's always-retry set — a capture could be dropped", rw.Code,
	)
}

func TestMinAppVersion_AllowsAtOrAboveMinimum(t *testing.T) {
	t.Parallel()

	mw := middleware.MinAppVersion("2.17.0")

	for _, v := range []string{"2.17.0", "2.17.1", "2.18.0", "3.0.0", "10.0.0"} {
		rw := httptest.NewRecorder()
		mw(okHandler()).ServeHTTP(rw, newVersionReq(v))
		assert.Equal(t, http.StatusOK, rw.Code, "version %q is at or above the minimum", v)
	}
}

// Fail OPEN. X-App-Version is sent only by the app's v2 client — the desktop
// (Tauri), the web frontend, v1 clients and curl send nothing. Blocking on an
// absent or unparseable header would take out everything that is not the phone.
func TestMinAppVersion_FailsOpenOnMissingOrUnparseableHeader(t *testing.T) {
	t.Parallel()

	mw := middleware.MinAppVersion("2.17.0")

	for _, v := range []string{"", "no-es-version", "v2.16.0-beta+build", "..", "2.x.0"} {
		rw := httptest.NewRecorder()
		mw(okHandler()).ServeHTTP(rw, newVersionReq(v))
		assert.Equal(
			t, http.StatusOK, rw.Code,
			"header %q is not a usable version — must fail open, not block", v,
		)
	}
}

// A malformed minimum disables the gate rather than blocking the whole fleet.
func TestMinAppVersion_MalformedMinimumDisablesTheGate(t *testing.T) {
	t.Parallel()

	mw := middleware.MinAppVersion("no-es-version")
	rw := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rw, newVersionReq("1.0.0"))

	assert.Equal(t, http.StatusOK, rw.Code, "a bad minimum must not lock out the fleet")
}

func TestMinAppVersion_ToleratesSuffixesAndShortVersions(t *testing.T) {
	t.Parallel()

	mw := middleware.MinAppVersion("2.17.0")

	// Flavor/build suffixes are stripped; missing components read as zero.
	blocked := []string{"2.16.0-dev", "2.16", "2"}
	for _, v := range blocked {
		rw := httptest.NewRecorder()
		mw(okHandler()).ServeHTTP(rw, newVersionReq(v))
		assert.Equal(t, http.StatusConflict, rw.Code, "version %q is below the minimum", v)
	}

	allowed := []string{"2.17.0-dev", "2.18", "3"}
	for _, v := range allowed {
		rw := httptest.NewRecorder()
		mw(okHandler()).ServeHTTP(rw, newVersionReq(v))
		assert.Equal(t, http.StatusOK, rw.Code, "version %q is at or above the minimum", v)
	}
}

// Numeric components compare as numbers, not strings: "2.9.0" < "2.10.0".
func TestMinAppVersion_ComparesNumericallyNotLexically(t *testing.T) {
	t.Parallel()

	mw := middleware.MinAppVersion("2.10.0")

	rw := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rw, newVersionReq("2.9.0"))
	assert.Equal(t, http.StatusConflict, rw.Code, `"2.9.0" is below "2.10.0"`)

	rw = httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rw, newVersionReq("2.10.0"))
	assert.Equal(t, http.StatusOK, rw.Code)
}
