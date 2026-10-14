//nolint:misspell // rutas vocabulary is Spanish per project convention.
package app

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/rutas"
	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

// Compile-time check: *Service satisfies the cross-module contract.
var _ rutas.CobranzaUsuariosReader = (*Service)(nil)

// UsuariosCobradores implements rutas.CobranzaUsuariosReader. Unlike
// ListarReporteUsuarios it does NOT swallow a calendar failure: desempeno must
// answer 503 instead of inventing an empty fleet (spec §6).
func (s *Service) UsuariosCobradores(ctx context.Context) ([]rutas.UsuarioCobrador, error) {
	usuarios, err := s.calendario.ListarCobradores(ctx)
	if err != nil {
		return nil, rutas.ErrCalendarioNoDisponible.WithError(err)
	}
	zonas, err := s.repo.ListarRutas(ctx)
	if err != nil {
		return nil, err
	}
	nombrePorZona := make(map[int]string, len(zonas))
	for _, z := range zonas {
		nombrePorZona[z.ZonaID] = z.ZonaNombre
	}
	out := make([]rutas.UsuarioCobrador, 0, len(usuarios))
	for _, u := range usuarios {
		c := rutas.UsuarioCobrador{
			UID: u.UID, Nombre: u.Nombre, Email: u.Email,
			CobradorID: u.CobradorID, ZonaID: u.ZonaID, ZonaNombre: nombrePorZona[u.ZonaID],
		}
		if !u.FechaInicio.IsZero() {
			fi := u.FechaInicio
			c.InicioSemana = &fi
		}
		out = append(out, c)
	}
	return out, nil
}

// CobranzaDeUsuario implements rutas.CobranzaUsuariosReader with the same
// computation as DesglosePorUsuario (VentasPorZona + enrichVentas +
// CalcularResumenPonderado), over the user's own window.
func (s *Service) CobranzaDeUsuario(ctx context.Context, u rutas.UsuarioCobrador) (rutas.CobranzaUsuario, error) {
	out := rutas.CobranzaUsuario{Usuario: u, Ventas: []rutas.VentaCobro{}}
	if u.ZonaID <= 0 || u.InicioSemana == nil {
		return out, nil
	}
	now := time.Now().UTC()
	ventas, err := s.cobranza.VentasPorZona(ctx, u.ZonaID, *u.InicioSemana, now)
	if err != nil {
		return rutas.CobranzaUsuario{}, err
	}
	enrichVentas(ventas, *u.InicioSemana, now)
	for _, v := range ventas {
		out.Ventas = append(out.Ventas, rutas.VentaCobro{
			VentaID: v.VentaID, ClienteID: v.ClienteID,
			Parcialidad: v.Parcialidad, AbonoSemana: v.AbonoSemana, Saldo: v.Saldo,
			Vencidas: v.Vencidas, Aporte: v.Aporte, AplicaPonderado: v.AplicaPonderado,
		})
	}
	out.PctPonderado = rutasdomain.CalcularResumenPonderado(ventas).Pct
	return out, nil
}
