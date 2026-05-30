//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package cobranzahttp

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// currentUserOrError extracts the CurrentUser from the context planted by the
// authn middleware, or returns a 401 Huma error.
func currentUserOrError(ctx context.Context) (auth.CurrentUser, error) {
	cu, ok := auth.CurrentUserFromContext(ctx)
	if !ok {
		return auth.CurrentUser{}, huma.Error401Unauthorized("no autenticado")
	}
	return cu, nil
}

// requirePerm asserts the principal holds every permission in perms. Returns a
// 403 Huma error on the first missing code, otherwise nil.
func requirePerm(cu auth.CurrentUser, perms ...auth.Permission) error {
	held := make(map[string]struct{}, len(cu.Permisos))
	for _, p := range cu.Permisos {
		held[p] = struct{}{}
	}
	for _, required := range perms {
		if _, ok := held[string(required)]; !ok {
			return huma.NewError(http.StatusForbidden, "permiso denegado",
				&huma.ErrorDetail{
					Message:  "el principal no tiene el permiso requerido",
					Location: "header.authorization",
					Value:    string(required),
				})
		}
	}
	return nil
}

// authorize is the standard handler preamble: pull the CurrentUser from the
// context, then assert it holds every required permission. Returns the
// translated huma error (401/403) on failure; nil on success.
func authorize(ctx context.Context, perms ...auth.Permission) error {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return err
	}
	return requirePerm(cu, perms...)
}

// mapAppError translates a typed apperror.Error into a huma.StatusError. Non-
// apperror errors fall through as 500.
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
	detail := &huma.ErrorDetail{
		Message: "code=" + ae.Code,
	}
	return huma.NewError(status, ae.Message, detail)
}
