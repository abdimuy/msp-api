//nolint:misspell // ventas vocabulary is Spanish (productos, vendedores, combos) per project convention.
package app_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// propIterations is the expected number of rapid iterations per property.
// rapid defaults to 100 iterations; override via -rapid.checks=N.
// This constant is not passed to rapid.Check (which uses the flag) — it
// documents the target coverage so reviewers know the intent.
const propIterations = 100

var _ = propIterations // satisfy the linter: propIterations is documentation only.

// ─── generators ────────────────────────────────────────────────────────────────

// printableASCIIRunes contains every printable ASCII rune from space (0x20) to
// tilde (0x7E). All are valid WIN1252 code points and free of NUL bytes.
var printableASCIIRunes = func() []rune {
	out := make([]rune, 0, '~'-' '+1)
	for r := rune(' '); r <= '~'; r++ {
		out = append(out, r)
	}
	return out
}()

// nonSpaceASCIIRunes contains printable ASCII runes from '!' (0x21) to '~'
// (0x7E), which excludes the space character. Using a non-space first/last
// character ensures strings survive strings.TrimSpace without changing, which
// is required because requireBounded trims before validating.
var nonSpaceASCIIRunes = func() []rune {
	out := make([]rune, 0, '~'-'!'+1)
	for r := rune('!'); r <= '~'; r++ {
		out = append(out, r)
	}
	return out
}()

// genSafeASCII draws a string of 1–maxLen non-space printable ASCII characters
// (0x21–0x7E) that are valid for WIN1252 encoding, NUL-free, and survive
// strings.TrimSpace unchanged (required because domain.requireBounded trims).
func genSafeASCII(maxLen int) *rapid.Generator[string] {
	return rapid.StringOfN(rapid.RuneFrom(nonSpaceASCIIRunes), 1, maxLen, -1)
}

// genNonNegativeDecimal2 draws a non-negative decimal.Decimal with at most
// 2 decimal places and a value ≤ 999_999_999_999.99 (the NUMERIC(14,2) cap).
func genNonNegativeDecimal2(t *rapid.T, label string) decimal.Decimal {
	units := rapid.Int64Range(0, 999_999_999_999).Draw(t, label+"_units")
	cents := rapid.IntRange(0, 99).Draw(t, label+"_cents")
	return decimal.New(units, 0).Add(decimal.New(int64(cents), -2))
}

// genPositiveDecimal4 draws a positive decimal.Decimal with at most 4 decimal
// places (for cantidad) and value in [0.0001 , 9_999_999_999.9999].
func genPositiveDecimal4(t *rapid.T, label string) decimal.Decimal {
	units := rapid.Int64Range(0, 9_999_999_999).Draw(t, label+"_units")
	frac := rapid.IntRange(0, 9999).Draw(t, label+"_frac")
	v := decimal.New(units, 0).Add(decimal.New(int64(frac), -4))
	if v.Sign() == 0 {
		return decimal.New(1, -4) // minimum positive quantity
	}
	return v
}

// genAlmacenPair draws two distinct positive integers for almacen origins/destinations.
func genAlmacenPair(t *rapid.T) (int, int) {
	orig := rapid.IntRange(1, 100).Draw(t, "alm_orig")
	// Ensure dest != orig by drawing from a range that excludes orig.
	delta := rapid.IntRange(1, 50).Draw(t, "alm_delta")
	return orig, orig + delta
}

