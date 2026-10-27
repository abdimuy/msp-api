package app_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// mundo es el escenario compartido por las pruebas del tablero, la jornada,
// la actividad y las cuentas. Zona 21, semana desde el jueves 9 de oct 08:46;
// hoy martes 14 de oct 16:40 (CDMX).
type mundo struct {
	juan, rosa, lupe domain.UsuarioCobranza
	ahora            time.Time
	usuarios         *fakeUsuarios
	actividades      *fakeActividades
	cuentas          *fakeCuentas
}

func nuevoMundo(t *testing.T) mundo {
	t.Helper()
	us := usuariosEscenario(t)
	juan, rosa, lupe := us[0], us[1], us[2]
	ahora := local(t, 14, "16:40") // hoy: martes 14 de oct
	noAplicado := pagoDe("pago:p-80", juan, local(t, 14, "16:20"), 6, 16, "80.00", domain.FormaCobroEfectivo)
	noAplicado.Pago.Aplicado = false
	noAplicado.Pago.DoctoCCID = 0
	acts := []domain.Actividad{
		pagoDe("pago:1001", juan, local(t, 14, "09:00"), 1, 10, "150.00", domain.FormaCobroEfectivo),
		pagoDe("pago:1002", juan, local(t, 14, "09:20"), 2, 11, "200.00", domain.FormaCobroTransferencia),
		visitaDe("visita:v-1", juan, local(t, 14, "10:30"), 0, 12, domain.TipoVisitaNoSeEncontraba),
		pagoDe("pago:1003", rosa, local(t, 14, "11:00"), 3, 13, "150.00", domain.FormaCobroEfectivo),
		oficinaDe("pago:1004", local(t, 14, "12:00"), 4, 14, "100.00"),
		pagoDe("pago:1005", juan, local(t, 14, "15:50"), 5, 15, "50.00", domain.FormaCobroCondonacion),
		noAplicado,
		pagoDe("pago:1007", juan, local(t, 10, "10:00"), 7, 17, "300.00", domain.FormaCobroCheque),
		pagoDe("pago:1000", juan, local(t, 8, "10:00"), 1, 10, "999.00", domain.FormaCobroEfectivo), // antes de la semana
	}
	universo := []domain.CuentaUniverso{
		cuenta(1, 10, "150.00", "1350.00"), cuenta(2, 11, "150.00", "800.00"),
		cuenta(3, 13, "150.00", "600.00"), cuenta(4, 14, "150.00", "900.00"),
		cuenta(5, 15, "150.00", "450.00"), cuenta(6, 16, "150.00", "300.00"),
		cuenta(7, 17, "150.00", "0.00"), cuenta(8, 18, "150.00", "1500.00"),
	}
	pct := decimal.RequireFromString("65.90")
	return mundo{
		juan: juan, rosa: rosa, lupe: lupe, ahora: ahora,
		usuarios: &fakeUsuarios{
			usuarios: []domain.UsuarioCobranza{rosa, lupe, juan},
			cobro: map[int]domain.CobroSemana{
				zonaRuta21: {PctPonderado: &pct, Ventas: []domain.CobroVenta{
					{VentaID: 1, Parcialidad: dinero("150"), AbonoSemana: dinero("150"), AplicaPonderado: true},
					{VentaID: 8, Parcialidad: dinero("150"), AbonoSemana: dinero("0"), Vencidas: dinero("2"), AplicaPonderado: true},
				}},
			},
		},
		actividades: &fakeActividades{porZona: map[int][]domain.Actividad{zonaRuta21: acts}},
		cuentas:     &fakeCuentas{porZona: map[int][]domain.CuentaUniverso{zonaRuta21: universo}},
	}
}
