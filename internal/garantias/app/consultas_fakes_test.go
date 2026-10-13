package app_test

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

type fakeConsultaIdentity struct {
	usuario       outbound.Usuario
	usuarioErr    error
	permitido     bool
	permisoErr    error
	usuarioCalls  int
	permisoCalls  int
	ultimoPermiso domain.Permiso
}

func (f *fakeConsultaIdentity) UsuarioActual(
	_ context.Context,
) (outbound.Usuario, error) {
	f.usuarioCalls++
	return f.usuario, f.usuarioErr
}

func (f *fakeConsultaIdentity) TienePermiso(
	_ context.Context,
	p domain.Permiso,
) (bool, error) {
	f.permisoCalls++
	f.ultimoPermiso = p

	return f.permitido, f.permisoErr
}

type fakeConsultaBandejaRepo struct {
	pagina     outbound.Pagina[*domain.Garantia]
	err        error
	calls      int
	filtros    outbound.ListarGarantiasFiltros
	paginacion outbound.Paginacion
}

func (f *fakeConsultaBandejaRepo) Listar(
	_ context.Context,
	filtros outbound.ListarGarantiasFiltros,
	paginacion outbound.Paginacion,
) (outbound.Pagina[*domain.Garantia], error) {
	f.calls++
	f.filtros = filtros
	f.paginacion = paginacion

	return f.pagina, f.err
}

type fakeConsultaGarantiaRepo struct {
	garantia     *domain.Garantia
	obtenerErr   error
	obtenerCalls int
	ultimoID     uuid.UUID
}

func (f *fakeConsultaGarantiaRepo) Crear(
	_ context.Context,
	_ *domain.Garantia,
) error {
	return nil
}

func (f *fakeConsultaGarantiaRepo) Guardar(
	_ context.Context,
	_ *domain.Garantia,
) error {
	return nil
}

func (f *fakeConsultaGarantiaRepo) ObtenerParaActualizar(
	_ context.Context,
	_ uuid.UUID,
) (*domain.Garantia, error) {
	return nil, nil
}

func (f *fakeConsultaGarantiaRepo) Obtener(
	_ context.Context,
	id uuid.UUID,
) (*domain.Garantia, error) {
	f.obtenerCalls++
	f.ultimoID = id

	return f.garantia, f.obtenerErr
}

func (f *fakeConsultaGarantiaRepo) ObtenerPorFolio(
	_ context.Context,
	_ domain.Folio,
) (*domain.Garantia, error) {
	return nil, nil
}

type fakeConsultaEventoRepo struct {
	eventos  []*domain.Evento
	err      error
	calls    int
	ultimoID uuid.UUID
}

func (f *fakeConsultaEventoRepo) ListarPorGarantia(
	_ context.Context,
	garantiaID uuid.UUID,
) ([]*domain.Evento, error) {
	f.calls++
	f.ultimoID = garantiaID

	return f.eventos, f.err
}

func (f *fakeConsultaEventoRepo) ObtenerPorClaveIdempotencia(
	_ context.Context,
	_ string,
) (*domain.Evento, error) {
	return nil, nil
}
