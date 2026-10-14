//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import "github.com/shopspring/decimal"

// Cobro es el `cobro {cobrado, esperado, falta, pct}` del tablero. Decisión
// D-API-1, aceptada por el arquitecto el 2026-10-14: pct = % ponderado de rutas
// (CalcularResumenPonderado, la cifra que oficina ya conoce en Cobranza
// semanal); cobrado = Σ AbonoSemana de rutas (concepto 87327: solo pagos ya
// aplicados en Microsip, así que un pago pendiente o rechazado nunca suma aquí,
// D-API-8); esperado = Σ parcialidad de las ventas que aplican esta semana.
// pct NO es cobrado/esperado: el popover rotula cada cifra.
type Cobro struct {
	Cobrado  decimal.Decimal
	Esperado decimal.Decimal
	// Falta = max(0, esperado − cobrado) (contrato v2 `cobro.falta`).
	Falta decimal.Decimal
	Pct   *decimal.Decimal
}

// CalcularCobro proyecta CobroSemana a Cobro.
func CalcularCobro(cs CobroSemana) Cobro {
	c := Cobro{Cobrado: decimal.Zero, Esperado: decimal.Zero, Pct: cs.PctPonderado}
	for _, v := range cs.Ventas {
		c.Cobrado = c.Cobrado.Add(v.AbonoSemana)
		if v.AplicaPonderado {
			c.Esperado = c.Esperado.Add(v.Parcialidad)
		}
	}
	c.Falta = decimal.Max(decimal.Zero, c.Esperado.Sub(c.Cobrado))
	return c
}
