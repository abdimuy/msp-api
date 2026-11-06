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
	imagenes          *fakeComandoImagenRepo
	eventos           *fakeComandoEventoRepo
	errParaActualizar error
	errCrear          error
	errGuardar        error
	creados           []*domain.Garantia
	guardados         int
}

func (f *fakeComandoGarantiaRepo) Crear(_ context.Context, g *domain.Garantia) error {
	f.creados = append(f.creados, g)
	if f.errCrear != nil {
		return f.errCrear
	}
	f.registrarPendientes(g)

	return nil
}

func (f *fakeComandoGarantiaRepo) Guardar(_ context.Context, g *domain.Garantia) error {
	f.guardados++
	if f.errGuardar != nil {
		return f.errGuardar
	}
	f.registrarPendientes(g)

	return nil
}

// registrarPendientes mimics the write side of the real repository: on a
// successful save the aggregate's new events and images are visible to the
// next lookup. Staging only after the error check models the rollback of a
// failed save.
func (f *fakeComandoGarantiaRepo) registrarPendientes(g *domain.Garantia) {
	if f.eventos != nil {
		f.eventos.eventos = append(f.eventos.eventos, g.EventosPendientesForRepo()...)
	}

	if f.imagenes != nil {
		f.imagenes.registradas = append(f.imagenes.registradas, g.ImagenesPendientesForRepo()...)

		for _, imagen := range g.ImagenesPendientesForRepo() {
			f.imagenes.porID[imagen.ID()] = imagen
		}
	}
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
// the delivery audit of Cerrar (empty by default: no_aplica) and, when no
// explicit vista is set, resolves a key against the events the fake repo
// staged, the same way the UNIQUE index would find the twin.
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

	if f.errBuscar != nil {
		return nil, f.errBuscar
	}

	if f.vista != nil {
		return f.vista, nil
	}

	for _, evento := range f.eventos {
		if evento.ClaveIdempotencia() == clave {
			return evento, nil
		}
	}

	//nolint:nilnil // igual que el repositorio real: la clave no existe todavía.
	return nil, nil
}

// eventosConClave returns the events staged under one idempotency key.
func (f *fakeComandoEventoRepo) eventosConClave(clave string) []*domain.Evento {
	var out []*domain.Evento

	for _, evento := range f.eventos {
		if evento.ClaveIdempotencia() == clave {
			out = append(out, evento)
		}
	}

	return out
}

// fakeComandoImagenRepo keeps the signature rows staged by the fake folio repo
// and records every lookup, so a test can prove the cleanup asked about the
// exact image id the command derived. A configured error exercises the path
// that keeps the blob when the lookup itself fails.
type fakeComandoImagenRepo struct {
	porID       map[uuid.UUID]*domain.Imagen
	registradas []*domain.Imagen
	errObtener  error
}

func (f *fakeComandoImagenRepo) Registrar(_ context.Context, imagen *domain.Imagen) error {
	f.registradas = append(f.registradas, imagen)

	if f.porID == nil {
		f.porID = make(map[uuid.UUID]*domain.Imagen)
	}

	f.porID[imagen.ID()] = imagen

	return nil
}

func (f *fakeComandoImagenRepo) Obtener(_ context.Context, id uuid.UUID) (*domain.Imagen, error) {
	if f.errObtener != nil {
		return nil, f.errObtener
	}

	if imagen, ok := f.porID[id]; ok {
		return imagen, nil
	}

	return nil, domain.ErrImagenNoEncontrada
}

func (f *fakeComandoImagenRepo) ListarPorEvento(
	_ context.Context,
	_ uuid.UUID,
) ([]*domain.Imagen, error) {
	return nil, nil
}

func (f *fakeComandoImagenRepo) ListarPorEventos(
	_ context.Context,
	_ []uuid.UUID,
) ([]*domain.Imagen, error) {
	return nil, nil
}

// fakeComandoStorage records every blob written and deleted. The assertions of
// the delivery tests are about exactly that: one write, and the delete only
// when the row did not keep that route.
type fakeComandoStorage struct {
	guardados []string
	borrados  []string
	errStore  error
	errDelete error
}

func (f *fakeComandoStorage) Store(
	_ context.Context,
	clave string,
	_ string,
	_ int64,
	_ io.Reader,
) error {
	if f.errStore != nil {
		return f.errStore
	}

	f.guardados = append(f.guardados, clave)

	return nil
}

