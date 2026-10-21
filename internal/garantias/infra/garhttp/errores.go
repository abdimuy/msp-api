package garhttp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

func mapAppError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		slog.ErrorContext(ctx, "garantias.http_internal_error",
			"error", err.Error(),
		)

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
		apperror.KindUnauthorized,
		apperror.KindServiceUnavailable,
		apperror.KindTooManyRequests:
		return huma.NewError(
			appErr.Kind.HTTPStatus(),
			appErr.Message,
			&huma.ErrorDetail{Message: "code=" + appErr.Code},
		)

	case apperror.KindUnknown,
		apperror.KindInternal:
		slog.ErrorContext(ctx, "garantias.http_internal_error",
			"code", appErr.Code,
			"error", appErr.Error(),
		)

		return huma.NewError(
			http.StatusInternalServerError,
			"ocurrió un error interno",
		)

	default:
		slog.ErrorContext(ctx, "garantias.http_internal_error",
			"code", appErr.Code,
			"error", appErr.Error(),
		)

		return huma.NewError(
			http.StatusInternalServerError,
			"ocurrió un error interno",
		)
	}
}

func errorValidacion(code, message string) error {
	return apperror.NewValidation(code, message)
}
