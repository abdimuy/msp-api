// Package clients tiene los adaptadores del módulo desempeno hacia otros
// módulos, solo a través de sus contratos (CLAUDE.md §2).
//
//nolint:misspell // vocabulario español por convención del proyecto.
package clients

import (
	"context"
	"errors"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
	"github.com/abdimuy/msp-api/internal/rutas"
)

// rutasUsuariosClient adapta rutas.CobranzaUsuariosReader (lo satisface el
// app.Service de rutas) al puerto outbound.UsuariosCobranza.
type rutasUsuariosClient struct {
	rutas rutas.CobranzaUsuariosReader
}

// NewRutasUsuariosClient envuelve el lector de rutas.
func NewRutasUsuariosClient(r rutas.CobranzaUsuariosReader) outbound.UsuariosCobranza {
	return &rutasUsuariosClient{rutas: r}
}

var (
	_ outbound.UsuariosCobranza = (*rutasUsuariosClient)(nil)
	_ outbound.CobrosLote       = (*rutasUsuariosClient)(nil)
)

// ListarUsuarios traduce la falla de Firestore al 503 del contrato.
func (c *rutasUsuariosClient) ListarUsuarios(ctx context.Context) ([]domain.UsuarioCobranza, error) {
	us, err := c.rutas.UsuariosCobradores(ctx)
	if err != nil {
		if errors.Is(err, rutas.ErrCalendarioNoDisponible) {
			return nil, domain.ErrFirestoreNoDisponible.WithError(err)
		}
		return nil, err
	}
	out := make([]domain.UsuarioCobranza, 0, len(us))
	for _, u := range us {
		out = append(out, aDominio(u))
	}
	return out, nil
}

// CobroSemana pide a rutas el desglose de la semana del usuario.
func (c *rutasUsuariosClient) CobroSemana(ctx context.Context, u domain.UsuarioCobranza) (domain.CobroSemana, error) {
	cu, err := c.rutas.CobranzaDeUsuario(ctx, aRutas(u))
	if err != nil {
		return domain.CobroSemana{}, err
	}
	return aCobro(cu), nil
}

// CobrosSemana pide a rutas el desglose de todos los usuarios en una sola
// lectura (rutas.CobranzaDeUsuarios); out[i] corresponde a us[i].
func (c *rutasUsuariosClient) CobrosSemana(ctx context.Context, us []domain.UsuarioCobranza) ([]domain.CobroSemana, error) {
	ru := make([]rutas.UsuarioCobrador, len(us))
	for i, u := range us {
		ru[i] = aRutas(u)
	}
	cs, err := c.rutas.CobranzaDeUsuarios(ctx, ru)
	if err != nil {
		return nil, err
	}
	out := make([]domain.CobroSemana, len(cs))
	for i, cu := range cs {
		out[i] = aCobro(cu)
	}
	return out, nil
}

func aRutas(u domain.UsuarioCobranza) rutas.UsuarioCobrador {
	return rutas.UsuarioCobrador{
		UID: u.UID, Nombre: u.Nombre, Email: u.Email, CobradorID: u.CobradorID,
		ZonaID: u.ZonaID, ZonaNombre: u.ZonaNombre, InicioSemana: u.InicioSemana,
	}
}

func aCobro(cu rutas.CobranzaUsuario) domain.CobroSemana {
	out := domain.CobroSemana{PctPonderado: cu.PctPonderado, Ventas: make([]domain.CobroVenta, 0, len(cu.Ventas))}
	for _, v := range cu.Ventas {
		out.Ventas = append(out.Ventas, domain.CobroVenta{
			VentaID: v.VentaID, Parcialidad: v.Parcialidad, AbonoSemana: v.AbonoSemana,
			Vencidas: v.Vencidas, AplicaPonderado: v.AplicaPonderado,
		})
	}
	return out
}

func aDominio(u rutas.UsuarioCobrador) domain.UsuarioCobranza {
	return domain.UsuarioCobranza{
		UID: u.UID, Nombre: u.Nombre, Email: u.Email, CobradorID: u.CobradorID,
		ZonaID: u.ZonaID, ZonaNombre: u.ZonaNombre, InicioSemana: u.InicioSemana,
	}
}
