//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import (
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

// EventoTimeline is one entry in a client's unified purchase/payment feed.
//
// Tipo is an extensible string: v1 emits compra_credito, compra_contado and pago.
// Future values (liquidacion, visita) may be appended without changing this type.
//
// v1 scope note: liquidacion events are intentionally excluded. Detecting a
// full pay-off from raw data is approximate — credit charges and interest are
// not reflected in VentaCruda.Total, and pagos without a resolvable DoctoPVID
// cannot be reliably attributed to a specific sale. Tipo is a plain string (not
// an iota/enum) precisely to allow extension in a future slice once per-sale
// balance reconstruction is reliable.
type EventoTimeline struct {
	Fecha    time.Time       // UTC timestamp of the event
	Tipo     string          // see TipoCompra* / TipoPago constants
	Monto    decimal.Decimal // positive amount in pesos
	Etiqueta string          // short human label: folio for compras, concepto/folio for pagos
	RefID    int             // DoctoPvID for compras; DoctoCCID for pagos
}

// Event-type constants for EventoTimeline.Tipo. These are the v1 values.
// The string representation is intentionally stable — frontends key on it.
//
//nolint:gosec // G101 false positive: these are event-type labels, not credentials.
const (
	TipoCompraCredito = "compra_credito"
	TipoCompraContado = "compra_contado"
	TipoPago          = "pago"
)

// BuildTimeline merges raw sale and payment events into a single chronological
// feed. The result is sorted by Fecha DESCENDING (most recent first); ties are
// broken by RefID DESCENDING, then by Tipo ASCENDING (lexicographic). The
// three-key ordering is a total order — every pair of distinct events has a
// deterministic relative position even when RefID spaces overlap (DoctoPvID for
// ventas vs DoctoCCID for pagos may collide by coincidence).
//
// Pure: no time.Now(), no I/O, no side effects. Calling BuildTimeline with nil
// slices returns an empty (non-nil) slice.
func BuildTimeline(pagos []PagoCrudo, ventas []VentaCruda) []EventoTimeline {
	eventos := make([]EventoTimeline, 0, len(pagos)+len(ventas))

	for _, v := range ventas {
		tipo := TipoCompraContado
		if v.EsCredito {
			tipo = TipoCompraCredito
		}
		eventos = append(eventos, EventoTimeline{
			Fecha:    v.Fecha.UTC(),
			Tipo:     tipo,
			Monto:    v.Total,
			Etiqueta: v.Folio,
			RefID:    v.DoctoPvID,
		})
	}

	for _, p := range pagos {
		etiqueta := p.Concepto
		if etiqueta == "" {
			etiqueta = p.Folio
		}
		eventos = append(eventos, EventoTimeline{
			Fecha:    p.Fecha.UTC(),
			Tipo:     TipoPago,
			Monto:    p.Importe,
			Etiqueta: etiqueta,
			RefID:    p.DoctoCCID,
		})
	}

	sort.Slice(eventos, func(i, j int) bool {
		if !eventos[i].Fecha.Equal(eventos[j].Fecha) {
			return eventos[i].Fecha.After(eventos[j].Fecha)
		}
		if eventos[i].RefID != eventos[j].RefID {
			return eventos[i].RefID > eventos[j].RefID
		}
		return eventos[i].Tipo < eventos[j].Tipo
	})

	return eventos
}
