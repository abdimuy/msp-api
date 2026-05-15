// Package httpdispatch hosts helpers for in-process dispatches of one HTTP
// request through a router from inside another request's handler. The
// canonical use case is replaying a captured failed intent: an admin
// handler synthesizes a fresh request and re-routes it through the same
// chi.Router that served the original.
//
// The single helper this package exports — InternalContext — exists to
// neutralise a footgun in chi: chi.Mux.ServeHTTP short-circuits routing
// when it finds an existing chi.RouteContext on the incoming context. A
// sub-request built off the parent handler's r.Context() therefore lands
// in the parent's stale routing state and 404s. Calling InternalContext
// strips the route key so chi sets up a fresh routing context for the
// dispatched request.
package httpdispatch

import (
	"context"

	"github.com/go-chi/chi/v5"
)

// InternalContext returns a child context that strips the chi.RouteContext
// inherited from a parent request, so an in-process re-dispatch can be
// routed from scratch by chi.Mux.ServeHTTP. All other values on ctx
// (request id, trace span, planted CurrentUser, cancellation) are
// preserved — only the chi routing key is overwritten with a typed-nil
// *chi.Context so chi's existence check (mux.go ServeHTTP: rctx != nil)
// evaluates false.
//
// Callers that wrap a chi.Router for internal dispatch must pass
// InternalContext(parentCtx) as the context of the synthesized request;
// failing to do so produces spurious 404s that are difficult to diagnose.
func InternalContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, chi.RouteCtxKey, (*chi.Context)(nil))
}
