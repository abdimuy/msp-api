//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// JornadaInput lleva los parámetros crudos de GET /desempeno/usuarios/{uid}/jornada.
// Fecha vacía = hoy (America/Mexico_City).
type JornadaInput struct {
	Fecha     string
	PausaMin  string
	DesdeHora string
	HastaHora string
}

// VistaJornada es lo que el handler serializa (contrato §3).
type VistaJornada struct {
	UID        string
	PausaMin   int
	GeneradoAt time.Time
	Jornada    domain.Jornada
}

// Jornada devuelve las actividades que registró ESTE usuario en el día, con pausas,
// línea y señales. Sirve para cualquier fecha (spec §1): solo exige zona.
func (s *Service) Jornada(ctx context.Context, uid string, in JornadaInput) (VistaJornada, error) {
	p, err := domain.NuevosParametrosJornada(in.PausaMin, in.DesdeHora, in.HastaHora)
	if err != nil {
		return VistaJornada{}, err
	}
	c, err := s.resolverUsuario(ctx, uid)
	if err != nil {
		return VistaJornada{}, err
	}
	if c.usuario.ZonaID <= 0 {
		return VistaJornada{}, domain.ErrUsuarioSinZona
	}
	if c.usuario.InicioSemanaFuturo(c.ahora) {
		return VistaJornada{}, domain.ErrUsuarioInicioSemanaFuturo
	}
	dia := domain.DiaDe(c.ahora, s.loc)
	if in.Fecha != "" {
		if dia, err = domain.ParseFecha(in.Fecha); err != nil {
			return VistaJornada{}, domain.ErrParametroInvalido.WithField("parametro", "fecha")
		}
	}
	acts, err := s.actividadesCompletas(ctx, c.indice, c.usuario.ZonaID,
		dia.InicioDelDia(s.loc), dia.Siguiente().InicioDelDia(s.loc))
	if err != nil {
		return VistaJornada{}, err
	}
	j := domain.CalcularJornada(registradasPor(acts, c.usuario), p, dia, s.loc, c.ahora)
	return VistaJornada{UID: uid, PausaMin: p.PausaMin, GeneradoAt: c.ahora, Jornada: j}, nil
}
