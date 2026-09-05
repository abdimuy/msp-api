//nolint:misspell // Spanish vocabulary by project convention.

// Package failedintents implementa, del lado de cobranza, el puerto que la
// plataforma declara para resumir un intento fallido.
//
// El puerto va invertido a propósito: `internal/platform/failedintent` no
// puede —ni debe— saber qué es un pago. La captura es un mecanismo genérico
// que sirve igual a ventas, a pagos y a visitas; el día que aprenda de
// cobranza deja de servir para lo demás. Quien sabe leer un pago es cobranza.
package failedintents

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// ResumenExtractor traduce el cuerpo de POST /v2/cobranza/pagos al resumen que
// la pantalla de intentos fallidos necesita.
//
// El cero valor sirve.
type ResumenExtractor struct{}

// NewResumenExtractor construye el extractor. Sin dependencias: es una función
// pura del cuerpo, y esa pureza es lo que permite llamarla dentro de la
// petición del cobrador sin arriesgar nada.
func NewResumenExtractor() *ResumenExtractor { return &ResumenExtractor{} }

var _ failedintent.ResumenExtractor = (*ResumenExtractor)(nil)

// cuerpoPago es la proyección MÍNIMA del cuerpo de un pago: sólo lo que el
// renglón muestra. No es el DTO de cobranzahttp a propósito — este extractor
// tiene que seguir leyendo cuerpos capturados hace semanas, con la forma que
// el contrato tenía entonces.
type cuerpoPago struct {
	ClienteID  int             `json:"cliente_id"`
	Cobrador   string          `json:"cobrador"`
	Importe    json.RawMessage `json:"importe"`
	CargoDocto int             `json:"cargo_docto_cc_id"`
}

// Extraer implementa failedintent.ResumenExtractor.
//
// Sobre el Titulo: el cuerpo de un pago **no trae el nombre del cliente**.
// Trae su id numérico, el del cargo y el nombre del COBRADOR. De esos tres, el
// único que una persona de oficina reconoce de un vistazo es el cobrador, y es
// el que va en el título — no como "de quién es el pago" sino como "quién lo
// capturó", que es la pregunta que se hace cuando un pago no entró.
//
// El id del cliente va en Referencia, que es el ancla para buscarlo. Si algún
// día el contrato del pago carga el nombre, este extractor lo prefiere y la
// pantalla no se entera.
//
// Devuelve nil cuando el cuerpo no tiene forma de pago.
func (e *ResumenExtractor) Extraer(_ string, body []byte, _ string) *failedintent.Resumen {
	var p cuerpoPago
	if err := json.Unmarshal(body, &p); err != nil {
		return nil
	}
	cobrador := strings.TrimSpace(p.Cobrador)
	importe := importeDePago(p.Importe)
	referencia := referenciaDePago(p)

	if cobrador == "" && importe == nil && referencia == "" {
		return nil
	}
	return &failedintent.Resumen{
		Titulo:     cobrador,
		Monto:      importe,
		Referencia: referencia,
		ClienteID:  clienteIDDePago(p),
	}
}

// clienteIDDePago devuelve el id del cliente SÓLO cuando el cuerpo lo trae.
//
// Existe aparte de referenciaDePago porque ésa cae al id del cargo cuando no
// hay cliente, y las dos cosas son indistinguibles una vez guardadas. El
// listado usa este campo para buscar el nombre en CLIENTES: con la referencia
// acabaría mostrando, de vez en cuando, el nombre de otro cliente.
func clienteIDDePago(p cuerpoPago) *int {
	if p.ClienteID <= 0 {
		return nil
	}
	id := p.ClienteID
	return &id
}

// referenciaDePago ancla el renglón al cliente cuando se puede, y al cargo
// cuando no. Un cero no es un id: es el campo ausente.
func referenciaDePago(p cuerpoPago) string {
	if p.ClienteID > 0 {
		return strconv.Itoa(p.ClienteID)
	}
	if p.CargoDocto > 0 {
		return strconv.Itoa(p.CargoDocto)
	}
	return ""
}

// importeDePago acepta tanto `"350.00"` (el contrato) como `350` (lo que manda
// un cliente descuidado). Los dos se han visto en capturas reales.
//
// Un importe de cero o negativo se trata como ausente: un pago de $0 no existe,
// y mostrar "$0.00" en el renglón se leería como un dato cuando es un hueco.
func importeDePago(raw json.RawMessage) *decimal.Decimal {
	if len(raw) == 0 {
		return nil
	}
	var comoCadena string
	if err := json.Unmarshal(raw, &comoCadena); err == nil {
		return positivoODescartar(strings.TrimSpace(comoCadena))
	}
	var comoNumero json.Number
	if err := json.Unmarshal(raw, &comoNumero); err != nil {
		return nil
	}
	return positivoODescartar(comoNumero.String())
}

// positivoODescartar parsea s y lo devuelve sólo si es un decimal mayor que
// cero.
func positivoODescartar(s string) *decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil || !d.IsPositive() {
		return nil
	}
	return &d
}
