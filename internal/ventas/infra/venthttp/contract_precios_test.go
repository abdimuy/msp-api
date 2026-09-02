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
			desc := schema.Properties[campo].Value.Description
			assert.NotEmpty(t, desc, "%s.%s must be documented", schemaName, campo)
			assert.Contains(t, strings.ToLower(desc), "unitario",
				"%s.%s must say it is a unit price, not a line total", schemaName, campo)
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
			desc := schema.Properties[campo].Value.Description
			assert.NotEmpty(t, desc, "%s.%s must be documented", schemaName, campo)
			assert.Contains(t, strings.ToLower(desc), "total",
				"%s.%s must say it is a venta total, not a price", schemaName, campo)
		}
	}
}
