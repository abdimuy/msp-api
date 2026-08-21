//nolint:misspell // Spanish vocabulary by project convention.
package failedintent_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// La conciliación existe porque el marcado por 2xx del middleware sólo atrapa
// un camino. Se le escapan que la app se rinda, que la venta entre por reenvío
// o captura manual, que el éxito viaje con otra clave, y todo el rezago
// actual. Como esto es dinero, la red le pregunta a la fuente en vez de
// escuchar el evento.

// checkerFalso responde por un conjunto fijo de claves y registra qué le
// preguntaron.
type checkerFalso struct {
	mu        sync.Mutex
	resueltas map[string]bool
	err       error
	preguntas [][]string
	rutas     []string
}

func (c *checkerFalso) ResueltasEntre(
	_ context.Context, path string, claves []string,
) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rutas = append(c.rutas, path)
	c.preguntas = append(c.preguntas, claves)
	if c.err != nil {
		return nil, c.err
	}
	var out []string
	for _, k := range claves {
		if c.resueltas[k] {
			out = append(out, k)
		}
	}
	return out, nil
}

func (c *checkerFalso) totalPreguntado() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, p := range c.preguntas {
		n += len(p)
	}
	return n
}

// pendiente arma un intento pendiente con la clave y fecha dadas.
func pendiente(clave string, recibido time.Time) failedintent.Intent {
	return failedintent.Intent{
		ID:             uuid.New(),
		ReceivedAt:     recibido,
		Method:         "POST",
		Path:           "/v2/ventas",
		IdempotencyKey: clave,
		RequestID:      uuid.New(),
		HTTPStatus:     422,
		Status:         failedintent.StatusNew,
	}
}

// correrUnCiclo arranca el janitor, espera a que termine su ciclo de arranque
// y lo detiene.
func correrUnCiclo(t *testing.T, cfg failedintent.JanitorConfig, store *memStore) {
	t.Helper()
	j := failedintent.NewJanitor(cfg)
	require.NoError(t, j.Start(t.Context()))
	select {
	case <-store.purgeCh:
	case <-time.After(3 * time.Second):
		t.Fatal("el ciclo de arranque del janitor no terminó")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, j.Stop(ctx))
}

func TestConciliacion_CierraLoQueLaFuenteConfirma(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	aterrizada := pendiente("venta-si", time.Now().Add(-2*time.Hour))
	enElAire := pendiente("venta-no", time.Now().Add(-1*time.Hour))
	store.add(aterrizada)
	store.add(enElAire)

	checker := &checkerFalso{resueltas: map[string]bool{"venta-si": true}}
	correrUnCiclo(t, failedintent.JanitorConfig{
		Store:      store,
		Resolution: checker,
		Interval:   time.Hour,
	}, store)

	cerrada := store.get(aterrizada.ID)
	require.NotNil(t, cerrada)
	assert.Equal(t, failedintent.StatusResolvedManual, cerrada.Status,
		"la fuente dijo que la venta ya existe: el intento no puede seguir pendiente")
	require.NotNil(t, cerrada.ResolvedAt)

	abierta := store.get(enElAire.ID)
	require.NotNil(t, abierta)
	assert.Equal(t, failedintent.StatusNew, abierta.Status,
		"la que la fuente NO reconoce se queda pendiente")
}

func TestConciliacion_SinCheckerElJanitorSigueFuncionando(t *testing.T) {
	t.Parallel()

	// Dependencia opcional, igual que Blob: sin ella no se concilia, pero la
	// purga —que es la parte que libera disco— corre igual.
	store := newMemStore()
	viejo := pendiente("venta-vieja", time.Now().Add(-200*24*time.Hour))
	store.add(viejo)

	correrUnCiclo(t, failedintent.JanitorConfig{Store: store, Interval: time.Hour}, store)

	assert.False(t, store.has(viejo.ID), "la purga corre sin checker conectado")
}

func TestConciliacion_ElCheckerQueRevientaNoTumbaLaPurga(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	viejo := pendiente("venta-vieja", time.Now().Add(-200*24*time.Hour))
	store.add(viejo)

	checker := &checkerFalso{err: errors.New("la fuente no contestó")}
	correrUnCiclo(t, failedintent.JanitorConfig{
		Store:      store,
		Resolution: checker,
		Interval:   time.Hour,
	}, store)

	assert.False(t, store.has(viejo.ID),
		"si el checker revienta, el disco se libera igual: la purga no depende de él")
}

