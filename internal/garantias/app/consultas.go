// Package app contiene los casos de uso de garantías.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

// BandejaQuery contiene los filtros y la paginación de la bandeja.
type BandejaQuery struct {
	Estado    *domain.EstadoFolio
	Origen    *domain.OrigenFolio
	ClienteID *int
	Etapa     *domain.Etapa
	Ubicacion *domain.Ubicacion
	Desde     *time.Time
	Hasta     *time.Time
	Cursor    string
	Limite    int
}

// Detalle contiene la garantía y su línea de tiempo.
type Detalle struct {
	Garantia *domain.Garantia
	Eventos  []*domain.Evento
}

type detalleReader interface {
	ObtenerDetalle(
		ctx context.Context,
		id uuid.UUID,
	) (*domain.Garantia, []*domain.Evento, error)
}

// ConsultasDeps contiene las dependencias del lado de lectura.
type ConsultasDeps struct {
	Garantias outbound.GarantiaRepo
	Bandeja   outbound.BandejaRepo
	Eventos   outbound.EventoRepo
	Identity  outbound.Identity
}

// Consultas implementa las operaciones de lectura del módulo.
type Consultas struct {
	garantias outbound.GarantiaRepo
	bandeja   outbound.BandejaRepo
	eventos   outbound.EventoRepo
	identity  outbound.Identity
}

// NewConsultas crea el servicio de consultas.
func NewConsultas(deps ConsultasDeps) *Consultas {
	return &Consultas{
		garantias: deps.Garantias,
		bandeja:   deps.Bandeja,
		eventos:   deps.Eventos,
		identity:  deps.Identity,
	}
}

// Bandeja devuelve una página de garantías después de validar el acceso.
func (c *Consultas) Bandeja(
	ctx context.Context,
	q BandejaQuery,
) (outbound.Pagina[*domain.Garantia], error) {
	if err := c.autorizarLectura(ctx); err != nil {
		return outbound.Pagina[*domain.Garantia]{}, err
	}

	filtros := outbound.ListarGarantiasFiltros{
		Estado:    q.Estado,
		Origen:    q.Origen,
		ClienteID: q.ClienteID,
		Etapa:     q.Etapa,
		Ubicacion: q.Ubicacion,
		Desde:     q.Desde,
		Hasta:     q.Hasta,
	}

	paginacion := outbound.Paginacion{
		Cursor: q.Cursor,
		Limite: q.Limite,
	}

	return c.bandeja.Listar(ctx, filtros, paginacion)
}

// Detalle devuelve una garantía junto con sus eventos.
func (c *Consultas) Detalle(
	ctx context.Context,
	id uuid.UUID,
) (Detalle, error) {
	if err := c.autorizarLectura(ctx); err != nil {
		return Detalle{}, err
	}

	if reader, ok := c.garantias.(detalleReader); ok {
		garantia, eventos, err := reader.ObtenerDetalle(ctx, id)
		if err != nil {
			return Detalle{}, err
		}

		return Detalle{
			Garantia: garantia,
			Eventos:  eventos,
		}, nil
	}

	garantia, err := c.garantias.Obtener(ctx, id)
	if err != nil {
		return Detalle{}, err
	}

	eventos, err := c.eventos.ListarPorGarantia(ctx, id)
	if err != nil {
		return Detalle{}, err
	}

	return Detalle{
		Garantia: garantia,
		Eventos:  eventos,
	}, nil
}

func (c *Consultas) autorizarLectura(ctx context.Context) error {
	_, err := c.identity.UsuarioActual(ctx)
	if err != nil {
		return err
	}

	permitido, err := c.identity.TienePermiso(ctx, domain.PermisoLeer)
	if err != nil {
		return err
	}

	if !permitido {
		return domain.ErrPermisoDenegado
	}

	return nil
}
