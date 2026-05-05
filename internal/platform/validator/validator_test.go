package validator_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/validator"
)

type fakeReq struct {
	Nombre string `json:"nombre" validate:"required"`
	Email  string `json:"email" validate:"required,email"`
	Edad   int    `json:"edad" validate:"gte=18,lte=130"`
}

func TestStruct_HappyPath(t *testing.T) {
	t.Parallel()
	v := validator.New()
	errs := v.Struct(fakeReq{Nombre: "Ana", Email: "ana@example.com", Edad: 30})
	assert.Nil(t, errs)
}

func TestStruct_RequiredField_ReturnsFieldErrorWithJSONName(t *testing.T) {
	t.Parallel()
	v := validator.New()
	errs := v.Struct(fakeReq{Email: "x@x.com", Edad: 20})
	require.Len(t, errs, 1)
	assert.Equal(t, "nombre", errs[0].Field)
	assert.Equal(t, "required", errs[0].Code)
	assert.Contains(t, errs[0].Message, "obligatorio")
}

func TestStruct_MultipleViolations(t *testing.T) {
	t.Parallel()
	v := validator.New()
	errs := v.Struct(fakeReq{Edad: 5}) // missing nombre, email; bad edad
	require.GreaterOrEqual(t, len(errs), 3)
}

func TestStruct_EmailRule(t *testing.T) {
	t.Parallel()
	v := validator.New()
	errs := v.Struct(fakeReq{Nombre: "Ana", Email: "not-an-email", Edad: 20})
	require.Len(t, errs, 1)
	assert.Equal(t, "email", errs[0].Field)
	assert.Equal(t, "email", errs[0].Code)
	assert.Contains(t, errs[0].Message, "correo")
}

func TestStruct_RangeRules(t *testing.T) {
	t.Parallel()
	v := validator.New()
	errs := v.Struct(fakeReq{Nombre: "x", Email: "x@x.com", Edad: 200})
	require.Len(t, errs, 1)
	assert.Equal(t, "edad", errs[0].Field)
	assert.Equal(t, "lte", errs[0].Code)
}

func TestVar_StandalonePrimitive(t *testing.T) {
	t.Parallel()
	v := validator.New()
	require.NoError(t, v.Var("hello@x.com", "email"))
	require.Error(t, v.Var("not-an-email", "email"))
}

func TestDefault_Singleton(t *testing.T) {
	t.Parallel()
	a := validator.Default()
	b := validator.Default()
	assert.Same(t, a, b, "Default() must return the same instance")
}
