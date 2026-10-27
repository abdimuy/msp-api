package desempenohttp_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/infra/desempenohttp"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/logger"
)

// usuariosContados cuenta los cálculos (cada Tablero/Jornada/... empieza por
// ListarUsuarios), puede retenerlos en una compuerta y recuerda el contexto con
// el que llegó la última llamada. Embebe fakeUsuarios para heredar el resto
// del puerto.
type usuariosContados struct {
	fakeUsuarios
	llamadas  atomic.Int32
	compuerta chan struct{} // nil → no retiene.
	mu        sync.Mutex
	ultimoCtx context.Context //nolint:containedctx // registrado para las aserciones.
	// errAlSoltar es ctx.Err() en el momento en que la compuerta se abre: si
	// el cálculo compartido se hubiera cancelado, aquí se ve.
	errAlSoltar error
}

func (u *usuariosContados) ListarUsuarios(ctx context.Context) ([]domain.UsuarioCobranza, error) {
	u.llamadas.Add(1)
	u.mu.Lock()
	u.ultimoCtx = ctx
	u.mu.Unlock()
	if u.compuerta != nil {
		<-u.compuerta
		u.mu.Lock()
		u.errAlSoltar = ctx.Err()
		u.mu.Unlock()
	}
	return u.fakeUsuarios.ListarUsuarios(ctx)
}

func (u *usuariosContados) ctx() context.Context {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.ultimoCtx
}

func servicioCon(t *testing.T, u *usuariosContados) *desempenoapp.Service {
	t.Helper()
	loc, err := time.LoadLocation("America/Mexico_City")
	require.NoError(t, err)
	return desempenoapp.NewService(u, fakeActividades{actividadesEscenario()}, fakeCuentas{cuentasEscenario()}, reloj{ahora}, loc)
}

func routerOpts(cu *auth.CurrentUser, svc *desempenoapp.Service, opts ...desempenohttp.Option) http.Handler {
	r := chi.NewRouter()
	if cu != nil {
		r.Use(planter(*cu))
	}
	desempenohttp.MountRouter(r, svc, opts...)
	return r
}

type respuesta struct {
	code int
	body string
}

// lanzar dispara n GET concurrentes a path y espera a que el servicio haya
// recibido la primera llamada.
func lanzar(h http.Handler, paths []string) (chan respuesta, *sync.WaitGroup) {
	out := make(chan respuesta, len(paths))
	wg := &sync.WaitGroup{}
	for _, p := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
			out <- respuesta{rec.Code, rec.Body.String()}
		}()
	}
	return out, wg
}

