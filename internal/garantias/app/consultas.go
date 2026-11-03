// Package app contiene los casos de uso de garantías.
package app

import (
	"context"
	"errors"
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

// Detalle contiene la garantía, su línea de tiempo y las imágenes agrupadas
// por evento.
type Detalle struct {
	Garantia          *domain.Garantia
	Eventos           []*domain.Evento
	ImagenesPorEvento map[uuid.UUID][]*domain.Imagen
}

// ImagenDescarga contiene la metadata persistida y el blob asociado.
type ImagenDescarga struct {
	Imagen *domain.Imagen
	Objeto outbound.StorageObject
}

// ConsultasDeps contiene las dependencias del lado de lectura.
type ConsultasDeps struct {
	Garantias outbound.GarantiaRepo
	Bandeja   outbound.BandejaRepo
	Eventos   outbound.EventoRepo
	Imagenes  outbound.ImagenRepo
	Storage   outbound.StorageProvider
	Identity  outbound.Identity
}

// Consultas implementa las operaciones de lectura del módulo.
type Consultas struct {
	garantias outbound.GarantiaRepo
	bandeja   outbound.BandejaRepo
	eventos   outbound.EventoRepo
	imagenes  outbound.ImagenRepo
	storage   outbound.StorageProvider
	identity  outbound.Identity
}

// NewConsultas crea el servicio de consultas.
func NewConsultas(deps ConsultasDeps) *Consultas {
	return &Consultas{
		garantias: deps.Garantias,
		bandeja:   deps.Bandeja,
		eventos:   deps.Eventos,
		imagenes:  deps.Imagenes,
		storage:   deps.Storage,
		identity:  deps.Identity,
	}
}

// ValidarAutenticacion comprueba que exista un usuario antes de procesar
// entradas HTTP que podrían fallar por validación.
func (c *Consultas) ValidarAutenticacion(ctx context.Context) error {
	_, err := c.identity.UsuarioActual(ctx)
	return err
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

	garantia, eventos, err := c.garantias.ObtenerDetalle(ctx, id)
	if err != nil {
		return Detalle{}, err
	}

	eventoIDs := make([]uuid.UUID, 0, len(eventos))
	imagenesPorEvento := make(map[uuid.UUID][]*domain.Imagen, len(eventos))

	for _, evento := range eventos {
		eventoIDs = append(eventoIDs, evento.ID())
		imagenesPorEvento[evento.ID()] = []*domain.Imagen{}
	}

	imagenes := make([]*domain.Imagen, 0)
	if len(eventoIDs) > 0 {
		imagenes, err = c.imagenes.ListarPorEventos(ctx, eventoIDs)
		if err != nil {
			return Detalle{}, err
		}
	}

	for _, imagen := range imagenes {
		imagenesPorEvento[imagen.EventoID()] = append(
			imagenesPorEvento[imagen.EventoID()],
			imagen,
		)
	}

	return Detalle{
		Garantia:          garantia,
		Eventos:           eventos,
		ImagenesPorEvento: imagenesPorEvento,
	}, nil
}

// ObtenerImagen valida que garantía, evento e imagen formen la misma ruta y
// devuelve el blob sin revelar qué identificador fue el que no coincidió.
func (c *Consultas) ObtenerImagen(
	ctx context.Context,
	garantiaID uuid.UUID,
	eventoID uuid.UUID,
	imagenID uuid.UUID,
) (ImagenDescarga, error) {
	if err := c.autorizarLectura(ctx); err != nil {
		return ImagenDescarga{}, err
	}

	_, eventos, err := c.garantias.ObtenerDetalle(ctx, garantiaID)
	if err != nil {
		if errors.Is(err, domain.ErrGarantiaNoEncontrada) {
			return ImagenDescarga{}, domain.ErrImagenNoEncontrada
		}
		return ImagenDescarga{}, err
	}

	eventoValido := false
	for _, evento := range eventos {
		if evento.ID() == eventoID && evento.GarantiaID() == garantiaID {
			eventoValido = true
			break
		}
	}

	if !eventoValido {
		return ImagenDescarga{}, domain.ErrImagenNoEncontrada
	}

	imagen, err := c.imagenes.Obtener(ctx, imagenID)
	if err != nil {
		if errors.Is(err, domain.ErrImagenNoEncontrada) {
			return ImagenDescarga{}, domain.ErrImagenNoEncontrada
		}
		return ImagenDescarga{}, err
	}

	if imagen.EventoID() != eventoID {
		return ImagenDescarga{}, domain.ErrImagenNoEncontrada
	}

	objeto, err := c.storage.Get(ctx, imagen.Ruta())
	if err != nil {
		return ImagenDescarga{}, err
	}

	return ImagenDescarga{
		Imagen: imagen,
		Objeto: objeto,
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
