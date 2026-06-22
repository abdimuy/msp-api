//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutashttp

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// currentUserOrError extracts the CurrentUser planted by the authn middleware
// or returns a 401 Huma error so the handler can simply propagate it.
func currentUserOrError(ctx context.Context) (auth.CurrentUser, error) {
	cu, ok := auth.CurrentUserFromContext(ctx)
	if !ok {
		return auth.CurrentUser{}, huma.Error401Unauthorized("no autenticado")
	}
	return cu, nil
}

// requirePerm asserts the principal holds every permission in perms. Returns
// a 403 Huma error on the first missing code, otherwise nil.
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

// mapAppError translates a typed apperror.Error into a huma.StatusError using
// the apperror Kind→HTTP map. Non-apperror errors fall through as 500.
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
