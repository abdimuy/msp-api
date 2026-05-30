//nolint:misspell // domain vocabulary is Spanish (ventas, cancelada, aprobada, borrador) per project convention.
package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ─── Shared helpers ────────────────────────────────────────────────────────

// validHeaderInput returns the minimum ActualizarHeaderInput that passes
// domain validation for a CONTADO venta. Tests reuse it to avoid coupling
// the status-matrix cells to specific field values.
func validHeaderInput(ventaID uuid.UUID) ventasapp.ActualizarHeaderInput {
	return ventasapp.ActualizarHeaderInput{
		VentaID:       ventaID,
		Calle:         "Av. Reforma",
		Colonia:       "Centro",
		Poblacion:     "Cuauhtemoc",
		Ciudad:        "CDMX",
		Latitud:       19.4326,
		Longitud:      -99.1332,
		FechaVenta:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		PrecioAnual:   decimal.NewFromInt(1200),
		PrecioCorto:   decimal.NewFromInt(1100),
		PrecioContado: decimal.NewFromInt(1000),
	}
}

func validProductosInput(ventaID uuid.UUID) ventasapp.ReemplazarProductosInput {
	one, two := 1, 2
	return ventasapp.ReemplazarProductosInput{
		VentaID: ventaID,
		Productos: []ventasapp.CrearVentaProductoInput{{
			ID:             uuid.New(),
			ArticuloID:     42,
			Articulo:       "Refrigerador",
			Cantidad:       decimal.NewFromInt(1),
			PrecioAnual:    decimal.NewFromInt(1200),
			PrecioCorto:    decimal.NewFromInt(1100),
			PrecioContado:  decimal.NewFromInt(1000),
			AlmacenOrigen:  &one,
			AlmacenDestino: &two,
		}},
	}
}

func validCombosInput(ventaID uuid.UUID) ventasapp.ReemplazarCombosInput {
	return ventasapp.ReemplazarCombosInput{
		VentaID: ventaID,
		Combos: []ventasapp.CrearVentaComboInput{{
			ID:             uuid.New(),
			Nombre:         "Combo Demo",
			PrecioAnual:    decimal.NewFromInt(500),
			PrecioCorto:    decimal.NewFromInt(450),
			PrecioContado:  decimal.NewFromInt(400),
			Cantidad:       decimal.NewFromInt(2),
			AlmacenOrigen:  1,
			AlmacenDestino: 2,
		}},
	}
}

func validVendedoresInput(ventaID uuid.UUID) ventasapp.ReemplazarVendedoresInput {
	return ventasapp.ReemplazarVendedoresInput{
		VentaID: ventaID,
		Vendedores: []ventasapp.CrearVentaVendedorInput{{
			ID:        uuid.New(),
			UsuarioID: uuid.New(),
			Email:     "gerente@muebleriamsp.mx",
			Nombre:    "Roberto Garza",
		}},
	}
}

// matrixOp is a single column of the status × operation matrix.
type matrixOp struct {
	opName string
	invoke func(svc *ventasapp.Service, id, by uuid.UUID) error
}

// editOps is the canonical ordered list of the five edit operations.
func editOps() []matrixOp {
	return []matrixOp{
		{
			opName: "ActualizarHeader",
			invoke: func(svc *ventasapp.Service, id, by uuid.UUID) error {
				_, err := svc.ActualizarHeader(context.Background(), validHeaderInput(id), by)
				return err
			},
		},
		{
			opName: "ActualizarCliente",
			invoke: func(svc *ventasapp.Service, id, by uuid.UUID) error {
				_, err := svc.ActualizarCliente(context.Background(), ventasapp.ActualizarClienteInput{
					VentaID:       id,
					ClienteNombre: "Lucía Hernández",
				}, by)
				return err
			},
		},
		{
			opName: "ReemplazarProductos",
			invoke: func(svc *ventasapp.Service, id, by uuid.UUID) error {
				_, err := svc.ReemplazarProductos(context.Background(), validProductosInput(id), by)
				return err
			},
		},
		{
			opName: "ReemplazarCombos",
			invoke: func(svc *ventasapp.Service, id, by uuid.UUID) error {
				_, err := svc.ReemplazarCombos(context.Background(), validCombosInput(id), by)
				return err
			},
		},
		{
			opName: "ReemplazarVendedores",
			invoke: func(svc *ventasapp.Service, id, by uuid.UUID) error {
				_, err := svc.ReemplazarVendedores(context.Background(), validVendedoresInput(id), by)
				return err
			},
		},
	}
}

