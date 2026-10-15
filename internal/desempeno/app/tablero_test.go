package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func filaDe(t *testing.T, tb desempenoapp.Tablero, uid string) desempenoapp.FilaTablero {
	t.Helper()
	for _, f := range tb.Filas {
		if f.Usuario.UID == uid {
			return f
		}
	}
	t.Fatalf("no hay fila para %s", uid)
	return desempenoapp.FilaTablero{}
}

func TestTablero_Hoy_CifrasDeJuan(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	tb, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{})
	require.NoError(t, err)
	assert.Equal(t, desempenoapp.PeriodoHoy, tb.Periodo)
	assert.Equal(t, 45, tb.PausaMin)
	assert.Equal(t, m.ahora, tb.GeneradoAt)
	require.Len(t, tb.Filas, 3)
	assert.Equal(t, []string{"uid-lupe", "uid-juan", "uid-rosa"},
		[]string{tb.Filas[0].Usuario.UID, tb.Filas[1].Usuario.UID, tb.Filas[2].Usuario.UID},
		"orden: zona y luego nombre")

	f := filaDe(t, tb, "uid-juan")
	assert.True(t, f.Configurado)
	assert.Equal(t, domain.EstadoAhoraActivo, f.EstadoAhora)
	assert.Equal(t, local(t, 14, "15:50"), *f.EstadoDesde, "inicio del tramo actual")
	assert.Equal(t, local(t, 14, "09:00"), *f.PrimeraActividad)
	assert.Equal(t, local(t, 14, "16:20"), *f.UltimaActividadHora)
	require.NotNil(t, f.Ultima)
	assert.Equal(t, "pago:p-80", f.Ultima.ID)
	assert.Equal(t, domain.ResumenPausas{N: 2, Minutos: 70 + 320}, f.Pausas)
	assert.Equal(t, 440-390, f.MinutosActivos)
	assert.Equal(t, 2, f.Pagos.N, "la condonación y el no aplicado no son pagos")
	assert.Equal(t, "350.00", f.Pagos.Importe.StringFixed(2))
	assert.Equal(t, "150.00", f.PagosPorForma.Efectivo.Importe.StringFixed(2))
	assert.Equal(t, "200.00", f.PagosPorForma.Transferencia.Importe.StringFixed(2))
	assert.Equal(t, 0, f.PagosPorForma.Cheque.N, "el cheque fue el día 10")
	assert.Equal(t, 1, f.Condonaciones.N)
	assert.Equal(t, "80.00", f.NoAplicados.Importe.StringFixed(2))
	assert.Equal(t, 1, f.Visitas, "la visita sin uid se le atribuye por COBRADOR_ID único")
	require.Len(t, f.Lineas, 1)
	assert.Equal(t, "2026-10-14", f.Lineas[0].Dia.String())

	// Cobertura de la semana sobre TODA la zona (D-API-5).
	assert.Equal(t, 8, f.VentasUniverso)
	assert.Equal(t, 7, f.CoberturaConAlgo)
	assert.Equal(t, 8, f.CoberturaTotal)
	assert.Equal(t, desempenoapp.CoberturaPorSegmento{SinVisitar: 2, Volver: 2, Despues: 0, Pagados: 4, SinNada: 1}, f.PorSegmento)

	// Cobro: lo que calcula rutas.
	assert.Equal(t, "150.00", f.Cobro.Cobrado.StringFixed(2))
	assert.Equal(t, "300.00", f.Cobro.Esperado.StringFixed(2))
	assert.Equal(t, "150.00", f.Cobro.Falta.StringFixed(2))
	assert.Equal(t, "65.90", f.Cobro.Pct.StringFixed(2))
}

func TestTablero_Semana_SumaLosDiasDesdeElInicio(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	tb, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{Periodo: "semana"})
	require.NoError(t, err)
	f := filaDe(t, tb, "uid-juan")
	require.Len(t, f.Lineas, 6, "del jueves 9 al martes 14")
	assert.Equal(t, "2026-10-09", f.Lineas[0].Dia.String())
	assert.Equal(t, 3, f.Pagos.N, "suma el cheque del día 10; el pago del 8 queda fuera")
	assert.Equal(t, "650.00", f.Pagos.Importe.StringFixed(2))
	assert.Equal(t, 1, f.PagosPorForma.Cheque.N)
	assert.Equal(t, local(t, 10, "10:00"), *f.PrimeraActividad)
	assert.Equal(t, domain.EstadoAhoraActivo, f.EstadoAhora, "el estado ahora siempre es de hoy")
}

