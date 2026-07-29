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

// internalDispatchKey is the private context key marking a request as an
// in-process re-dispatch. It is unexported and set only by InternalContext,
// so an external client cannot forge it via a header or query param — the
// only way to be "internal" is to have been synthesized server-side.
type internalDispatchKey struct{}

// InternalContext returns a child context that (1) strips the chi.RouteContext
// inherited from a parent request, so an in-process re-dispatch can be
// routed from scratch by chi.Mux.ServeHTTP, and (2) marks the context as an
// internal dispatch (see IsInternal). All other values on ctx
// (request id, trace span, planted CurrentUser, cancellation) are
// preserved — only the chi routing key is overwritten with a typed-nil
// *chi.Context so chi's existence check (mux.go ServeHTTP: rctx != nil)
// evaluates false.
//
// Callers that wrap a chi.Router for internal dispatch must pass
// InternalContext(parentCtx) as the context of the synthesized request;
// failing to do so produces spurious 404s that are difficult to diagnose.
func InternalContext(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, chi.RouteCtxKey, (*chi.Context)(nil))
	return context.WithValue(ctx, internalDispatchKey{}, true)
}

// IsInternal reports whether ctx belongs to an in-process re-dispatch created
// by InternalContext (e.g. a failed-intent replay routed back through the same
// router). Handlers use it to relax client-facing transport guards — such as
// the cobranza pago "Idempotency-Key must equal datos.id" check — that a
// trusted server-side replay legitimately violates when it mints a fresh
// transport key. The marker is unforgeable: external requests never carry it.
func IsInternal(ctx context.Context) bool {
	v, _ := ctx.Value(internalDispatchKey{}).(bool)
	return v
}