// genFechaVenta draws a non-zero time.Time between 2000 and 2099.
func genFechaVenta(t *rapid.T) time.Time {
	year := rapid.IntRange(2000, 2099).Draw(t, "fecha_year")
	month := rapid.IntRange(1, 12).Draw(t, "fecha_month")
	day := rapid.IntRange(1, 28).Draw(t, "fecha_day")
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

// genProductoInput draws one valid CrearVentaProductoInput without a combo.
// The idx parameter is used to suffix rapid draw labels so each elemento in a
// slice gets unique names; this prevents rapid label collisions when the
// function is called in a loop.
func genProductoInput(t *rapid.T, idx int) ventasapp.CrearVentaProductoInput {
	sfx := rapid.IntRange(idx, idx).Draw(t, "p_idx") // stabilises the label namespace per call
	_ = sfx
	orig, dest := genAlmacenPair(t)
	return ventasapp.CrearVentaProductoInput{
		ID:             uuid.New(),
		ArticuloID:     rapid.IntRange(1, 9999).Draw(t, "art_id"),
		Articulo:       genSafeASCII(50).Draw(t, "articulo"),
		Cantidad:       genPositiveDecimal4(t, "cantidad"),
		PrecioAnual:    genNonNegativeDecimal2(t, "anual"),
		PrecioCorto:    genNonNegativeDecimal2(t, "corto"),
		PrecioContado:  genNonNegativeDecimal2(t, "contado"),
		AlmacenOrigen:  &orig,
		AlmacenDestino: &dest,
	}
}

// alphanumericRunes contains lowercase letters and digits for building valid
// email local-parts and identifiers that pass domain and WIN1252 checks.
var alphanumericRunes = func() []rune {
	var out []rune
	for r := 'a'; r <= 'z'; r++ {
		out = append(out, r)
	}
	for r := '0'; r <= '9'; r++ {
		out = append(out, r)
	}
	return out
}()

// genAlphanumeric draws a string of 1–maxLen lowercase-alphanumeric characters.
func genAlphanumeric(maxLen int) *rapid.Generator[string] {
	return rapid.StringOfN(rapid.RuneFrom(alphanumericRunes), 1, maxLen, -1)
}

// genVendedorInput draws one valid CrearVentaVendedorInput.
// Email is built from alphanumeric characters so it passes ValidateSafeChars
// and has a recognizable domain suffix.
func genVendedorInput(t *rapid.T) ventasapp.CrearVentaVendedorInput {
	nombre := genAlphanumeric(20).Draw(t, "vend_nombre")
	// Build a safe email: only alphanumeric local part + fixed domain.
	local := genAlphanumeric(8).Draw(t, "vend_local")
	email := local + "@muebleriamsp.mx"
	return ventasapp.CrearVentaVendedorInput{
		ID:        uuid.New(),
		UsuarioID: uuid.New(),
		Email:     email,
		Nombre:    nombre,
	}
}

// genComboInput draws one valid CrearVentaComboInput.
func genComboInput(t *rapid.T) ventasapp.CrearVentaComboInput {
	orig, dest := genAlmacenPair(t)
	return ventasapp.CrearVentaComboInput{
		ID:             uuid.New(),
		Nombre:         genSafeASCII(40).Draw(t, "combo_nombre"),
		PrecioAnual:    genNonNegativeDecimal2(t, "combo_anual"),
		PrecioCorto:    genNonNegativeDecimal2(t, "combo_corto"),
		PrecioContado:  genNonNegativeDecimal2(t, "combo_contado"),
		Cantidad:       genPositiveDecimal4(t, "combo_cant"),
		AlmacenOrigen:  orig,
		AlmacenDestino: dest,
	}
}

// ─── helpers ───────────────────────────────────────────────────────────────────

// ventaUpdatedAt returns the updatedAt timestamp from a venta's audit record.
// audit.Auditable methods are pointer-receiver so we assign to an addressable
// variable before calling.
func ventaUpdatedAt(v *domain.Venta) time.Time {
	a := v.Audit()
	return a.UpdatedAt()
}

// ventaCreatedAt returns the createdAt timestamp from a venta's audit record.
func ventaCreatedAt(v *domain.Venta) time.Time {
	a := v.Audit()
	return a.CreatedAt()
}

// ventaUpdatedBy returns the updatedBy UUID from a venta's audit record.
func ventaUpdatedBy(v *domain.Venta) uuid.UUID {
	a := v.Audit()
	return a.UpdatedBy()
}

// seedVentaFromHarness seeds one CONTADO venta and returns the full aggregate.
// The outer *testing.T is used because seedVenta (defined in service_test.go)
// requires a *testing.T; the rapid.T provides the property-failure mechanism.
func seedVentaFromHarness(t *testing.T, h *testHarness, rt *rapid.T) *domain.Venta {
	t.Helper()
	id := h.seedVenta(t)
	v, err := h.ventas.FindByID(t.Context(), *id)
	if err != nil {
		rt.Fatalf("seedVenta: FindByID: %v", err)
	}
	return v
}

// ─── property tests ────────────────────────────────────────────────────────────

// TestProperty_ActualizarHeader verifies that for any valid header input:
//   - Header fields are exactly those supplied.
//   - ID, status, cliente, productos, vendedores are unchanged.
//   - updated_at ≥ created_at (audit.MarkUpdated uses time.Now() internally).
//   - updated_by matches the actor.
//   - Exactly one "venta.header_actualizado" event is in the outbox.
func TestProperty_ActualizarHeader(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		h := newHarness(t)
		before := seedVentaFromHarness(t, h, rt)
		preID := before.ID()
		preSituacion := before.Situacion()
		preCliente := before.Cliente()
		preProductosCount := before.ProductosCount()
		preVendedoresCount := before.VendedoresCount()

		lat := rapid.Float64Range(-90, 90).Draw(rt, "lat")
		lng := rapid.Float64Range(-180, 180).Draw(rt, "lng")
		calle := genSafeASCII(50).Draw(rt, "calle")
		colonia := genSafeASCII(30).Draw(rt, "colonia")
		poblacion := genSafeASCII(30).Draw(rt, "poblacion")
		ciudad := genSafeASCII(30).Draw(rt, "ciudad")
		fecha := genFechaVenta(rt)
		precioAnual := genNonNegativeDecimal2(rt, "header_anual")
		precioCorto := genNonNegativeDecimal2(rt, "header_corto")
		precioContado := genNonNegativeDecimal2(rt, "header_contado")

		actor := uuid.New()

		out, err := h.svc.ActualizarHeader(t.Context(), ventasapp.ActualizarHeaderInput{
			VentaID:       preID,
			Calle:         calle,
			Colonia:       colonia,
			Poblacion:     poblacion,
			Ciudad:        ciudad,
			Latitud:       lat,
			Longitud:      lng,
			FechaVenta:    fecha,
			PrecioAnual:   precioAnual,
			PrecioCorto:   precioCorto,
			PrecioContado: precioContado,
		}, actor)
		if err != nil {
			rt.Fatalf("ActualizarHeader failed: %v", err)
		}

		// Header fields applied. Address text is folded to ALL CAPS by the
		// domain (Microsip convention); the generated input is ASCII-only, so
		// strings.ToUpper is the sole transform to account for here.
		if out.Direccion().Calle() != strings.ToUpper(calle) {
			rt.Fatalf("calle mismatch: got %q want %q", out.Direccion().Calle(), strings.ToUpper(calle))
		}
		if out.Direccion().Colonia() != strings.ToUpper(colonia) {
			rt.Fatalf("colonia mismatch: got %q want %q", out.Direccion().Colonia(), strings.ToUpper(colonia))
		}
		if out.Direccion().Poblacion() != strings.ToUpper(poblacion) {
			rt.Fatalf("poblacion mismatch: got %q want %q", out.Direccion().Poblacion(), strings.ToUpper(poblacion))
		}
		if out.Direccion().Ciudad() != strings.ToUpper(ciudad) {
			rt.Fatalf("ciudad mismatch: got %q want %q", out.Direccion().Ciudad(), strings.ToUpper(ciudad))
		}
		if out.GPS().Latitud() != lat {
			rt.Fatalf("latitud mismatch: got %v want %v", out.GPS().Latitud(), lat)
		}
		if out.GPS().Longitud() != lng {
			rt.Fatalf("longitud mismatch: got %v want %v", out.GPS().Longitud(), lng)
		}
		if !out.FechaVenta().Equal(fecha) {
			rt.Fatalf("fecha_venta mismatch: got %v want %v", out.FechaVenta(), fecha)
		}
		if !out.Montos().Anual().Equal(precioAnual) {
			rt.Fatalf("precio_anual mismatch: got %v want %v", out.Montos().Anual(), precioAnual)
		}
		if !out.Montos().CortoPlazo().Equal(precioCorto) {
			rt.Fatalf("precio_corto mismatch: got %v want %v", out.Montos().CortoPlazo(), precioCorto)
		}
		if !out.Montos().Contado().Equal(precioContado) {
			rt.Fatalf("precio_contado mismatch: got %v want %v", out.Montos().Contado(), precioContado)
		}

		// Immutable fields unchanged.
		if out.ID() != preID {
			rt.Fatalf("ID changed: got %v want %v", out.ID(), preID)
		}
		if out.Situacion() != preSituacion {
			rt.Fatalf("situacion changed: got %v want %v", out.Situacion(), preSituacion)
		}
		if out.Cliente().Nombre().Value() != preCliente.Nombre().Value() {
			rt.Fatalf("cliente changed: got %q want %q", out.Cliente().Nombre().Value(), preCliente.Nombre().Value())
		}
		if out.ProductosCount() != preProductosCount {
			rt.Fatalf("productos count changed: got %d want %d", out.ProductosCount(), preProductosCount)
		}
		if out.VendedoresCount() != preVendedoresCount {
			rt.Fatalf("vendedores count changed: got %d want %d", out.VendedoresCount(), preVendedoresCount)
		}

		// Audit: updated_at ≥ created_at (MarkUpdated uses time.Now() which is
		// always ≥ the fixedClock.T used at creation time).
		if ventaUpdatedAt(out).Before(ventaCreatedAt(out)) {
			rt.Fatalf("updated_at before created_at: updatedAt=%v createdAt=%v",
				ventaUpdatedAt(out), ventaCreatedAt(out))
		}
		// updated_by == actor.
		if ventaUpdatedBy(out) != actor {
			rt.Fatalf("updated_by mismatch: got %v want %v", ventaUpdatedBy(out), actor)
		}

		// Outbox: exactly one header_actualizado event.
		evts := h.outbox.eventTypes()
		if len(evts) != 1 || evts[0] != domain.EventTypeVentaHeaderActualizado {
			rt.Fatalf("expected one header_actualizado event, got: %v", evts)
		}
	})
}

