package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/response"
)

// HeaderAppVersion is the request header the mobile app stamps with its
// version. Only the app's v2 client sends it (see AppVersionInterceptor);
// the desktop, the web frontend and v1 clients send nothing.
const HeaderAppVersion = "X-App-Version"

// versionParts is how many numeric components a version is compared on.
const versionParts = 3

// MinAppVersion rejects requests from app versions below minVersion. It is the
// server-side backstop for the in-app version gate: the app can be blocked from
// Firestore, but a phone that never reads Firestore still reaches the API.
//
// minVersion is a version NAME ("2.17.0"), not a versionCode, because that is
// what the header carries — and the whole point is to stop OLD builds, which
// only ever send the name. An empty or malformed minVersion disables the gate.
//
// Two properties this middleware must keep:
//
//  1. The rejection is 409 Conflict — a status the phone's decision table
//     retries in EVERY combination. A plain 4xx would be released as permanent
//     as soon as a capture confirmation is present, discarding the capture.
//     See docs/module-standards/ENTREGA_GARANTIZADA.md.
//  2. It must be mounted BEFORE the failed-intent capture middleware, so a
//     fleet of outdated phones does not write one MSP_FAILED_INTENTS row per
//     attempt.
//
// It fails OPEN: a missing or unparseable header passes through. Blocking on an
// absent header would take out every client that is not the phone.
func MinAppVersion(minVersion string) func(http.Handler) http.Handler {
	minParts, ok := parseVersion(minVersion)
	if !ok {
		// Flag off, or misconfigured. Never lock out the fleet over a typo.
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got, parsed := parseVersion(r.Header.Get(HeaderAppVersion))
			if parsed && compareVersions(got, minParts) < 0 {
				response.Error(w, r, apperror.NewConflict(
					"app_version_no_soportada",
					"esta versión de la aplicación ya no es compatible, actualízala para continuar",
				))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// parseVersion turns "2.16.0", "2.16" or "2.16.0-dev" into its numeric
// components. Missing components read as zero. Reports false when the value is
// empty or has no leading numeric component, which callers treat as "unknown"
// rather than "old".
func parseVersion(v string) ([versionParts]int, bool) {
	var out [versionParts]int

	// Drop any build/flavor suffix: "2.16.0-dev", "2.16.0+build".
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return out, false
	}

	segs := strings.Split(v, ".")
	if len(segs) > versionParts {
		segs = segs[:versionParts]
	}
	for i, seg := range segs {
		n, err := strconv.Atoi(seg)
		if err != nil || n < 0 {
			// A non-numeric component makes the rest meaningless. Anything
			// before it was already recorded, but a version we cannot read in
			// full is not evidence of being outdated.
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// compareVersions returns -1, 0 or 1 comparing component by component, so
// 2.9.0 sorts below 2.10.0 (numeric, not lexical).
func compareVersions(a, b [versionParts]int) int {
	for i := range a {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	return 0
}
