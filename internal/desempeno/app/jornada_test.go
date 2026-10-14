package app_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestJornada_SoloLoQueRegistroEsteUsuario(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	v, err := svc.Jornada(context.Background(), "uid-juan", desempenoapp.JornadaInput{})
	require.NoError(t, err)
	assert.Equal(t, "uid-juan", v.UID)
	assert.Equal(t, 45, v.PausaMin)
	assert.Equal(t, m.ahora, v.GeneradoAt)
	assert.Equal(t, "2026-10-14", v.Jornada.Dia.String(), "sin fecha es hoy en CDMX")
	ids := make([]string, 0, len(v.Jornada.Actividades))
	for _, a := range v.Jornada.Actividades {
		ids = append(ids, a.ID)
	}
	assert.Equal(t, []string{"pago:1001", "pago:1002", "visita:v-1", "pago:1005", "pago:p-80"}, ids,
		"sin lo de Rosa ni lo de oficina")
	assert.Equal(t, "Juan Carlos Méndez", v.Jornada.Actividades[2].RegistradoPor.Nombre, "nombre resuelto")

	ll := m.actividades.llamadas[len(m.actividades.llamadas)-1]
	assert.Equal(t, local(t, 14, "00:00"), ll.desde)
	assert.Equal(t, local(t, 15, "00:00"), ll.hasta)
}

func TestJornada_FechaPasadaYParametros(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	v, err := svc.Jornada(context.Background(), "uid-juan", desempenoapp.JornadaInput{Fecha: "2026-10-10", PausaMin: "30"})
	require.NoError(t, err)
	require.Len(t, v.Jornada.Actividades, 1)
	assert.Equal(t, "pago:1007", v.Jornada.Actividades[0].ID)
	assert.Equal(t, 30, v.PausaMin)
	assert.Empty(t, v.Jornada.Linea.Pausas, "un día pasado no tiene pausa en curso")
}

func TestJornada_Errores(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
	ctx := context.Background()

	_, err := svc.Jornada(ctx, "uid-nadie", desempenoapp.JornadaInput{})
	require.ErrorIs(t, err, domain.ErrUsuarioNoEncontrado)

	_, err = svc.Jornada(ctx, "uid-lupe", desempenoapp.JornadaInput{})
	require.ErrorIs(t, err, domain.ErrUsuarioSinZona)

	_, err = svc.Jornada(ctx, "uid-juan", desempenoapp.JornadaInput{Fecha: "14/10/2026"})
	require.ErrorIs(t, err, domain.ErrParametroInvalido)

	_, err = svc.Jornada(ctx, "uid-juan", desempenoapp.JornadaInput{DesdeHora: "08:00"})
	require.ErrorIs(t, err, domain.ErrParametroInvalido)

	_, err = nuevoServicio(t, &fakeUsuarios{err: domain.ErrFirestoreNoDisponible}, m.actividades, m.cuentas, m.ahora).
		Jornada(ctx, "uid-juan", desempenoapp.JornadaInput{})
	require.ErrorIs(t, err, domain.ErrFirestoreNoDisponible)
}
