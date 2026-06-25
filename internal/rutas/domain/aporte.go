//nolint:misspell // rutas vocabulary is Spanish per project convention.
package domain

import "github.com/shopspring/decimal"

// Frecuencia represents the payment cadence stored in LIBRES_CARGOS_CC.FORMA_DE_PAGO
// (resolved via LISTAS_ATRIBUTOS.VALOR_DESPLEGADO → "SEMANAL"/"QUINCENAL"/"MENSUAL").
type Frecuencia string

// Payment cadence constants matching LISTAS_ATRIBUTOS.VALOR_DESPLEGADO values.
const (
	Semanal   Frecuencia = "SEMANAL"
	Quincenal Frecuencia = "QUINCENAL"
	Mensual   Frecuencia = "MENSUAL"
	// Contado is the forma-de-pago for cash sales. These are NOT credit
	// collection: they have no real parcialidad/plazos, so they must be
	// excluded from every weekly cobranza metric (cobertura, ponderado) and
	// from the breakdown. VALOR_DESPLEGADO arrives uppercased from the query.
	Contado Frecuencia = "CONTADO"
)

// EsContado reports whether this cadence is a cash sale (de contado). Cash
// sales never participate in weekly credit-collection metrics.
func (f Frecuencia) EsContado() bool { return f == Contado }

// CadenciaDias returns the number of days between expected payments for a
// given cadence. Defaults to 7 (semanal) for any unrecognized value.
func CadenciaDias(f Frecuencia) int {
	switch f {
	case Semanal:
		return 7
	case Quincenal:
		return 15
	case Mensual:
		return 30
	case Contado:
		// Cash sales are not periodic; they are excluded from cobranza upstream.
		return 7
	default:
		return 7
	}
}

// AporteInput holds the inputs to CalcAporte. All fields are decimal to
// guarantee fractional precision — NUMERIC columns from Firebird must be
// scanned with firebird.ScanDecimal before being placed here.
type AporteInput struct {
	// Parcialidad is the expected periodic payment amount (LIBRES_CARGOS_CC).
	Parcialidad decimal.Decimal
	// Plazos is (fechaInicioSemana − fechaCargo) / cadenciaDias, may be fractional.
	Plazos decimal.Decimal
	// TotalImporte is the original credit total (MSP_SALDOS_VENTAS.PRECIO_TOTAL).
	// NOTE: it is PRECIO_TOTAL, not the misleadingly-named TOTAL_IMPORTE column
	// (which is the sum of payments). See cobranza_repo queryVentasPorZona.
	TotalImporte decimal.Decimal
	// AbonoSemana is the sum of all valid payments in the reporting window.
	AbonoSemana decimal.Decimal
	// SaldoHoy is the current outstanding balance (MSP_SALDOS_VENTAS.SALDO).
	SaldoHoy decimal.Decimal
}

// CalcAporte computes how many "quotas" this venta contributed during the
// reporting window, capped correctly for overdue accounts.
//
// Formula (all decimal, never integer division):
//
//	saldoAlInicio = SaldoHoy + AbonoSemana
//	pagadoAntes   = TotalImporte − saldoAlInicio
//	debia         = MIN(Parcialidad × Plazos, TotalImporte)   ← cap to credit total
//	vencidas      = MAX(0, (debia − pagadoAntes) / Parcialidad)
//	aporte        = MIN(AbonoSemana / Parcialidad, vencidas + 1)
//
// Returns decimal.Zero when Parcialidad ≤ 0 (guard against divide-by-zero and
// non-credit accounts).
func CalcAporte(in AporteInput) decimal.Decimal {
	if in.Parcialidad.IsZero() || in.Parcialidad.IsNegative() {
		return decimal.Zero
	}

	saldoAlInicio := in.SaldoHoy.Add(in.AbonoSemana)
	pagadoAntes := in.TotalImporte.Sub(saldoAlInicio)

	// expected debt capped at original credit total
	expectedDebt := in.Parcialidad.Mul(in.Plazos)
	debia := decimal.Min(expectedDebt, in.TotalImporte)

	// overdue quotas, floored at zero
	diff := debia.Sub(pagadoAntes)
	vencidasRaw := diff.Div(in.Parcialidad)
	vencidas := decimal.Max(decimal.Zero, vencidasRaw)

	// final aporte: payment-in-quotas capped at (vencidas + 1)
	abonoEnCuotas := in.AbonoSemana.Div(in.Parcialidad)
	return decimal.Min(abonoEnCuotas, vencidas.Add(decimal.NewFromInt(1)))
}
