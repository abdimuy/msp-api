//nolint:misspell // vocabulario español por convención del proyecto.
package desempenohttp

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// autorizar exige usuario autenticado (401) con desempeno:ver (403). El
// chequeo vive en el handler porque depguard prohíbe importar authhttp
// (HUMA_WIRING.md).
func autorizar(ctx context.Context) error {
	cu, ok := auth.CurrentUserFromContext(ctx)
	if !ok {
		return huma.Error401Unauthorized("no autenticado")
	}
	for _, p := range cu.Permisos {
		if p == string(auth.PermDesempenoVer) {
			return nil
		}
	}
	return huma.NewError(http.StatusForbidden, "permiso denegado", &huma.ErrorDetail{
		Message:  "el principal no tiene el permiso requerido",
		Location: "header.authorization",
		Value:    string(auth.PermDesempenoVer),
	})
}

// mapAppError traduce apperror al formato Huma con "code=<código>" en el
// detalle, igual que rutas; el parámetro inválido viaja en Location
// ("query.<parametro>"). Lo que no es apperror es 500 SIN el texto interno
// (puede traer SQL): se registra en el log y al cliente va un mensaje fijo.
//
// Un apperror 5xx (el 503 de Firestore caído o de Firebird que no respondió a
// tiempo) también se registra con su causa encadenada (WithError): sin eso el
// 503 llegaba al log de acceso sin ninguna pista y había que adivinar qué lo
// produjo. La causa nunca viaja al cliente.
func (h *Handlers) mapAppError(ctx context.Context, err error) error {
	var ae *apperror.Error
	if !errors.As(err, &ae) {
		h.logger().ErrorContext(ctx, "desempeno.http.error_interno", "error", err)
		return huma.NewError(http.StatusInternalServerError, "ocurrió un error interno")
	}
	status := ae.Kind.HTTPStatus()
	if status >= http.StatusInternalServerError {
		h.logger().WarnContext(ctx, "desempeno.http.no_disponible",
			"code", ae.Code, "status", status, "error", err)
	}
	detalle := &huma.ErrorDetail{Message: "code=" + ae.Code}
	if p, ok := ae.Fields["parametro"].(string); ok {
		detalle.Location = "query." + p
	}
	return huma.NewError(status, ae.Message, detalle)
}
