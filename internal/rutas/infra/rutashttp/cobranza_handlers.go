//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutashttp

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/auth"
	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

// DesglosePorZona handles GET /rutas/{zona_id}/cobranza.
// Requires auth.PermRutasLeer.
func (h *Handlers) DesglosePorZona(ctx context.Context, in *DesglosePorZonaInput) (*DesglosePorZonaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermRutasLeer); err != nil {
		return nil, err
	}

	ventas, fechaInicio, err := h.svc.DesglosePorZona(ctx, in.ZonaID)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &DesglosePorZonaOutput{}
	out.Body.ZonaID = in.ZonaID
	if fechaInicio != nil {
		s := fechaInicio.UTC().Format(time.RFC3339)
		out.Body.FechaInicioSemana = &s
	}
	out.Body.Items = toVentaCobranzaDTOs(ventas)
	return out, nil
}

func toVentaCobranzaDTOs(ventas []rutasdomain.VentaCobranza) []VentaCobranzaDTO {
	if ventas == nil {
		return []VentaCobranzaDTO{}
	}
	dtos := make([]VentaCobranzaDTO, len(ventas))
	for i, v := range ventas {
		dtos[i] = VentaCobranzaDTO{
			VentaID:     v.VentaID,
			ClienteID:   v.ClienteID,
			Parcialidad: v.Parcialidad.StringFixed(2),
			Frecuencia:  string(v.Frecuencia),
			AbonoSemana: v.AbonoSemana.StringFixed(2),
			Vencidas:    v.Vencidas.StringFixed(4),
			Aporte:      v.Aporte.StringFixed(4),
			Saldo:       v.Saldo.StringFixed(2),
		}
	}
	return dtos
}
