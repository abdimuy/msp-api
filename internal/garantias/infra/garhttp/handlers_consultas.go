package garhttp

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// Handlers contiene los handlers HTTP de lectura de garantías.
type Handlers struct {
	consultas *app.Consultas
}

// NewHandlers crea los handlers HTTP del módulo.
func NewHandlers(consultas *app.Consultas) *Handlers {
	return &Handlers{
		consultas: consultas,
	}
}

// ListarGarantias atiende la bandeja de garantías.
func (h *Handlers) ListarGarantias(
	ctx context.Context,
	in *ListarGarantiasRequest,
) (*ListarGarantiasResponse, error) {
	query, err := buildBandejaQuery(in)
	if err != nil {
		return nil, mapAppError(err)
	}

	pagina, err := h.consultas.Bandeja(ctx, query)
	if err != nil {
		return nil, mapAppError(err)
	}

	items := make([]GarantiaDTO, 0, len(pagina.Items))
	for _, garantia := range pagina.Items {
		items = append(items, garantiaDTO(garantia))
	}

	out := &ListarGarantiasResponse{}
	out.Body.Items = items
	out.Body.NextCursor = pagina.SiguienteCursor
	out.Body.HasMore = pagina.SiguienteCursor != ""

	return out, nil
}

// ObtenerGarantia devuelve un folio con sus artículos y línea de tiempo.
func (h *Handlers) ObtenerGarantia(
	ctx context.Context,
	in *ObtenerGarantiaRequest,
) (*ObtenerGarantiaResponse, error) {
	id, err := uuid.Parse(in.ID)
	if err != nil {
		return nil, mapAppError(errorValidacion(
			"invalid_uuid",
			"el identificador de la garantía no es un UUID válido",
		))
	}

	detalle, err := h.consultas.Detalle(ctx, id)
	if err != nil {
		return nil, mapAppError(err)
	}

	eventos := make([]EventoDTO, 0, len(detalle.Eventos))
	for _, evento := range detalle.Eventos {
		eventos = append(eventos, eventoDTO(evento))
	}

	out := &ObtenerGarantiaResponse{}
	out.Body.Garantia = garantiaDTO(detalle.Garantia)
	out.Body.Eventos = eventos

	return out, nil
}

func buildBandejaQuery(in *ListarGarantiasRequest) (app.BandejaQuery, error) {
	limit, err := parseLimit(in.Limit)
	if err != nil {
		return app.BandejaQuery{}, err
	}

	estado, err := parseEstado(in.Estado)
	if err != nil {
		return app.BandejaQuery{}, err
	}

	origen, err := parseOrigen(in.Origen)
	if err != nil {
		return app.BandejaQuery{}, err
	}

	clienteID, err := parseClienteID(in.ClienteID)
	if err != nil {
		return app.BandejaQuery{}, err
	}

	etapa, err := parseEtapa(in.Etapa)
	if err != nil {
		return app.BandejaQuery{}, err
	}

	ubicacion, err := parseUbicacion(in.Ubicacion)
	if err != nil {
		return app.BandejaQuery{}, err
	}

	desde, err := parseFechaFiltro(in.Desde, "desde")
	if err != nil {
		return app.BandejaQuery{}, err
	}

	hasta, err := parseFechaFiltro(in.Hasta, "hasta")
	if err != nil {
		return app.BandejaQuery{}, err
	}

	return app.BandejaQuery{
		Estado:    estado,
		Origen:    origen,
		ClienteID: clienteID,
		Etapa:     etapa,
		Ubicacion: ubicacion,
		Desde:     desde,
		Hasta:     hasta,
		Cursor:    in.Cursor,
		Limite:    limit,
	}, nil
}

func parseLimit(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, errorValidacion(
			"invalid_limit",
			"el parámetro limit debe ser un entero positivo",
		)
	}

	return limit, nil
}

func parseClienteID(raw string) (*int, error) {
	if raw == "" {
		//nolint:nilnil // nil indica que el filtro opcional no fue enviado.
		return nil, nil
	}

	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		return nil, errorValidacion(
			"invalid_cliente_id",
			"el parámetro cliente_id debe ser un entero positivo",
		)
	}

	return &id, nil
}

func parseEstado(raw string) (*domain.EstadoFolio, error) {
	if raw == "" {
		//nolint:nilnil // nil indica que el filtro opcional no fue enviado.
		return nil, nil
	}

	estado, err := domain.ParseEstadoFolio(raw)
	if err != nil {
		return nil, err
	}

	return &estado, nil
}

func parseOrigen(raw string) (*domain.OrigenFolio, error) {
	if raw == "" {
		//nolint:nilnil // nil indica que el filtro opcional no fue enviado.
		return nil, nil
	}

	origen, err := domain.ParseOrigenFolio(raw)
	if err != nil {
		return nil, err
	}

	return &origen, nil
}

func parseEtapa(raw string) (*domain.Etapa, error) {
	if raw == "" {
		//nolint:nilnil // nil indica que el filtro opcional no fue enviado.
		return nil, nil
	}

	etapa, err := domain.ParseEtapa(raw)
	if err != nil {
		return nil, err
	}

	return &etapa, nil
}

func parseUbicacion(raw string) (*domain.Ubicacion, error) {
	if raw == "" {
		//nolint:nilnil // nil indica que el filtro opcional no fue enviado.
		return nil, nil
	}

	ubicacion, err := domain.ParseUbicacion(raw)
	if err != nil {
		return nil, err
	}

	return &ubicacion, nil
}

func parseFechaFiltro(raw, nombre string) (*time.Time, error) {
	if raw == "" {
		//nolint:nilnil // nil indica que el filtro opcional no fue enviado.
		return nil, nil
	}

	fecha, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, errorValidacion(
			"invalid_"+nombre,
			"el parámetro "+nombre+" no es una fecha ISO8601 válida",
		)
	}

	fecha = fecha.UTC()

	return &fecha, nil
}
