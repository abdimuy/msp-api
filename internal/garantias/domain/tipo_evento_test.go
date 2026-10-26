package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestParseTipoEvento_HappyPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  domain.TipoEvento
	}{
		{"folio_abierto", domain.TipoEventoFolioAbierto},
		{"articulo_agregado", domain.TipoEventoArticuloAgregado},
		{"etapa_avanzada", domain.TipoEventoEtapaAvanzada},
		{"diagnostico_registrado", domain.TipoEventoDiagnosticoRegistrado},
		{"dictamen_registrado", domain.TipoEventoDictamenRegistrado},
		{"cambio_autorizado", domain.TipoEventoCambioAutorizado},
		{"desenlace_registrado", domain.TipoEventoDesenlaceRegistrado},
		{"folio_entregado", domain.TipoEventoFolioEntregado},
		{"folio_cerrado", domain.TipoEventoFolioCerrado},
		{"folio_cancelado", domain.TipoEventoFolioCancelado},
		{"evidencia_adjuntada", domain.TipoEventoEvidenciaAdjuntada},
		{"correccion", domain.TipoEventoCorrection},
		{"nota", domain.TipoEventoNota},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			te, err := domain.ParseTipoEvento(tc.input)
			if err != nil {
				t.Fatalf("ParseTipoEvento(%q): unexpected error %v", tc.input, err)
			}
			if te != tc.want {
				t.Errorf("ParseTipoEvento(%q) = %q, want %q", tc.input, te, tc.want)
			}
			if te.String() != tc.input {
				t.Errorf("String() = %q, want %q", te.String(), tc.input)
			}
			if !te.IsValid() {
				t.Errorf("%q should be valid", te)
			}
		})
	}
}

func TestParseTipoEvento_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "folio_abiert", "FOLIO_ABIERTO", "folio abierto", "x", "folio_abierto "}
	for _, tc := range cases {
		t.Run("invalid_"+tc, func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseTipoEvento(tc)
			if !errors.Is(err, domain.ErrTipoEventoInvalido) {
				t.Fatalf("ParseTipoEvento(%q): want ErrTipoEventoInvalido, got %v", tc, err)
			}
		})
	}
}

func TestTipoEvento_IsValid(t *testing.T) {
	t.Parallel()
	if domain.TipoEvento("folio_abierto").IsValid() != true {
		t.Error("folio_abierto should be valid")
	}
	if domain.TipoEvento("bogus").IsValid() {
		t.Error("bogus should not be valid")
	}
}
