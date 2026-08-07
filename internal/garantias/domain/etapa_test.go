package domain_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestEtapa_WireValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		constant domain.Etapa
		want     string
	}{
		{domain.EtapaRegistrado, "registrado"},
		{domain.EtapaPendienteRecoleccion, "pendiente_recoleccion"},
		{domain.EtapaRecolectado, "recolectado"},
		{domain.EtapaEnRevision, "en_revision"},
		{domain.EtapaOrdenGenerada, "orden_generada"},
		{domain.EtapaEnviadoProveedor, "enviado_proveedor"},
		{domain.EtapaDictamenRecibido, "dictamen_recibido"},
		{domain.EtapaReparadoProveedor, "reparado_proveedor"},
		{domain.EtapaEsperaRespuestaCliente, "espera_respuesta_cliente"},
		{domain.EtapaEnTaller, "en_taller"},
		{domain.EtapaReparadoTaller, "reparado_taller"},
		{domain.EtapaCambioAutorizado, "cambio_autorizado"},
		{domain.EtapaListoEntrega, "listo_entrega"},
		{domain.EtapaEntregado, "entregado"},
		{domain.EtapaReingresadoInventario, "reingresado_inventario"},
		{domain.EtapaStandby, "standby"},
		{domain.EtapaSegundaMano, "segunda_mano"},
		{domain.EtapaDesarmado, "desarmado"},
		{domain.EtapaMerma, "merma"},
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

func TestParseEtapa_HappyPath(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"registrado", "pendiente_recoleccion", "recolectado",
		"en_revision", "orden_generada", "enviado_proveedor",
		"dictamen_recibido", "reparado_proveedor", "espera_respuesta_cliente",
		"en_taller", "reparado_taller", "cambio_autorizado",
		"listo_entrega", "entregado", "reingresado_inventario",
		"standby", "segunda_mano", "desarmado", "merma",
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseEtapa(in)
			if err != nil {
				t.Fatalf("ParseEtapa(%q) returned error: %v", in, err)
			}
			if got.String() != in {
				t.Errorf("got %q, want %q", got.String(), in)
			}
		})
	}
}

func TestParseEtapa_RejectsInvalid(t *testing.T) {
	t.Parallel()
	invalid := []string{
		"", "REGISTRADO", "registrado ", " registrado",
		"pendiente", "en taller", "entregado_falso",
	}
	for _, in := range invalid {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseEtapa(in)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, domain.ErrEtapaInvalida) {
				t.Errorf("expected ErrEtapaInvalida, got %v", err)
			}
		})
	}
}

func TestEtapa_EsTerminal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		stage    domain.Etapa
		terminal bool
	}{
		{domain.EtapaRegistrado, false},
		{domain.EtapaPendienteRecoleccion, false},
		{domain.EtapaRecolectado, false},
		{domain.EtapaEnRevision, false},
		{domain.EtapaOrdenGenerada, false},
		{domain.EtapaEnviadoProveedor, false},
		{domain.EtapaDictamenRecibido, false},
		{domain.EtapaReparadoProveedor, false},
		{domain.EtapaEsperaRespuestaCliente, false},
		{domain.EtapaEnTaller, false},
		{domain.EtapaReparadoTaller, false},
		{domain.EtapaCambioAutorizado, false},
		{domain.EtapaListoEntrega, false},
		{domain.EtapaEntregado, true},
		{domain.EtapaReingresadoInventario, true},
		{domain.EtapaStandby, false},
		{domain.EtapaSegundaMano, true},
		{domain.EtapaDesarmado, true},
		{domain.EtapaMerma, true},
	}
	for _, tt := range tests {
		t.Run(tt.stage.String(), func(t *testing.T) {
			t.Parallel()
			if got := tt.stage.EsTerminal(); got != tt.terminal {
				t.Errorf("EsTerminal() = %v, want %v", got, tt.terminal)
			}
		})
	}
}
