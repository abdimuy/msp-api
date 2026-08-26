package main

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"sync/atomic"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/auth"
	authoutbound "github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	cobranzafailedintents "github.com/abdimuy/msp-api/internal/cobranza/infra/failedintents"
	apperror "github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintentblobfs "github.com/abdimuy/msp-api/internal/platform/failedintent/blobfs"
	failedintentfb "github.com/abdimuy/msp-api/internal/platform/failedintent/firebird"
	failedintenthttp "github.com/abdimuy/msp-api/internal/platform/failedintent/http"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/lifecycle"
	"github.com/abdimuy/msp-api/internal/platform/response"
	ventasfailedintents "github.com/abdimuy/msp-api/internal/ventas/infra/failedintents"
)

// SettableReplayDispatcher wraps the root chi router but allows the router to
// be assigned AFTER construction. It breaks the natural dependency cycle:
//
//	router → admin handler → dispatcher → router
//
// The fx providers build the dispatcher first with a nil handler; once the
// router has been assembled, an fx.Invoke calls Set to publish it. Loads use
// atomic.Pointer so Dispatch is safe to call concurrently with Set.
type SettableReplayDispatcher struct {
	h atomic.Pointer[http.Handler]
}

// NewSettableReplayDispatcher builds an unwired dispatcher. Use Set to
// publish the chi router once it has been assembled.
func NewSettableReplayDispatcher() *SettableReplayDispatcher {
	return &SettableReplayDispatcher{}
}

// Set publishes h as the dispatcher's target. Safe to call any number of
// times; the latest call wins.
func (d *SettableReplayDispatcher) Set(h http.Handler) {
	d.h.Store(&h)
}

// Dispatch forwards r to the published handler. When called before the
// router has been wired (Set never invoked) it responds with a stable 5xx
// apperror so the failure is observable in logs instead of panicking.
func (d *SettableReplayDispatcher) Dispatch(w http.ResponseWriter, r *http.Request) {
	ptr := d.h.Load()
	if ptr == nil {
		response.Error(w, r, apperror.NewInternal(
			"failedintent_replay_unwired",
			"el dispatcher de replay aún no está listo",
		))
		return
	}
	(*ptr).ServeHTTP(w, r)
}

// usuarioLookup adapts the auth UsuarioRepo into the narrow port that
// failedintenthttp.Service depends on. Lives in cmd/api so the http subpackage
// stays decoupled from the auth domain.
type usuarioLookup struct {
	repo authoutbound.UsuarioRepo
}

// BuildCurrentUserByID reconstructs the auth.CurrentUser for the original
// requester. Mirrors the projection performed by authhttp.AuthnMiddleware so
// replays observe the exact same effective permission set.
func (u *usuarioLookup) BuildCurrentUserByID(
	ctx context.Context, id uuid.UUID,
) (auth.CurrentUser, error) {
	usuario, err := u.repo.FindByID(ctx, id)
	if err != nil {
		return auth.CurrentUser{}, err
	}
	if !usuario.Activo() {
		return auth.CurrentUser{}, apperror.NewForbidden(
			"user_inactive",
			"el usuario asociado al intento se encuentra inactivo",
		)
	}
	perms, err := u.repo.PermisosFor(ctx, usuario.ID())
	if err != nil {
		return auth.CurrentUser{}, err
	}
	return auth.ToContract(usuario, perms), nil
}

var _ failedintenthttp.UsuarioLookup = (*usuarioLookup)(nil)

// provideFailedIntentStore builds the Firebird-backed Store. The concrete
// type is exposed alongside the interface so the orphan-sweep wiring can
// consume the ReferencedPaths method without dragging it into the Store
// interface from cross-package callers.
func provideFailedIntentStore(p *firebird.Pool) *failedintentfb.Store {
	return failedintentfb.New(p)
}

// provideFailedIntentStoreInterface narrows the concrete *firebird.Store to
// the Store interface that consumers depend on.
func provideFailedIntentStoreInterface(s *failedintentfb.Store) failedintent.Store {
	return s
}

// provideFailedIntentBlobStorage builds the filesystem-backed BlobStorage
// adapter rooted at the resolved blob dir (defaults to
// STORAGE_DIR/failed-intents).
func provideFailedIntentBlobStorage(cfg *config.Config) (*failedintentblobfs.Store, error) {
	return failedintentblobfs.New(cfg.FailedIntentBlobDir())
}

// provideFailedIntentBlobStorageInterface exposes the concrete blobfs.Store
// as the BlobStorage interface that the http subpackage and the middleware
// consume.
func provideFailedIntentBlobStorageInterface(s *failedintentblobfs.Store) failedintent.BlobStorage {
	return s
}

