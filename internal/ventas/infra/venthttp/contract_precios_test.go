//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The venta Z00002678 was captured with the LINE TOTAL in two of the three
// price fields and the UNIT price in the third, because nothing in the
// published contract said which one the server expects. It multiplies all
// three by cantidad, so the header ended up claiming five times the cash
// price it should have.
//
// These tests hold the published OpenAPI document to saying it: every
// precio_* field is a unit price, and the montos block is a total.

// schemasConPropiedades returns every component schema of the generated spec
// that declares all the given property names, so the assertions below do not
// depend on the names Huma derives for the Go structs.
func schemasConPropiedades(spec *openapi3.T, names ...string) map[string]*openapi3.Schema {
	out := map[string]*openapi3.Schema{}
	for schemaName, ref := range spec.Components.Schemas {
		if ref == nil || ref.Value == nil {
			continue
		}
		complete := true
		for _, n := range names {
			if _, ok := ref.Value.Properties[n]; !ok {
				complete = false
				break
			}
		}
		if complete {
			out[schemaName] = ref.Value
		}
	}
	return out
}

// TestContrato_PreciosDeLineaSeDocumentanComoUnitarios covers the line-level
// half: the three precio_* fields of a combo and of a producto.
func TestContrato_PreciosDeLineaSeDocumentanComoUnitarios(t *testing.T) {
	t.Parallel()
	rig := newContractLineasRig(t, fullPerms(uuid.New()))

	campos := []string{"precio_anual", "precio_corto", "precio_contado"}
	schemas := schemasConPropiedades(rig.spec, campos...)
	require.GreaterOrEqual(t, len(schemas), 2,
		"the probe must find at least the combo and the producto schemas; got %d", len(schemas))

	for schemaName, schema := range schemas {
		for _, campo := range campos {
			desc := strings.ToLower(schema.Properties[campo].Value.Description)
			assert.NotEmpty(t, desc, "%s.%s must be documented", schemaName, campo)
			// Two phrases, both discriminating. "unitario" alone is not: the
			// montos block also contains the word, negated ("No es un precio
			// unitario"), so asserting on it passes with the two texts SWAPPED
			// — that is, with the exact confusion that caused Z00002678
			// published as the contract.
			assert.Containsf(t, desc, "precio unitario",
				"%s.%s must state it is a unit price; got %q", schemaName, campo, desc)
			assert.Containsf(t, desc, "no el total de la línea",
				"%s.%s must rule out the line total explicitly; got %q", schemaName, campo, desc)
		}
	}
}

// TestContrato_MontosDeEncabezadoSeDocumentanComoTotales covers the header
// half: the montos block is Σ(precio × cantidad), never a price.
func TestContrato_MontosDeEncabezadoSeDocumentanComoTotales(t *testing.T) {
	t.Parallel()
	rig := newContractLineasRig(t, fullPerms(uuid.New()))

	campos := []string{"anual", "corto_plazo", "contado"}
	schemas := schemasConPropiedades(rig.spec, campos...)
	require.NotEmpty(t, schemas, "the probe must find the montos schema")

	for schemaName, schema := range schemas {
		for _, campo := range campos {
			desc := strings.ToLower(schema.Properties[campo].Value.Description)
			assert.NotEmpty(t, desc, "%s.%s must be documented", schemaName, campo)
			// "total" alone is not discriminating either: the line texts carry
			// it too, negated ("no el total de la línea"). What only a header
			// total can say is how it is derived.
			assert.Containsf(t, desc, "suma de",
				"%s.%s must state it is a sum over the lines; got %q", schemaName, campo, desc)
			assert.NotContainsf(t, desc, "lo que cuesta",
				"%s.%s reads like a unit price; that phrase belongs to the line fields, got %q",
				schemaName, campo, desc)
		}
	}
}
