package failedintent

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// DefaultJanitorInterval is how often the janitor purges expired intents.
const DefaultJanitorInterval = time.Hour

// DefaultRetain is the lifetime of a captured intent.
//
// 90 days is chosen as a compromise between legal-evidence preservation
// (long enough to survive a customer dispute cycle) and PII minimisation
// (short enough to limit blast radius if the admin endpoint is ever
// compromised). See ADR-0005.
const DefaultRetain = 90 * 24 * time.Hour

// DefaultRetainResueltos es la vida de un intento YA CERRADO.
//
// 90 días son los que necesita la evidencia de un problema abierto. Un intento
// resuelto no es evidencia de nada: la venta entró, o alguien decidió que no
// entraba. Lo que queda de él es un cuerpo con las fotos del cliente ocupando
// disco y ampliando la superficie de datos personales sin que nadie lo mire.
//
// Siete días dejan margen para revisar la semana y auditar lo que se cerró
// solo, que es justo el periodo en que alguien podría querer mirarlo.
const DefaultRetainResueltos = 7 * 24 * time.Hour

// estadosResueltos son los estados cuyo cuerpo ya no le sirve a nadie: el
// trabajo aterrizó (retried_ok, resolved_manual) o alguien decidió
// deliberadamente que no aterriza (ignored).
//
// `retried_fail` NO está aquí, y la ausencia es la decisión: un reenvío que
// falló es un intento cerrado cuyo trabajo **no se hizo**. Su cuerpo sigue
// siendo la única copia de lo que capturó el vendedor y conserva la retención
// larga.
var estadosResueltos = []Status{StatusRetriedOK, StatusResolvedManual, StatusIgnored}

// JanitorConfig configures the periodic purge.
type JanitorConfig struct {
	// Store is the persistence backend whose rows are purged.
	Store Store
	// Blob, when non-nil, is invoked to delete every on-disk blob whose
	// owning row was just purged. Leaving it nil is valid for tests and
	// deployments that never opt into multipart capture.
	Blob BlobStorage
	// Interval is the period between purge ticks. Defaults to one hour.
	Interval time.Duration
	// Retain is how long an intent is kept after capture. Defaults to 90 days.
	Retain time.Duration
	// RetainResueltos es el corte corto para los intentos ya cerrados.
	// Defaults to DefaultRetainResueltos. Un valor NEGATIVO desactiva sólo
	// esa mitad; el corte largo de Retain sigue aplicando a todo.
	RetainResueltos time.Duration
	// Resolution, cuando no es nil, le pregunta a la fuente cuáles de los
	// intentos pendientes corresponden a trabajo que ya aterrizó, y los
	// cierra. Dependencia OPCIONAL, igual que Blob: sin ella el janitor
	// purga como siempre.
	Resolution ResolutionChecker
	// Clock supplies the current time. Defaults to time.Now.
	Clock func() time.Time
}

func (c *JanitorConfig) defaults() {
	if c.Interval <= 0 {
		c.Interval = DefaultJanitorInterval
	}
	if c.Retain <= 0 {
		c.Retain = DefaultRetain
	}
	// Cero es "no lo configuré" → default. Negativo es "lo apagué a
	// propósito". Distinguirlos importa: si cero apagara el corte, olvidar el
	// campo dejaría los cuerpos resueltos 90 días sin que nadie lo note.
	if c.RetainResueltos == 0 {
		c.RetainResueltos = DefaultRetainResueltos
	}
	if c.Clock == nil {
		c.Clock = time.Now
	}
}

// Janitor periodically purges captured intents older than its retention
// window. Implements lifecycle.Hooks so it can be wired into the application
// fx graph alongside other long-running components.
type Janitor struct {
	cfg     JanitorConfig
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewJanitor builds a Janitor with cfg defaults applied.
func NewJanitor(cfg JanitorConfig) *Janitor {
	cfg.defaults()
	return &Janitor{cfg: cfg}
}

// Start launches the ticker goroutine. Idempotent — a second call while
// already running is a no-op.
func (j *Janitor) Start(_ context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.running {
		return nil
	}
	// Decouple the goroutine context from the start ctx — start ctx is
	// short-lived per fx convention, but the janitor must outlive it.
	runCtx, cancel := context.WithCancel(context.Background())
	j.cancel = cancel
	j.done = make(chan struct{})
	j.running = true
	//nolint:contextcheck // intentional: the loop must outlive the fx start ctx.
	go j.run(runCtx)
	return nil
}

// Stop signals the goroutine to exit and waits for it to drain. Bounded by
// the supplied context's deadline.
func (j *Janitor) Stop(ctx context.Context) error {
	j.mu.Lock()
	if !j.running {
		j.mu.Unlock()
		return nil
	}
	cancel := j.cancel
	done := j.done
	j.running = false
	j.mu.Unlock()

	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// run is the ticker loop. Each tick fires purgeOnce; transient errors are
// logged and the loop continues.
func (j *Janitor) run(ctx context.Context) {
	defer close(j.done)
	ticker := time.NewTicker(j.cfg.Interval)
	defer ticker.Stop()

	// Purge once at boot so the first cycle isn't delayed a full interval.
	j.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			j.tick(ctx)
		}
	}
}

// tick es un ciclo completo: primero se cierra lo que ya aterrizó, después se
// purga.
//
// El orden importa: un intento que la fuente acaba de confirmar entra al corte
// corto en este mismo ciclo en vez de esperar una hora más. Al revés no se
// pierde nada, pero se retrasa cada cierre un ciclo entero.
func (j *Janitor) tick(ctx context.Context) {
	j.conciliarOnce(ctx)
	j.purgeOnce(ctx)
}

