//go:build !ci_skip_firebird

package firebird_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintentfb "github.com/abdimuy/msp-api/internal/platform/failedintent/firebird"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

// Estas pruebas miden las columnas de la migración 000061 contra Firebird de
// verdad. Lo que se juega es que el dato que hace legible un renglón
// —el nombre del cliente y el monto— sobreviva la ida y la vuelta, y que el
// filtro de los chips sea SQL: filtrar en memoria sobre la página ya recortada
// mostraría "las ventas que cupieron en los primeros veinte renglones" y nada
// advertiría del resto.

func intentoConResumen(modulo, titulo, monto, referencia string) failedintent.Intent {
	i := failedintent.Intent{
		ID:           uuid.New(),
		ReceivedAt:   time.Now().UTC().Truncate(time.Millisecond),
		Method:       "POST",
		Path:         "/v2/ventas",
		RequestID:    uuid.New(),
		Body:         json.RawMessage(`{"x":1}`),
		HTTPStatus:   422,
		ErrorCode:    "validation_failed",
		ErrorMessage: "mensaje de prueba",
		Status:       failedintent.StatusNew,
		Modulo:       modulo,
	}
	if titulo == "" && monto == "" && referencia == "" {
		return i
	}
	res := &failedintent.Resumen{Modulo: modulo, Titulo: titulo, Referencia: referencia}
	if monto != "" {
		d := decimal.RequireFromString(monto)
		res.Monto = &d
	}
	i.Resumen = res
	return i
}

func TestResumen_IdaYVuelta(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		// Acentos y ñ: la columna es UTF8 y el nombre de un cliente mexicano
		// los trae casi siempre.
		intent := intentoConResumen("ventas", "María Ángeles Muñoz Ñandú", "12500.50", "F-1001")

		require.NoError(t, saveOK(s.Save(ctx, intent)))

		got, err := s.Get(ctx, intent.ID)
		require.NoError(t, err)
		require.NotNil(t, got)

		assert.Equal(t, "ventas", got.Modulo)
		require.NotNil(t, got.Resumen)
		assert.Equal(t, "María Ángeles Muñoz Ñandú", got.Resumen.Titulo)
		assert.Equal(t, "F-1001", got.Resumen.Referencia)
		assert.Equal(t, "ventas", got.Resumen.Modulo)
		require.NotNil(t, got.Resumen.Monto)
		// El monto viaja como cadena decimal a propósito: pasarlo por un
		// float64 es justo lo que no se le hace a un importe.
		assert.Equal(t, "12500.5", got.Resumen.Monto.String())
	})
}

func TestResumen_NuloSobreviveComoNulo(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		intent := intentoConResumen("", "", "", "")

		require.NoError(t, saveOK(s.Save(ctx, intent)))

		got, err := s.Get(ctx, intent.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Empty(t, got.Modulo, "NULL significa 'no se extrajo', no cadena vacía inventada")
		assert.Nil(t, got.Resumen)
	})
}

// La combinación honesta: la ruta se reconoció, el cuerpo no.
func TestResumen_ModuloSinResumen(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		intent := intentoConResumen("ventas", "", "", "")

		require.NoError(t, saveOK(s.Save(ctx, intent)))

		got, err := s.Get(ctx, intent.ID)
		require.NoError(t, err)
		assert.Equal(t, "ventas", got.Modulo)
		assert.Nil(t, got.Resumen)
	})
}

// El filtro de los chips. Es SQL y no un recorte en memoria: la prueba pide
// una página de UNO sobre tres filas de módulos distintos y exige que el único
// renglón sea del módulo pedido.
func TestList_FiltraPorModuloEnSQL(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		base := time.Now().UTC().Truncate(time.Millisecond)
		// El pago es el MÁS VIEJO: si el filtro fuera en memoria sobre una
		// página de uno, se quedaría con la venta más nueva y devolvería cero.
		pago := intentoConResumen("pagos", "Gabriel Roque", "350", "12345")
		pago.Path = "/v2/cobranza/pagos"
		pago.ReceivedAt = base.Add(-2 * time.Hour)
		venta1 := intentoConResumen("ventas", "Ana", "1000", "a")
		venta1.ReceivedAt = base.Add(-time.Hour)
		venta2 := intentoConResumen("ventas", "Beto", "2000", "b")
		venta2.ReceivedAt = base

		for _, i := range []failedintent.Intent{pago, venta1, venta2} {
			require.NoError(t, saveOK(s.Save(ctx, i)))
		}

		page, err := s.List(ctx, failedintent.ListParams{Modulo: "pagos", PageSize: 1})
		require.NoError(t, err)
		require.Len(t, page.Items, 1, "el filtro debe correr en SQL, no sobre la página")
		assert.Equal(t, pago.ID, page.Items[0].ID)
		assert.Equal(t, "pagos", page.Items[0].Modulo)

		ventas, err := s.List(ctx, failedintent.ListParams{Modulo: "ventas", PageSize: 50})
		require.NoError(t, err)
		require.Len(t, ventas.Items, 2)
		for _, i := range ventas.Items {
			assert.Equal(t, "ventas", i.Modulo)
		}
	})
}