// esperaYSuelta espera a que haya `minimo` cálculos en curso, deja tiempo para
// que las demás peticiones se sumen al cálculo compartido y abre la compuerta.
func esperaYSuelta(t *testing.T, u *usuariosContados, minimo int32) {
	t.Helper()
	require.Eventually(t, func() bool { return u.llamadas.Load() >= minimo }, 5*time.Second, 5*time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	close(u.compuerta)
}

func TestTablero_PeticionesSimultaneasIdenticasCompartenUnCalculo(t *testing.T) {
	t.Parallel()
	u := &usuariosContados{fakeUsuarios: fakeUsuarios{us: usuariosEscenario()}, compuerta: make(chan struct{})}
	cu := usuarioCon(auth.PermDesempenoVer)
	h := routerOpts(&cu, servicioCon(t, u))

	const n = 8
	paths := make([]string, n)
	for i := range paths {
		paths[i] = "/desempeno/usuarios?periodo=semana&pausa_min=30"
	}
	out, wg := lanzar(h, paths)
	esperaYSuelta(t, u, 1)
	wg.Wait()
	close(out)

	assert.Equal(t, int32(1), u.llamadas.Load(), "ocho peticiones idénticas simultáneas deben compartir un solo cálculo")
	var primero string
	for r := range out {
		require.Equal(t, http.StatusOK, r.code, r.body)
		if primero == "" {
			primero = r.body
		}
		assert.JSONEq(t, primero, r.body, "todas reciben la misma respuesta")
	}
}

func TestTablero_ParametrosDistintosNoSeMezclan(t *testing.T) {
	t.Parallel()
	u := &usuariosContados{fakeUsuarios: fakeUsuarios{us: usuariosEscenario()}, compuerta: make(chan struct{})}
	cu := usuarioCon(auth.PermDesempenoVer)
	h := routerOpts(&cu, servicioCon(t, u))

	// Una base y una variante por cada campo de la clave, que difiere de la
	// base SOLO en ese campo: quitar cualquiera de los cuatro de la clave
	// junta dos de estas peticiones y la cuenta baja.
	paths := []string{
		"/desempeno/usuarios?periodo=semana&pausa_min=30&desde_hora=08:00&hasta_hora=18:00",
		"/desempeno/usuarios?periodo=hoy&pausa_min=30&desde_hora=08:00&hasta_hora=18:00",
		"/desempeno/usuarios?periodo=semana&pausa_min=60&desde_hora=08:00&hasta_hora=18:00",
		"/desempeno/usuarios?periodo=semana&pausa_min=30&desde_hora=09:00&hasta_hora=18:00",
		"/desempeno/usuarios?periodo=semana&pausa_min=30&desde_hora=08:00&hasta_hora=17:00",
	}
	out, wg := lanzar(h, paths)
	esperaYSuelta(t, u, int32(len(paths)))
	wg.Wait()
	close(out)

	assert.Equal(t, int32(len(paths)), u.llamadas.Load(), "periodo, pausa_min y horario son parte de la clave")
	for r := range out {
		require.Equal(t, http.StatusOK, r.code, r.body)
	}
}

func TestTablero_SinCacheEntrePeticionesSucesivas(t *testing.T) {
	t.Parallel()
	u := &usuariosContados{fakeUsuarios: fakeUsuarios{us: usuariosEscenario()}}
	cu := usuarioCon(auth.PermDesempenoVer)
	h := routerOpts(&cu, servicioCon(t, u))
	for range 3 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/desempeno/usuarios", nil))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	assert.Equal(t, int32(3), u.llamadas.Load(), "la coalescencia no es caché: una petición posterior recalcula")
}

func TestTablero_SinPermisoNoSeSumaAlCalculoEnCurso(t *testing.T) {
	t.Parallel()
	u := &usuariosContados{fakeUsuarios: fakeUsuarios{us: usuariosEscenario()}, compuerta: make(chan struct{})}
	svc := servicioCon(t, u)
	conPermiso := usuarioCon(auth.PermDesempenoVer)
	sinPermiso := usuarioCon(auth.PermCobranzaVerPagos)

	out, wg := lanzar(routerOpts(&conPermiso, svc), []string{"/desempeno/usuarios"})
	require.Eventually(t, func() bool { return u.llamadas.Load() == 1 }, 5*time.Second, 5*time.Millisecond)

	// Con el cálculo en curso, una petición sin desempeno:ver es 403 al
	// instante: el permiso se valida antes de entrar a la coalescencia.
	rec := httptest.NewRecorder()
	routerOpts(&sinPermiso, svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/desempeno/usuarios", nil))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.NotContains(t, rec.Body.String(), "Juan Carlos")

	close(u.compuerta)
	wg.Wait()
	close(out)
	r := <-out
	assert.Equal(t, http.StatusOK, r.code)
	assert.Equal(t, int32(1), u.llamadas.Load())
}

func TestTablero_ElCalculoCompartidoSobreviveALaDesconexionDelPrimero(t *testing.T) {
	t.Parallel()
	u := &usuariosContados{fakeUsuarios: fakeUsuarios{us: usuariosEscenario()}, compuerta: make(chan struct{})}
	cu := usuarioCon(auth.PermDesempenoVer)
	h := routerOpts(&cu, servicioCon(t, u))

	// El primero se va (cliente cerró la pestaña) a mitad del cálculo.
	ctx, cancel := context.WithCancel(context.Background())
	primero := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/desempeno/usuarios", nil).WithContext(ctx))
		primero <- rec.Code
	}()
	require.Eventually(t, func() bool { return u.llamadas.Load() == 1 }, 5*time.Second, 5*time.Millisecond)
	out, wg := lanzar(h, []string{"/desempeno/usuarios"})
	time.Sleep(200 * time.Millisecond)
	cancel()
	close(u.compuerta)
	wg.Wait()
	close(out)
	<-primero

	r := <-out
	assert.Equal(t, http.StatusOK, r.code, "el segundo no debe heredar la cancelación del primero: %s", r.body)
	u.mu.Lock()
	errAlSoltar := u.errAlSoltar
	u.mu.Unlock()
	require.NoError(t, errAlSoltar, "el cálculo compartido no se cancela porque un cliente se desconecte")
}

func TestLecturas_LlevanElTopeDeSentenciaDeFirebird(t *testing.T) {
	t.Parallel()
	cu := usuarioCon(auth.PermDesempenoVer)
	for _, p := range rutas[1:] {
		u := &usuariosContados{fakeUsuarios: fakeUsuarios{us: usuariosEscenario()}}
		h := routerOpts(&cu, servicioCon(t, u), desempenohttp.WithStatementTimeout(25*time.Second))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		require.Equal(t, http.StatusOK, rec.Code, "%s: %s", p, rec.Body.String())
		require.NotNil(t, u.ctx(), p)
		d, ok := firebird.StatementTimeout(u.ctx())
		assert.True(t, ok, "%s: las sentencias deben correr con el tope acotado", p)
		assert.Equal(t, 25*time.Second, d, p)
	}
}

func TestLecturas_SinOpcionNoCambianElTopeDelProceso(t *testing.T) {
	t.Parallel()
	cu := usuarioCon(auth.PermDesempenoVer)
	u := &usuariosContados{fakeUsuarios: fakeUsuarios{us: usuariosEscenario()}}
	h := routerOpts(&cu, servicioCon(t, u))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/desempeno/usuarios", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	_, ok := firebird.StatementTimeout(u.ctx())
	assert.False(t, ok)
}

func TestErrores_503RegistraLaCausaSinExponerla(t *testing.T) {
	t.Parallel()
	causa := errors.New("rpc error: code = Unavailable desc = firestore token expirado proyecto msp-db-1c2ce")
	buf := &bytes.Buffer{}
	lg := logger.New(logger.Options{Level: "info", Format: "json", Output: buf})
	u := &usuariosContados{fakeUsuarios: fakeUsuarios{err: domain.ErrFirestoreNoDisponible.WithError(causa)}}
	cu := usuarioCon(auth.PermDesempenoVer)
	h := routerOpts(&cu, servicioCon(t, u), desempenohttp.WithLogger(lg))

	req := httptest.NewRequest(http.MethodGet, "/desempeno/usuarios", nil)
	req = req.WithContext(logger.WithRequestID(req.Context(), "req-503"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "code=desempeno_firestore_unavailable", codigoDe(t, rec.Body.Bytes()))
	assert.NotContains(t, rec.Body.String(), "token expirado", "la causa interna no viaja al cliente")
	assert.NotContains(t, rec.Body.String(), "msp-db-1c2ce")

	log := buf.String()
	assert.Contains(t, log, "desempeno.http.no_disponible")
	assert.Contains(t, log, "token expirado", "la causa encadenada debe quedar en el log")
	assert.Contains(t, log, `"code":"desempeno_firestore_unavailable"`)
	assert.Contains(t, log, `"request_id":"req-503"`)
}

func TestErrores_503DeFirebirdRegistraElTimeout(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	lg := slog.New(slog.NewJSONHandler(buf, nil))
	timeout := firebird.MapError(context.DeadlineExceeded)
	u := &usuariosContados{fakeUsuarios: fakeUsuarios{err: timeout}}
	cu := usuarioCon(auth.PermDesempenoVer)
	h := routerOpts(&cu, servicioCon(t, u), desempenohttp.WithLogger(lg))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/desempeno/usuarios/abc123/cuentas", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.NotContains(t, rec.Body.String(), "deadline")
	assert.Contains(t, buf.String(), "firebird_timeout")
	assert.Contains(t, buf.String(), "context deadline exceeded")
}

// Control: un 4xx no es incidente del servidor y no ensucia el log.
func TestErrores_4xxNoSeRegistraComoNoDisponible(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	lg := slog.New(slog.NewJSONHandler(buf, nil))
	cu := usuarioCon(auth.PermDesempenoVer)
	u := &usuariosContados{fakeUsuarios: fakeUsuarios{us: usuariosEscenario()}}
	h := routerOpts(&cu, servicioCon(t, u), desempenohttp.WithLogger(lg))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/desempeno/usuarios?pausa_min=4", nil))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.NotContains(t, buf.String(), "no_disponible")
}
