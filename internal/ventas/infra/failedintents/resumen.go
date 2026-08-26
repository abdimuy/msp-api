//nolint:misspell // Spanish vocabulary by project convention.

package failedintents

import (
	"encoding/json"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// ResumenExtractor traduce el cuerpo de POST /v2/ventas al resumen que la
// pantalla de intentos fallidos necesita para escribir un renglón legible.
//
// Va aquí y no en la plataforma por la misma razón que el conciliador: la
// captura es un mecanismo genérico —también sirve a pagos y a visitas— y el
// día que aprenda qué es una venta deja de servir para lo demás. Quien sabe
// leer una venta es ventas.
//
// El cero valor sirve.
type ResumenExtractor struct{}

// NewResumenExtractor construye el extractor. Sin dependencias a propósito: es
// una función pura del cuerpo, y esa pureza es lo que permite llamarla dentro
// de la petición del vendedor sin arriesgar nada.
func NewResumenExtractor() *ResumenExtractor { return &ResumenExtractor{} }

var _ failedintent.ResumenExtractor = (*ResumenExtractor)(nil)

// cuerpoVenta es la proyección MÍNIMA del cuerpo de una venta: sólo lo que el
// renglón muestra. Deliberadamente no es el DTO de venthttp — este extractor
// tiene que seguir funcionando sobre cuerpos capturados hace semanas, con la
// forma que el contrato tenía entonces, y atarlo al DTO vivo haría que un
// campo renombrado apagara en silencio el nombre de todas las filas viejas.
type cuerpoVenta struct {
	ID      string `json:"id"`
	Cliente struct {
		Nombre string `json:"nombre"`
	} `json:"cliente"`
	TipoVenta string `json:"tipo_venta"`
	Montos    struct {
		Anual      json.RawMessage `json:"anual"`
		CortoPlazo json.RawMessage `json:"corto_plazo"`
		Contado    json.RawMessage `json:"contado"`
	} `json:"montos"`
}

// Extraer implementa failedintent.ResumenExtractor.
//
// Devuelve nil cuando el cuerpo no tiene forma de venta. Nil es una respuesta,
// no un fallo: esta pantalla existe para decidir si una venta entró o no, y un
// nombre inventado ahí es peor que un hueco.
//
// El Modulo se deja vacío: lo estampa el registro a partir de la ruta, que es
// la única autoridad sobre eso.
func (e *ResumenExtractor) Extraer(_ string, body []byte, _ string) *failedintent.Resumen {
	var v cuerpoVenta
	if err := json.Unmarshal(body, &v); err != nil {
		return nil
	}
	nombre := strings.TrimSpace(v.Cliente.Nombre)
	monto := montoDeVenta(v)
	referencia := strings.TrimSpace(v.ID)

	if nombre == "" && monto == nil && referencia == "" {
		// Un JSON válido que no trae nada de una venta —`{}`, `123`, el
		// cuerpo de otro endpoint— no es una venta.
		return nil
	}
	return &failedintent.Resumen{
		Titulo:     nombre,
		Monto:      monto,
		Referencia: referencia,
	}
}

// montoDeVenta elige el importe que le importa a quien mira la pantalla.
//
// Los montos viajan como CADENAS decimales a propósito (el contrato evita el
// float binario). Para una venta a crédito el monto que importa es el de corto
// plazo o el anual, no el de contado; se toma el primero mayor que cero en ese
// orden, y el de contado sólo cuando la venta es de contado.
//
// Es la MISMA precedencia que usa el escritorio cuando no hay resumen del
// servidor (`cuantoDe` en IntentoAgrupado.ts). Que coincidan no es cosmético:
// si difirieran, una fila cambiaría de monto al desplegarse el binario y nadie
// sabría cuál de los dos números creer.
func montoDeVenta(v cuerpoVenta) *decimal.Decimal {
	orden := []json.RawMessage{v.Montos.CortoPlazo, v.Montos.Anual, v.Montos.Contado}
	if v.TipoVenta == "CONTADO" {
		orden = []json.RawMessage{v.Montos.Contado, v.Montos.CortoPlazo, v.Montos.Anual}
	}
	for _, crudo := range orden {
		d, ok := decimalDeJSON(crudo)
		if ok && d.IsPositive() {
			return &d
		}
	}
	return nil
}

// decimalDeJSON acepta tanto `"12500.50"` (el contrato) como `12500.50` (lo
// que manda un cliente descuidado). Los dos se han visto en capturas reales y
// rechazar el número obligaría a que un renglón perdiera el monto por una
// comilla.
func decimalDeJSON(raw json.RawMessage) (decimal.Decimal, bool) {
	if len(raw) == 0 {
		return decimal.Decimal{}, false
	}
	var comoCadena string
	if err := json.Unmarshal(raw, &comoCadena); err == nil {
		d, err := decimal.NewFromString(strings.TrimSpace(comoCadena))
		if err != nil {
			return decimal.Decimal{}, false
		}
		return d, true
	}
	var comoNumero json.Number
	if err := json.Unmarshal(raw, &comoNumero); err != nil {
		return decimal.Decimal{}, false
	}
	d, err := decimal.NewFromString(comoNumero.String())
	if err != nil {
		return decimal.Decimal{}, false
	}
	return d, true
}
