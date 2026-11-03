//nolint:misspell // vocabulario español (comando, Comandos) por convención del proyecto.
package garhttp_test

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garhttp"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

// fakeComandoGarantiaRepo is the write-side repo of the command tests. The
// aggregate handed to the command (paraActualizar) is separate from the one
// returned to the client (respuesta): the domain preconditions of each route
// vary, but every route answers with the same full folio so the JSON assertion
// is written once.
type fakeComandoGarantiaRepo struct {
	paraActualizar    *domain.Garantia
	respuesta         *domain.Garantia
	errParaActualizar error
	errCrear          error
	errGuardar        error
	creados           []*domain.Garantia
	guardados         int
}

func (f *fakeComandoGarantiaRepo) Crear(_ context.Context, g *domain.Garantia) error {
	f.creados = append(f.creados, g)
	return f.errCrear
}

func (f *fakeComandoGarantiaRepo) Guardar(_ context.Context, _ *domain.Garantia) error {
	f.guardados++
	return f.errGuardar
}

func (f *fakeComandoGarantiaRepo) ObtenerParaActualizar(
	_ context.Context,
	_ uuid.UUID,
) (*domain.Garantia, error) {
	if f.errParaActualizar != nil {
		return nil, f.errParaActualizar
	}
	return f.paraActualizar, nil
}

func (f *fakeComandoGarantiaRepo) Obtener(
	_ context.Context,
	_ uuid.UUID,
) (*domain.Garantia, error) {
	return f.respuesta, nil
}

func (f *fakeComandoGarantiaRepo) ObtenerDetalle(
	_ context.Context,
	_ uuid.UUID,
) (*domain.Garantia, []*domain.Evento, error) {
	return f.respuesta, nil, nil
}

func (f *fakeComandoGarantiaRepo) ObtenerPorFolio(
	_ context.Context,
	_ domain.Folio,
) (*domain.Garantia, error) {
	return f.respuesta, nil
}

// fakeComandoEventoRepo records the idempotency key the handler passes to the
// command, which is what proves the header is not regenerated. It also feeds
// the delivery audit of Cerrar (empty by default: no_aplica).
type fakeComandoEventoRepo struct {
	eventos     []*domain.Evento
	errListar   error
	errBuscar   error
	vista       *domain.Evento
	ultimaClave string
	busquedas   int
}

func (f *fakeComandoEventoRepo) ListarPorGarantia(
	_ context.Context,
	_ uuid.UUID,
) ([]*domain.Evento, error) {
	return f.eventos, f.errListar
}

func (f *fakeComandoEventoRepo) ObtenerPorClaveIdempotencia(
	_ context.Context,
	clave string,
) (*domain.Evento, error) {
	f.ultimaClave = clave
	f.busquedas++
	return f.vista, f.errBuscar
}

// fakeComandoImagenRepo satisfies the port; the JSON routes never write images.
type fakeComandoImagenRepo struct{}

func (fakeComandoImagenRepo) Registrar(_ context.Context, _ *domain.Imagen) error { return nil }

func (fakeComandoImagenRepo) Obtener(_ context.Context, _ uuid.UUID) (*domain.Imagen, error) {
	return nil, domain.ErrImagenNoEncontrada
}

func (fakeComandoImagenRepo) ListarPorEvento(
	_ context.Context,
	_ uuid.UUID,
) ([]*domain.Imagen, error) {
	return nil, nil
}

func (fakeComandoImagenRepo) ListarPorEventos(
	_ context.Context,
	_ []uuid.UUID,
) ([]*domain.Imagen, error) {
	return nil, nil
}

// fakeComandoStorage satisfies the port; no JSON route stores a blob.
type fakeComandoStorage struct{}

func (fakeComandoStorage) Store(
	_ context.Context,
	_ string,
	_ string,
	_ int64,
	_ io.Reader,
) error {
	return nil
}

func (fakeComandoStorage) Get(_ context.Context, _ string) (outbound.StorageObject, error) {
	return outbound.StorageObject{}, nil
}

func (fakeComandoStorage) Delete(_ context.Context, _ string) error { return nil }

// fakeComandoIdentity is the authenticated operator of the command tests.
type fakeComandoIdentity struct {
	usuario    outbound.Usuario
	usuarioErr error
	permitido  bool
	permisoErr error
}

func (f *fakeComandoIdentity) UsuarioActual(_ context.Context) (outbound.Usuario, error) {
	return f.usuario, f.usuarioErr
}

func (f *fakeComandoIdentity) TienePermiso(_ context.Context, _ domain.Permiso) (bool, error) {
	return f.permitido, f.permisoErr
}

// fakeComandoTx runs the callback inline: the fakes model a commit directly.
type fakeComandoTx struct{}

func (fakeComandoTx) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// fakeComandoClock freezes the time the events are stamped with.
type fakeComandoClock struct{}

func (fakeComandoClock) Now() time.Time {
	return time.Date(2026, time.October, 13, 15, 0, 0, 0, time.UTC)
}

// fakeComandoFolios hands out one stable folio number.
type fakeComandoFolios struct{}

func (fakeComandoFolios) Siguiente(_ context.Context) (int, error) { return 123, nil }

