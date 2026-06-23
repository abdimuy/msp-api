//nolint:misspell // rutas vocabulary is Spanish per project convention.
package domain

import "github.com/shopspring/decimal"

// AporteDesglose is the per-venta breakdown of how a sale contributes to the
// weighted percentage, expressed in both quotas and pesos. It makes the
// office-facing calculation auditable. Pure projection of existing values; it
// does NOT re-run CalcAporte.
type AporteDesglose struct {
	AtrasoAntesCuotas   decimal.Decimal // overdue quotas at window start (= vencidas)
	AtrasoAntesPesos    decimal.Decimal // vencidas × parcialidad
	PagoCuotas          decimal.Decimal // abonoSemana ÷ parcialidad (0 if parcialidad ≤ 0)
	AporteCuotas        decimal.Decimal // capped contribution (= aporte argument)
	AtrasoDespuesCuotas decimal.Decimal // max(0, vencidas − pagoCuotas)
	AtrasoDespuesPesos  decimal.Decimal // atrasoDespuesCuotas × parcialidad
}

// DesglosarAporte projects the aporte calculation into an auditable breakdown.
func DesglosarAporte(parcialidad, vencidas, abonoSemana, aporte decimal.Decimal) AporteDesglose {
	pagoCuotas := decimal.Zero
	if parcialidad.IsPositive() {
		pagoCuotas = abonoSemana.Div(parcialidad)
	}
	atrasoDespues := decimal.Max(decimal.Zero, vencidas.Sub(pagoCuotas))
	return AporteDesglose{
		AtrasoAntesCuotas:   vencidas,
		AtrasoAntesPesos:    vencidas.Mul(parcialidad),
		PagoCuotas:          pagoCuotas,
		AporteCuotas:        aporte,
		AtrasoDespuesCuotas: atrasoDespues,
		AtrasoDespuesPesos:  atrasoDespues.Mul(parcialidad),
	}
}

// ResumenPonderado is the aggregate behind the weighted percentage for a set of
// ventas: numerador = Σ aporte of applicable sales, denominador = count of
// applicable sales, Pct = numerador/denominador×100 (nil when none apply).
type ResumenPonderado struct {
	Numerador   decimal.Decimal
	Denominador int
	Pct         *decimal.Decimal
}

// CalcularResumenPonderado computes the weighted-percentage aggregate. It is the
// single source of truth shared by the zona listing and the breakdown modal so
// both always agree.
func CalcularResumenPonderado(ventas []VentaCobranza) ResumenPonderado {
	var (
		num decimal.Decimal
		den int
	)
	for _, v := range ventas {
		if v.AplicaPonderado {
			den++
			num = num.Add(v.Aporte)
		}
	}
	r := ResumenPonderado{Numerador: num, Denominador: den}
	if den > 0 {
		pct := num.Div(decimal.NewFromInt(int64(den))).Mul(decimal.NewFromInt(100))
		r.Pct = &pct
	}
	return r
}
