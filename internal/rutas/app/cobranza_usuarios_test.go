//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/rutas"
	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

func TestService_UsuariosCobradores_IncluyeNoConfiguradosYNombreDeZona(t *testing.T) {
	t.Parallel()
	inicio := time.Date(2026, 10, 9, 13, 46, 0, 0, time.UTC)
	svc := NewService(
		&fakeRutasRepo{rows: []rutasdomain.RutaResumen{{ZonaID: 21563, ZonaNombre: "RUTA 21"}}},
		&fakeCobranzaRepo{},
		&fakeCalendario{usuarios: []outbound.UsuarioCobrador{
			{UID: "juan", Nombre: "Juan Carlos Méndez", CobradorID: 11502, ZonaID: 21563, FechaInicio: inicio},
			{UID: "sinzona", Nombre: "Rosa Elena Pérez", CobradorID: 11600},
			{UID: "sinfecha", Nombre: "Guadalupe Torres", CobradorID: 11700, ZonaID: 21563},
		}},
	)

	got, err := svc.UsuariosCobradores(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 3, "los no configurados también salen: el tablero los marca")
	assert.Equal(t, "RUTA 21", got[0].ZonaNombre)
	require.NotNil(t, got[0].InicioSemana)
	assert.Equal(t, inicio, *got[0].InicioSemana)
	assert.Equal(t, 0, got[1].ZonaID)
	assert.Empty(t, got[1].ZonaNombre)
	assert.Nil(t, got[2].InicioSemana)
}

func TestService_UsuariosCobradores_FirestoreCaidoEsErrorTipado(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeRutasRepo{}, &fakeCobranzaRepo{},
		&fakeCalendario{usuariosE: errors.New("firestore: deadline exceeded")})

	_, err := svc.UsuariosCobradores(context.Background())
	require.ErrorIs(t, err, rutas.ErrCalendarioNoDisponible)
}

func TestService_UsuariosCobradores_ErrorDeZonasSePropaga(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeRutasRepo{err: errors.New("firebird caído")}, &fakeCobranzaRepo{},
		&fakeCalendario{usuarios: []outbound.UsuarioCobrador{{UID: "juan", CobradorID: 1}}})

	_, err := svc.UsuariosCobradores(context.Background())
	require.Error(t, err)
	require.NotErrorIs(t, err, rutas.ErrCalendarioNoDisponible)
}

func TestService_CobranzaDeUsuario_IgualQueDesglosePorUsuario(t *testing.T) {
	t.Parallel()
	zonaID := 21563
	// Ventana de 8 días hacia atrás desde el reloj real (los dos servicios
	// usan time.Now): siempre contiene un vencimiento semanal, así que la venta
	// SÍ aplica al ponderado y PctPonderado no puede ser nil.
	inicio := time.Now().UTC().AddDate(0, 0, -8).Truncate(time.Minute)
	ventas := []rutasdomain.VentaCobranza{{
		VentaID: 1, ClienteID: 10, ZonaID: zonaID,
		Parcialidad: decimal.NewFromInt(100), Frecuencia: rutasdomain.Semanal,
		AbonoSemana: decimal.NewFromInt(100), Saldo: decimal.NewFromInt(900),
		TotalImporte: decimal.NewFromInt(4000), FechaCargo: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}}
	cal := &fakeCalendario{usuarios: []outbound.UsuarioCobrador{
		{UID: "juan", CobradorID: 11502, ZonaID: zonaID, FechaInicio: inicio},
	}}
	repo := &fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{zonaID: ventas}}
	svc := NewService(&fakeRutasRepo{}, repo, cal)

	got, err := svc.CobranzaDeUsuario(context.Background(), rutas.UsuarioCobrador{UID: "juan", CobradorID: 11502, ZonaID: zonaID, InicioSemana: &inicio})
	require.NoError(t, err)

	// Mismo resultado que el endpoint que oficina ya usa (/v2/rutas/usuarios/{uid}/cobranza).
	desglose, _, _, err := NewService(&fakeRutasRepo{}, &fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{zonaID: {ventas[0]}}}, cal).
		DesglosePorUsuario(context.Background(), "juan")
	require.NoError(t, err)
	require.Len(t, got.Ventas, 1)
	require.Len(t, desglose, 1)
	assert.Equal(t, desglose[0].VentaID, got.Ventas[0].VentaID)
	assert.True(t, desglose[0].Aporte.Equal(got.Ventas[0].Aporte))
	assert.True(t, desglose[0].Vencidas.Equal(got.Ventas[0].Vencidas))
	require.True(t, got.Ventas[0].AplicaPonderado, "la venta debe aplicar al ponderado para que la prueba distinga")
	assert.Equal(t, desglose[0].AplicaPonderado, got.Ventas[0].AplicaPonderado)
	esperado := rutasdomain.CalcularResumenPonderado(desglose).Pct
	require.NotNil(t, esperado)
	require.NotNil(t, got.PctPonderado, "el % ponderado se asigna")
	assert.True(t, esperado.Equal(*got.PctPonderado), "%s vs %s", esperado, got.PctPonderado)
}