// conciliarOnce le pregunta a la fuente cuáles de los intentos PENDIENTES
// corresponden a trabajo que ya aterrizó, y los cierra.
//
// Existe porque el marcado por 2xx del middleware sólo atrapa un camino. Se le
// escapan que la app se rinda, que la venta entre por reenvío o captura
// manual, que el éxito viaje con otra clave, y **todo el rezago actual**. Como
// esto es dinero, la red le pregunta a la fuente en vez de escuchar el evento.
//
// Sin `Resolution` conectado es un no-op: es una dependencia opcional, igual
// que `Blob`.
//
// Los errores se registran y no abortan nada: el checker que revienta no puede
// tumbar la purga, que es la parte que libera disco.
func (j *Janitor) conciliarOnce(ctx context.Context) {
	if j.cfg.Resolution == nil {
		return
	}
	pendientes, err := j.clavesPendientes(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.ErrorContext(ctx, "failedintent.janitor: no se pudieron leer los pendientes", "error", err)
		return
	}

	var cerrados int64
	for path, claves := range pendientes {
		resueltas, checkErr := j.cfg.Resolution.ResueltasEntre(ctx, path, claves)
		if checkErr != nil {
			if errors.Is(checkErr, context.Canceled) {
				return
			}
			slog.WarnContext(
				ctx, "failedintent.janitor: la fuente no pudo responder por sus claves",
				"error", checkErr, "path", path, "claves", len(claves),
			)
			continue
		}
		if len(resueltas) == 0 {
			continue
		}
		n, markErr := j.cfg.Store.MarkResolvedByKeys(ctx, path, resueltas, j.cfg.Clock())
		if markErr != nil {
			slog.ErrorContext(
				ctx, "failedintent.janitor: no se pudieron cerrar los intentos conciliados",
				"error", markErr, "path", path,
			)
			continue
		}
		cerrados += n
	}
	if cerrados > 0 {
		slog.InfoContext(ctx, "failedintent.janitor: intentos cerrados por conciliación", "count", cerrados)
	}
}

// clavesPendientes reúne las claves de idempotencia de los intentos
// pendientes, agrupadas por ruta.
//
// Pagina con el cursor de List en vez de pedir "todo": MSP_FAILED_INTENTS
// puede crecer, y una consulta sin cota es la que se cae el día del incidente
// —justo cuando hay más filas pendientes que nunca—. El tope duro de páginas
// es la misma idea: si un ciclo no alcanza a barrerlo todo, el siguiente sigue.
func (j *Janitor) clavesPendientes(ctx context.Context) (map[string][]string, error) {
	const (
		porPagina  = 100
		maxPaginas = 50
	)
	out := make(map[string][]string)
	vistas := make(map[string]struct{})

	var params ListParams
	params.Status = StatusNew
	params.PageSize = porPagina

	for range maxPaginas {
		page, err := j.cfg.Store.List(ctx, params)
		if err != nil {
			return nil, err
		}
		for _, i := range page.Items {
			if i.IdempotencyKey == "" {
				// Sin clave no hay nada que preguntarle a la fuente.
				continue
			}
			marca := i.Path + "\x00" + i.IdempotencyKey
			if _, ya := vistas[marca]; ya {
				continue
			}
			vistas[marca] = struct{}{}
			out[i.Path] = append(out[i.Path], i.IdempotencyKey)
		}
		if !page.HasMore {
			return out, nil
		}
		params.CursorReceivedAt = page.NextReceivedAt
		params.CursorID = page.NextID
	}
	slog.WarnContext(ctx,
		"failedintent.janitor: la conciliación se detuvo en el tope de páginas; "+
			"el resto se revisa en el siguiente ciclo",
		"paginas", maxPaginas, "por_pagina", porPagina)
	return out, nil
}

// purgeOnce performs one purge cycle. Errors are logged, not returned: the
// janitor must keep ticking even when the DB is temporarily unhappy.
// When the deletion freed any on-disk blobs and a Blob storage is wired,
// the blobs are removed best-effort — a Delete error never aborts the cycle
// (the boot-time orphan sweep is the safety net).
func (j *Janitor) purgeOnce(ctx context.Context) {
	// Corte corto: los ya cerrados. Un valor negativo lo desactiva sin tocar
	// el corte largo.
	if j.cfg.RetainResueltos > 0 {
		j.purgar(ctx, j.cfg.Clock().Add(-j.cfg.RetainResueltos), estadosResueltos...)
	}
	// Corte largo: todo lo demás (y también los cerrados que hayan
	// sobrevivido a un corte corto desactivado).
	j.purgar(ctx, j.cfg.Clock().Add(-j.cfg.Retain))
}

// purgar ejecuta un corte y limpia los blobs que quedaron sin fila.
func (j *Janitor) purgar(ctx context.Context, cutoff time.Time, estados ...Status) {
	result, err := j.cfg.Store.PurgeOlderThan(ctx, cutoff, estados...)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.ErrorContext(
			ctx, "failedintent.janitor: purge failed",
			"error", err, "cutoff", cutoff,
		)
		return
	}
	if result.RowsDeleted == 0 {
		return
	}
	if j.cfg.Blob != nil {
		for _, path := range result.BlobPaths {
			if delErr := j.cfg.Blob.Delete(ctx, path); delErr != nil {
				slog.WarnContext(
					ctx, "failedintent.janitor: blob delete failed",
					"error", delErr, "path", path,
				)
			}
		}
	}
	slog.InfoContext(
		ctx, "failedintent.janitor: purged",
		"count", result.RowsDeleted,
		"blobs_deleted", len(result.BlobPaths),
		"cutoff", cutoff,
	)
}
