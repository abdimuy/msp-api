package response_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/logger"
	"github.com/abdimuy/msp-api/internal/platform/response"
)

func TestJSON_WritesContentTypeAndStatus(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)

	response.JSON(rec, r, http.StatusCreated, map[string]string{"hello": "world"})

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, response.ContentTypeJSON, rec.Header().Get("Content-Type"))
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "world", body["hello"])
}

func TestJSON_NilBody_NoOutput(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	response.JSON(rec, r, http.StatusOK, nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestNoContent(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	response.NoContent(rec)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestError_NilError_NoOp(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	response.Error(rec, r, nil)
	assert.Equal(t, http.StatusOK, rec.Code) // default
	assert.Empty(t, rec.Body.String())
}

func TestError_AppError_RendersProblem(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v2/clientes/abc", nil)
	r = r.WithContext(logger.WithRequestID(r.Context(), "req-7"))

	err := apperror.NewNotFound("cliente_not_found", "cliente no encontrado")
	response.Error(rec, r, err)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, response.ContentTypeProblem, rec.Header().Get("Content-Type"))

	var p response.Problem
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
	assert.Equal(t, http.StatusNotFound, p.Status)
	assert.Equal(t, "cliente_not_found", p.Code)
	assert.Equal(t, "cliente no encontrado", p.Detail)
	assert.Equal(t, "/v2/clientes/abc", p.Instance)
	assert.Equal(t, "req-7", p.RequestID)
}

func TestError_PlainError_FallsBackToInternal(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)

	response.Error(rec, r, errors.New("db: down"))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	var p response.Problem
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
	assert.Equal(t, "internal_error", p.Code)
}

func TestError_IncludesFieldsFromAppError(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)

	err := apperror.NewValidation("bad", "x").WithField("max", 100)
	response.Error(rec, r, err)

	var p response.Problem
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
	require.NotNil(t, p.Fields)
	assert.EqualValues(t, 100, p.Fields["max"])
}

func TestValidationError_RendersFieldErrors(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v2/clientes", nil)
	r = r.WithContext(logger.WithRequestID(r.Context(), "req-1"))

	fields := []response.FieldError{
		{Field: "nombre", Code: "required", Message: "nombre es obligatorio"},
		{Field: "rfc", Code: "invalid", Message: "rfc no es válido"},
	}
	response.ValidationError(rec, r, fields)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, response.ContentTypeProblem, rec.Header().Get("Content-Type"))

	var p response.Problem
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
	assert.Equal(t, "validation_failed", p.Code)
	assert.Len(t, p.Errors, 2)
	assert.Equal(t, "req-1", p.RequestID)
}