// El filtro del relleno: sólo las filas a las que nunca se les corrió el
// extractor. Una fila con MODULO puesto y RESUMEN nulo NO entra — es un
// resultado, no un pendiente, y volver a mirarla cada hora releería su blob
// para siempre.
func TestList_SinExtraerExcluyeLoQueYaSeIntento(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		virgen := intentoConResumen("", "", "", "")
		soloModulo := intentoConResumen("ventas", "", "", "")
		completa := intentoConResumen("ventas", "Ana", "1000", "a")
		for _, i := range []failedintent.Intent{virgen, soloModulo, completa} {
			require.NoError(t, saveOK(s.Save(ctx, i)))
		}

		page, err := s.List(ctx, failedintent.ListParams{SinExtraer: true, PageSize: 50})
		require.NoError(t, err)

		ids := make(map[uuid.UUID]bool, len(page.Items))
		for _, i := range page.Items {
			ids[i.ID] = true
		}
		assert.True(t, ids[virgen.ID], "la fila sin extraer debe entrar")
		assert.False(t, ids[soloModulo.ID], "con MODULO puesto ya se intentó")
		assert.False(t, ids[completa.ID])
	})
}

func TestGuardarResumen_RellenaLaFilaVieja(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		vieja := intentoConResumen("", "", "", "")
		require.NoError(t, saveOK(s.Save(ctx, vieja)))

		monto := decimal.RequireFromString("999.99")
		require.NoError(t, s.GuardarResumen(ctx, vieja.ID, "ventas", &failedintent.Resumen{
			Titulo: "Ana Pérez", Monto: &monto, Referencia: "F-9",
		}))

		got, err := s.Get(ctx, vieja.ID)
		require.NoError(t, err)
		assert.Equal(t, "ventas", got.Modulo)
		require.NotNil(t, got.Resumen)
		assert.Equal(t, "Ana Pérez", got.Resumen.Titulo)
		assert.Equal(t, "999.99", got.Resumen.Monto.String())

		// Y NADA más se movió: la fila es evidencia, el resumen es un adorno
		// encima de ella.
		assert.Equal(t, vieja.RetryCount, got.RetryCount)
		assert.Equal(t, vieja.Status, got.Status)
		assert.Equal(t, vieja.ErrorCode, got.ErrorCode)
		assert.WithinDuration(t, vieja.ReceivedAt, got.ReceivedAt, time.Second)
	})
}

func TestGuardarResumen_ModuloSinResumen(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		vieja := intentoConResumen("", "", "", "")
		require.NoError(t, saveOK(s.Save(ctx, vieja)))

		require.NoError(t, s.GuardarResumen(ctx, vieja.ID, "ventas", nil))

		got, err := s.Get(ctx, vieja.ID)
		require.NoError(t, err)
		assert.Equal(t, "ventas", got.Modulo)
		assert.Nil(t, got.Resumen)

		// Y ya no vuelve a salir en el filtro del relleno.
		page, err := s.List(ctx, failedintent.ListParams{SinExtraer: true, PageSize: 50})
		require.NoError(t, err)
		for _, i := range page.Items {
			assert.NotEqual(t, vieja.ID, i.ID)
		}
	})
}

// La carrera que el WHERE del UPDATE cierra: si entre la lectura del janitor y
// su escritura llegó un reintento que YA trajo su propio resumen —extraído del
// cuerpo MÁS NUEVO—, el del janitor viene del cuerpo viejo y no debe pisarlo.
func TestGuardarResumen_NoPisaUnResumenQueYaLlego(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		nueva := intentoConResumen("ventas", "Nombre corregido", "2000", "F-2")
		require.NoError(t, saveOK(s.Save(ctx, nueva)))

		require.NoError(t, s.GuardarResumen(ctx, nueva.ID, "ventas", &failedintent.Resumen{
			Titulo: "Nombre viejo",
		}))

		got, err := s.Get(ctx, nueva.ID)
		require.NoError(t, err)
		require.NotNil(t, got.Resumen)
		assert.Equal(t, "Nombre corregido", got.Resumen.Titulo)
	})
}

