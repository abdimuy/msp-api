//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import "strings"

// Direccion is a read-only composite value object holding the native address
// components sourced from the Microsip table DIRS_CLIENTES.
//
// Hydrate-only deviation: this module is a read-only projection of Microsip.
// There is no validating New constructor — the canonical NewXxx pattern (from
// the module standards) applies to owned/write entities. For Microsip-sourced
// read-models, HydrateDireccion is the only constructor and performs zero
// validation, trusting the repository to supply valid UTF-8.
//
// Passed and stored by value (immutable, no setters).
type Direccion struct {
	calle     string
	colonia   string
	poblacion string
	estado    string
}

// HydrateDireccionParams holds the raw address components returned by the
// repository from DIRS_CLIENTES. Used exclusively by the repository layer.
type HydrateDireccionParams struct {
	Calle     string
	Colonia   string
	Poblacion string
	Estado    string
}

// HydrateDireccion reconstructs a Direccion from Microsip persistence with
// zero validation. Called only from the repository layer.
func HydrateDireccion(p HydrateDireccionParams) Direccion {
	return Direccion{
		calle:     p.Calle,
		colonia:   p.Colonia,
		poblacion: p.Poblacion,
		estado:    p.Estado,
	}
}

// ─── Getters ──────────────────────────────────────────────────────────────────

// Calle returns the street address component.
func (d Direccion) Calle() string { return d.calle }

// Colonia returns the neighborhood (colonia) component.
func (d Direccion) Colonia() string { return d.colonia }

// Poblacion returns the city/town component.
func (d Direccion) Poblacion() string { return d.poblacion }

// Estado returns the state (e.g. "Jalisco") component.
func (d Direccion) Estado() string { return d.estado }

// ─── Domain methods ───────────────────────────────────────────────────────────

// Corta returns a concise one-line address joining the non-empty components
// calle, colonia, and poblacion (in that order) separated by ", ".
// Each component is trimmed; those empty or whitespace-only after trimming are
// skipped (Firebird CHAR columns may pad values with trailing spaces).
// Estado is intentionally excluded — it is available via Estado() for detail
// views but is omitted here to keep the short label compact.
func (d Direccion) Corta() string {
	parts := make([]string, 0, 3)
	for _, p := range []string{d.calle, d.colonia, d.poblacion} {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, ", ")
}
