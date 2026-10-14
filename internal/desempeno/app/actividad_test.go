package app_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func idsDe(acts []domain.Actividad) []string {
	out := make([]string, 0, len(acts))
	for _, a := range acts {
		out = append(out, a.ID)
	}
	return out
}

func TestActividad_TodaLaZonaPorOmision(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	pag, err := svc.Actividad(context.Background(), "uid-juan", desempenoapp.FiltroActividadInput{})
	require.NoError(t, err)
	// semana del 9 al 14, cualquier registrador, hora desc.
	assert.Equal(t, []string{
		"pago:p-80", "pago:1005", "pago:1004", "pago:1003", "visita:v-1", "pago:1002", "pago:1001", "pago:1007",
	}, idsDe(pag.Items))
	assert.Empty(t, pag.SiguienteCursor)
	assert.Equal(t, 8, pag.Totales.N)
	assert.Equal(t, "900.00", pag.Totales.Importe.StringFixed(2),
		"solo dinero (contrato §4): 150+200+150+100+300; ni la condonación (50) ni el no aplicado (80)")
	assert.Equal(t, map[domain.TipoActividad]int{domain.TipoPago: 6, domain.TipoVisita: 1, domain.TipoCondonacion: 1}, pag.Totales.PorTipo)

	ll := m.actividades.llamadas[0]
	assert.Equal(t, *m.juan.InicioSemana, ll.desde)
	assert.Equal(t, m.ahora, ll.hasta)
}

func TestActividad_Filtros(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nombre string
		in     desempenoapp.FiltroActividadInput
		ids    []string
	}{
		{"tipo visita", desempenoapp.FiltroActividadInput{Tipos: []string{"visita"}}, []string{"visita:v-1"}},
		{"tipo pago y condonación", desempenoapp.FiltroActividadInput{Tipos: []string{"condonacion"}}, []string{"pago:1005"}},
		{"forma transferencia", desempenoapp.FiltroActividadInput{FormasCobro: []string{"transferencia"}}, []string{"pago:1002"}},
		{"forma cheque o condonación", desempenoapp.FiltroActividadInput{FormasCobro: []string{"cheque", "condonacion"}}, []string{"pago:1005", "pago:1007"}},
		{"tipo de visita literal", desempenoapp.FiltroActividadInput{TiposVisita: []string{"No se encontraba"}}, []string{"visita:v-1"}},
		{"registrado por oficina", desempenoapp.FiltroActividadInput{RegistradoPor: []string{"oficina"}}, []string{"pago:1004"}},
		{"registrado por Rosa", desempenoapp.FiltroActividadInput{RegistradoPor: []string{"uid-rosa"}}, []string{"pago:1003"}},
		{"monto entre 150 y 200", desempenoapp.FiltroActividadInput{MontoMin: "150", MontoMax: "200"}, []string{"pago:1003", "pago:1002", "pago:1001"}},
		{"con fotos", desempenoapp.FiltroActividadInput{ConFotos: "true"}, []string{}},
		{"sin GPS", desempenoapp.FiltroActividadInput{ConGPS: "false"}, []string{}},
		{"q sin acentos ni mayúsculas", desempenoapp.FiltroActividadInput{Q: "cliente 17"}, []string{"pago:1007"}},
		{"q por folio", desempenoapp.FiltroActividadInput{Q: "z03"}, []string{"pago:1003"}},
		{"rango explícito", desempenoapp.FiltroActividadInput{Desde: "2026-10-10T00:00:00Z", Hasta: "2026-10-11T00:00:00Z"}, []string{"pago:1007"}},
	}
	for _, c := range casos {
		m := nuevoMundo(t)
		svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
		pag, err := svc.Actividad(context.Background(), "uid-juan", c.in)
		require.NoError(t, err, c.nombre)
		assert.Equal(t, c.ids, idsDe(pag.Items), c.nombre)
		assert.Equal(t, len(c.ids), pag.Totales.N, c.nombre)
	}
}