// La dedup funde un reintento en la fila pendiente. El resumen sigue la misma
// regla que el cuerpo: nunca se sustituye uno bueno por uno peor.
func TestSave_Dedup_NoBorraElResumenConUnReintentoSinEl(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		primero := intentoConResumen("ventas", "Ana Pérez", "1500", "F-1")
		primero.IdempotencyKey = "clave-compartida"
		require.NoError(t, saveOK(s.Save(ctx, primero)))

		// El reintento no trajo resumen: su blob no se pudo guardar.
		segundo := intentoConResumen("", "", "", "")
		segundo.IdempotencyKey = "clave-compartida"
		segundo.Path = primero.Path
		segundo.ReceivedAt = primero.ReceivedAt.Add(time.Minute)
		require.NoError(t, saveOK(s.Save(ctx, segundo)))

		got, err := s.Get(ctx, primero.ID)
		require.NoError(t, err)
		require.NotNil(t, got.Resumen, "un reintento sin resumen no puede apagar el renglón")
		assert.Equal(t, "Ana Pérez", got.Resumen.Titulo)
		assert.Equal(t, "ventas", got.Modulo)
		assert.Equal(t, 1, got.RetryCount, "y sí se fundió")
	})
}

// El caso del despliegue: la fila la capturó el binario VIEJO (sin módulo ni
// resumen) y el reintento llega con el binario nuevo. La fusión tiene que
// escribir el módulo, o esa venta se queda fuera del chip "Ventas" hasta que
// pase el janitor.
func TestSave_Dedup_LaFusionEstrenaElModuloEnUnaFilaVieja(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		vieja := intentoConResumen("", "", "", "")
		vieja.IdempotencyKey = "clave-3"
		require.NoError(t, saveOK(s.Save(ctx, vieja)))

		nueva := intentoConResumen("ventas", "Ana Pérez", "1500", "F-1")
		nueva.IdempotencyKey = "clave-3"
		nueva.Path = vieja.Path
		nueva.ReceivedAt = vieja.ReceivedAt.Add(time.Minute)
		require.NoError(t, saveOK(s.Save(ctx, nueva)))

		got, err := s.Get(ctx, vieja.ID)
		require.NoError(t, err)
		assert.Equal(t, "ventas", got.Modulo, "la fusión debe estrenar el módulo")
		require.NotNil(t, got.Resumen)
		assert.Equal(t, "Ana Pérez", got.Resumen.Titulo)

		// Y con el módulo puesto, la fila sale del filtro del relleno: el
		// janitor no tiene que volver a mirarla.
		page, err := s.List(ctx, failedintent.ListParams{SinExtraer: true, PageSize: 50})
		require.NoError(t, err)
		for _, i := range page.Items {
			assert.NotEqual(t, vieja.ID, i.ID)
		}
	})
}

// A la inversa: un reintento CON resumen refresca el de la fila. El cuerpo más
// nuevo es el corregido, y su nombre es el que vale.
func TestSave_Dedup_ElResumenNuevoRefrescaAlViejo(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		primero := intentoConResumen("ventas", "Nombre con error", "1500", "F-1")
		primero.IdempotencyKey = "clave-2"
		require.NoError(t, saveOK(s.Save(ctx, primero)))

		segundo := intentoConResumen("ventas", "Nombre corregido", "1800", "F-1")
		segundo.IdempotencyKey = "clave-2"
		segundo.Path = primero.Path
		segundo.ReceivedAt = primero.ReceivedAt.Add(time.Minute)
		require.NoError(t, saveOK(s.Save(ctx, segundo)))

		got, err := s.Get(ctx, primero.ID)
		require.NoError(t, err)
		require.NotNil(t, got.Resumen)
		assert.Equal(t, "Nombre corregido", got.Resumen.Titulo)
		assert.Equal(t, "1800", got.Resumen.Monto.String())
	})
}

// ─── El corte por etapa ───────────────────────────────────────────────────────
//
// Esta pantalla existe para la etapa 1: la creación que NO llegó a la base.
// Un `POST /v2/ventas` fallido no dejó venta en ninguna parte, y su fila aquí
// es el único rastro. Todo lo que lleva un id en la ruta —`PATCH
// /v2/ventas/{id}`, `POST /v2/ventas/{id}/aplicar`— es posterior: la venta ya
// existe, y el renglón jamás podrá decir de quién es porque el cuerpo de esa
// petición nunca llevó un nombre.
//
// La discriminación es estructural, no una heurística sobre mensajes de error:
// si la ruta trae id, la fila ya existe.

// rutasRaizDePrueba es la unión que en producción arma la raíz de composición
// a partir de los prefijos de captura de cada módulo.
var rutasRaizDePrueba = []string{"/v2/ventas", "/v2/cobranza/pagos", "/v2/visitas"}

