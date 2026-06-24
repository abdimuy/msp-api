//nolint:misspell // domain vocabulary is Spanish (receta, componente, etc.) per project convention.
package domain

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ─── Sentinel errors ──────────────────────────────────────────────────────────

var (
	// ErrRecetaArticuloIDInvalido is returned when articuloID is not > 0.
	ErrRecetaArticuloIDInvalido = apperror.NewValidation(
		"receta_articulo_id_invalido",
		"el artículo de la receta debe ser mayor a cero",
	)
	// ErrRecetaUnidadesNoPositivas is returned when unidades is not > 0.
	ErrRecetaUnidadesNoPositivas = apperror.NewValidation(
		"receta_unidades_no_positivas",
		"las unidades del componente deben ser mayores a cero",
	)
	// ErrComboSinComponentes is returned when a combo has no child productos.
	ErrComboSinComponentes = apperror.NewValidation(
		"combo_sin_componentes",
		"el combo no tiene componentes",
	)
	// ErrComboNoEncontrado is returned when RecetaDeCombo is called with a
	// comboID that does not exist in the venta.
	ErrComboNoEncontrado = apperror.NewNotFound(
		"combo_no_encontrado",
		"el combo no se encontró en la venta",
	)
)

// ─── RecetaComponente ─────────────────────────────────────────────────────────

// RecetaComponente is an immutable value object that pairs one Microsip
// articulo with the quantity used PER JUEGO (i.e., per single bundle).
// It maps directly to one row in JUEGOS_DET.
type RecetaComponente struct {
	articuloID int
	unidades   decimal.Decimal
}

// NewRecetaComponente constructs a validated RecetaComponente.
// articuloID must be > 0; unidades must be > 0 with at most cantidadScaleMax
// decimal places (4 dp, matching the NUMERIC(10,4) column).
func NewRecetaComponente(articuloID int, unidades decimal.Decimal) (RecetaComponente, error) {
	if articuloID <= 0 {
		return RecetaComponente{}, ErrRecetaArticuloIDInvalido
	}
	if unidades.Sign() <= 0 {
		return RecetaComponente{}, ErrRecetaUnidadesNoPositivas
	}
	if err := validateCantidadScale(unidades); err != nil {
		return RecetaComponente{}, err
	}
	return RecetaComponente{articuloID: articuloID, unidades: unidades}, nil
}

// ArticuloID returns the Microsip articulo identifier.
func (rc RecetaComponente) ArticuloID() int { return rc.articuloID }

// Unidades returns the per-juego quantity for this component.
func (rc RecetaComponente) Unidades() decimal.Decimal { return rc.unidades }

// Equal reports whether two RecetaComponente values are equal.
// Uses decimal.Equal to avoid pointer-identity bugs with decimal.Decimal.
func (rc RecetaComponente) Equal(other RecetaComponente) bool {
	return rc.articuloID == other.articuloID && rc.unidades.Equal(other.unidades)
}

// ─── Receta ───────────────────────────────────────────────────────────────────

// Receta is an ordered, canonical recipe for one juego (kit). Its components
// are sorted ascending by articuloID and deduplicated — duplicate articuloIDs
// within the same combo are merged by SUMMING their unidades, because
// JUEGOS_DET has exactly one row per (JUEGO_ID, ARTICULO_ID) pair.
type Receta struct {
	componentes []RecetaComponente // sorted ascending by articuloID
}

// Componentes returns a read-only copy of the sorted component slice.
func (r Receta) Componentes() []RecetaComponente {
	out := make([]RecetaComponente, len(r.componentes))
	copy(out, r.componentes)
	return out
}

// firmaScaleDP is the fixed decimal places used when serialising unidades
// into the canonical signature. 6 dp gives enough precision to distinguish
// 1 from 1.000001, while aligning with the writer's UNIDADES scale.
const firmaScaleDP = 6

// Firma returns a canonical, deterministic string representation of the
// recipe. Format: "artID:unidades.StringFixed(6)|artID:unidades.StringFixed(6)|..."
// Components are already sorted by articuloID, so Firma is stable under any
// permutation of the source productos.
func (r Receta) Firma() string {
	parts := make([]string, len(r.componentes))
	for i, c := range r.componentes {
		parts[i] = fmt.Sprintf("%d:%s", c.articuloID, c.unidades.StringFixed(firmaScaleDP))
	}
	return strings.Join(parts, "|")
}

// Equal reports structural equality between two Receta values by comparing
// their canonical signatures.
func (r Receta) Equal(other Receta) bool {
	return r.Firma() == other.Firma()
}

// buildReceta aggregates raw (articuloID, unidades) pairs into a canonical
// Receta: duplicates are merged by summing unidades, then sorted ascending
// by articuloID. Returns ErrComboSinComponentes when pairs is empty.
func buildReceta(pairs []RecetaComponente) (Receta, error) {
	if len(pairs) == 0 {
		return Receta{}, ErrComboSinComponentes
	}

	// Aggregate duplicate articuloIDs by summing their unidades. A combo's
	// JUEGOS_DET has one row per component, so a recipe is a multiset keyed
	// by articuloID.
	merged := make(map[int]decimal.Decimal, len(pairs))
	order := make([]int, 0, len(pairs))
	for _, c := range pairs {
		if _, seen := merged[c.articuloID]; !seen {
			order = append(order, c.articuloID)
		}
		merged[c.articuloID] = merged[c.articuloID].Add(c.unidades)
	}

	// Sort ascending by articuloID for determinism.
	sort.Ints(order)

	componentes := make([]RecetaComponente, 0, len(order))
	for _, id := range order {
		componentes = append(componentes, RecetaComponente{
			articuloID: id,
			unidades:   merged[id],
		})
	}
	return Receta{componentes: componentes}, nil
}

// ─── Venta derivation methods ─────────────────────────────────────────────────

// RecetaDeCombo derives the canonical Receta for the given comboID by
// gathering all child productos (those whose ComboID() == comboID) and
// building the sorted, deduplicated component list from their Cantidad()
// values. A combo with no child productos returns ErrComboSinComponentes.
// An unknown comboID returns ErrComboNoEncontrado.
func (v *Venta) RecetaDeCombo(comboID uuid.UUID) (Receta, error) {
	// Verify the combo exists in this venta.
	found := false
	for _, c := range v.combos {
		if c.id == comboID {
			found = true
			break
		}
	}
	if !found {
		return Receta{}, ErrComboNoEncontrado
	}

	var pairs []RecetaComponente
	for _, p := range v.productos {
		if p.comboID == nil || *p.comboID != comboID {
			continue
		}
		pairs = append(pairs, RecetaComponente{
			articuloID: p.articuloID,
			unidades:   p.cantidad,
		})
	}
	return buildReceta(pairs)
}

// RecetasDeCombos derives a canonical Receta for each combo in the venta,
// keyed by combo ID. If any combo has no child productos, it returns
// ErrComboSinComponentes (propagated from RecetaDeCombo). A venta with no
// combos returns an empty map without error.
func (v *Venta) RecetasDeCombos() (map[uuid.UUID]Receta, error) {
	result := make(map[uuid.UUID]Receta, len(v.combos))
	for _, c := range v.combos {
		r, err := v.RecetaDeCombo(c.id)
		if err != nil {
			return nil, err
		}
		result[c.id] = r
	}
	return result, nil
}
