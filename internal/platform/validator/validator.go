// Package validator wraps go-playground/validator with Spanish error messages
// and convenience helpers for HTTP handlers.
package validator

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	pv "github.com/go-playground/validator/v10"

	"github.com/abdimuy/msp-api/internal/platform/response"
)

// Validator wraps the underlying validator instance.
type Validator struct {
	v *pv.Validate
}

var (
	defaultOnce sync.Once
	defaultV    *Validator
)

// Default returns a process-wide Validator. Most callers should use this.
func Default() *Validator {
	defaultOnce.Do(func() { defaultV = New() })
	return defaultV
}

// New builds a fresh Validator with custom registrations.
func New() *Validator {
	v := pv.New(pv.WithRequiredStructEnabled())

	// Use struct json tag as the field name in error messages.
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		if name == "" {
			return fld.Name
		}
		return name
	})

	return &Validator{v: v}
}

// Struct validates v and returns either nil or a slice of FieldError suitable
// for response.ValidationError.
func (val *Validator) Struct(v any) []response.FieldError {
	if err := val.v.Struct(v); err != nil {
		return mapError(err)
	}
	return nil
}

// Var validates a single value against a tag.
func (val *Validator) Var(field any, tag string) error {
	return val.v.Var(field, tag)
}

func mapError(err error) []response.FieldError {
	var ve pv.ValidationErrors
	if !errors.As(err, &ve) {
		return []response.FieldError{{Field: "", Code: "invalid", Message: err.Error()}}
	}
	out := make([]response.FieldError, 0, len(ve))
	for _, fe := range ve {
		out = append(out, response.FieldError{
			Field:   fe.Field(),
			Code:    fe.Tag(),
			Message: messageFor(fe),
		})
	}
	return out
}

// messageFor returns a Spanish message for the given validator failure.
func messageFor(fe pv.FieldError) string {
	field := fe.Field()
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("el campo %q es obligatorio", field)
	case "email":
		return fmt.Sprintf("%q no es un correo válido", field)
	case "min":
		return fmt.Sprintf("%q debe tener al menos %s caracteres/elementos", field, fe.Param())
	case "max":
		return fmt.Sprintf("%q no debe exceder %s caracteres/elementos", field, fe.Param())
	case "len":
		return fmt.Sprintf("%q debe tener exactamente %s caracteres", field, fe.Param())
	case "gt":
		return fmt.Sprintf("%q debe ser mayor que %s", field, fe.Param())
	case "gte":
		return fmt.Sprintf("%q debe ser mayor o igual que %s", field, fe.Param())
	case "lt":
		return fmt.Sprintf("%q debe ser menor que %s", field, fe.Param())
	case "lte":
		return fmt.Sprintf("%q debe ser menor o igual que %s", field, fe.Param())
	case "oneof":
		return fmt.Sprintf("%q debe ser uno de: %s", field, fe.Param())
	case "uuid", "uuid4":
		return fmt.Sprintf("%q no es un UUID válido", field)
	case "url":
		return fmt.Sprintf("%q no es una URL válida", field)
	case "datetime":
		return fmt.Sprintf("%q no es una fecha/hora válida", field)
	default:
		return fmt.Sprintf("%q no cumple con la regla %q", field, fe.Tag())
	}
}