// hydrateOverrides carries the lifecycle dimensions a seed helper wants to
// stamp onto a cloned venta. Empty values fall back to the active/borrador/
// pendiente defaults.
type hydrateOverrides struct {
	situacion          domain.Situacion
	sincronizacion     domain.Sincronizacion
	microsipDoctoPVID  *int
	microsipFolio      *string
	microsipAplicadaAt *time.Time
}

// seedWithSituacion clones the seeded venta into a hydrated copy carrying the
// requested lifecycle dimensions and swaps it into the fake repo. Used to
// stage revisada/aprobada/aplicada ventas without driving every transition.
func (h *testHarness) seedWithSituacion(t *testing.T, o hydrateOverrides) uuid.UUID {
	t.Helper()
	ventaID := h.seedVenta(t)

	h.ventas.mu.Lock()
	original := h.ventas.byID[*ventaID]
	h.ventas.mu.Unlock()

	now := h.clock.T
	sincronizacion := o.sincronizacion
	if sincronizacion == "" {
		sincronizacion = domain.SincronizacionPendiente
	}

	clone := domain.HydrateVenta(domain.HydrateVentaParams{
		ID:                 original.ID(),
		ClienteID:          original.ClienteID(),
		Cliente:            original.Cliente(),
		Direccion:          original.Direccion(),
		GPS:                original.GPS(),
		FechaVenta:         original.FechaVenta(),
		TipoVenta:          original.TipoVenta(),
		Montos:             original.Montos(),
		PlanCredito:        original.PlanCredito(),
		DiaCobranza:        original.DiaCobranza(),
		Nota:               original.Nota(),
		Estado:             domain.EstadoActive,
		Situacion:          o.situacion,
		Sincronizacion:     sincronizacion,
		MicrosipDoctoPVID:  o.microsipDoctoPVID,
		MicrosipFolio:      o.microsipFolio,
		MicrosipAplicadaAt: o.microsipAplicadaAt,
		CreatedAt:          now,
		UpdatedAt:          now,
		CreatedBy:          uuid.Nil,
		UpdatedBy:          uuid.Nil,
	})

	h.ventas.mu.Lock()
	h.ventas.byID[*ventaID] = clone
	h.ventas.mu.Unlock()

	return *ventaID
}

// seedRevisada stages a venta in situación 'revisada'.
func (h *testHarness) seedRevisada(t *testing.T) uuid.UUID {
	t.Helper()
	return h.seedWithSituacion(t, hydrateOverrides{situacion: domain.SituacionRevisada})
}

// seedAprobada stages a venta in situación 'aprobada' (sincronización pendiente).
func (h *testHarness) seedAprobada(t *testing.T) uuid.UUID {
	t.Helper()
	return h.seedWithSituacion(t, hydrateOverrides{situacion: domain.SituacionAprobada})
}

// seedAplicada stages an aprobada venta already materialized in Microsip.
func (h *testHarness) seedAplicada(t *testing.T) uuid.UUID {
	t.Helper()
	doctoID := 15239197
	folio := "Y00002266"
	at := h.clock.T
	return h.seedWithSituacion(t, hydrateOverrides{
		situacion:          domain.SituacionAprobada,
		sincronizacion:     domain.SincronizacionAplicada,
		microsipDoctoPVID:  &doctoID,
		microsipFolio:      &folio,
		microsipAplicadaAt: &at,
	})
}

// seedCancelada creates a venta via the service and then cancels it, returning
// the venta's ID.
func (h *testHarness) seedCancelada(t *testing.T) uuid.UUID {
	t.Helper()
	ventaID := h.seedVenta(t)
	_, err := h.svc.CancelarVenta(context.Background(), *ventaID, "cliente rechazó la entrega", uuid.New())
	require.NoError(t, err)
	// Clear cancel event from outbox so later assertions start clean.
	h.outbox.mu.Lock()
	h.outbox.calls = nil
	h.outbox.mu.Unlock()
	return *ventaID
}

// ─── Happy-path matrix (borrador allows all edits) ─────────────────────────

