//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionhttp

import (
	"crypto/hmac"
	"net"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// tiendaTokenHeader carries the pre-shared secret the VPS's ForwarderClient
// sends on every push to POST /reactivacion/tienda/mensaje-entrante — the
// SAME header name and SAME secret (config.Canal.SharedToken) it already
// sends today. See internal/canal/infra/canalhttp/forwarder_client.go's
// sharedTokenHeader constant.
//
// Duplicated here rather than imported: internal/canal is a sealed module
// (ADR-0009) and its infra package is not something reactivación may depend
// on either way. This mirrors the precedent canal's own auth.go already
// set for mapAppError — "copied verbatim ... rather than imported".
const tiendaTokenHeader = "X-Canal-Token" //nolint:gosec // header name, not a credential value.

// secureCompareTienda reports whether got equals want, using hmac.Equal
// instead of == so the comparison runs in constant time regardless of
// where the first differing byte is — gosec flags a non-constant-time
// comparison of a credential. want empty always fails closed: an
// unconfigured shared token must never make every request accepted rather
// than every request refused.
func secureCompareTienda(got, want string) bool {
	if want == "" {
		return false
	}
	return hmac.Equal([]byte(got), []byte(want))
}

// tiendaTokenMiddleware builds a per-operation Huma middleware that rejects
// any request whose tiendaTokenHeader does not match sharedToken with a 403
// and no body — the same "never tell an attacker which half of the check
// failed" posture as internal/canal/infra/canalhttp's own
// sharedTokenMiddleware, which this mirrors.
func tiendaTokenMiddleware(api huma.API, sharedToken string) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if !secureCompareTienda(ctx.Header(tiendaTokenHeader), sharedToken) {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "acceso denegado")
			return
		}
		next(ctx)
	}
}

// tiendaIPMiddleware builds a per-operation Huma middleware that, when
// allowedIPs is non-empty, rejects any request whose remote address is not
// in allowedIPs with a 403 and no body. An empty allowedIPs disables the
// check entirely — the default, and the only setting that keeps local
// development and the test suite working without any configuration.
//
// 🔴 Best-effort, not a real boundary, under the store's current
// deployment: per docs/adr/0010-whatsapp-cloud-api-and-the-always-on-edge.md
// §2, the Windows box is reached only through a pinggy tunnel that
// terminates locally and forwards to the box — net/http's RemoteAddr on
// that path is typically the tunnel's local forwarder, not the VPS's real
// public IP, so this filter may see the same address for every caller and
// either allow everyone or nobody depending on what the operator
// configures. It is offered because the task calls for it and because it
// does add a real layer WHEREVER the box is reached directly or through a
// tunnel that preserves the original address; the shared token
// (tiendaTokenMiddleware) is the actual security boundary regardless.
func tiendaIPMiddleware(api huma.API, allowedIPs []string) func(huma.Context, func(huma.Context)) {
	allowed := make(map[string]struct{}, len(allowedIPs))
	for _, ip := range allowedIPs {
		allowed[ip] = struct{}{}
	}
	return func(ctx huma.Context, next func(huma.Context)) {
		if _, ok := allowed[remoteIP(ctx)]; !ok {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "acceso denegado")
			return
		}
		next(ctx)
	}
}

// remoteIP extracts the caller's IP from ctx, stripping a port when
// present. huma.Context.RemoteAddr() returns the same value
// net/http.Request.RemoteAddr would.
func remoteIP(ctx huma.Context) string {
	addr := ctx.RemoteAddr()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
