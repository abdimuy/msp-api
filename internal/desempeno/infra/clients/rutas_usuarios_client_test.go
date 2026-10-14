package clients_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	desempenoclients "github.com/abdimuy/msp-api/internal/desempeno/infra/clients"
	"github.com/abdimuy/msp-api/internal/rutas"
)

type fakeRutas struct {
	usuarios []rutas.UsuarioCobrador
	cobranza rutas.CobranzaUsuario
	errUs    error
	errCob   error
	pedido   rutas.UsuarioCobrador
}

func (f *fakeRutas) UsuariosCobradores(context.Context) ([]rutas.UsuarioCobrador, error) {
	return f.usuarios, f.errUs
}

func (f *fakeRutas) CobranzaDeUsuario(_ context.Context, u rutas.UsuarioCobrador) (rutas.CobranzaUsuario, error) {
	f.pedido = u
	return f.cobranza, f.errCob
}

func TestRutasUsuariosClient_ListarUsuarios(t *testing.T) {
	t.Parallel()
	inicio := time.Date(2026, 10, 9, 14, 46, 0, 0, time.UTC)
	f := &fakeRutas{usuarios: []rutas.UsuarioCobrador{{
		UID: "uid-juan", Nombre: "Juan Carlos Méndez", Email: "juan.mendez@muebleriamsp.mx",
		CobradorID: 11502, ZonaID: 21563, ZonaNombre: "RUTA 21", InicioSemana: &inicio,
	}}}
	got, err := desempenoclients.NewRutasUsuariosClient(f).ListarUsuarios(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []domain.UsuarioCobranza{{
		UID: "uid-juan", Nombre: "Juan Carlos Méndez", Email: "juan.mendez@muebleriamsp.mx",
		CobradorID: 11502, ZonaID: 21563, ZonaNombre: "RUTA 21", InicioSemana: &inicio,
	}}, got)
}

func TestRutasUsuariosClient_FirestoreCaidoEs503(t *testing.T) {
	t.Parallel()
	f := &fakeRutas{errUs: rutas.ErrCalendarioNoDisponible.WithError(errors.New("deadline"))}
	_, err := desempenoclients.NewRutasUsuariosClient(f).ListarUsuarios(context.Background())
	require.ErrorIs(t, err, domain.ErrFirestoreNoDisponible)

	otro := errors.New("firebird caído")
	_, err = desempenoclients.NewRutasUsuariosClient(&fakeRutas{errUs: otro}).ListarUsuarios(context.Background())
	require.ErrorIs(t, err, otro)
	require.NotErrorIs(t, err, domain.ErrFirestoreNoDisponible, "un error de Firebird no se disfraza de Firestore")
}

func TestRutasUsuariosClient_CobroSemana(t *testing.T) {
	t.Parallel()
	pct := decimal.RequireFromString("65.9")
	f := &fakeRutas{cobranza: rutas.CobranzaUsuario{
		PctPonderado: &pct,
		Ventas: []rutas.VentaCobro{{
			VentaID: 1, Parcialidad: decimal.NewFromInt(150), AbonoSemana: decimal.NewFromInt(150),
			Vencidas: decimal.NewFromInt(2), AplicaPonderado: true,
		}},
	}}
	u := domain.UsuarioCobranza{UID: "uid-juan", CobradorID: 11502, ZonaID: 21563}
	got, err := desempenoclients.NewRutasUsuariosClient(f).CobroSemana(context.Background(), u)
	require.NoError(t, err)
	assert.Equal(t, "uid-juan", f.pedido.UID)
	assert.Equal(t, 21563, f.pedido.ZonaID)
	require.Len(t, got.Ventas, 1)
	assert.True(t, got.Ventas[0].Vencidas.Equal(decimal.NewFromInt(2)))
	assert.Equal(t, &pct, got.PctPonderado)

	boom := errors.New("boom")
	_, err = desempenoclients.NewRutasUsuariosClient(&fakeRutas{errCob: boom}).CobroSemana(context.Background(), u)
	require.ErrorIs(t, err, boom)
}