// TestStatus_Borrador_AllowsEdits verifies that every edit operation succeeds
// when the venta is in StatusBorrador. This locks in the "must succeed" side
// of the matrix so regressions in the guard logic are immediately visible.
func TestStatus_Borrador_AllowsEdits(t *testing.T) {
	t.Parallel()
	for _, op := range editOps() {
		op := op
		t.Run(op.opName, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			ventaID := h.seedVenta(t)
			by := uuid.New()
			err := op.invoke(h.svc, *ventaID, by)
			require.NoError(t, err, "borrador should allow %s", op.opName)
		})
	}
}

// ─── Revisada rejects every edit ───────────────────────────────────────────

// TestStatus_Revisada_RejectsEdits verifies that every edit operation returns
// domain.ErrVentaNoEditable once the venta leaves borrador for revisada.
func TestStatus_Revisada_RejectsEdits(t *testing.T) {
	t.Parallel()
	for _, op := range editOps() {
		op := op
		t.Run(op.opName, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			ventaID := h.seedRevisada(t)
			by := uuid.New()
			err := op.invoke(h.svc, ventaID, by)
			require.ErrorIs(t, err, domain.ErrVentaNoEditable,
				"revisada should reject %s with ErrVentaNoEditable", op.opName)
		})
	}
}

// ─── Aprobada rejects every edit ───────────────────────────────────────────

// TestStatus_Aprobada_RejectsEdits verifies that every edit operation returns
// domain.ErrVentaNoEditable when the venta is in situación aprobada.
func TestStatus_Aprobada_RejectsEdits(t *testing.T) {
	t.Parallel()
	for _, op := range editOps() {
		op := op
		t.Run(op.opName, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			ventaID := h.seedAprobada(t)
			by := uuid.New()
			err := op.invoke(h.svc, ventaID, by)
			require.ErrorIs(t, err, domain.ErrVentaNoEditable,
				"aprobada should reject %s with ErrVentaNoEditable", op.opName)
		})
	}
}

// ─── Cancelada rejects every edit ──────────────────────────────────────────

// TestStatus_Cancelada_RejectsEdits verifies that every edit operation returns
// domain.ErrVentaNoEditable when the venta is in StatusCancelada.
// Note: AdjuntarImagen / EliminarImagen (not in the edit matrix) return
// ErrVentaCanceladaInmutable instead — they check IsCanceled() directly.
func TestStatus_Cancelada_RejectsEdits(t *testing.T) {
	t.Parallel()
	for _, op := range editOps() {
		op := op
		t.Run(op.opName, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			ventaID := h.seedCancelada(t)
			by := uuid.New()
			err := op.invoke(h.svc, ventaID, by)
			require.ErrorIs(t, err, domain.ErrVentaNoEditable,
				"cancelada should reject %s with ErrVentaNoEditable", op.opName)
		})
	}
}

// ─── Cancel transition edge cases ──────────────────────────────────────────

// TestStatus_Cancelada_RejectsCancelAgain verifies that attempting to cancel
// an already-canceled venta returns domain.ErrVentaYaCancelada.
func TestStatus_Cancelada_RejectsCancelAgain(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ventaID := h.seedCancelada(t)

	_, err := h.svc.CancelarVenta(context.Background(), ventaID, "intento duplicado", uuid.New())
	require.ErrorIs(t, err, domain.ErrVentaYaCancelada)
}

// TestStatus_Aprobada_AllowsCancel verifies that an aprobada venta that has
// NOT been materialized in Microsip can still be canceled directly. Cancelar
// is generalized to any non-canceled, non-aplicada situación.
func TestStatus_Aprobada_AllowsCancel(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ventaID := h.seedAprobada(t)

	v, err := h.svc.CancelarVenta(context.Background(), ventaID, "gerencia revocó la aprobación", uuid.New())
	require.NoError(t, err)
	require.Equal(t, domain.SituacionCancelada, v.Situacion())
}

// TestStatus_Aplicada_RejectsCancel verifies that a venta already materialized
// in Microsip cannot be canceled through this flow — reversing an applied
// venta requires reversing the DOCTOS_PV documents (out of scope).
func TestStatus_Aplicada_RejectsCancel(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ventaID := h.seedAplicada(t)

	_, err := h.svc.CancelarVenta(context.Background(), ventaID, "ya no", uuid.New())
	require.ErrorIs(t, err, domain.ErrVentaYaAplicada)
}
