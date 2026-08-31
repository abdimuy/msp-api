package canalhttp

import (
	"crypto/hmac"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// sharedTokenHeader carries the pre-shared secret on internal traffic in
// both directions: the store sends it to POST /canal/v1/salientes, and
// ForwarderClient sends it when pushing an entrante to the store. See
// config.Canal.SharedToken's doc comment for why one value covers both.
const sharedTokenHeader = "X-Canal-Token" //nolint:gosec // header name, not a credential value.

// secureCompare reports whether got equals want, using hmac.Equal instead
// of == so the comparison runs in constant time regardless of where the
// first differing byte is — required for every secret this package checks
// (gosec flags a non-constant-time comparison of a credential). want empty
// always fails closed: an unconfigured secret must never make every
// request accepted rather than every request refused.
func secureCompare(got, want string) bool {
	if want == "" {
		return false
	}
	return hmac.Equal([]byte(got), []byte(want))
}

// mapAppError translates a typed apperror.Error into a huma.StatusError
// using the apperror Kind→HTTP map. Non-apperror errors fall through as
// 500.
//
// Copied verbatim from
// internal/reactivacion/infra/reactivacionhttp/auth.go rather than
// imported: canal is a sealed module (ADR-0009) and may not import
// internal/reactivacion.
func mapAppError(err error) error {
	if err == nil {
		return nil
	}
	var ae *apperror.Error
	if !errors.As(err, &ae) {
		return huma.NewError(http.StatusInternalServerError, "ocurrió un error interno",
			&huma.ErrorDetail{Message: err.Error()})
	}
	status := ae.Kind.HTTPStatus()
	msg := ae.Message
	detail := &huma.ErrorDetail{
		Message: "code=" + ae.Code,
	}
	return huma.NewError(status, msg, detail)
}

// sharedTokenMiddleware builds a Huma per-operation middleware
// (huma.Operation.Middlewares) that rejects any request whose
// sharedTokenHeader does not match cfg.SharedToken with a 403 and no body —
// mirroring the webhook's own "don't explain to an attacker which half was
// wrong" posture. Attached only to POST /canal/v1/salientes: GET
// /canal/v1/salud is deliberately not wrapped in this (see registerSalud).
func sharedTokenMiddleware(api huma.API, sharedToken string) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if !secureCompare(ctx.Header(sharedTokenHeader), sharedToken) {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "acceso denegado")
			return
		}
		next(ctx)
	}
}