// sembrarEtapas guarda seis filas del mismo usuario cubriendo las tres etapas
// de dos módulos. Las DOS creaciones son las más viejas a propósito: es lo que
// vuelve detectable un filtro aplicado sobre la página ya recortada.
func sembrarEtapas(
	ctx context.Context, t *testing.T, s *failedintentfb.Store, usuario uuid.UUID,
) map[string]failedintent.Intent {
	t.Helper()

	base := time.Now().UTC().Truncate(time.Millisecond)
	rutas := []struct {
		clave string
		path  string
		edad  time.Duration
	}{
		{"venta_creacion", "/v2/ventas", -5 * time.Hour},
		{"pago_creacion", "/v2/cobranza/pagos", -4 * time.Hour},
		{"venta_detalle", "/v2/ventas/" + uuid.New().String(), -3 * time.Hour},
		{"venta_aplicar", "/v2/ventas/" + uuid.New().String() + "/aplicar", -2 * time.Hour},
		{"pago_aplicar", "/v2/cobranza/pagos/" + uuid.New().String() + "/aplicar", -time.Hour},
		{"venta_search", "/v2/ventas/_search/refresh", 0},
	}

	sembradas := make(map[string]failedintent.Intent, len(rutas))
	for _, r := range rutas {
		i := intentoConResumen("ventas", "Ana", "1000", "a")
		i.Path = r.path
		i.ReceivedAt = base.Add(r.edad)
		i.UsuarioID = &usuario
		require.NoError(t, saveOK(s.Save(ctx, i)))
		sembradas[r.clave] = i
	}
	return sembradas
}

func TestList_AcotaALasRutasRaiz(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		usuario := uuid.New()
		sembradas := sembrarEtapas(ctx, t, s, usuario)

		// Control positivo. La MISMA consulta sin el corte devuelve las seis:
		// sin esto, un resultado "bien filtrado" abajo no se distingue de una
		// consulta rota que no devuelve nada.
		todas, err := s.List(ctx, failedintent.ListParams{
			UsuarioID: &usuario, PageSize: 50,
		})
		require.NoError(t, err)
		require.Len(t, todas.Items, len(sembradas), "el control positivo debe ver las seis")

		acotada, err := s.List(ctx, failedintent.ListParams{
			UsuarioID: &usuario, RutasRaiz: rutasRaizDePrueba, PageSize: 50,
		})
		require.NoError(t, err)

		vistas := make(map[uuid.UUID]bool, len(acotada.Items))
		for _, i := range acotada.Items {
			vistas[i.ID] = true
		}
		assert.True(t, vistas[sembradas["venta_creacion"].ID], "POST /v2/ventas es una creación")
		assert.True(t, vistas[sembradas["pago_creacion"].ID], "POST /v2/cobranza/pagos es una creación")
		assert.False(t, vistas[sembradas["venta_detalle"].ID], "la ruta trae id: la venta ya existe")
		assert.False(t, vistas[sembradas["venta_aplicar"].ID], "/aplicar es el empujón a Microsip")
		assert.False(t, vistas[sembradas["pago_aplicar"].ID], "/aplicar es el empujón a Microsip")
		assert.False(t, vistas[sembradas["venta_search"].ID], "no es la ruta exacta del recurso")
		assert.Len(t, acotada.Items, 2)
	})
}

// El corte va en SQL, no sobre la página ya recibida. La prueba pide UNA fila
// sobre un conjunto donde las dos creaciones son las MÁS VIEJAS: si se filtrara
// en memoria, la página de uno traería la más nueva —`/_search/refresh`— y el
// filtro la descartaría, devolviendo cero.
func TestList_ElCorteDeEtapaVaEnSQL(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		usuario := uuid.New()
		sembradas := sembrarEtapas(ctx, t, s, usuario)

		page, err := s.List(ctx, failedintent.ListParams{
			UsuarioID: &usuario, RutasRaiz: rutasRaizDePrueba, PageSize: 1,
		})
		require.NoError(t, err)
		require.Len(t, page.Items, 1, "el corte debe correr en SQL, no sobre la página")
		assert.Equal(t, sembradas["pago_creacion"].ID, page.Items[0].ID,
			"la creación más nueva, no la fila más nueva")
		assert.True(t, page.HasMore, "queda la otra creación por paginar")
	})
}

// Una lista vacía no acota. Es lo que ve quien no tiene esa lista —el janitor,
// las pruebas— y debe seguir viendo la tabla entera.
func TestList_SinRutasRaizNoAcota(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		usuario := uuid.New()
		sembradas := sembrarEtapas(ctx, t, s, usuario)

		page, err := s.List(ctx, failedintent.ListParams{
			UsuarioID: &usuario, RutasRaiz: nil, PageSize: 50,
		})
		require.NoError(t, err)
		assert.Len(t, page.Items, len(sembradas))
	})
}