func TestService_UsuariosCobradores_DescartaCobradorIDNoPositivo(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeRutasRepo{}, &fakeCobranzaRepo{}, &fakeCalendario{usuarios: []outbound.UsuarioCobrador{
		{UID: "cero", Nombre: "Pedro Salinas", CobradorID: 0, ZonaID: 1},
		{UID: "negativo", Nombre: "Lucía Ortiz", CobradorID: -1, ZonaID: 1},
		{UID: "juan", Nombre: "Juan Carlos Méndez", CobradorID: 11502, ZonaID: 1},
	}})

	got, err := svc.UsuariosCobradores(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1, "solo usuarios con COBRADOR_ID > 0 (D-API-9)")
	assert.Equal(t, "juan", got[0].UID)
}

// calendarioNoConfigurado imita a rutasfirestore.NoopCalendarioClient: no
// falla, pero tampoco está conectado a Firestore.
type calendarioNoConfigurado struct{ fakeCalendario }

func (calendarioNoConfigurado) Configurado() bool { return false }

func TestService_UsuariosCobradores_CalendarioNoConfiguradoEs503(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeRutasRepo{}, &fakeCobranzaRepo{}, &calendarioNoConfigurado{})

	_, err := svc.UsuariosCobradores(context.Background())
	require.ErrorIs(t, err, rutas.ErrCalendarioNoDisponible, "sin Firestore no se inventa una flota vacía")
}

func TestService_CobranzaDeUsuario_SinConfigurarNoConsulta(t *testing.T) {
	t.Parallel()
	repo := &fakeCobranzaRepo{err: errors.New("no debió consultarse")}
	svc := NewService(&fakeRutasRepo{}, repo, &fakeCalendario{})
	inicio := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)

	for _, u := range []rutas.UsuarioCobrador{{UID: "a"}, {UID: "b", ZonaID: 1}, {UID: "c", InicioSemana: &inicio}} {
		got, err := svc.CobranzaDeUsuario(context.Background(), u)
		require.NoError(t, err, u.UID)
		assert.Empty(t, got.Ventas, u.UID)
		assert.Nil(t, got.PctPonderado, u.UID)
	}
}

func TestService_CobranzaDeUsuario_ErrorDeFirebirdSePropaga(t *testing.T) {
	t.Parallel()
	inicio := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	svc := NewService(&fakeRutasRepo{}, &fakeCobranzaRepo{err: errors.New("firebird caído")}, &fakeCalendario{})
	_, err := svc.CobranzaDeUsuario(context.Background(), rutas.UsuarioCobrador{UID: "a", ZonaID: 1, InicioSemana: &inicio})
	require.Error(t, err)
}