// buildCommandRouter mounts the real HTTP router over the fake outbound ports.
func buildCommandRouter(
	identity outbound.Identity,
	garantias outbound.GarantiaRepo,
	eventos outbound.EventoRepo,
) http.Handler {
	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: garantias,
		Eventos:   eventos,
		Identity:  identity,
	})

	servicio := app.NewService(app.Deps{
		GarantiaRepo:   garantias,
		EventoRepo:     eventos,
		ImagenRepo:     fakeComandoImagenRepo{},
		FolioGenerator: fakeComandoFolios{},
		TxRunner:       fakeComandoTx{},
		Identity:       identity,
		Clock:          fakeComandoClock{},
		Storage:        fakeComandoStorage{},
	})

	r := chi.NewRouter()
	garhttp.MountRouter(r, garhttp.Deps{
		Consultas: consultas,
		Comandos:  servicio,
	})

	return r
}

// folioDeComando builds the aggregate each route needs to satisfy its domain
// preconditions. The wire response is a separate fixture (fixtureHTTPGarantia),
// so only the precondition varies here.
func folioDeComando(estado domain.EstadoFolio, etapa domain.Etapa, ubicacion domain.Ubicacion, ruta *domain.RutaReparacion) *domain.Garantia {
	garantiaID := uuid.MustParse(idGarantiaEjemplo)
	articuloID := uuid.MustParse(idArticuloEjemplo)

	clienteID := 25
	ventaID := 80
	articuloMicrosipID := 150
	estadoCuenta := domain.EstadoCuentaLiquidada

	lat := 19.4326
	lon := -99.1332

	vigencia := time.Date(2027, time.January, 31, 0, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, time.October, 13, 9, 30, 0, 0, time.UTC)

	articulo := domain.HydrateArticulo(domain.HydrateArticuloParams{
		ID:          articuloID,
		GarantiaID:  garantiaID,
		Rol:         domain.RolArticuloOriginal,
		ArticuloID:  &articuloMicrosipID,
		Clave:       "SALA-001",
		Description: "Respaldo dañado",
		Ruta:        ruta,
		Etapa:       etapa,
		Ubicacion:   ubicacion,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	})

	return domain.HydrateGarantia(domain.HydrateGarantiaParams{
		ID:             garantiaID,
		Folio:          domain.Folio("GA-000123"),
		Origen:         domain.OrigenFolioCliente,
		ClienteID:      &clienteID,
		VentaID:        &ventaID,
		EstadoCuenta:   &estadoCuenta,
		Estado:         estado,
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
}

// folioPara returns the precondition aggregate of each operation.
//
//nolint:cyclop // one case per route, each one a fixture.
func folioPara(operationID string) *domain.Garantia {
	rutaTaller := domain.RutaReparacionTaller
	rutaProveedor := domain.RutaReparacionProveedor

	switch operationID {
	case "abrir-garantia":
		// AbrirGarantia creates the aggregate; no precondition to load.
		return nil
	case "iniciar-proceso":
		return folioDeComando(domain.EstadoFolioAbierto, domain.EtapaRegistrado, domain.UbicacionAlmacenRevision, nil)
	case "marcar-listo-entrega":
		return folioDeComando(domain.EstadoFolioEnProceso, domain.EtapaListoEntrega, domain.UbicacionAlmacenRevision, nil)
	case "cerrar-garantia":
		return folioDeComando(domain.EstadoFolioEntregado, domain.EtapaEntregado, domain.UbicacionEntregado, nil)
	case "cancelar-garantia":
		return folioDeComando(domain.EstadoFolioEnProceso, domain.EtapaEnTaller, domain.UbicacionTaller, &rutaTaller)
	case "agregar-articulo":
		return folioDeComando(domain.EstadoFolioEnProceso, domain.EtapaEnTaller, domain.UbicacionTaller, &rutaTaller)
	case "avanzar-articulo":
		return folioDeComando(domain.EstadoFolioEnProceso, domain.EtapaEnTaller, domain.UbicacionTaller, &rutaTaller)
	case "registrar-diagnostico":
		return folioDeComando(domain.EstadoFolioEnProceso, domain.EtapaEnRevision, domain.UbicacionAlmacenRevision, nil)
	case "registrar-dictamen":
		return folioDeComando(domain.EstadoFolioEnProceso, domain.EtapaDictamenRecibido, domain.UbicacionProveedor, &rutaProveedor)
	case "mover-articulo":
		return folioDeComando(domain.EstadoFolioEnProceso, domain.EtapaEnTaller, domain.UbicacionTaller, &rutaTaller)
	case "autorizar-cambio-fisico":
		return folioDeComando(domain.EstadoFolioEnProceso, domain.EtapaEnTaller, domain.UbicacionTaller, &rutaTaller)
	case "marcar-no-devuelto":
		return folioDeComando(domain.EstadoFolioEnProceso, domain.EtapaEsperaRespuestaCliente, domain.UbicacionTaller, &rutaTaller)
	case "registrar-desenlace":
		return folioDeComando(domain.EstadoFolioEnProceso, domain.EtapaStandby, domain.UbicacionAlmacenSegundaMano, &rutaTaller)
	default:
		panic("operación sin fixture: " + operationID)
	}
}

var (
	_ outbound.GarantiaRepo    = (*fakeComandoGarantiaRepo)(nil)
	_ outbound.EventoRepo      = (*fakeComandoEventoRepo)(nil)
	_ outbound.ImagenRepo      = fakeComandoImagenRepo{}
	_ outbound.StorageProvider = fakeComandoStorage{}
	_ outbound.Identity        = (*fakeComandoIdentity)(nil)
	_ outbound.TxRunner        = fakeComandoTx{}
	_ outbound.Clock           = fakeComandoClock{}
	_ outbound.FolioGenerator  = fakeComandoFolios{}
)
