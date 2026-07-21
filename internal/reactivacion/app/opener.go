//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"fmt"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

// plantillaRecienLiquidado is the opener template for SegmentoRecienLiquidado:
// a congratulation for finishing the payment plus an invitation to the next
// purchase. No real amounts — that is the copiloto de IA's job in Fase 3.
const plantillaRecienLiquidado = "%s le saluda Mueblería MSP. ¡Felicidades por completar su pago! " +
	"Tenemos algo especial para usted en su próxima compra. ¿Le comparto opciones?"

// plantillaPorLiquidarHueco is the opener template for SegmentoPorLiquidarHueco:
// a tactful nudge to close out the remaining balance with a payment that fits.
const plantillaPorLiquidarHueco = "%s le saluda Mueblería MSP. Ya casi termina de pagar su compra. " +
	"¿Le gustaría completar su juego con un pago que se acomode a usted?"

// Opener generates the initial (opener) message body for a cohorte cliente,
// one static template per Segmento with the cliente's name interpolated.
// Fase 3 replaces this with a copiloto-generated opener behind the same
// Generar signature — callers never need to change.
type Opener struct{}

// NewOpener builds an Opener. Stateless — safe to share across goroutines.
func NewOpener() Opener { return Opener{} }

// Generar returns the opener body for seg, addressing nombre. nombre may be
// empty — in that case a generic greeting ("¿Cómo está?") replaces "Hola
// {nombre},". Never returns an empty string on success.
func (Opener) Generar(seg domain.Segmento, nombre string) (string, error) {
	if !seg.Valido() {
		return "", domain.ErrSegmentoInvalido
	}

	var plantilla string
	switch seg {
	case domain.SegmentoRecienLiquidado:
		plantilla = plantillaRecienLiquidado
	case domain.SegmentoPorLiquidarHueco:
		plantilla = plantillaPorLiquidarHueco
	}

	return fmt.Sprintf(plantilla, saludo(nombre)), nil
}

// saludo returns "Hola {nombre}," when nombre is set, or a generic greeting
// otherwise.
func saludo(nombre string) string {
	if nombre == "" {
		return "¿Cómo está?"
	}
	return fmt.Sprintf("Hola %s,", nombre)
}
