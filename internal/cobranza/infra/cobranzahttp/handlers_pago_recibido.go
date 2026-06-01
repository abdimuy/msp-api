//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/auth"
	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// CrearPago handles POST /cobranza/pagos.
//
// Persists a PagoRecibido into the MSP_PAGOS_RECIBIDOS outbox and
// best-effort-applies it to Microsip in the same request. If the Microsip
// writer fails the row stays ESTADO='P' for the retry worker; the HTTP
// response always returns 201 with whatever state the pago ended up in.
//
// The client-generated UUID (Body.ID) is the idempotency key end-to-end: a
// second request with the same UUID returns the existing row without a second
// INSERT.
func (h *Handlers) CrearPago(ctx context.Context, in *CrearPagoInput) (*CrearPagoOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}

	pagoID, err := uuid.Parse(in.Body.ID)
	if err != nil {
		return nil, mapAppError(
			apperror.NewValidation("pago_id_invalido", "el id del pago no es un UUID válido").WithError(err),
		)
	}

	// Idempotency-Key header must match body.id when present.
	if in.IdempotencyKey != "" && in.IdempotencyKey != in.Body.ID {
		return nil, mapAppError(
			apperror.NewValidation("idempotency_key_mismatch", "Idempotency-Key debe coincidir con body.id"),
		)
	}

	fecha, err := time.Parse(time.RFC3339, in.Body.FechaHoraPago)
	if err != nil {
		return nil, mapAppError(
			apperror.NewValidation("fecha_hora_pago_invalida", "fecha_hora_pago no es una fecha-hora RFC3339 válida").WithError(err),
		)
	}

	importe, err := decimal.NewFromString(in.Body.Importe)
	if err != nil {
		return nil, mapAppError(
			apperror.NewValidation("importe_invalido", "importe no es un decimal válido").WithError(err),
		)
	}

	appIn := cobranzaapp.CrearPagoInput{
		ID:             pagoID,
		CargoDoctoCCID: in.Body.CargoDoctoCCID,
		ClienteID:      in.Body.ClienteID,
		CobradorID:     in.Body.CobradorID,
		Cobrador:       in.Body.Cobrador,
		Importe:        importe,
		FormaCobroID:   in.Body.FormaCobroID,
		FechaHoraPago:  fecha.UTC(),
		Lat:            stringToOptional(in.Body.Lat),
		Lon:            stringToOptional(in.Body.Lon),
	}
	pago, err := h.svc.CrearPago(ctx, appIn, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &CrearPagoOutput{Body: toPagoRecibidoDTO(pago)}, nil
}

// ObtenerPagoRecibido handles GET /cobranza/pagos/{id}.
//
// Loads a PagoRecibido from the outbox by UUID. Returns 404 when not found.
func (h *Handlers) ObtenerPagoRecibido(ctx context.Context, in *ObtenerPagoInput) (*ObtenerPagoOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}
	id, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	pago, err := h.svc.ObtenerPago(ctx, id)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &ObtenerPagoOutput{Body: toPagoRecibidoDTO(pago)}, nil
}

// ListarPendientes handles GET /_admin/cobranza/pagos/pendientes.
//
// Returns pagos with ESTADO='P' being drained by the retry worker. Requires
// PermCobranzaReconciliar (admin-only).
func (h *Handlers) ListarPendientes(ctx context.Context, in *ListarPendientesInput) (*ListarPendientesOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaReconciliar); err != nil {
		return nil, err
	}
	maxIntentos := in.MaxIntentos
	if maxIntentos <= 0 {
		maxIntentos = 10
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	pagos, err := h.svc.ListarPagosPendientes(ctx, maxIntentos, limit)
	if err != nil {
		return nil, mapAppError(err)
	}
	items := make([]PagoRecibidoDTO, 0, len(pagos))
	for _, p := range pagos {
		items = append(items, toPagoRecibidoDTO(p))
	}
	return &ListarPendientesOutput{Body: items}, nil
}

// AplicarPagoForzar handles POST /_admin/cobranza/pagos/{id}/aplicar.
//
// Forces a manual application attempt on a pending pago. Requires
// PermCobranzaReconciliar (admin-only).
func (h *Handlers) AplicarPagoForzar(ctx context.Context, in *AplicarPagoInput) (*AplicarPagoOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaReconciliar); err != nil {
		return nil, err
	}
	id, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	pago, err := h.svc.AplicarPago(ctx, id, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &AplicarPagoOutput{Body: toPagoRecibidoDTO(pago)}, nil
}

// ─── Local helpers ────────────────────────────────────────────────────────────

// stringToOptional converts an empty string to nil and a non-empty string
// to a pointer. Used for optional Lat/Lon fields.
func stringToOptional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ─── Compile-time handler signature checks ────────────────────────────────────

var (
	_ func(context.Context, *CrearPagoInput) (*CrearPagoOutput, error)               = (*Handlers)(nil).CrearPago
	_ func(context.Context, *ObtenerPagoInput) (*ObtenerPagoOutput, error)           = (*Handlers)(nil).ObtenerPagoRecibido
	_ func(context.Context, *ListarPendientesInput) (*ListarPendientesOutput, error) = (*Handlers)(nil).ListarPendientes
	_ func(context.Context, *AplicarPagoInput) (*AplicarPagoOutput, error)           = (*Handlers)(nil).AplicarPagoForzar
)
