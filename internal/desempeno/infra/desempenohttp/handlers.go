//nolint:misspell // vocabulario español por convención del proyecto.
package desempenohttp

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Handlers agrupa las operaciones Huma del módulo. Todas exigen desempeno:ver.
type Handlers struct {
	svc *desempenoapp.Service
	// tope es el STATEMENT TIMEOUT de Firebird de cada lectura del módulo
	// (0 → el del proceso). Ver WithStatementTimeout.
	tope time.Duration
	log  *slog.Logger
	// tablero junta peticiones idénticas simultáneas en un solo cálculo.
	tablero singleflight.Group
}

// Option configura MountRouter.
type Option func(*Handlers)

// WithStatementTimeout acota a d el tiempo que Firebird deja correr cada
// sentencia de desempeño (firebird.WithStatementTimeout). Debe quedar por
// debajo del Timeout HTTP: el driver ignora la cancelación del contexto, así
// que sin este tope la sentencia seguía en Firebird después del 503.
func WithStatementTimeout(d time.Duration) Option {
	return func(h *Handlers) { h.tope = d }
}

// WithLogger fija el logger de los errores del módulo (por omisión slog.Default).
func WithLogger(l *slog.Logger) Option {
	return func(h *Handlers) { h.log = l }
}

// logger devuelve el logger configurado o el global.
func (h *Handlers) logger() *slog.Logger {
	if h.log == nil {
		return slog.Default()
	}
	return h.log
}

// lectura marca ctx para que sus sentencias corran con el tope del módulo.
func (h *Handlers) lectura(ctx context.Context) context.Context {
	return firebird.WithStatementTimeout(ctx, h.tope)
}

// contextoCompartido es el contexto del cálculo que comparten varias
// peticiones: conserva los valores y la fecha límite de la primera, pero no su
// cancelación — que un cliente cierre la pestaña no debe tumbar la respuesta
// de los demás que esperan el mismo cálculo.
func contextoCompartido(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.WithoutCancel(ctx)
	if limite, ok := ctx.Deadline(); ok {
		return context.WithDeadline(base, limite)
	}
	return context.WithCancel(base)
}

// TableroInput — GET /desempeno/usuarios. Todo llega como texto y se valida en
// el dominio para que el 422 lleve el código desempeno_parametro_invalido.
type TableroInput struct {
	Periodo   string `query:"periodo"    doc:"hoy | semana (por omisión hoy)"`
	PausaMin  string `query:"pausa_min"  doc:"umbral de pausa en minutos, 5–240 (por omisión 45)"`
	DesdeHora string `query:"desde_hora" doc:"HH:MM, con hasta_hora"`
	HastaHora string `query:"hasta_hora" doc:"HH:MM, con desde_hora"`
}

// TableroOutput envuelve TableroDTO.
type TableroOutput struct{ Body TableroDTO }

// JornadaInput — GET /desempeno/usuarios/{uid}/jornada.
type JornadaInput struct {
	UID       string `path:"uid"         doc:"uid de Firestore del usuario"`
	Fecha     string `query:"fecha"      doc:"YYYY-MM-DD (por omisión hoy, America/Mexico_City)"`
	PausaMin  string `query:"pausa_min"`
	DesdeHora string `query:"desde_hora"`
	HastaHora string `query:"hasta_hora"`
}

// JornadaOutput envuelve JornadaDTO.
type JornadaOutput struct{ Body JornadaDTO }

// ActividadInput — GET /desempeno/usuarios/{uid}/actividad. Los repetibles van
// "explode" (?tipo=pago&tipo=visita), como pide el contrato §4.
type ActividadInput struct {
	UID           string   `path:"uid"`
	Desde         string   `query:"desde"`
	Hasta         string   `query:"hasta"`
	Tipo          []string `query:"tipo,explode"`
	FormaCobro    []string `query:"forma_cobro,explode"`
	TipoVisita    []string `query:"tipo_visita,explode"`
	RegistradoPor []string `query:"registrado_por,explode"`
	MontoMin      string   `query:"monto_min"`
	MontoMax      string   `query:"monto_max"`
	ConFotos      string   `query:"con_fotos"`
	ConGPS        string   `query:"con_gps"`
	Q             string   `query:"q"`
	Orden         string   `query:"orden"`
	Dir           string   `query:"dir"`
	Cursor        string   `query:"cursor"`
	Limit         string   `query:"limit"`
}

// ActividadOutput envuelve PaginaActividadDTO.
type ActividadOutput struct{ Body PaginaActividadDTO }

// CuentasInput — GET /desempeno/usuarios/{uid}/cuentas.
type CuentasInput struct {
	UID      string `path:"uid"`
	Segmento string `query:"segmento"`
	Q        string `query:"q"`
	Orden    string `query:"orden"`
	Dir      string `query:"dir"`
}