func TestConciliacion_TroceaConMasClavesQueUnaPagina(t *testing.T) {
	t.Parallel()

	// El barrido pagina de 100 en 100. Con 250 pendientes, un cursor roto
	// —el clásico que repite la primera página— dejaría 150 sin preguntar.
	store := newMemStore()
	base := time.Now().Add(-48 * time.Hour)
	const total = 250
	for n := range total {
		store.add(pendiente("venta-"+uuid.NewString(), base.Add(time.Duration(n)*time.Second)))
	}

	checker := &checkerFalso{resueltas: map[string]bool{}}
	correrUnCiclo(t, failedintent.JanitorConfig{
		Store:      store,
		Resolution: checker,
		Interval:   time.Hour,
	}, store)

	assert.Equal(t, total, checker.totalPreguntado(),
		"las %d claves pendientes tienen que llegar a la fuente, no sólo la primera página", total)
}

func TestConciliacion_LosIntentosSinClaveNoSePreguntan(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	store.add(pendiente("", time.Now().Add(-2*time.Hour)))
	store.add(pendiente("venta-con-clave", time.Now().Add(-1*time.Hour)))

	checker := &checkerFalso{resueltas: map[string]bool{}}
	correrUnCiclo(t, failedintent.JanitorConfig{
		Store:      store,
		Resolution: checker,
		Interval:   time.Hour,
	}, store)

	assert.Equal(t, 1, checker.totalPreguntado(),
		"sin clave no hay nada que preguntarle a la fuente")
}

func TestConciliacion_PreguntaPorLaRutaDeCadaIntento(t *testing.T) {
	t.Parallel()

	// Una clave sólo es única dentro de su recurso. El checker necesita la
	// ruta para poder ignorar las que no son suyas.
	store := newMemStore()
	venta := pendiente("clave-compartida", time.Now().Add(-2*time.Hour))
	pago := pendiente("clave-compartida", time.Now().Add(-1*time.Hour))
	pago.Path = "/v2/cobranza/pagos"
	store.add(venta)
	store.add(pago)

	checker := &checkerFalso{resueltas: map[string]bool{}}
	correrUnCiclo(t, failedintent.JanitorConfig{
		Store:      store,
		Resolution: checker,
		Interval:   time.Hour,
	}, store)

	assert.ElementsMatch(t, []string{"/v2/ventas", "/v2/cobranza/pagos"}, checker.rutas)
}

func TestRetencion_ElResueltoSePurgaAntesQueElPendiente(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	edad := time.Now().Add(-30 * 24 * time.Hour)

	resuelto := pendiente("venta-cerrada", edad)
	resuelto.Status = failedintent.StatusResolvedManual
	resuelto.BodyBlobPath = "/blobs/cerrada.bin"
	store.add(resuelto)

	abierto := pendiente("venta-abierta", edad)
	store.add(abierto)

	blobs := &fakeBlobs{}
	correrUnCiclo(t, failedintent.JanitorConfig{
		Store:           store,
		Blob:            blobs,
		Interval:        time.Hour,
		RetainResueltos: 7 * 24 * time.Hour,
	}, store)

	assert.False(t, store.has(resuelto.ID),
		"su cuerpo ya no le sirve a nadie: la venta entró o alguien decidió que no")
	assert.True(t, store.has(abierto.ID),
		"el pendiente de la MISMA edad conserva la retención larga")
	assert.Contains(t, blobs.borrados(), "/blobs/cerrada.bin",
		"el blob se va con su fila; si no, queda un archivo con fotos sin dueño")
}

func TestRetencion_ElReintentoFallidoNoEsUnResuelto(t *testing.T) {
	t.Parallel()

	// retried_fail es un intento CERRADO cuyo trabajo NO se hizo. Su cuerpo
	// sigue siendo la única copia de lo que capturó el vendedor.
	store := newMemStore()
	fallido := pendiente("venta-reintento-fallido", time.Now().Add(-30*24*time.Hour))
	fallido.Status = failedintent.StatusRetriedFail
	store.add(fallido)

	correrUnCiclo(t, failedintent.JanitorConfig{
		Store:           store,
		Interval:        time.Hour,
		RetainResueltos: 7 * 24 * time.Hour,
	}, store)

	assert.True(t, store.has(fallido.ID))
}

func TestRetencion_CorteCortoDesactivadoSoloApagaEsaMitad(t *testing.T) {
	t.Parallel()

	store := newMemStore()

	reciente := pendiente("venta-cerrada-reciente", time.Now().Add(-30*24*time.Hour))
	reciente.Status = failedintent.StatusIgnored
	store.add(reciente)

	antiquisimo := pendiente("venta-cerrada-antigua", time.Now().Add(-200*24*time.Hour))
	antiquisimo.Status = failedintent.StatusIgnored
	store.add(antiquisimo)

	correrUnCiclo(t, failedintent.JanitorConfig{
		Store:    store,
		Interval: time.Hour,
		// Negativo = apagado a propósito. Cero significaría "no lo configuré"
		// y tomaría el default, que es justo la confusión que hay que evitar.
		RetainResueltos: -1,
	}, store)

	assert.True(t, store.has(reciente.ID), "sin corte corto, el resuelto de 30 días sobrevive")
	assert.False(t, store.has(antiquisimo.ID), "el corte largo sigue aplicando a todo")
}
