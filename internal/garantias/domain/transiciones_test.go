package domain_test

import (
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestEtapa_CanTransitionTo_ValidTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		from domain.Etapa
		to   domain.Etapa
	}{
		// Tronco común
		{domain.EtapaRegistrado, domain.EtapaPendienteRecoleccion},
		{domain.EtapaPendienteRecoleccion, domain.EtapaRecolectado},
		{domain.EtapaRecolectado, domain.EtapaEnRevision},

		// Desde en_revision
		{domain.EtapaEnRevision, domain.EtapaOrdenGenerada},
		{domain.EtapaEnRevision, domain.EtapaEnTaller},
		{domain.EtapaEnRevision, domain.EtapaReingresadoInventario},

		// Ruta proveedor
		{domain.EtapaOrdenGenerada, domain.EtapaEnviadoProveedor},
		{domain.EtapaEnviadoProveedor, domain.EtapaDictamenRecibido},
		{domain.EtapaDictamenRecibido, domain.EtapaReparadoProveedor},
		{domain.EtapaDictamenRecibido, domain.EtapaListoEntrega},
		{domain.EtapaDictamenRecibido, domain.EtapaEsperaRespuestaCliente},
		{domain.EtapaReparadoProveedor, domain.EtapaListoEntrega},

		// espera_respuesta_cliente → exactamente 3 salidas
		{domain.EtapaEsperaRespuestaCliente, domain.EtapaListoEntrega},
		{domain.EtapaEsperaRespuestaCliente, domain.EtapaCambioAutorizado},
		{domain.EtapaEsperaRespuestaCliente, domain.EtapaStandby},

		// Ruta taller
		{domain.EtapaEnTaller, domain.EtapaReparadoTaller},
		{domain.EtapaEnTaller, domain.EtapaCambioAutorizado},
		{domain.EtapaReparadoTaller, domain.EtapaListoEntrega},

		// Cambio físico
		{domain.EtapaCambioAutorizado, domain.EtapaListoEntrega},
		{domain.EtapaCambioAutorizado, domain.EtapaStandby},

		// Convergencia
		{domain.EtapaListoEntrega, domain.EtapaEntregado},

		// Standby → terminales
		{domain.EtapaStandby, domain.EtapaSegundaMano},
		{domain.EtapaStandby, domain.EtapaDesarmado},
		{domain.EtapaStandby, domain.EtapaMerma},
	}

	for _, tt := range tests {
		t.Run(tt.from.String()+"_to_"+tt.to.String(), func(t *testing.T) {
			t.Parallel()
			if !tt.from.CanTransitionTo(tt.to) {
				t.Errorf("CanTransitionTo(%q, %q) = false, want true", tt.from, tt.to)
			}
		})
	}
}