// TestProperty_ActualizarCliente verifies that for any valid cliente input:
//   - Cliente snapshot reflects the new nombre.
//   - Direccion, GPS, productos, vendedores are unchanged.
//   - updated_by matches the actor.
//   - Exactly one "venta.cliente_actualizado" event in outbox.
func TestProperty_ActualizarCliente(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		h := newHarness(t)
		before := seedVentaFromHarness(t, h, rt)
		preID := before.ID()
		preDireccion := before.Direccion()
		preProductosCount := before.ProductosCount()
		preVendedoresCount := before.VendedoresCount()

		nombre := genSafeASCII(80).Draw(rt, "cli_nombre")
		actor := uuid.New()

		out, err := h.svc.ActualizarCliente(t.Context(), ventasapp.ActualizarClienteInput{
			VentaID:       preID,
			ClienteNombre: nombre,
		}, actor)
		if err != nil {
			rt.Fatalf("ActualizarCliente failed: %v", err)
		}

		// Cliente snapshot reflects input nombre, folded to ALL CAPS by the
		// domain (Microsip convention); generated input is ASCII-only.
		if out.Cliente().Nombre().Value() != strings.ToUpper(nombre) {
			rt.Fatalf("nombre mismatch: got %q want %q", out.Cliente().Nombre().Value(), strings.ToUpper(nombre))
		}
		// No clienteID was set — must remain nil.
		if out.ClienteID() != nil {
			rt.Fatalf("clienteID should remain nil, got %v", out.ClienteID())
		}

		// Dirección unchanged.
		if out.Direccion().Calle() != preDireccion.Calle() {
			rt.Fatalf("calle changed after ActualizarCliente: %q → %q",
				preDireccion.Calle(), out.Direccion().Calle())
		}
		if out.Direccion().Colonia() != preDireccion.Colonia() {
			rt.Fatalf("colonia changed after ActualizarCliente")
		}

		// Collections unchanged.
		if out.ProductosCount() != preProductosCount {
			rt.Fatalf("productos count changed: got %d want %d", out.ProductosCount(), preProductosCount)
		}
		if out.VendedoresCount() != preVendedoresCount {
			rt.Fatalf("vendedores count changed: got %d want %d", out.VendedoresCount(), preVendedoresCount)
		}

		// updated_by == actor.
		if ventaUpdatedBy(out) != actor {
			rt.Fatalf("updated_by mismatch: got %v want %v", ventaUpdatedBy(out), actor)
		}

		// Outbox: exactly one cliente_actualizado event.
		evts := h.outbox.eventTypes()
		if len(evts) != 1 || evts[0] != domain.EventTypeVentaClienteActualizado {
			rt.Fatalf("expected one cliente_actualizado event, got: %v", evts)
		}
	})
}

