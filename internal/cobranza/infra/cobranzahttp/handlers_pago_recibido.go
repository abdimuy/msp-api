//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/auth"
	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// CrearPago handles POST /cobranza/pagos.
//
// Accepts a multipart/form-data body that carries both the pago JSON
// (`datos` field) and zero or more comprobantes (`imagen` repeated). Server
// persists pago + imagenes atómicamente inside one Firebird tx via
// [cobranzaapp.Service.CrearPagoConImagenes]; any failure rolls back the
// whole write and best-effort-cleans the blobs already on disk.
//
// After the atomic write commits, the legacy best-effort fast-path
// AplicarPago runs unchanged — if the Microsip writer fails the pago stays
// ESTADO='P' for the retry worker and the HTTP response still returns 201.
//
// Idempotency: the client-generated UUID in `datos.id` is the idempotency
// key end-to-end. A second request with the same UUID returns the existing
// pago without rewriting anything (its already-stored imagenes are kept;
// any new blobs in the replay request are cleaned up).
//
// The legacy POST /pagos/{id}/imagenes endpoint is retained for adding
// comprobantes after this initial atomic write.
func (h *Handlers) CrearPago(ctx context.Context, in *CrearPagoInput) (*CrearPagoOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}

	fields := in.RawBody.Data()
	body, err := decodeCrearPagoDatos(fields.Datos)
	if err != nil {
		return nil, mapAppError(err)
	}

	pagoID, err := uuid.Parse(body.ID)
	if err != nil {
		return nil, mapAppError(
			apperror.NewValidation("pago_id_invalido", "el id del pago no es un UUID válido").WithError(err),
		)
	}

	if in.IdempotencyKey != "" && in.IdempotencyKey != body.ID {
		return nil, mapAppError(
			apperror.NewValidation("idempotency_key_mismatch", "Idempotency-Key debe coincidir con datos.id"),
		)
	}

	fecha, err := time.Parse(time.RFC3339, body.FechaHoraPago)
	if err != nil {
		return nil, mapAppError(
			apperror.NewValidation("fecha_hora_pago_invalida", "fecha_hora_pago no es una fecha-hora RFC3339 válida").WithError(err),
		)
	}

	importe, err := decimal.NewFromString(body.Importe)
	if err != nil {
		return nil, mapAppError(
			apperror.NewValidation("importe_invalido", "importe no es un decimal válido").WithError(err),
		)
	}

	imgUploads, openedFiles, err := parseImagenesFromMultipart(pagoID, fields.Imagen, in.RawBody.Form)
	defer func() {
		for _, f := range openedFiles {
			_ = f.Close()
		}
	}()
	if err != nil {
		return nil, mapAppError(err)
	}

	appIn := cobranzaapp.CrearPagoInput{
		ID:             pagoID,
		CargoDoctoCCID: body.CargoDoctoCCID,
		ClienteID:      body.ClienteID,
		CobradorID:     body.CobradorID,
		Cobrador:       body.Cobrador,
		Importe:        importe,
		FormaCobroID:   body.FormaCobroID,
		FechaHoraPago:  fecha.UTC(),
		Lat:            stringToOptional(body.Lat),
		Lon:            stringToOptional(body.Lon),
	}
	pago, err := h.svc.CrearPagoConImagenes(ctx, appIn, imgUploads, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &CrearPagoOutput{Body: toPagoRecibidoDTO(pago)}, nil
}

// decodeCrearPagoDatos validates that `datos` was supplied and parses it as
// JSON into CrearPagoBody. Returns stable apperror codes so the client knows
// whether the field was missing vs. malformed.
func decodeCrearPagoDatos(raw string) (CrearPagoBody, error) {
	if raw == "" {
		return CrearPagoBody{}, apperror.NewValidation(
			"datos_requerido", "el campo multipart 'datos' es obligatorio",
		)
	}
	var body CrearPagoBody
	dec := json.NewDecoder(jsonStringReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		return CrearPagoBody{}, apperror.NewValidation(
			"datos_json_invalido", "el campo 'datos' no es un JSON válido",
		).WithError(err)
	}
	return body, nil
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