func TestActividad_OrdenYCursorEstable(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
	ctx := context.Background()

	for _, orden := range []string{"hora", "cliente", "monto", "tipo", "registrado_por"} {
		for _, dir := range []string{"asc", "desc"} {
			todo, err := svc.Actividad(ctx, "uid-juan", desempenoapp.FiltroActividadInput{Orden: orden, Dir: dir, Limit: "500"})
			require.NoError(t, err)
			var paginado []domain.Actividad
			cursor := ""
			for range 10 {
				pag, err := svc.Actividad(ctx, "uid-juan", desempenoapp.FiltroActividadInput{Orden: orden, Dir: dir, Limit: "3", Cursor: cursor})
				require.NoError(t, err)
				paginado = append(paginado, pag.Items...)
				assert.Equal(t, 8, pag.Totales.N, "los totales son del filtro completo")
				if pag.SiguienteCursor == "" {
					break
				}
				cursor = pag.SiguienteCursor
			}
			assert.Equal(t, idsDe(todo.Items), idsDe(paginado), "orden=%s dir=%s: páginas = lista completa", orden, dir)
		}
	}

	monto, err := svc.Actividad(ctx, "uid-juan", desempenoapp.FiltroActividadInput{Orden: "monto", Dir: "desc", Limit: "2"})
	require.NoError(t, err)
	assert.Equal(t, []string{"pago:1007", "pago:1002"}, idsDe(monto.Items))
}

func TestActividad_ElCursorSobreviveAQueDesaparezcaSuFila(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
	ctx := context.Background()
	pag, err := svc.Actividad(ctx, "uid-juan", desempenoapp.FiltroActividadInput{Limit: "3"})
	require.NoError(t, err)
	require.Equal(t, []string{"pago:p-80", "pago:1005", "pago:1004"}, idsDe(pag.Items))

	// pago:1004 (la última de la página) se borra antes de pedir la siguiente.
	acts := m.actividades.porZona[zonaRuta21]
	for i, a := range acts {
		if a.ID == "pago:1004" {
			m.actividades.porZona[zonaRuta21] = append(acts[:i:i], acts[i+1:]...)
			break
		}
	}
	sig, err := svc.Actividad(ctx, "uid-juan", desempenoapp.FiltroActividadInput{Limit: "3", Cursor: pag.SiguienteCursor})
	require.NoError(t, err)
	assert.Equal(t, []string{"pago:1003", "visita:v-1", "pago:1002"}, idsDe(sig.Items), "ni salta ni repite")
}

func TestActividad_ParametrosInvalidos(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
	ctx := context.Background()
	malos := []desempenoapp.FiltroActividadInput{
		{Tipos: []string{"llamada"}},
		{FormasCobro: []string{"deposito"}},
		{MontoMin: "cien"},
		{ConFotos: "si"},
		{ConGPS: "1"},
		{Orden: "folio"},
		{Dir: "arriba"},
		{Limit: "0"},
		{Limit: "501"},
		{Cursor: "no-es-base64!"},
		{Desde: "2026-10-10"},
		{Hasta: "ayer"},
		{Desde: "2026-07-01T00:00:00Z", Hasta: "2026-10-14T00:00:00Z"}, // > 92 días
		{Desde: "2026-10-14T00:00:00Z", Hasta: "2026-10-10T00:00:00Z"},
	}
	for _, in := range malos {
		_, err := svc.Actividad(ctx, "uid-juan", in)
		require.ErrorIs(t, err, domain.ErrParametroInvalido, "%+v", in)
	}

	pag, err := svc.Actividad(ctx, "uid-juan", desempenoapp.FiltroActividadInput{Orden: "hora", Limit: "2"})
	require.NoError(t, err)
	_, err = svc.Actividad(ctx, "uid-juan", desempenoapp.FiltroActividadInput{Orden: "cliente", Limit: "2", Cursor: pag.SiguienteCursor})
	require.ErrorIs(t, err, domain.ErrParametroInvalido, "un cursor de otro orden se rechaza")

	_, err = svc.Actividad(ctx, "uid-lupe", desempenoapp.FiltroActividadInput{})
	require.ErrorIs(t, err, domain.ErrUsuarioSinZona)

	sinInicio := m.juan
	sinInicio.UID, sinInicio.InicioSemana = "uid-sin-inicio", nil
	m.usuarios.usuarios = append(m.usuarios.usuarios, sinInicio)
	_, err = svc.Actividad(ctx, "uid-sin-inicio", desempenoapp.FiltroActividadInput{})
	require.ErrorIs(t, err, domain.ErrUsuarioSinInicioSemana, "sin desde y sin inicio de semana no hay rango")
	_, err = svc.Actividad(ctx, "uid-sin-inicio", desempenoapp.FiltroActividadInput{Desde: "2026-10-10T00:00:00Z"})
	require.NoError(t, err, "con desde explícito sí")
}
