package firebird

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// resumenFila es la forma en que el Resumen viaja dentro de la columna
// RESUMEN (BLOB SUB_TYPE TEXT UTF8).
//
// Es un JSON y no tres columnas porque un módulo nuevo no debe pedir
// migración: hoy son ventas y pagos, mañana otros, y cada uno llena los mismos
// campos. Deja sitio además para lo que quedó fuera de alcance a propósito —el
// detalle del 422 de existencias— sin rehacer nada.
//
// MODULO NO va aquí: es columna real e indexada porque los chips del listado
// filtran por él, y eso tiene que ser SQL.
//
// El monto viaja como CADENA, igual que en el contrato HTTP. Meterlo como
// número JSON lo pasaría por un float64 de ida y de vuelta, y un importe es
// justo el dato que no se redondea.
type resumenFila struct {
	Titulo     string `json:"titulo,omitempty"`
	Monto      string `json:"monto,omitempty"`
	Referencia string `json:"referencia,omitempty"`
	// ClienteID va aparte de Referencia a propósito: ver Resumen.ClienteID.
	// `omitempty` sobre un puntero omite el nil, no el cero — que aquí no
	// puede llegar porque el extractor descarta el cero como ausente.
	ClienteID *int `json:"cliente_id,omitempty"`
}

// codificarResumen serializa el resumen para la columna. Devuelve nil cuando
// no hay nada que guardar, y ese nil se escribe como NULL: "no se extrajo" es
// un estado distinto de "se extrajo y salió vacío".
func codificarResumen(r *failedintent.Resumen) (any, error) {
	if r.Vacio() {
		return nil, nil //nolint:nilnil // contrato: (nil, nil) significa NULL
	}
	fila := resumenFila{
		Titulo:     strings.TrimSpace(r.Titulo),
		Referencia: strings.TrimSpace(r.Referencia),
		ClienteID:  r.ClienteID,
	}
	if r.Monto != nil {
		fila.Monto = r.Monto.String()
	}
	raw, err := json.Marshal(fila)
	if err != nil {
		return nil, fmt.Errorf("failedintent.firebird: codificar resumen: %w", err)
	}
	return raw, nil
}

// decodificarResumen reconstruye el resumen leído de la columna.
//
// Un JSON ilegible NO es un error de lectura de la fila: la fila es evidencia
// y el resumen es un adorno encima. Se devuelve (nil, false) y quien llame
// sigue como si la columna estuviera vacía — perder el listado entero por un
// adorno corrupto sería el peor intercambio posible.
func decodificarResumen(raw []byte, modulo string) *failedintent.Resumen {
	modulo = strings.TrimSpace(modulo)
	if len(raw) == 0 {
		if modulo == "" {
			return nil
		}
		// Módulo sin resumen: la ruta se reconoció, el cuerpo no. Se devuelve
		// nil como resumen — el módulo viaja en su propio campo del Intent.
		return nil
	}
	var fila resumenFila
	if err := json.Unmarshal(raw, &fila); err != nil {
		return nil
	}
	res := &failedintent.Resumen{
		Modulo:     modulo,
		Titulo:     fila.Titulo,
		Referencia: fila.Referencia,
		ClienteID:  fila.ClienteID,
	}
	if fila.Monto != "" {
		if d, err := decimal.NewFromString(fila.Monto); err == nil {
			res.Monto = &d
		}
	}
	if res.Vacio() {
		return nil
	}
	return res
}
