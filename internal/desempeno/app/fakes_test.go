package app_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// ─── fakes de los puertos (sin librerías de mocks) ───────────────────────────

// fakeUsuarios: el cobro de rutas depende solo de zona e inicio de semana
// (rutas app/cobranza_usuarios.go: VentasPorZona(zona, inicio, now)), así que
// el fake lo indexa por zona. Seguro para goroutines.
type fakeUsuarios struct {
	mu          sync.Mutex
	usuarios    []domain.UsuarioCobranza
	cobro       map[int]domain.CobroSemana
	err         error
	errCobro    error
	llamadasCob int
}

func (f *fakeUsuarios) ListarUsuarios(context.Context) ([]domain.UsuarioCobranza, error) {
	return f.usuarios, f.err
}

func (f *fakeUsuarios) CobroSemana(_ context.Context, u domain.UsuarioCobranza) (domain.CobroSemana, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.llamadasCob++
	return f.cobro[u.ZonaID], f.errCobro
}

type llamadaActividades struct {
	zona         int
	desde, hasta time.Time
}

// fakeActividades es seguro para goroutines: el tablero calcula las filas en
// paralelo. espera, si no es nil, retiene cada llamada hasta que se cierre
// (para medir cuántas corren a la vez).
type fakeActividades struct {
	mu        sync.Mutex
	porZona   map[int][]domain.Actividad
	err       error
	llamadas  []llamadaActividades
	enCurso   int
	maxAVez   int
	espera    chan struct{}
	empezadas chan struct{}
}

// ActividadesZona respeta el contrato del puerto: [desde, hasta] inclusivo.
func (f *fakeActividades) ActividadesZona(_ context.Context, zona int, desde, hasta time.Time) ([]domain.Actividad, error) {
	f.mu.Lock()
	f.llamadas = append(f.llamadas, llamadaActividades{zona, desde, hasta})
	f.enCurso++
	f.maxAVez = max(f.maxAVez, f.enCurso)
	espera, empezadas := f.espera, f.empezadas
	f.mu.Unlock()
	if empezadas != nil {
		empezadas <- struct{}{}
	}
	if espera != nil {
		<-espera
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enCurso--
	if f.err != nil {
		return nil, f.err
	}
	var out []domain.Actividad
	for _, a := range f.porZona[zona] {
		if !a.Hora.Before(desde) && !a.Hora.After(hasta) {
			out = append(out, a)
		}
	}
	return out, nil
}

type fakeCuentas struct {
	mu       sync.Mutex
	porZona  map[int][]domain.CuentaUniverso
	err      error
	llamadas int
}

func (f *fakeCuentas) UniversoSemana(_ context.Context, zona int, _ time.Time) ([]domain.CuentaUniverso, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.llamadas++
	return f.porZona[zona], f.err
}

type relojFijo struct{ t time.Time }

func (r relojFijo) Now() time.Time { return r.t }

// ─── fixtures ────────────────────────────────────────────────────────────────

func cdmx(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Mexico_City")
	require.NoError(t, err)
	return loc
}

// local: instante a una hora local de CDMX de un día de octubre de 2026.
func local(t *testing.T, dia int, hhmm string) time.Time {
	t.Helper()
	tt, err := time.ParseInLocation("2006-01-02 15:04", "2026-10-"+dos(dia)+" "+hhmm, cdmx(t))
	require.NoError(t, err)
	return tt.UTC()
}

func dos(n int) string { return fmt.Sprintf("%02d", n) }

func dinero(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func f64(v float64) *float64 { return &v }

const (
	zonaRuta21 = 21563
	zonaRuta34 = 34001
)

// usuarios del escenario: Juan y Rosa comparten la zona 21 desde el jueves 9
// de oct 08:46 (CDMX); Lupe no tiene zona.
func usuariosEscenario(t *testing.T) []domain.UsuarioCobranza {
	t.Helper()
	inicio := local(t, 9, "08:46")
	return []domain.UsuarioCobranza{
		{
			UID: "uid-juan", Nombre: "Juan Carlos Méndez", Email: "juan.mendez@muebleriamsp.mx",
			CobradorID: 11502, ZonaID: zonaRuta21, ZonaNombre: "RUTA 21", InicioSemana: &inicio,
		},
		{
			UID: "uid-rosa", Nombre: "Rosa Elena Pérez", Email: "rosa.perez@muebleriamsp.mx",
			CobradorID: 11600, ZonaID: zonaRuta21, ZonaNombre: "RUTA 21", InicioSemana: &inicio,
		},
		{UID: "uid-lupe", Nombre: "Guadalupe Torres", CobradorID: 11700},
	}
}

func pagoDe(id string, u domain.UsuarioCobranza, h time.Time, ventaID, clienteID int, importe string, forma int) domain.Actividad {
	return domain.Actividad{
		ID: id, Tipo: domain.ClasificarTipoPago(forma, 0), Hora: h, ClienteID: clienteID,
		ClienteNombre: "CLIENTE " + dos(clienteID%100), VentaID: ventaID, Folio: "Z" + dos(ventaID%100),
		RegistradoPor: domain.RegistradoPor{UsuarioUID: u.UID, CobradorID: u.CobradorID, Origen: domain.OrigenApp},
		Lat:           f64(18.4617), Lng: f64(-97.3928),
		Pago: &domain.DatosPago{Importe: dinero(importe), FormaCobroID: forma, Aplicado: true, DoctoCCID: 1},
	}
}

func visitaDe(id string, u domain.UsuarioCobranza, h time.Time, ventaID, clienteID int, tipo string) domain.Actividad {
	return domain.Actividad{
		ID: id, Tipo: domain.TipoVisita, Hora: h, ClienteID: clienteID,
		ClienteNombre: "CLIENTE " + dos(clienteID%100), VentaID: ventaID,
		RegistradoPor: domain.RegistradoPor{CobradorID: u.CobradorID, Nombre: "CRUDO", Origen: domain.OrigenApp},
		Lat:           f64(18.4617), Lng: f64(-97.3928),
		Visita: &domain.DatosVisita{VisitaID: id, TipoVisita: tipo},
	}
}

func oficinaDe(id string, h time.Time, ventaID, clienteID int, importe string) domain.Actividad {
	a := pagoDe(id, domain.UsuarioCobranza{}, h, ventaID, clienteID, importe, domain.FormaCobroEfectivo)
	a.RegistradoPor = domain.RegistradoPor{Origen: domain.OrigenOficina}
	return a
}

func cuenta(ventaID, clienteID int, parcialidad, saldo string) domain.CuentaUniverso {
	return domain.CuentaUniverso{
		VentaID: ventaID, ClienteID: clienteID, ClienteNombre: "CLIENTE " + dos(clienteID%100),
		Folio: "Z" + dos(ventaID%100), Parcialidad: dinero(parcialidad), Saldo: dinero(saldo),
		PrecioTotal: dinero("4000"), Enganche: dinero("400"), FrecPago: "SEMANAL",
		FechaCargo: time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC),
	}
}

func nuevoServicio(t *testing.T, u *fakeUsuarios, a *fakeActividades, c *fakeCuentas, ahora time.Time) *desempenoapp.Service {
	t.Helper()
	return desempenoapp.NewService(u, a, c, relojFijo{ahora}, cdmx(t))
}
