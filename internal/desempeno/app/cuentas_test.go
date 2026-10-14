package app_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func ventasDe(cs []desempenoapp.CuentaVista) []int {
	out := make([]int, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Cuenta.VentaID)
	}
	return out
}

func TestCuentas_EstadoPorVentaYConteos(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	// La venta 8 nadie la ha abonado: saldo 1500 = 1900 − 400 (D-API-7).
	u := m.cuentas.porZona[zonaRuta21]
	u[7].PrecioTotal = dinero("1900")
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	v, err := svc.Cuentas(context.Background(), "uid-juan", desempenoapp.CuentasInput{})
	require.NoError(t, err)
	assert.Equal(t, *m.juan.InicioSemana, v.InicioSemana)
	assert.Equal(t, desempenoapp.ConteosCuentas{SinVisitar: 2, Volver: 2, Despues: 0, Pagados: 4, SinNada: 1, Total: 8}, v.Conteos)
	require.Len(t, v.Cuentas, 8)
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8}, ventasDe(v.Cuentas), "orden cliente asc")

	por := map[int]desempenoapp.CuentaVista{}
	for _, c := range v.Cuentas {
		por[c.Cuenta.VentaID] = c
	}
	assert.Equal(t, domain.EstadoPago, por[1].Resultado.Estado)
	assert.Equal(t, "Pagó esta semana", por[1].Etiqueta)
	assert.Equal(t, "pago:1001", por[1].Resultado.DeterminadoPor)
	assert.Equal(t, domain.EstadoAbonoParcial, por[4].Resultado.Estado, "lo de oficina también cuenta")
	assert.Equal(t, domain.EstadoSinTocar, por[5].Resultado.Estado, "la condonación no es dinero")
	assert.True(t, por[5].Resultado.ConAlgo)
	require.NotNil(t, por[5].UltimaActividad)
	assert.Equal(t, domain.TipoCondonacion, por[5].UltimaActividad.Tipo)
	assert.Equal(t, domain.EstadoAbonoParcial, por[6].Resultado.Estado, "un pago aún no aplicado sí es dinero en el estado (como el teléfono)")
	assert.False(t, por[8].Resultado.ConAlgo)
	assert.Nil(t, por[8].UltimaActividad)
	// Fórmula de overdue_payments_view (D-API-7): 642 d / 7 = 91.7 parcialidades
	// transcurridas; (91.7·150 − (4000 − 1500))/150 > 1500/150 → tope 10.
	assert.Equal(t, 10, por[8].NumAtrasos, "fórmula del teléfono, no Vencidas de rutas (que dice 2)")
	assert.Equal(t, 0, por[7].NumAtrasos, "liquidada sin FECHA_ULT_PAGO: NULL → 0")
	require.NotNil(t, por[8].Segmento)
	assert.Equal(t, domain.SegmentoSinVisitar, *por[8].Segmento)
	assert.False(t, por[1].Nueva, "ya abonó: 1350 ≠ 4000 − 400")
	assert.True(t, por[8].Nueva, "saldo == total − enganche (OrdenDeCobranza.kt:103)")
	assert.False(t, por[7].Nueva, "liquidada no es nueva")
}

func TestCuentas_FiltrosYOrden(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nombre string
		in     desempenoapp.CuentasInput
		ventas []int
	}{
		{"pagados", desempenoapp.CuentasInput{Segmento: "pagados"}, []int{1, 2, 3, 7}},
		{"volver", desempenoapp.CuentasInput{Segmento: "volver"}, []int{4, 6}},
		{"sin nada", desempenoapp.CuentasInput{Segmento: "sin_nada"}, []int{8}},
		{"q", desempenoapp.CuentasInput{Q: "cliente 1"}, []int{1, 2, 3, 4, 5, 6, 7, 8}},
		{"q por folio", desempenoapp.CuentasInput{Q: "Z07"}, []int{7}},
		{"saldo desc", desempenoapp.CuentasInput{Orden: "saldo", Dir: "desc"}, []int{8, 1, 4, 2, 3, 5, 6, 7}},
		// atrasos = tope saldo/150: 8→10, 1→9, 4→6, 2→5, 3→4, 5→3, 6→2, 7→0.
		{"atrasos desc", desempenoapp.CuentasInput{Orden: "atrasos", Dir: "desc"}, []int{8, 1, 4, 2, 3, 5, 6, 7}},
		{"atrasos asc", desempenoapp.CuentasInput{Orden: "atrasos"}, []int{7, 6, 5, 3, 2, 4, 1, 8}},
		{"última actividad asc", desempenoapp.CuentasInput{Orden: "ultima_actividad"}, []int{8, 7, 1, 2, 3, 4, 5, 6}},
	}
	for _, c := range casos {
		m := nuevoMundo(t)
		svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
		v, err := svc.Cuentas(context.Background(), "uid-juan", c.in)
		require.NoError(t, err, c.nombre)
		assert.Equal(t, c.ventas, ventasDe(v.Cuentas), c.nombre)
		assert.Equal(t, 8, v.Conteos.Total, "%s: los conteos no dependen del filtro", c.nombre)
	}
}

func TestCuentas_Errores(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
	ctx := context.Background()
	for _, in := range []desempenoapp.CuentasInput{{Segmento: "todos"}, {Orden: "folio"}, {Dir: "abajo"}} {
		_, err := svc.Cuentas(ctx, "uid-juan", in)
		require.ErrorIs(t, err, domain.ErrParametroInvalido, "%+v", in)
	}
	_, err := svc.Cuentas(ctx, "uid-lupe", desempenoapp.CuentasInput{})
	require.ErrorIs(t, err, domain.ErrUsuarioSinZona)
	_, err = svc.Cuentas(ctx, "uid-nadie", desempenoapp.CuentasInput{})
	require.ErrorIs(t, err, domain.ErrUsuarioNoEncontrado)
}
