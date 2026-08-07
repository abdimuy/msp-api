package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestUbicacion_WireValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		constant domain.Ubicacion
		want     string
	}{
		{domain.UbicacionDomicilioCliente, "domicilio_cliente"},
		{domain.UbicacionEnTransito, "en_transito"},
		{domain.UbicacionAlmacenRevision, "almacen_revision"},
		{domain.UbicacionTaller, "taller"},
		{domain.UbicacionProveedor, "proveedor"},
		{domain.UbicacionAlmacenSegundaMano, "almacen_segunda_mano"},
		{domain.UbicacionEntregado, "entregado"},
		{domain.UbicacionBaja, "baja"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.constant.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseUbicacion_HappyPath(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"domicilio_cliente", "en_transito", "almacen_revision",
		"taller", "proveedor", "almacen_segunda_mano",
		"entregado", "baja",
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseUbicacion(in)
			if err != nil {
				t.Fatalf("ParseUbicacion(%q) returned error: %v", in, err)
			}
			if got.String() != in {
				t.Errorf("got %q, want %q", got.String(), in)
			}
		})
	}
}

func TestParseUbicacion_RejectsInvalid(t *testing.T) {
	t.Parallel()
	invalid := []string{
		"", "DOMICILIO_CLIENTE", "domicilio_cliente ",
		"almacen", "entregado_falso",
	}
	for _, in := range invalid {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseUbicacion(in)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, domain.ErrUbicacionInvalida) {
				t.Errorf("expected ErrUbicacionInvalida, got %v", err)
			}
		})
	}
}
