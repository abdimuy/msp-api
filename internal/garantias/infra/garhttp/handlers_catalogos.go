package garhttp

import (
	"context"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// ObtenerCatalogos devuelve los catálogos disponibles para el módulo de garantías.
// ObtenerCatalogos devuelve los catalogos disponibles del modulo de garantias.
func (h *Handlers) ObtenerCatalogos(
	ctx context.Context,
	_ *ObtenerCatalogosRequest,
) (*ObtenerCatalogosResponse, error) {
	if err := h.consultas.AutorizarCatalogos(ctx); err != nil {
		return nil, mapAppError(ctx, err)
	}

	out := &ObtenerCatalogosResponse{}

	for _, etapa := range domain.Etapas() {
		out.Body.Etapas = append(out.Body.Etapas, etapa.String())
	}

	for _, ubicacion := range domain.Ubicaciones() {
		out.Body.Ubicaciones = append(
			out.Body.Ubicaciones, ubicacion.String(),
		)
	}

	estados := []domain.EstadoFolio{
		domain.EstadoFolioAbierto,
		domain.EstadoFolioEnProceso,
		domain.EstadoFolioListoEntrega,
		domain.EstadoFolioEntregado,
		domain.EstadoFolioCerrado,
		domain.EstadoFolioCancelado,
	}
	for _, estado := range estados {
		out.Body.Estados = append(out.Body.Estados, estado.String())
	}

	tipos := []domain.TipoEvento{
		domain.TipoEventoFolioAbierto,
		domain.TipoEventoFolioEnProceso,
		domain.TipoEventoFolioListoEntrega,
		domain.TipoEventoArticuloAgregado,
		domain.TipoEventoArticuloMovido,
		domain.TipoEventoEtapaAvanzada,
		domain.TipoEventoDiagnosticoRegistrado,
		domain.TipoEventoDictamenRegistrado,
		domain.TipoEventoCambioAutorizado,
		domain.TipoEventoDesenlaceRegistrado,
		domain.TipoEventoDevolucionFallida,
		domain.TipoEventoFolioEntregado,
		domain.TipoEventoFolioCerrado,
		domain.TipoEventoFolioCancelado,
		domain.TipoEventoEvidenciaAdjuntada,
		domain.TipoEventoCorrection,
		domain.TipoEventoNota,
	}
	for _, tipo := range tipos {
		out.Body.TiposEvento = append(out.Body.TiposEvento, tipo.String())
	}

	for _, origen := range []domain.OrigenFolio{
		domain.OrigenFolioCliente,
		domain.OrigenFolioPiso,
	} {
		for _, etapa := range domain.Etapas() {
			transition := CatalogosTransicionDTO{
				Origen:     origen.String(),
				Etapa:      etapa.String(),
				Siguientes: []string{},
			}
			for _, siguiente := range domain.SiguientesPorAvanzar(etapa, origen) {
				transition.Siguientes = append(
					transition.Siguientes, siguiente.String(),
				)
			}
			out.Body.Transiciones = append(
				out.Body.Transiciones, transition,
			)
		}
	}

	return out, nil
}
