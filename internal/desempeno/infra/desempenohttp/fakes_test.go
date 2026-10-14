package desempenohttp_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/infra/desempenohttp"
)

// ─── fakes de los puertos (sin librerías de mocks) ───────────────────────────

type fakeUsuarios struct {
	us  []domain.UsuarioCobranza
	err error
}

func (f fakeUsuarios) ListarUsuarios(context.Context) ([]domain.UsuarioCobranza, error) {
	return f.us, f.err
}

func (f fakeUsuarios) CobroSemana(context.Context, domain.UsuarioCobranza) (domain.CobroSemana, error) {
	pct := decimal.RequireFromString("65.9")
	return domain.CobroSemana{PctPonderado: &pct, Ventas: []domain.CobroVenta{
		{VentaID: 13785217, Parcialidad: decimal.NewFromInt(150), AbonoSemana: decimal.NewFromInt(150), Vencidas: decimal.NewFromInt(1), AplicaPonderado: true},
	}}, nil
}

type fakeActividades struct{ acts []domain.Actividad }

// ActividadesZona respeta el contrato del puerto: [desde, hasta] inclusivo.
func (f fakeActividades) ActividadesZona(_ context.Context, _ int, desde, hasta time.Time) ([]domain.Actividad, error) {
	var out []domain.Actividad
	for _, a := range f.acts {
		if !a.Hora.Before(desde) && !a.Hora.After(hasta) {
			out = append(out, a)
		}
	}
	return out, nil
}

type fakeCuentas struct{ cs []domain.CuentaUniverso }

func (f fakeCuentas) UniversoSemana(context.Context, int, time.Time) ([]domain.CuentaUniverso, error) {
	return f.cs, nil
}

type reloj struct{ t time.Time }

func (r reloj) Now() time.Time { return r.t }

func f64(v float64) *float64 { return &v }

// Hoy: miércoles 14 de oct de 2026, 16:40 CDMX (22:40Z). Semana desde el
// viernes 9 08:46 CDMX (14:46Z).
var (
	ahora  = time.Date(2026, 10, 14, 22, 40, 0, 0, time.UTC)
	inicio = time.Date(2026, 10, 9, 14, 46, 0, 0, time.UTC)
)

// Usuarios del escenario: uno configurado y uno por cada motivo de "sin
// configurar" del contrato v2.1 §2.
func usuariosEscenario() []domain.UsuarioCobranza {
	ini := inicio
	futuro := ahora.Add(26 * time.Hour)
	return []domain.UsuarioCobranza{
		{
			UID: "abc123", Nombre: "Juan Carlos Méndez", Email: "juan.mendez@muebleriamsp.mx",
			CobradorID: 11502, ZonaID: 21563, ZonaNombre: "RUTA 21", InicioSemana: &ini,
		},
		{
			UID: "sinzona", Nombre: "Rosa María Hernández", Email: "rosa.hernandez@muebleriamsp.mx",
			CobradorID: 11503, InicioSemana: &ini,
		},
		{
			UID: "sininicio", Nombre: "Pedro Ramírez Soto", Email: "pedro.ramirez@muebleriamsp.mx",
			CobradorID: 11504, ZonaID: 21564, ZonaNombre: "RUTA 22",
		},
		{
			UID: "futuro", Nombre: "Lucía Gómez Torres", Email: "lucia.gomez@muebleriamsp.mx",
			CobradorID: 11505, ZonaID: 21565, ZonaNombre: "RUTA 23", InicioSemana: &futuro,
		},
	}
}

func actividadesEscenario() []domain.Actividad {
	recibido := time.Date(2026, 10, 14, 16, 31, 5, 0, time.UTC)
	return []domain.Actividad{
		{
			ID: "pago:15082369", Tipo: domain.TipoPago, Hora: time.Date(2026, 10, 14, 16, 31, 0, 0, time.UTC),
			RecibidoAt: &recibido, ClienteID: 1544855, ClienteNombre: "JUANA ANDRADE LECHUGA",
			VentaID: 13785217, DoctoPVID: 9921, Folio: "Z1677035", NumFotos: 2, Lat: f64(18.4617), Lng: f64(-97.3928),
			RegistradoPor: domain.RegistradoPor{UsuarioUID: "abc123", CobradorID: 11502, Origen: domain.OrigenApp},
			Pago: &domain.DatosPago{
				Importe: decimal.NewFromInt(150), FormaCobroID: 157, Aplicado: true, DoctoCCID: 15082300,
				PagoRecibidoID: "7a1f39e4-0000-4000-8000-000000000001",
			},
		},
		{
			ID: "visita:412cc2fc-0000-4000-8000-000000000002", Tipo: domain.TipoVisita,
			Hora: time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC), ClienteID: 1544900, ClienteNombre: "ROSARIO VILLA",
			RegistradoPor: domain.RegistradoPor{CobradorID: 11502, Nombre: "JUAN CARLOS", Origen: domain.OrigenApp},
			Visita:        &domain.DatosVisita{VisitaID: "412cc2fc-0000-4000-8000-000000000002", TipoVisita: "No se encontraba", Nota: "Vecina dice que regresa el lunes"},
		},
	}
}

func cuentasEscenario() []domain.CuentaUniverso {
	return []domain.CuentaUniverso{{
		VentaID: 13785217, DoctoPVID: 9921, Folio: "Z1677035", ClienteID: 1544855, ClienteNombre: "JUANA ANDRADE LECHUGA",
		Producto: "ROPERO LABRADO CALIFORNIA VINO", Parcialidad: decimal.NewFromInt(150), Saldo: decimal.NewFromInt(16650),
		PrecioTotal: decimal.NewFromInt(18000), Enganche: decimal.NewFromInt(600), FrecPago: "SEMANAL",
		FechaCargo: time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
	}}
}

func nuevoServicio(t *testing.T, usuarios fakeUsuarios) *desempenoapp.Service {
	t.Helper()
	loc, err := time.LoadLocation("America/Mexico_City")
	require.NoError(t, err)
	return desempenoapp.NewService(usuarios, fakeActividades{actividadesEscenario()}, fakeCuentas{cuentasEscenario()}, reloj{ahora}, loc)
}

func servicio(t *testing.T) *desempenoapp.Service {
	t.Helper()
	return nuevoServicio(t, fakeUsuarios{us: usuariosEscenario()})
}

func planter(cu auth.CurrentUser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.PlantCurrentUser(r.Context(), cu)))
		})
	}
}

func usuarioCon(perms ...auth.Permission) auth.CurrentUser {
	ps := make([]string, 0, len(perms))
	for _, p := range perms {
		ps = append(ps, string(p))
	}
	return auth.CurrentUser{
		ID: uuid.New(), FirebaseUID: "fb-araceli", Email: "araceli.soto@muebleriamsp.mx",
		Nombre: "Araceli Soto", Permisos: ps,
	}
}

func routerCon(cu *auth.CurrentUser, svc *desempenoapp.Service) http.Handler {
	r := chi.NewRouter()
	if cu != nil {
		r.Use(planter(*cu))
	}
	desempenohttp.MountRouter(r, svc)
	return r
}

func router(t *testing.T, cu *auth.CurrentUser) http.Handler {
	t.Helper()
	return routerCon(cu, servicio(t))
}
