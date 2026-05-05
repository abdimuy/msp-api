package apperror_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

func TestKind_HTTPStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		kind apperror.Kind
		want int
	}{
		{"validation", apperror.KindValidation, http.StatusUnprocessableEntity},
		{"not_found", apperror.KindNotFound, http.StatusNotFound},
		{"conflict", apperror.KindConflict, http.StatusConflict},
		{"unauthorized", apperror.KindUnauthorized, http.StatusUnauthorized},
		{"forbidden", apperror.KindForbidden, http.StatusForbidden},
		{"internal", apperror.KindInternal, http.StatusInternalServerError},
		{"unknown", apperror.KindUnknown, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.kind.HTTPStatus())
		})
	}
}

func TestConstructors_KindAndCodeAndMessage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		err     *apperror.Error
		kind    apperror.Kind
		status  int
		code    string
		message string
	}{
		{"validation", apperror.NewValidation("c", "m"), apperror.KindValidation, 422, "c", "m"},
		{"not_found", apperror.NewNotFound("c", "m"), apperror.KindNotFound, 404, "c", "m"},
		{"conflict", apperror.NewConflict("c", "m"), apperror.KindConflict, 409, "c", "m"},
		{"unauthorized", apperror.NewUnauthorized("c", "m"), apperror.KindUnauthorized, 401, "c", "m"},
		{"forbidden", apperror.NewForbidden("c", "m"), apperror.KindForbidden, 403, "c", "m"},
		{"internal", apperror.NewInternal("c", "m"), apperror.KindInternal, 500, "c", "m"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.kind, tc.err.Kind)
			assert.Equal(t, tc.code, tc.err.Code)
			assert.Equal(t, tc.message, tc.err.Message)
			assert.Equal(t, tc.status, tc.err.Kind.HTTPStatus())
		})
	}
}

func TestError_FormatsCodeAndCause(t *testing.T) {
	t.Parallel()

	plain := apperror.NewNotFound("cliente_not_found", "cliente no encontrado")
	assert.Equal(t, "cliente_not_found: cliente no encontrado", plain.Error())

	cause := errors.New("db: connection refused")
	wrapped := plain.WithError(cause)
	assert.Contains(t, wrapped.Error(), "cliente_not_found")
	assert.Contains(t, wrapped.Error(), "db: connection refused")
}

func TestUnwrap_ExposesCauseForErrorsIs(t *testing.T) {
	t.Parallel()
	cause := errors.New("boom")
	wrapped := apperror.NewInternal("internal", "x").WithError(cause)

	require.ErrorIs(t, wrapped, cause)
}

func TestIs_MatchesByCode(t *testing.T) {
	t.Parallel()

	sentinel := apperror.NewNotFound("cliente_not_found", "cliente no encontrado")
	created := apperror.NewNotFound("cliente_not_found", "otro mensaje")
	other := apperror.NewNotFound("vendedor_not_found", "x")
	plain := errors.New("plain")

	require.ErrorIs(t, created, sentinel, "same code must match")
	require.NotErrorIs(t, other, sentinel, "different code must not match")
	require.NotErrorIs(t, created, plain, "non-apperror must not match")
}

func TestWithSource_DoesNotMutateSentinel(t *testing.T) {
	t.Parallel()
	sentinel := apperror.NewValidation("required", "campo obligatorio")
	derived := sentinel.WithSource("cliente.service.Create")

	assert.Empty(t, sentinel.Source, "sentinel must remain untouched")
	assert.Equal(t, "cliente.service.Create", derived.Source)
	assert.Equal(t, sentinel.Code, derived.Code)
}

func TestWithError_DoesNotMutateSentinel(t *testing.T) {
	t.Parallel()
	sentinel := apperror.NewInternal("internal", "ocurrió un error")
	cause := errors.New("db down")
	derived := sentinel.WithError(cause)

	require.NoError(t, sentinel.Cause)
	require.Equal(t, cause, derived.Cause)
}

func TestWithField_CopyOnWriteMap(t *testing.T) {
	t.Parallel()
	sentinel := apperror.NewValidation("invalid", "x")
	a := sentinel.WithField("field", "nombre")
	b := a.WithField("max_len", 100)

	// sentinel never gains a Fields map.
	assert.Nil(t, sentinel.Fields)
	// a only has "field"
	assert.Equal(t, map[string]any{"field": "nombre"}, a.Fields)
	// b has both, and a wasn't mutated.
	assert.Equal(t, map[string]any{"field": "nombre", "max_len": 100}, b.Fields)
}

func TestAs_ExtractsAppError(t *testing.T) {
	t.Parallel()

	t.Run("nil returns false", func(t *testing.T) {
		t.Parallel()
		got, ok := apperror.As(nil)
		assert.False(t, ok)
		assert.Nil(t, got)
	})

	t.Run("plain error returns false", func(t *testing.T) {
		t.Parallel()
		err := errors.New("plain")
		got, ok := apperror.As(err)
		assert.False(t, ok)
		assert.Nil(t, got)
	})

	t.Run("direct apperror returns true", func(t *testing.T) {
		t.Parallel()
		original := apperror.NewConflict("dup", "duplicado")
		got, ok := apperror.As(original)
		require.True(t, ok)
		assert.Equal(t, "dup", got.Code)
	})

	t.Run("wrapped apperror returns true", func(t *testing.T) {
		t.Parallel()
		original := apperror.NewConflict("dup", "duplicado")
		wrapped := fmt.Errorf("service: %w", original)
		got, ok := apperror.As(wrapped)
		require.True(t, ok)
		assert.Equal(t, "dup", got.Code)
	})
}
