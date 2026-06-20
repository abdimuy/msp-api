//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

// Categoria classifies a payment movement by its economic role in Microsip.
// The mapping is deterministic: a single source of truth in Go keyed by
// CONCEPTO_CC_ID. Never parse the concepto name — IDs are stable, names drift.
type Categoria string

const (
	// CategoriaIngresoPago represents a standard cobranza payment (cobro en mostrador,
	// cobro en ruta). Corresponds to CONCEPTO_CC_ID ∈ {11, 155, 87327}.
	CategoriaIngresoPago Categoria = "pago"

	// CategoriaIngresoEnganche represents an enganche collected at the point of sale.
	// Corresponds to CONCEPTO_CC_ID = 24533.
	CategoriaIngresoEnganche Categoria = "enganche"

	// CategoriaCondonacion represents a debt forgiveness movement.
	// Corresponds to CONCEPTO_CC_ID ∈ {25116, 27969}.
	CategoriaCondonacion Categoria = "condonacion"

	// CategoriaPerdida represents a write-off movement (fuga, mal cliente, cancelación).
	// Corresponds to CONCEPTO_CC_ID ∈ {27966, 27967, 27968, 25117}.
	CategoriaPerdida Categoria = "perdida"

	// CategoriaOtro is the fallback for any CONCEPTO_CC_ID not in the known set.
	CategoriaOtro Categoria = "otro"
)

// pagoConceptoIDs contains the cobranza set — must stay identical to migration
// 000010's {11, 155, 87327}.
var pagoConceptoIDs = map[int]struct{}{
	11:    {},
	155:   {},
	87327: {},
}

// engancheConceptoIDs contains the enganche set.
var engancheConceptoIDs = map[int]struct{}{
	24533: {},
}

// condonacionConceptoIDs contains the condonacion set.
var condonacionConceptoIDs = map[int]struct{}{
	25116: {},
	27969: {},
}

// perdidaConceptoIDs contains the write-off set.
var perdidaConceptoIDs = map[int]struct{}{
	27966: {},
	27967: {},
	27968: {},
	25117: {},
}

// ClasificarConcepto returns the Categoria for a given CONCEPTO_CC_ID.
// Classification is deterministic and based solely on the ID — never on the name.
func ClasificarConcepto(conceptoCCID int) Categoria {
	if _, ok := pagoConceptoIDs[conceptoCCID]; ok {
		return CategoriaIngresoPago
	}
	if _, ok := engancheConceptoIDs[conceptoCCID]; ok {
		return CategoriaIngresoEnganche
	}
	if _, ok := condonacionConceptoIDs[conceptoCCID]; ok {
		return CategoriaCondonacion
	}
	if _, ok := perdidaConceptoIDs[conceptoCCID]; ok {
		return CategoriaPerdida
	}
	return CategoriaOtro
}

// EsIngreso reports whether this categoria represents actual cash inflow.
// Income is defined by exclusion: any movement that is NOT condonacion or perdida
// counts as income (pago, enganche, and otro all qualify).
func (c Categoria) EsIngreso() bool {
	return c != CategoriaCondonacion && c != CategoriaPerdida
}