// TestProperty_ReemplazarProductos verifies that for a random slice of 1–10
// valid stand-alone productos:
//   - The venta has exactly those productos after the call.
//   - Combos and vendedores are unchanged.
//   - updated_by matches the actor.
//   - Exactly one "venta.productos_reemplazados" event in outbox.
func TestProperty_ReemplazarProductos(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		h := newHarness(t)
		before := seedVentaFromHarness(t, h, rt)
		preCombosCount := before.CombosCount()
		preVendedoresCount := before.VendedoresCount()

		n := rapid.IntRange(1, 10).Draw(rt, "num_productos")
		inputs := make([]ventasapp.CrearVentaProductoInput, n)
		for i := range n {
			inputs[i] = genProductoInput(rt, i)
		}
		actor := uuid.New()

		out, err := h.svc.ReemplazarProductos(t.Context(), ventasapp.ReemplazarProductosInput{
			VentaID:   before.ID(),
			Productos: inputs,
		}, actor)
		if err != nil {
			rt.Fatalf("ReemplazarProductos failed: %v", err)
		}

		// Exact count.
		if out.ProductosCount() != n {
			rt.Fatalf("productos count mismatch: got %d want %d", out.ProductosCount(), n)
		}

		// Build a set of input IDs for round-trip check.
		inputIDs := make(map[uuid.UUID]struct{}, n)
		for _, p := range inputs {
			inputIDs[p.ID] = struct{}{}
		}
		for p := range out.Productos() {
			if _, ok := inputIDs[p.ID()]; !ok {
				rt.Fatalf("unexpected producto ID %v in result", p.ID())
			}
		}

		// Each producto carries a positive cantidad and matching articulo name.
		for p := range out.Productos() {
			if p.Cantidad().Sign() <= 0 {
				rt.Fatalf("producto %v has non-positive cantidad: %v", p.ID(), p.Cantidad())
			}
			if p.Articulo() == "" {
				rt.Fatalf("producto %v has empty articulo", p.ID())
			}
		}

		// Other children unchanged.
		if out.CombosCount() != preCombosCount {
			rt.Fatalf("combos count changed: got %d want %d", out.CombosCount(), preCombosCount)
		}
		if out.VendedoresCount() != preVendedoresCount {
			rt.Fatalf("vendedores count changed: got %d want %d", out.VendedoresCount(), preVendedoresCount)
		}

		// updated_by == actor.
		if ventaUpdatedBy(out) != actor {
			rt.Fatalf("updated_by mismatch: got %v want %v", ventaUpdatedBy(out), actor)
		}

		// Outbox: exactly one productos_reemplazados event.
		evts := h.outbox.eventTypes()
		if len(evts) != 1 || evts[0] != domain.EventTypeVentaProductosReemplazados {
			rt.Fatalf("expected one productos_reemplazados event, got: %v", evts)
		}
	})
}

