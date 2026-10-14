package domain_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// Ayudantes compartidos por todas las pruebas del dominio (Tarea 0). Las
// tareas A1/A2 los usan y NO los redefinen.

// cdmx es la zona de negocio, America/Mexico_City.
func cdmx(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Mexico_City")
	require.NoError(t, err)
	return loc
}

func dinero(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func f64(v float64) *float64 { return &v }
