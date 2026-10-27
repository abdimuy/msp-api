package garhttp_test

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garhttp"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

type fakeHTTPIdentity struct {
	usuario    outbound.Usuario
	usuarioErr error
	permitido  bool
	permisoErr error

	usuarioCalls int
	permisoCalls int
}

func (f *fakeHTTPIdentity) UsuarioActual(
	_ context.Context,
) (outbound.Usuario, error) {
	f.usuarioCalls++
	return f.usuario, f.usuarioErr
}

func (f *fakeHTTPIdentity) TienePermiso(
	_ context.Context,
	_ domain.Permiso,
) (bool, error) {
	f.permisoCalls++
	return f.permitido, f.permisoErr
}

type fakeHTTPBandejaRepo struct {
	pagina outbound.Pagina[*domain.Garantia]
	err    error
	calls  int

	filtros    outbound.ListarGarantiasFiltros
	paginacion outbound.Paginacion
}

func (f *fakeHTTPBandejaRepo) Listar(
	_ context.Context,
	filtros outbound.ListarGarantiasFiltros,
	paginacion outbound.Paginacion,
) (outbound.Pagina[*domain.Garantia], error) {
	f.calls++
	f.filtros = filtros
	f.paginacion = paginacion

	return f.pagina, f.err
}

type fakeHTTPGarantiaRepo struct {
	garantia *domain.Garantia
	eventos  []*domain.Evento
	err      error
	calls    int
	ultimoID uuid.UUID
}

func (f *fakeHTTPGarantiaRepo) Crear(
	_ context.Context,
	_ *domain.Garantia,
) error {
	return nil
}

func (f *fakeHTTPGarantiaRepo) Guardar(
	_ context.Context,
	_ *domain.Garantia,
) error {
	return nil
}

func (f *fakeHTTPGarantiaRepo) ObtenerParaActualizar(
	_ context.Context,
	_ uuid.UUID,
) (*domain.Garantia, error) {
	return nil, nil
}

func (f *fakeHTTPGarantiaRepo) Obtener(
	_ context.Context,
	id uuid.UUID,
) (*domain.Garantia, error) {
	f.calls++
	f.ultimoID = id

	return f.garantia, f.err
}

func (f *fakeHTTPGarantiaRepo) ObtenerDetalle(
	_ context.Context,
	id uuid.UUID,
) (*domain.Garantia, []*domain.Evento, error) {
	f.calls++
	f.ultimoID = id

	return f.garantia, f.eventos, f.err
}

func (f *fakeHTTPGarantiaRepo) ObtenerPorFolio(
	_ context.Context,
	_ domain.Folio,
) (*domain.Garantia, error) {
	return nil, nil
}

type fakeHTTPEventoRepo struct {
	eventos  []*domain.Evento
	err      error
	calls    int
	ultimoID uuid.UUID
}

func (f *fakeHTTPEventoRepo) ListarPorGarantia(
	_ context.Context,
	garantiaID uuid.UUID,
) ([]*domain.Evento, error) {
	f.calls++
	f.ultimoID = garantiaID

	return f.eventos, f.err
}

func (f *fakeHTTPEventoRepo) ObtenerPorClaveIdempotencia(
	_ context.Context,
	_ string,
) (*domain.Evento, error) {
	return nil, nil
}

func buildHTTPRouter(
	identity outbound.Identity,
	garantias outbound.GarantiaRepo,
	bandeja outbound.BandejaRepo,
	eventos outbound.EventoRepo,
) http.Handler {
	if garantiaFake, ok := garantias.(*fakeHTTPGarantiaRepo); ok {
		if eventoFake, ok := eventos.(*fakeHTTPEventoRepo); ok {
			garantiaFake.eventos = eventoFake.eventos
			if garantiaFake.err == nil && eventoFake.err != nil {
				garantiaFake.err = eventoFake.err
			}
		}
	}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: garantias,
		Bandeja:   bandeja,
		Eventos:   eventos,
		Identity:  identity,
	})

	r := chi.NewRouter()
	garhttp.MountRouter(r, garhttp.Deps{
		Consultas: consultas,
	})

	return r
}

func fixtureHTTPGarantia() (*domain.Garantia, *domain.Evento) {
	garantiaID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	articuloID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	eventoID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	clienteID := 25
	ventaID := 80
	articuloMicrosipID := 150
	estadoCuenta := domain.EstadoCuentaLiquidada
	ruta := domain.RutaReparacionTaller
	rolDecisor := domain.RolDecisorTecnica

	lat := 19.4326
	lon := -99.1332

	vigencia := time.Date(
		2027, time.January, 31,
		0, 0, 0, 0,
		time.UTC,
	)

	zonaMexico := time.FixedZone("CST", -6*60*60)

	createdAt := time.Date(
		2026, time.October, 13,
		9, 0, 0, 0,
		zonaMexico,
	)

	updatedAt := time.Date(
		2026, time.October, 13,
		9, 30, 0, 0,
		zonaMexico,
	)

	articulo := domain.HydrateArticulo(domain.HydrateArticuloParams{
		ID:          articuloID,
		GarantiaID:  garantiaID,
		Rol:         domain.RolArticuloOriginal,
		ArticuloID:  &articuloMicrosipID,
		Clave:       "SALA-001",
		Description: "Respaldo dañado",
		Ruta:        &ruta,
		Etapa:       domain.EtapaEnTaller,
		Ubicacion:   domain.UbicacionTaller,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	})

	garantia := domain.HydrateGarantia(domain.HydrateGarantiaParams{
		ID:             garantiaID,
		Folio:          domain.Folio("GA-000123"),
		Origen:         domain.OrigenFolioCliente,
		ClienteID:      &clienteID,
		VentaID:        &ventaID,
		EstadoCuenta:   &estadoCuenta,
		Estado:         domain.EstadoFolioEnProceso,
		Description:    "Garantía de sala",
		VigenciaHasta:  &vigencia,
		Calle:          "Reforma",
		NumeroExterior: "123",
		Colonia:        "Centro",
		Localidad:      "Centro",
		Ciudad:         "Ciudad de México",
		CodigoPostal:   "06000",
		GPSLat:         &lat,
		GPSLon:         &lon,
		AbiertoPor:     "ruben",
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		Articulos:      []*domain.Articulo{articulo},
	})

	etapaDesde := domain.EtapaEnRevision
	etapaHasta := domain.EtapaEnTaller

	evento := domain.HydrateEvento(domain.HydrateEventoParams{
		ID:                eventoID,
		GarantiaID:        garantiaID,
		ArticuloRef:       &articuloID,
		Tipo:              domain.TipoEventoDiagnosticoRegistrado,
		Description:       "Se envía al taller",
		EtapaDesde:        &etapaDesde,
		EtapaHasta:        &etapaHasta,
		Usuario:           "ruben",
		RolDecisor:        &rolDecisor,
		GPSLat:            &lat,
		GPSLon:            &lon,
		CreatedAt:         updatedAt,
		DeviceCreatedAt:   updatedAt,
		ClaveIdempotencia: "44444444-4444-4444-4444-444444444444",
	})

	return garantia, evento
}