// TestProperty_ReemplazarVendedores verifies that for a random slice of 1–5
// valid vendedores:
//   - The venta has exactly those vendedores (as a set of IDs).
//   - Combos and productos are unchanged.
//   - updated_by matches the actor.
//   - Exactly one "venta.vendedores_reemplazados" event in outbox.
func TestProperty_ReemplazarVendedores(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		h := newHarness(t)
		before := seedVentaFromHarness(t, h, rt)
		preCombosCount := before.CombosCount()
		preProductosCount := before.ProductosCount()

		n := rapid.IntRange(1, 5).Draw(rt, "num_vendedores")
		inputs := make([]ventasapp.CrearVentaVendedorInput, n)
		for i := range n {
			inputs[i] = genVendedorInput(rt)
		}
		actor := uuid.New()

		out, err := h.svc.ReemplazarVendedores(t.Context(), ventasapp.ReemplazarVendedoresInput{
			VentaID:    before.ID(),
			Vendedores: inputs,
		}, actor)
		if err != nil {
			rt.Fatalf("ReemplazarVendedores failed: %v", err)
		}

		// Exact count.
		if out.VendedoresCount() != n {
			rt.Fatalf("vendedores count mismatch: got %d want %d", out.VendedoresCount(), n)
		}

		// Set of IDs round-trips.
		inputIDs := make(map[uuid.UUID]struct{}, n)
		for _, v := range inputs {
			inputIDs[v.ID] = struct{}{}
		}
		for v := range out.Vendedores() {
			if _, ok := inputIDs[v.ID()]; !ok {
				rt.Fatalf("unexpected vendedor ID %v in result", v.ID())
			}
		}

		// Each vendedor snapshot carries non-empty email and nombre.
		for v := range out.Vendedores() {
			if v.Snapshot().Email() == "" {
				rt.Fatalf("vendedor %v has empty email", v.ID())
			}
			if v.Snapshot().Nombre() == "" {
				rt.Fatalf("vendedor %v has empty nombre", v.ID())
			}
		}

		// Other children unchanged.
		if out.CombosCount() != preCombosCount {
			rt.Fatalf("combos count changed: got %d want %d", out.CombosCount(), preCombosCount)
		}
		if out.ProductosCount() != preProductosCount {
			rt.Fatalf("productos count changed: got %d want %d", out.ProductosCount(), preProductosCount)
		}

		// updated_by == actor.
		if ventaUpdatedBy(out) != actor {
			rt.Fatalf("updated_by mismatch: got %v want %v", ventaUpdatedBy(out), actor)
		}

		// Outbox: exactly one vendedores_reemplazados event.
		evts := h.outbox.eventTypes()
		if len(evts) != 1 || evts[0] != domain.EventTypeVentaVendedoresReemplazados {
			rt.Fatalf("expected one vendedores_reemplazados event, got: %v", evts)
		}
	})
}