func (f *fakeComandoStorage) Get(_ context.Context, _ string) (outbound.StorageObject, error) {
	return outbound.StorageObject{}, nil
}

func (f *fakeComandoStorage) Delete(_ context.Context, clave string) error {
	if f.errDelete != nil {
		return f.errDelete
	}

	f.borrados = append(f.borrados, clave)

	return nil
}

// fakeComandoIdentity is the authenticated operator of the command tests. It
// records the permissions app asked for, because the route's permission is a
// rule the handler must not decide and the fake must not hide.
type fakeComandoIdentity struct {
	usuario    outbound.Usuario
	usuarioErr error
	permitido  bool
	permisoErr error
	permisos   []domain.Permiso
}

func (f *fakeComandoIdentity) UsuarioActual(_ context.Context) (outbound.Usuario, error) {
	return f.usuario, f.usuarioErr
}

func (f *fakeComandoIdentity) TienePermiso(_ context.Context, permiso domain.Permiso) (bool, error) {
	f.permisos = append(f.permisos, permiso)
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
	return buildCommandRouterCon(
		identity,
		garantias,
		eventos,
		&fakeComandoImagenRepo{},
		&fakeComandoStorage{},
	)
}

// buildCommandRouterCon es la variante que recibe las dependencias de la
// firma: las pruebas de entregar-garantia inspeccionan la imagen staged y los
// blobs guardados/borrados después de la petición.
func buildCommandRouterCon(
	identity outbound.Identity,
	garantias outbound.GarantiaRepo,
	eventos outbound.EventoRepo,
	imagenes outbound.ImagenRepo,
	storage outbound.StorageProvider,
) http.Handler {
	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: garantias,
		Eventos:   eventos,
		Identity:  identity,
	})

	servicio := app.NewService(app.Deps{
		GarantiaRepo:   garantias,
		EventoRepo:     eventos,
		ImagenRepo:     imagenes,
		FolioGenerator: fakeComandoFolios{},
		TxRunner:       fakeComandoTx{},
		Identity:       identity,
		Clock:          fakeComandoClock{},
		Storage:        storage,
	})

	r := chi.NewRouter()
	garhttp.MountRouter(r, garhttp.Deps{
		Consultas: consultas,
		Comandos:  servicio,
	})

	return r
}

// comandoFakes is the full set of ports a command test may need to inspect.
type comandoFakes struct {
	router      http.Handler
	identity    *fakeComandoIdentity
	repositorio *fakeComandoGarantiaRepo
	eventos     *fakeComandoEventoRepo
	imagenes    *fakeComandoImagenRepo
	storage     *fakeComandoStorage
}

// nuevoRouterCompleto wires the fakes to each other so a successful save stages
// its events and images where the next lookup finds them, like the real repos
// do inside the test transaction.
func nuevoRouterCompleto(op string) *comandoFakes {
	return nuevoRouterCompletoCon(op, operadorAutenticado())
}

// nuevoRouterCompletoCon is the same wiring with a caller-supplied identity,
// for the tests that need to see which permission the command asked for.
func nuevoRouterCompletoCon(op string, identity *fakeComandoIdentity) *comandoFakes {
	imagenes := &fakeComandoImagenRepo{porID: make(map[uuid.UUID]*domain.Imagen)}
	storage := &fakeComandoStorage{}
	eventos := &fakeComandoEventoRepo{}
	repositorio := &fakeComandoGarantiaRepo{
		paraActualizar: folioPara(op),
		respuesta:      fixtureRespuestaComando(),
		imagenes:       imagenes,
		eventos:        eventos,
	}

	return &comandoFakes{
		router:      buildCommandRouterCon(identity, repositorio, eventos, imagenes, storage),
		identity:    identity,
		repositorio: repositorio,
		eventos:     eventos,
		imagenes:    imagenes,
		storage:     storage,
	}
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
	case "entregar-garantia":
		return folioDeComando(domain.EstadoFolioListoEntrega, domain.EtapaListoEntrega, domain.UbicacionAlmacenRevision, nil)
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
	_ outbound.ImagenRepo      = (*fakeComandoImagenRepo)(nil)
	_ outbound.StorageProvider = (*fakeComandoStorage)(nil)
	_ outbound.Identity        = (*fakeComandoIdentity)(nil)
	_ outbound.TxRunner        = fakeComandoTx{}
	_ outbound.Clock           = fakeComandoClock{}
	_ outbound.FolioGenerator  = fakeComandoFolios{}
)