func TestTablero_UsuarioSinConfigurar(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	tb, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{})
	require.NoError(t, err)
	f := filaDe(t, tb, "uid-lupe")
	assert.False(t, f.Configurado)
	assert.Equal(t, domain.MotivoSinZona, f.MotivoNoConfigurado)
	assert.Equal(t, domain.EstadoAhoraNoHaEmpezado, f.EstadoAhora)
	assert.Empty(t, f.Lineas)
	assert.NotNil(t, f.Lineas, "lineas = [] y no null")
	assert.Equal(t, 0, f.Pagos.N)
	assert.Nil(t, f.Cobro.Pct)
	for _, ll := range m.actividades.llamadas {
		assert.NotEqual(t, 0, ll.zona, "nunca se consulta la zona 0")
	}
}

func TestTablero_Errores(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)

	_, err := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora).
		Tablero(context.Background(), desempenoapp.TableroInput{Periodo: "mes"})
	require.ErrorIs(t, err, domain.ErrParametroInvalido)

	_, err = nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora).
		Tablero(context.Background(), desempenoapp.TableroInput{PausaMin: "3"})
	require.ErrorIs(t, err, domain.ErrParametroInvalido)

	caido := &fakeUsuarios{err: domain.ErrFirestoreNoDisponible}
	_, err = nuevoServicio(t, caido, m.actividades, m.cuentas, m.ahora).Tablero(context.Background(), desempenoapp.TableroInput{})
	require.ErrorIs(t, err, domain.ErrFirestoreNoDisponible)

	boom := errors.New("firebird caído")
	_, err = nuevoServicio(t, m.usuarios, &fakeActividades{err: boom}, m.cuentas, m.ahora).
		Tablero(context.Background(), desempenoapp.TableroInput{})
	require.ErrorIs(t, err, boom)

	_, err = nuevoServicio(t, m.usuarios, m.actividades, &fakeCuentas{err: boom}, m.ahora).
		Tablero(context.Background(), desempenoapp.TableroInput{})
	require.ErrorIs(t, err, boom)

	m.usuarios.errCobro = boom
	_, err = nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora).
		Tablero(context.Background(), desempenoapp.TableroInput{})
	require.ErrorIs(t, err, boom)
}

func TestTablero_SemanaQueEmpiezaHoyConsultaDesdeMedianoche(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	inicioHoy := local(t, 14, "10:00")
	m.juan.InicioSemana = &inicioHoy
	m.usuarios.usuarios = []domain.UsuarioCobranza{m.juan}
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	tb, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{})
	require.NoError(t, err)
	f := filaDe(t, tb, "uid-juan")
	assert.Equal(t, local(t, 14, "09:00"), *f.PrimeraActividad, "las de la mañana cuentan para hoy")
	assert.Equal(t, 2, f.Pagos.N)
}

// Lecturas por lote (2026-10-14): con repositorios que saben leer por lote,
// el tablero hace UNA lectura de cada tipo para toda la flota, sea cual sea
// el número de zonas e inicios — incluida la ventana de hoy de una semana que
// empezó a media jornada —, y el resultado es idéntico al de leer zona por
// zona.
func TestTablero_PorLoteUnaLecturaDeCadaTipoYMismoResultado(t *testing.T) {
	t.Parallel()
	for _, periodo := range []string{"hoy", "semana"} {
		armar := func() mundo {
			m := nuevoMundo(t)
			us := []domain.UsuarioCobranza{m.juan, m.rosa}
			for i := range 6 {
				u := m.juan
				u.UID, u.Nombre = "uid-"+dos(i), "Cobrador "+dos(i)
				u.ZonaID = []int{zonaRuta21, zonaRuta34, 50000 + i}[i%3]
				if i == 4 {
					hoy := local(t, 14, "10:00") // empezó hoy a media jornada
					u.InicioSemana = &hoy
				}
				us = append(us, u)
			}
			m.usuarios.usuarios = append(us, m.lupe)
			return m
		}
		zonaPorZona := armar()
		esperado, err := nuevoServicio(t, zonaPorZona.usuarios, zonaPorZona.actividades, zonaPorZona.cuentas, zonaPorZona.ahora).
			Tablero(context.Background(), desempenoapp.TableroInput{Periodo: periodo})
		require.NoError(t, err)
		require.Greater(t, len(zonaPorZona.actividades.llamadas), 1, "sin lote se lee por zona")

		m := armar()
		usu := &fakeUsuariosLote{fakeUsuarios: m.usuarios}
		acts := &fakeActividadesLote{fakeActividades: m.actividades}
		cs := &fakeCuentasLote{fakeCuentas: m.cuentas}
		got, err := desempenoapp.NewService(usu, acts, cs, relojFijo{m.ahora}, cdmx(t)).
			Tablero(context.Background(), desempenoapp.TableroInput{Periodo: periodo})
		require.NoError(t, err)
		assert.Equal(t, esperado, got, "%s: por lote da exactamente lo mismo", periodo)
		assert.Len(t, acts.lotes, 1, "%s: una sola lectura de actividades", periodo)
		assert.Len(t, cs.lotes, 1, "%s: una sola lectura de universo", periodo)
		assert.Len(t, usu.lotes, 1, "%s: una sola lectura de cobro de rutas", periodo)
		assert.Len(t, acts.lotes[0], len(zonaPorZona.actividades.llamadas),
			"%s: las mismas ventanas que zona por zona, en una lectura", periodo)
	}
}