// provideFailedIntentResumenExtractor arma el registro de extractores de
// resumen: el ÚNICO sitio donde la plataforma de captura se entera de que
// existen módulos con forma propia.
//
// Cada línea asocia un prefijo de ruta con el nombre del módulo y su
// implementación del puerto. Añadir un módulo a la pantalla de intentos
// fallidos es exactamente eso: una implementación y una línea aquí. Ni la
// migración, ni el DTO, ni el escritorio se tocan — el escritorio lee el
// módulo que le manda el servidor en vez de deducirlo de la ruta.
//
// Los nombres de módulo son los que el escritorio usa en sus chips ('ventas',
// 'pagos'), no los de los paquetes Go. Es un valor de presentación que viaja
// por el DTO; renombrarlo aquí renombra el chip.
//
// `visitas` NO tiene extractor a propósito: se captura, pero su cuerpo no
// tiene ni cliente ni monto que valga un renglón. Sus filas quedan con MODULO
// nulo y el escritorio las degrada a "otro", que es lo honesto.
func provideFailedIntentResumenExtractor() failedintent.ResumenExtractor {
	return failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", ventasfailedintents.NewResumenExtractor()).
		Registrar("/v2/cobranza/pagos", "pagos", cobranzafailedintents.NewResumenExtractor())
}

// failedIntentCapturas es el único sitio donde se declara qué prefijo captura
// cada módulo. La ruta raíz de un módulo ES exactamente su prefijo de
// captura, así que la lista que alimenta el listado (RutasRaiz) se deriva de
// aquí en vez de escribirse aparte: no hay una segunda lista con la que
// desincronizarse: registrar un capturador nuevo y olvidar el listado deja de
// ser posible.
//
// Lo que sí puede pasar es registrar uno sin pensar en la pantalla. Contra eso
// está TestFailedIntentRutasRaiz_SonLasTresConocidas, que ancla el conjunto:
// añadir un cuarto módulo rompe la prueba y obliga a decidir a propósito si su
// ruta raíz entra al listado.
type failedIntentCapturas struct {
	Ventas   failedintent.Config
	Cobranza failedintent.Config
	Visitas  failedintent.Config
}

// todas devuelve los tres capturadores en orden fijo. El orden importa para
// que RutasRaiz produzca una salida estable.
func (c failedIntentCapturas) todas() []failedintent.Config {
	return []failedintent.Config{c.Ventas, c.Cobranza, c.Visitas}
}

// RutasRaiz devuelve la unión de los PathPrefixes de los tres capturadores,
// deduplicada y en el orden estable de todas() — sin mapas, para que el
// orden no dependa de la iteración no determinista de Go sobre mapas.
func (c failedIntentCapturas) RutasRaiz() []string {
	var rutas []string
	for _, cfg := range c.todas() {
		for _, p := range cfg.PathPrefixes {
			if !slices.Contains(rutas, p) {
				rutas = append(rutas, p)
			}
		}
	}
	return rutas
}

// provideFailedIntentCapturas assembles the CaptureMiddleware config for each
// captured module. It replaces provideFailedIntentCaptureConfig: only the
// ventas config lived here, while cobranza's and visitas' were built inline
// inside provideRootHandler — their prefixes were declared nowhere a caller
// could read them, which is why the listing could not derive its own list.
func provideFailedIntentCapturas(
	store failedintent.Store,
	blob failedintent.BlobStorage,
	resumen failedintent.ResumenExtractor,
	cfg *config.Config,
) failedIntentCapturas {
	ventas := failedintent.Config{
		Store:             store,
		Blob:              blob,
		Resumen:           resumen,
		MaxMultipartBytes: cfg.FailedIntent.MaxMultipartBytes,
		// Explícito porque hoy es el default implícito de Config.defaults();
		// un prefijo implícito no aparece en PathPrefixes hasta que
		// defaults() corre dentro del middleware, y para entonces ya es
		// tarde para la unión que arma RutasRaiz — declararlo aquí es la
		// única forma de que la ruta de ventas entre al listado.
		PathPrefixes: []string{"/v2/ventas"},
	}

	// cobranza is a second capture instance scoped to the cobranza pago
	// WRITE path. Pagos are real money: a POST /v2/cobranza/pagos that the
	// server rejects (422 pago_cargo_no_encontrado / pago_fecha_muy_antigua /
	// importe_excede_saldo, etc.) must leave a durable audit row a human can
	// inspect, correct via /replay-with-multipart, and re-dispatch — never a
	// silently-lost payment. Reuses the same Store, Blob and size cap; only
	// POST is captured (GET reads and the streaming imagen downloads never
	// match). Idempotency stays at the repo layer (body.id), so no idem
	// middleware is added here.
	cobranza := failedintent.Config{
		Store:             store,
		Blob:              blob,
		Resumen:           resumen,
		MaxMultipartBytes: cfg.FailedIntent.MaxMultipartBytes,
		PathPrefixes:      []string{"/v2/cobranza/pagos"},
		Methods:           []string{http.MethodPost},
	}

	// visitas is a third capture instance scoped to the visitas write path.
	// JSON-only (no multipart, no Blob/MaxMultipartBytes) — a visita has no
	// comprobante attachments, unlike a pago.
	visitas := failedintent.Config{
		Store:   store,
		Resumen: resumen,
		// El registro no tiene extractor para /v2/visitas, así que estas
		// filas quedan sin módulo y sin resumen. Se pasa igual para que el
		// día que visitas quiera un renglón legible baste registrar su
		// extractor — nada más en esta línea puede olvidarse.
		PathPrefixes: []string{"/v2/visitas"},
		Methods:      []string{http.MethodPost},
	}

	return failedIntentCapturas{
		Ventas:   ventas,
		Cobranza: cobranza,
		Visitas:  visitas,
	}
}