// CuentasOutput envuelve CuentasDTO.
type CuentasOutput struct{ Body CuentasDTO }

// CatalogosInput — GET /desempeno/catalogos (sin parámetros).
type CatalogosInput struct{}

// CatalogosOutput envuelve CatalogosDTO.
type CatalogosOutput struct{ Body CatalogosDTO }

// claveTablero identifica peticiones con la misma respuesta. El tablero es el
// mismo para todo usuario con desempeno:ver (el permiso se valida antes, por
// petición), así que el usuario no forma parte de la clave.
func claveTablero(in *TableroInput) string {
	return strings.Join([]string{in.Periodo, in.PausaMin, in.DesdeHora, in.HastaHora}, "\x1f")
}

// Catalogos — formas de cobro y tipos de visita para armar los filtros: el
// escritorio no copia catálogos (contrato §4b).
func (h *Handlers) Catalogos(ctx context.Context, _ *CatalogosInput) (*CatalogosOutput, error) {
	if err := autorizar(ctx); err != nil {
		return nil, err
	}
	return &CatalogosOutput{Body: aCatalogosDTO()}, nil
}

// Tablero — una fila por usuario, sin totales del equipo (D14).
//
// Peticiones idénticas simultáneas (varias pantallas abiertas refrescando a la
// vez) comparten un solo cálculo: Firebird serializa y apilar el mismo cálculo
// N veces solo alarga la espera de todos. No es caché: en cuanto el cálculo
// termina, la siguiente petición recalcula.
func (h *Handlers) Tablero(ctx context.Context, in *TableroInput) (*TableroOutput, error) {
	if err := autorizar(ctx); err != nil {
		return nil, err
	}
	v, err, _ := h.tablero.Do(claveTablero(in), func() (any, error) {
		cctx, cancel := contextoCompartido(h.lectura(ctx))
		defer cancel()
		return h.svc.Tablero(cctx, desempenoapp.TableroInput{
			Periodo: in.Periodo, PausaMin: in.PausaMin, DesdeHora: in.DesdeHora, HastaHora: in.HastaHora,
		})
	})
	if err != nil {
		return nil, h.mapAppError(ctx, err)
	}
	t, _ := v.(desempenoapp.Tablero) //nolint:errcheck // Do solo devuelve lo que la función de arriba produce.
	// Cada petición arma su propio DTO; t se comparte y solo se lee.
	return &TableroOutput{Body: aTableroDTO(t)}, nil
}

// Jornada — actividades del usuario en un día, con pausas y señales.
func (h *Handlers) Jornada(ctx context.Context, in *JornadaInput) (*JornadaOutput, error) {
	if err := autorizar(ctx); err != nil {
		return nil, err
	}
	v, err := h.svc.Jornada(h.lectura(ctx), in.UID, desempenoapp.JornadaInput{
		Fecha: in.Fecha, PausaMin: in.PausaMin, DesdeHora: in.DesdeHora, HastaHora: in.HastaHora,
	})
	if err != nil {
		return nil, h.mapAppError(ctx, err)
	}
	return &JornadaOutput{Body: aJornadaDTO(v)}, nil
}

// Actividad — página de la actividad de toda la zona del usuario.
func (h *Handlers) Actividad(ctx context.Context, in *ActividadInput) (*ActividadOutput, error) {
	if err := autorizar(ctx); err != nil {
		return nil, err
	}
	p, err := h.svc.Actividad(h.lectura(ctx), in.UID, desempenoapp.FiltroActividadInput{
		Desde: in.Desde, Hasta: in.Hasta, Tipos: in.Tipo, FormasCobro: in.FormaCobro, TiposVisita: in.TipoVisita,
		RegistradoPor: in.RegistradoPor, MontoMin: in.MontoMin, MontoMax: in.MontoMax,
		ConFotos: in.ConFotos, ConGPS: in.ConGPS, Q: in.Q, Orden: in.Orden, Dir: in.Dir,
		Cursor: in.Cursor, Limit: in.Limit,
	})
	if err != nil {
		return nil, h.mapAppError(ctx, err)
	}
	return &ActividadOutput{Body: aPaginaDTO(p)}, nil
}

// Cuentas — estado por venta de la semana del usuario.
func (h *Handlers) Cuentas(ctx context.Context, in *CuentasInput) (*CuentasOutput, error) {
	if err := autorizar(ctx); err != nil {
		return nil, err
	}
	v, err := h.svc.Cuentas(h.lectura(ctx), in.UID, desempenoapp.CuentasInput{
		Segmento: in.Segmento, Q: in.Q, Orden: in.Orden, Dir: in.Dir,
	})
	if err != nil {
		return nil, h.mapAppError(ctx, err)
	}
	return &CuentasOutput{Body: aCuentasDTO(v)}, nil
}
