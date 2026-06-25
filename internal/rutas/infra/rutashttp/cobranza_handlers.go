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

	res := rutasdomain.CalcularResumenPonderado(ventas)
	out := &DesglosePorZonaOutput{}
	out.Body.ZonaID = in.ZonaID
	if fechaInicio != nil {
		s := fechaInicio.UTC().Format(time.RFC3339)
		out.Body.FechaInicioSemana = &s
	}
	out.Body.Items = toVentaCobranzaDTOs(ventas)
	out.Body.Resumen = ResumenPonderadoDTO{
		Numerador:   res.Numerador.StringFixed(4),
		Denominador: res.Denominador,
	}
	if res.Pct != nil {
		s := res.Pct.StringFixed(2)
		out.Body.Resumen.PctPonderado = &s
	}
	return out, nil
}

// DesglosePorUsuario handles GET /rutas/usuarios/{uid}/cobranza.
// Requires auth.PermRutasLeer. Uses the user's own FECHA_CARGA_INICIAL window so
// the breakdown matches that user's row in the per-user report.
func (h *Handlers) DesglosePorUsuario(ctx context.Context, in *DesglosePorUsuarioInput) (*DesglosePorZonaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermRutasLeer); err != nil {
		return nil, err
	}

	ventas, fechaInicio, zonaID, err := h.svc.DesglosePorUsuario(ctx, in.UID)
	if err != nil {
		return nil, mapAppError(err)
	}

	res := rutasdomain.CalcularResumenPonderado(ventas)
	out := &DesglosePorZonaOutput{}
	out.Body.ZonaID = zonaID
	if fechaInicio != nil {
		s := fechaInicio.UTC().Format(time.RFC3339)
		out.Body.FechaInicioSemana = &s
	}
	out.Body.Items = toVentaCobranzaDTOs(ventas)
	out.Body.Resumen = ResumenPonderadoDTO{
		Numerador:   res.Numerador.StringFixed(4),
		Denominador: res.Denominador,
	}
	if res.Pct != nil {
		s := res.Pct.StringFixed(2)
		out.Body.Resumen.PctPonderado = &s
	}
	return out, nil
}

func toVentaCobranzaDTOs(ventas []rutasdomain.VentaCobranza) []VentaCobranzaDTO {
	if ventas == nil {
		return []VentaCobranzaDTO{}
	}
	dtos := make([]VentaCobranzaDTO, len(ventas))
	for i, v := range ventas {
		dg := rutasdomain.DesglosarAporte(v.Parcialidad, v.Vencidas, v.AbonoSemana, v.Aporte)
		dtos[i] = VentaCobranzaDTO{
			VentaID:             v.VentaID,
			ClienteID:           v.ClienteID,
			ClienteNombre:       v.ClienteNombre,
			Folio:               v.Folio,
			DoctoPVID:           v.DoctoPVID,
			Parcialidad:         v.Parcialidad.StringFixed(2),
			Frecuencia:          string(v.Frecuencia),
			AbonoSemana:         v.AbonoSemana.StringFixed(2),
			Vencidas:            v.Vencidas.StringFixed(4),
			Aporte:              v.Aporte.StringFixed(4),
			Saldo:               v.Saldo.StringFixed(2),
			AplicaPonderado:     v.AplicaPonderado,
			AtrasoAntesCuotas:   dg.AtrasoAntesCuotas.StringFixed(4),
			AtrasoAntesPesos:    dg.AtrasoAntesPesos.StringFixed(2),
			PagoCuotas:          dg.PagoCuotas.StringFixed(4),
			AtrasoDespuesCuotas: dg.AtrasoDespuesCuotas.StringFixed(4),
			AtrasoDespuesPesos:  dg.AtrasoDespuesPesos.StringFixed(2),
		}
	}
	return dtos
}
