package garhttp

import (
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

func mapAppError(err error) error {
	if err == nil {
		return nil
	}

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		return huma.NewError(
			http.StatusInternalServerError,
			"ocurrió un error interno",
		)
	}

	switch appErr.Kind {
	case apperror.KindValidation,
		apperror.KindNotFound,
		apperror.KindConflict,
		apperror.KindForbidden,
		apperror.KindUnauthorized:
		return huma.NewError(
			appErr.Kind.HTTPStatus(),
			appErr.Message,
			&huma.ErrorDetail{Message: "code=" + appErr.Code},
		)

	case apperror.KindUnknown,
		apperror.KindInternal,
		apperror.KindServiceUnavailable,
		apperror.KindTooManyRequests:
		return huma.NewError(
			http.StatusInternalServerError,
			"ocurrió un error interno",
		)

	default:
		return huma.NewError(
			http.StatusInternalServerError,
			"ocurrió un error interno",
		)
	}
}

func errorValidacion(code, message string) error {
	return apperror.NewValidation(code, message)
}
