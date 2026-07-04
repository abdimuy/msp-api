//nolint:misspell // domain vocabulary is Spanish (ventas) per project convention.
package app

import (
	"strings"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// VentaToSearchDoc projects a domain.Venta into the outbound.VentaSearchDoc
// shape consumed by the Meilisearch ventas index. Used by both the
// incremental outbox reindex handler (one venta at a time, see
// infra/ventoutbox) and ReconciliarVentas (bulk).
//
// Two field mappings have no single obvious domain accessor and were
// resolved as explicit decisions (documented in full in the task report):
//
//   - PrecioTotal: MontoSnapshot carries three price tiers (anual, corto
//     plazo, contado) with no "total" accessor. We use precioTotalDe, which
//     picks Contado for TipoVentaContado sales (there is no credit plan, so
//     "contado" IS the sale price) and Anual for TipoVentaCredito sales
//     (DefaultPlazoMeses is 12 — "anual" is the standard 12-month credit
//     total the office quotes unless a shorter term is negotiated; the DTO
//     surfaces all three tiers with no single headline field, so this
//     mirrors the tier callers most commonly treat as canonical).
//   - VendedorEmail: a venta can carry multiple vendedores, but the port
//     models VendedorEmail as a single string (filterable exact-match
//     field, not a multi-value facet). We store the FIRST vendedor's email
//     in iteration order ("" when there are none).
func VentaToSearchDoc(v *domain.Venta) outbound.VentaSearchDoc {
	dir := v.Direccion()
	direccion := strings.Join(nonEmptyDireccionParts(dir.Calle(), dir.Colonia(), dir.Poblacion(), dir.Ciudad()), " ")

	var telefono string
	if t := v.Cliente().Telefono(); t != nil {
		telefono = t.String()
	}

	var folio string
	if f := v.MicrosipFolio(); f != nil {
		folio = *f
	}

	vendedorNombre, vendedorEmail := vendedorFields(v)

	var zonaClienteID int
	if z := dir.ZonaClienteID(); z != nil {
		zonaClienteID = *z
	}

	var clienteID int
	if c := v.ClienteID(); c != nil {
		clienteID = *c
	}

	aud := v.Audit()

	return outbound.VentaSearchDoc{
		ID:             v.ID(),
		NombreCliente:  v.Cliente().Nombre().String(),
		Telefono:       telefono,
		Direccion:      direccion,
		Folio:          folio,
		Vendedor:       vendedorNombre,
		TipoVenta:      v.TipoVenta().String(),
		Situacion:      v.Situacion().String(),
		Sincronizacion: v.Sincronizacion().String(),
		ZonaClienteID:  zonaClienteID,
		VendedorEmail:  vendedorEmail,
		ClienteID:      clienteID,
		Estado:         v.Estado().String(),
		FechaVenta:     v.FechaVenta(),
		PrecioTotal:    precioTotalDe(v),
		CreatedAt:      aud.CreatedAt(),
	}
}

// vendedorFields concatenates every vendedor's nombre (space-separated, for
// full-text search) and returns the FIRST vendedor's email in iteration
// order — see VentaToSearchDoc's doc comment for the VendedorEmail decision.
func vendedorFields(v *domain.Venta) (string, string) {
	var nombres []string
	var email string
	first := true
	for vd := range v.Vendedores() {
		snap := vd.Snapshot()
		nombres = append(nombres, snap.Nombre())
		if first {
			email = snap.Email()
			first = false
		}
	}
	return strings.Join(nombres, " "), email
}

// precioTotalDe picks the MontoSnapshot tier that represents the sale's
// headline price. See VentaToSearchDoc's doc comment for the rule.
func precioTotalDe(v *domain.Venta) decimal.Decimal {
	if v.TipoVenta() == domain.TipoVentaContado {
		return v.Montos().Contado()
	}
	return v.Montos().Anual()
}

// nonEmptyDireccionParts filters out blank address components, preserving
// order, so the joined Direccion string never carries stray double spaces.
func nonEmptyDireccionParts(parts ...string) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}