// provideSettableReplayDispatcher constructs the cycle-breaking dispatcher.
func provideSettableReplayDispatcher() *SettableReplayDispatcher {
	return NewSettableReplayDispatcher()
}

// provideReplayDispatcher exposes the settable dispatcher as the interface
// the http subpackage consumes.
func provideReplayDispatcher(d *SettableReplayDispatcher) failedintent.ReplayDispatcher {
	return d
}

// provideFailedIntentUsuarioLookup adapts the auth UsuarioRepo for replay.
func provideFailedIntentUsuarioLookup(repo authoutbound.UsuarioRepo) failedintenthttp.UsuarioLookup {
	return &usuarioLookup{repo: repo}
}

// provideFailedIntentHTTPService wires the admin handlers. The blob storage
// is required so /replay can stream multipart bodies from disk back through
// the dispatcher byte-exact.
//
// capturas.RutasRaiz() acota el listado por defecto a la etapa 1: la
// pantalla existe para las creaciones que no llegaron a quedar en ninguna
// tabla — un intento con id ya tiene fila, así que no pertenece aquí por
// default. Esas rutas son exactamente los prefijos que cada capturador
// declara, por eso se derivan de capturas en vez de escribirse aparte.
func provideFailedIntentHTTPService(
	store failedintent.Store,
	dispatcher failedintent.ReplayDispatcher,
	usuarios failedintenthttp.UsuarioLookup,
	blobs failedintent.BlobStorage,
	capturas failedIntentCapturas,
) *failedintenthttp.Service {
	return failedintenthttp.NewService(store, dispatcher, usuarios, blobs, nil, nil, capturas.RutasRaiz())
}

// provideFailedIntentResolutionChecker conecta el puerto invertido de
// conciliación con la implementación de ventas.
//
// Aquí es donde la plataforma se entera de que existe un módulo capaz de
// contestar por sus claves — y es el ÚNICO lugar donde eso pasa. El día que
// cobranza quiera lo mismo, este proveedor devuelve un compuesto que pregunta
// a los dos y la plataforma no cambia.
func provideFailedIntentResolutionChecker(pool *firebird.Pool) failedintent.ResolutionChecker {
	return ventasfailedintents.NewResolutionChecker(pool)
}

// provideFailedIntentJanitor builds the background purge component wired to
// delete blobs alongside their parent rows, cerrar los intentos cuyo trabajo
// ya aterrizó y aplicar los dos cortes de retención.
func provideFailedIntentJanitor(
	store failedintent.Store,
	blobs failedintent.BlobStorage,
	resolution failedintent.ResolutionChecker,
	resumen failedintent.ResumenExtractor,
) *failedintent.Janitor {
	return failedintent.NewJanitor(failedintent.JanitorConfig{
		Store:      store,
		Blob:       blobs,
		Resolution: resolution,
		Resumen:    resumen,
	})
}

// wireReplayDispatcher publishes the assembled root handler to the
// dispatcher. Runs as an fx.Invoke AFTER all providers have built — at that
// point the chi router exists and Dispatch can safely forward to it.
func wireReplayDispatcher(d *SettableReplayDispatcher, root RootHandler) {
	d.Set(root)
}

// registerFailedIntentJanitorLifecycle hooks the janitor into the fx
// lifecycle so it starts at boot and drains at shutdown.
func registerFailedIntentJanitorLifecycle(lc fx.Lifecycle, j *failedintent.Janitor) {
	lifecycle.Append(lc, "failedintent-janitor", j)
}

// invokeFailedIntentOrphanSweep registers a boot-time sweep that removes
// .bin files left behind by a previous run (crashed after rename, dropped
// row via a manual migration, etc.). Failures are logged, not fatal — the
// service still boots when the sweep cannot run.
func invokeFailedIntentOrphanSweep(
	lc fx.Lifecycle,
	store *failedintentfb.Store,
	blobs *failedintentblobfs.Store,
) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			report, err := failedintentblobfs.SweepOrphans(ctx, blobs, store)
			if err != nil {
				slog.WarnContext(
					ctx,
					"failedintent: orphan sweep failed at boot",
					"error", err,
				)
				return nil
			}
			slog.InfoContext(
				ctx,
				"failedintent: orphan sweep complete",
				"scanned", report.Scanned,
				"deleted", report.Deleted,
			)
			return nil
		},
	})
}