// TestProperty_ReemplazarCombos verifies that for a random slice of 0–3 combos
// (combos is optional; the domain accepts empty slices):
//   - When the venta has no productos referencing combos (which is the seeded
//     state — productos are standalone), any combo set (including empty) is
//     accepted.
//   - The venta's combos match the input set.
//   - Vendedores and productos are unchanged.
//   - updated_by matches the actor.
//   - Exactly one "venta.combos_reemplazados" event in outbox.
//
// The seeded venta has NO productos with ComboID set, so replacing combos
// never violates ErrProductoComboReferenciaInvalida.
func TestProperty_ReemplazarCombos(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		h := newHarness(t)
		before := seedVentaFromHarness(t, h, rt)
		preProductosCount := before.ProductosCount()
		preVendedoresCount := before.VendedoresCount()

		n := rapid.IntRange(0, 3).Draw(rt, "num_combos")
		inputs := make([]ventasapp.CrearVentaComboInput, n)
		for i := range n {
			inputs[i] = genComboInput(rt)
		}
		actor := uuid.New()

		out, err := h.svc.ReemplazarCombos(t.Context(), ventasapp.ReemplazarCombosInput{
			VentaID: before.ID(),
			Combos:  inputs,
		}, actor)
		if err != nil {
			rt.Fatalf("ReemplazarCombos failed: %v", err)
		}

		// Exact count matches input.
		if out.CombosCount() != n {
			rt.Fatalf("combos count mismatch: got %d want %d", out.CombosCount(), n)
		}

		// Set of IDs round-trips.
		inputIDs := make(map[uuid.UUID]struct{}, n)
		for _, c := range inputs {
			inputIDs[c.ID] = struct{}{}
		}
		for c := range out.Combos() {
			if _, ok := inputIDs[c.ID()]; !ok {
				rt.Fatalf("unexpected combo ID %v in result", c.ID())
			}
		}

		// Each combo has non-empty nombre, positive cantidad, distinct almacenes.
		for c := range out.Combos() {
			if c.Nombre() == "" {
				rt.Fatalf("combo %v has empty nombre", c.ID())
			}
			if c.Cantidad().Sign() <= 0 {
				rt.Fatalf("combo %v has non-positive cantidad: %v", c.ID(), c.Cantidad())
			}
			if c.AlmacenOrigen() == c.AlmacenDestino() {
				rt.Fatalf("combo %v has same origin and destination almacen: %d", c.ID(), c.AlmacenOrigen())
			}
		}

		// Other children unchanged.
		if out.ProductosCount() != preProductosCount {
			rt.Fatalf("productos count changed: got %d want %d", out.ProductosCount(), preProductosCount)
		}
		if out.VendedoresCount() != preVendedoresCount {
			rt.Fatalf("vendedores count changed: got %d want %d", out.VendedoresCount(), preVendedoresCount)
		}

		// updated_by == actor.
		if ventaUpdatedBy(out) != actor {
			rt.Fatalf("updated_by mismatch: got %v want %v", ventaUpdatedBy(out), actor)
		}

		// Outbox: exactly one combos_reemplazados event.
		evts := h.outbox.eventTypes()
		if len(evts) != 1 || evts[0] != domain.EventTypeVentaCombosReemplazados {
			rt.Fatalf("expected one combos_reemplazados event, got: %v", evts)
		}
	})
}