// Un lote que devuelve otro número de respuestas es un defecto del adaptador:
// falla la consulta, nunca reparte cifras a la zona equivocada.
func TestTablero_LoteIncompletoFalla(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	acts := &fakeActividadesLote{fakeActividades: m.actividades, recortar: true}
	_, err := desempenoapp.NewService(m.usuarios, acts, m.cuentas, relojFijo{m.ahora}, cdmx(t)).
		Tablero(context.Background(), desempenoapp.TableroInput{})
	require.ErrorIs(t, err, domain.ErrLoteIncompleto)
}

// Revisión del carril A (punto 3): el pool de Firebird es de 10 conexiones; la
// semana (universo, actividades, cobro de rutas) se calcula UNA vez por zona e
// inicio dentro de cada petición del tablero. Juan y Rosa comparten la zona 21.
func TestTablero_DosUsuariosDeLaMismaZonaHacenUnaConsultaDeCadaTipo(t *testing.T) {
	t.Parallel()
	for periodo, pagosJuan := range map[string]int{"hoy": 2, "semana": 3} {
		m := nuevoMundo(t)
		svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
		tb, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{Periodo: periodo})
		require.NoError(t, err)
		assert.Equal(t, 1, m.cuentas.llamadas, "%s: un universo para la zona 21", periodo)
		assert.Len(t, m.actividades.llamadas, 1, "%s: una lectura de actividades para la zona 21", periodo)
		assert.Equal(t, 1, m.usuarios.llamadasCob, "%s: un cobro de rutas para la zona 21", periodo)
		juan, rosa := filaDe(t, tb, "uid-juan"), filaDe(t, tb, "uid-rosa")
		assert.Equal(t, pagosJuan, juan.Pagos.N, "%s: cada fila sigue contando solo lo suyo", periodo)
		assert.Equal(t, 1, rosa.Pagos.N, "%s: Rosa solo registró pago:1003 hoy", periodo)
		assert.Equal(t, juan.CoberturaConAlgo, rosa.CoberturaConAlgo, "%s: misma zona, misma cobertura", periodo)
	}
}

// Inicio de semana distinto en la misma zona = otra ventana: no se comparte.
func TestTablero_MismaZonaConOtroInicioNoComparteSemana(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	otro := local(t, 10, "08:00")
	m.rosa.InicioSemana = &otro
	m.usuarios.usuarios = []domain.UsuarioCobranza{m.juan, m.rosa}
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
	_, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{})
	require.NoError(t, err)
	assert.Equal(t, 2, m.cuentas.llamadas)
	assert.Equal(t, 2, m.usuarios.llamadasCob)
}

// Revisión del carril A (punto 1, contrato v2.1): un inicio de semana futuro
// no tumba el tablero; esa fila sale sin configurar y las demás se calculan.
func TestTablero_InicioSemanaFuturoEsFilaNoConfigurada(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	futuro := m.ahora.Add(3 * time.Hour)
	m.rosa.InicioSemana = &futuro
	m.usuarios.usuarios = []domain.UsuarioCobranza{m.juan, m.rosa, m.lupe}
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	tb, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{})
	require.NoError(t, err)
	rosa := filaDe(t, tb, "uid-rosa")
	assert.False(t, rosa.Configurado)
	assert.Equal(t, domain.MotivoInicioSemanaFuturo, rosa.MotivoNoConfigurado)
	assert.Empty(t, rosa.Lineas)
	assert.Equal(t, 0, rosa.Pagos.N)
	assert.True(t, filaDe(t, tb, "uid-juan").Configurado, "las demás filas se calculan")
}

// Revisión del carril A (punto 4): sin_nada == total − con_algo aunque el
// repositorio devuelva una venta repetida; la repetida se cuenta una vez.
func TestTablero_UniversoConVentaRepetidaSeCuentaUnaVez(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	u := m.cuentas.porZona[zonaRuta21]
	m.cuentas.porZona[zonaRuta21] = append(u, u[7], u[0]) // 8 (sin nada) y 1 (pagada) otra vez
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
	tb, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{})
	require.NoError(t, err)
	f := filaDe(t, tb, "uid-juan")
	assert.Equal(t, 8, f.CoberturaTotal)
	assert.Equal(t, 8, f.VentasUniverso)
	assert.Equal(t, f.CoberturaTotal-f.CoberturaConAlgo, f.PorSegmento.SinNada)
	ps := f.PorSegmento
	assert.Equal(t, f.CoberturaTotal, ps.SinVisitar+ps.Volver+ps.Despues+ps.Pagados, "cada venta en un segmento")
}