func TestEtapa_CanTransitionTo_RejectsInvalid(t *testing.T) {
	t.Parallel()

	// Casos representativos de transiciones inválidas
	tests := []struct {
		from domain.Etapa
		to   domain.Etapa
	}{
		// No se puede volver a espera_respuesta_cliente
		{domain.EtapaListoEntrega, domain.EtapaEsperaRespuestaCliente},
		{domain.EtapaCambioAutorizado, domain.EtapaEsperaRespuestaCliente},
		{domain.EtapaStandby, domain.EtapaEsperaRespuestaCliente},

		// Solo dos entradas a standby
		{domain.EtapaRegistrado, domain.EtapaStandby},
		{domain.EtapaPendienteRecoleccion, domain.EtapaStandby},
		{domain.EtapaRecolectado, domain.EtapaStandby},
		{domain.EtapaEnRevision, domain.EtapaStandby},
		{domain.EtapaOrdenGenerada, domain.EtapaStandby},
		{domain.EtapaEnviadoProveedor, domain.EtapaStandby},
		{domain.EtapaDictamenRecibido, domain.EtapaStandby},
		{domain.EtapaReparadoProveedor, domain.EtapaStandby},
		{domain.EtapaEnTaller, domain.EtapaStandby},
		{domain.EtapaReparadoTaller, domain.EtapaStandby},
		{domain.EtapaListoEntrega, domain.EtapaStandby},
		{domain.EtapaEntregado, domain.EtapaStandby},
		{domain.EtapaReingresadoInventario, domain.EtapaStandby},

		// Terminales no tienen salidas
		{domain.EtapaEntregado, domain.EtapaListoEntrega},
		{domain.EtapaEntregado, domain.EtapaSegundaMano},
		{domain.EtapaReingresadoInventario, domain.EtapaListoEntrega},
		{domain.EtapaSegundaMano, domain.EtapaStandby},
		{domain.EtapaDesarmado, domain.EtapaMerma},
		{domain.EtapaMerma, domain.EtapaSegundaMano},

		// Retrocesos comunes que no están en el diagrama
		{domain.EtapaEnviadoProveedor, domain.EtapaOrdenGenerada},
		{domain.EtapaDictamenRecibido, domain.EtapaEnviadoProveedor},
		{domain.EtapaReparadoProveedor, domain.EtapaDictamenRecibido},
		{domain.EtapaEnTaller, domain.EtapaEnRevision},
		{domain.EtapaReparadoTaller, domain.EtapaEnTaller},

		// listo_entrega solo va a entregado
		{domain.EtapaListoEntrega, domain.EtapaCambioAutorizado},
		{domain.EtapaListoEntrega, domain.EtapaReingresadoInventario},

		// Sin retrocesos desde cambio_autorizado
		{domain.EtapaCambioAutorizado, domain.EtapaDictamenRecibido},
	}

	for _, tt := range tests {
		t.Run(tt.from.String()+"_to_"+tt.to.String(), func(t *testing.T) {
			t.Parallel()
			if tt.from.CanTransitionTo(tt.to) {
				t.Errorf("CanTransitionTo(%q, %q) = true, want false", tt.from, tt.to)
			}
		})
	}
}

func TestEtapa_CanTransitionTo_UnknownStageReturnsFalse(t *testing.T) {
	t.Parallel()
	unknown := domain.Etapa("etapa_que_no_existe")
	if unknown.CanTransitionTo(domain.EtapaRegistrado) {
		t.Error("CanTransitionTo on unknown stage should return false")
	}
	if unknown.CanTransitionTo(unknown) {
		t.Error("CanTransitionTo on unknown stage to itself should return false")
	}
}

func TestEtapa_EsTerminal_Consistency(t *testing.T) {
	t.Parallel()
	// Verifica que las etapas terminales también tengan lista vacía en el mapa
	terminales := []domain.Etapa{
		domain.EtapaEntregado,
		domain.EtapaReingresadoInventario,
		domain.EtapaSegundaMano,
		domain.EtapaDesarmado,
		domain.EtapaMerma,
	}

	for _, e := range terminales {
		t.Run(e.String(), func(t *testing.T) {
			t.Parallel()
			if !e.EsTerminal() {
				t.Errorf("%s: EsTerminal() = false, want true", e)
			}
			if e.CanTransitionTo(e) {
				t.Errorf("%s: CanTransitionTo(self) should be false for terminal stages", e)
			}
		})
	}

	// Verifica que las no-terminales no estén vacías
	noTerminales := []domain.Etapa{
		domain.EtapaRegistrado,
		domain.EtapaPendienteRecoleccion,
		domain.EtapaRecolectado,
		domain.EtapaEnRevision,
		domain.EtapaOrdenGenerada,
		domain.EtapaEnviadoProveedor,
		domain.EtapaDictamenRecibido,
		domain.EtapaReparadoProveedor,
		domain.EtapaEsperaRespuestaCliente,
		domain.EtapaEnTaller,
		domain.EtapaReparadoTaller,
		domain.EtapaCambioAutorizado,
		domain.EtapaListoEntrega,
		domain.EtapaStandby,
	}

	for _, e := range noTerminales {
		t.Run(e.String(), func(t *testing.T) {
			t.Parallel()
			if e.EsTerminal() {
				t.Errorf("%s: EsTerminal() = true, want false", e)
			}
		})
	}
}
