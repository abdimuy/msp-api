// Package cobranzahttp hosts the cobranza module's HTTP transport: handlers,
// DTOs, and the Huma-over-chi router mount points.
//
//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package cobranzahttp

import (
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// ─── Response DTOs ────────────────────────────────────────────────────────────

// SaldoDTO is the JSON projection of a domain.Saldo for the HTTP boundary.
// Decimal values are serialized as strings to avoid JSON precision loss.
// Timestamps are RFC3339 UTC.
type SaldoDTO struct {
	DoctoCCID      int     `json:"docto_cc_id"     doc:"PK del cargo en DOCTOS_CC"`
	DoctoPVID      *int    `json:"docto_pv_id"     doc:"ID del documento PV origen, null si no hay PV"`
	ClienteID      int     `json:"cliente_id"      doc:"ID del cliente en CLIENTES de Microsip"`
	ZonaClienteID  *int    `json:"zona_cliente_id" doc:"ID de zona del cliente, null si no tiene zona"`
	Folio          string  `json:"folio"           doc:"Folio del cargo en Microsip"`
	FechaCargo     string  `json:"fecha_cargo"     doc:"Fecha del cargo (RFC3339 UTC)"`
	PrecioTotal    string  `json:"precio_total"    doc:"Importe total del cargo"`
	TotalImporte   string  `json:"total_importe"   doc:"Suma de cobros activos (conceptos 87327, 155, 11)"`
	ImpteRest      string  `json:"impte_rest"      doc:"Otros descuentos (enganches, condonaciones, fugas)"`
	Saldo          string  `json:"saldo"           doc:"Saldo pendiente = precio_total - total_importe - impte_rest"`
	NumPagos       int     `json:"num_pagos"       doc:"Número de pagos aplicados"`
	FechaUltPago   *string `json:"fecha_ult_pago"  doc:"Fecha del último pago (RFC3339 UTC), null si sin pagos"`
	CargoCancelado bool    `json:"cargo_cancelado" doc:"true si el cargo fue cancelado en Microsip"`
	UpdatedAt      string  `json:"updated_at"      doc:"Timestamp del último refresco del caché (RFC3339 UTC)"`
}

// ResumenZonaDTO is the JSON projection of a domain.ResumenZona.
type ResumenZonaDTO struct {
	ZonaID      int    `json:"zona_id"      doc:"ID de la zona"`
	TotalVentas int    `json:"total_ventas" doc:"Número de ventas abiertas en la zona"`
	SaldoTotal  string `json:"saldo_total"  doc:"Suma de saldos pendientes en la zona"`
}

// ReconcileReportDTO is the JSON projection of app.ReconcileReport.
type ReconcileReportDTO struct {
	Checked    int    `json:"checked"     doc:"Total de cargos revisados"`
	Drift      int    `json:"drift"       doc:"Cargos con saldo incorrecto detectados"`
	Errors     int    `json:"errors"      doc:"Cargos que no pudieron revisarse por error transitorio"`
	StartedAt  string `json:"started_at"  doc:"Inicio del paso de reconciliación (RFC3339 UTC)"`
	FinishedAt string `json:"finished_at" doc:"Fin del paso de reconciliación (RFC3339 UTC)"`
}

// SaldoErrorDTO is the JSON projection of outbound.SaldoError.
type SaldoErrorDTO struct {
	ErrorID  int    `json:"error_id"  doc:"PK del error en MSP_SALDOS_ERRORS"`
	CargoID  int    `json:"cargo_id"  doc:"ID del cargo que produjo el error"`
	ErrorMsg string `json:"error_msg" doc:"Mensaje del error (SQLCODE y GDSCODE)"`
	ErrorAt  string `json:"error_at"  doc:"Timestamp del error (RFC3339 UTC)"`
}

// ─── Request Input DTOs ───────────────────────────────────────────────────────

// PorVentaInput is the path parameter for GET /cobranza/saldos/venta/{id}.
type PorVentaInput struct {
	ID int `path:"id" doc:"DOCTO_PV_ID del documento de punto de venta"`
}

// PorClienteInput is the path parameter for GET /cobranza/saldos/cliente/{cliente_id}.
type PorClienteInput struct {
	ClienteID int `path:"cliente_id" doc:"ID del cliente en Microsip CLIENTES"`
}

// PorZonaInput contains the path parameter and optional query params for
// GET /cobranza/saldos/zona/{zona_id}.
//
// Exactly one of `desde` or `ventana_dias` should be supplied. When both are
// omitted, the handler defaults to ventana_dias=7. When both are present the
// service returns 422 cobranza_parametros_excluyentes.
//
// `desde` is the recommended parameter for deterministic results — the cutoff
// stays fixed across polling calls instead of drifting with the server clock.
// Accepts YYYY-MM-DD or RFC3339 (e.g. 2026-05-23 or 2026-05-23T08:00:00Z).
// The time component is truncated to DATE precision by the cache schema.
type PorZonaInput struct {
	ZonaID      int    `path:"zona_id"                                doc:"ID de la zona de cobranza"`
	Desde       string `query:"desde"                                 doc:"Fecha absoluta (YYYY-MM-DD o RFC3339). Excluyente con ventana_dias"`
	VentanaDias int    `query:"ventana_dias" minimum:"-1" maximum:"90" default:"-1" doc:"Días hacia atrás desde hoy. -1 = no supplied (usa default 7). Excluyente con desde"`
}

// ResumenZonasInput is the (empty) input for GET /cobranza/resumen-zonas.
type ResumenZonasInput struct{}

// ReconcileInput is the (empty) input for POST /_admin/saldos/reconcile. The
// operation takes no parameters; a body would just be ignored.
type ReconcileInput struct{}

// BackfillInput is the (empty) input for POST /_admin/saldos/backfill. The
// backfill runs unconditionally over every cargo in the cache.
type BackfillInput struct{}

// ErrorsInput are the query params for GET /_admin/saldos/errors.
type ErrorsInput struct {
	Limit int `query:"limit" default:"100" minimum:"1" maximum:"500" doc:"Número máximo de errores a retornar"`
}

// ─── Response Output DTOs ─────────────────────────────────────────────────────

// SaldoOutput wraps a single SaldoDTO.
type SaldoOutput struct {
	Body SaldoDTO
}

// SaldosOutput wraps a slice of SaldoDTO.
type SaldosOutput struct {
	Body []SaldoDTO
}

// ResumenZonasOutput wraps a slice of ResumenZonaDTO.
type ResumenZonasOutput struct {
	Body []ResumenZonaDTO
}

// ReconcileOutput wraps the reconcile report.
type ReconcileOutput struct {
	Body ReconcileReportDTO
}

// BackfillOutput wraps the backfill report (reuses ReconcileReportDTO).
type BackfillOutput struct {
	Body ReconcileReportDTO
}

// ErrorsOutput wraps a slice of SaldoErrorDTO.
type ErrorsOutput struct {
	Body []SaldoErrorDTO
}

// ─── Mapping helpers ──────────────────────────────────────────────────────────

// toSaldoDTO projects a domain.Saldo into a SaldoDTO.
func toSaldoDTO(s domain.Saldo) SaldoDTO {
	dto := SaldoDTO{
		DoctoCCID:      s.DoctoCCID(),
		DoctoPVID:      s.DoctoPVID(),
		ClienteID:      s.ClienteID(),
		ZonaClienteID:  s.ZonaClienteID(),
		Folio:          s.Folio(),
		FechaCargo:     s.FechaCargo().UTC().Format(time.RFC3339),
		PrecioTotal:    s.PrecioTotal().StringFixed(2),
		TotalImporte:   s.TotalImporte().StringFixed(2),
		ImpteRest:      s.ImpteRest().StringFixed(2),
		Saldo:          s.Saldo().StringFixed(2),
		NumPagos:       s.NumPagos(),
		CargoCancelado: s.CargoCancelado(),
		UpdatedAt:      s.UpdatedAt().UTC().Format(time.RFC3339),
	}
	if fup := s.FechaUltPago(); fup != nil {
		formatted := fup.UTC().Format(time.RFC3339)
		dto.FechaUltPago = &formatted
	}
	return dto
}

// toResumenZonaDTO projects a domain.ResumenZona into a ResumenZonaDTO.
func toResumenZonaDTO(r domain.ResumenZona) ResumenZonaDTO {
	return ResumenZonaDTO{
		ZonaID:      r.ZonaID(),
		TotalVentas: r.TotalVentas(),
		SaldoTotal:  r.SaldoTotal().StringFixed(2),
	}
}

// toReconcileReportDTO projects an app.ReconcileReport into a ReconcileReportDTO.
func toReconcileReportDTO(r app.ReconcileReport) ReconcileReportDTO {
	return ReconcileReportDTO{
		Checked:    r.Checked,
		Drift:      r.Drift,
		Errors:     r.Errors,
		StartedAt:  r.StartedAt.UTC().Format(time.RFC3339),
		FinishedAt: r.FinishedAt.UTC().Format(time.RFC3339),
	}
}

// toErrorDTO projects an outbound.SaldoError into a SaldoErrorDTO.
func toErrorDTO(e outbound.SaldoError) SaldoErrorDTO {
	return SaldoErrorDTO{
		ErrorID:  e.ErrorID,
		CargoID:  e.CargoID,
		ErrorMsg: e.ErrorMsg,
		ErrorAt:  e.ErrorAt.UTC().Format(time.RFC3339),
	}
}
