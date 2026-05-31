// Package app contains the cobranza module's query services. It depends only
// on the cobranza domain, the module's outbound ports, and the standard
// library. Wiring (database pool, http handlers) lives in infra; cross-module
// surfaces live in the cobranza root package.
package app

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// DefaultVentanaDias is the value the HTTP handler supplies when neither
// `desde` nor `ventana_dias` is provided. 7 days matches the cobrador's
// typical routing window.
const DefaultVentanaDias = 7

// MaxVentanaDias caps the relative window at 90 days; beyond that the caller
// is expected to use the absolute `desde` parameter, which has no cap.
const MaxVentanaDias = 90

// Sync paging defaults / cap. Limit ≤ 5000 prevents request-side abuse;
// the default of 1000 matches the mobile-app worker's batch size.
const (
	DefaultSyncLimit = 1000
	MaxSyncLimit     = 5000
)

// Service is the cobranza module's query surface. Handlers depend on
// *Service; everything Service depends on goes through the outbound ports.
type Service struct {
	saldos outbound.SaldosRepo
	pagos  outbound.PagosRepo
	ventas outbound.VentasRepo
	clock  outbound.Clock
}

// NewService builds a Service wired against the given ports. ventas may be
// nil for tests that only exercise the saldos/pagos surface; the sync-ventas
// endpoint will panic if called without a wired VentasRepo (caught by the
// fx wiring at startup).
func NewService(
	saldos outbound.SaldosRepo,
	pagos outbound.PagosRepo,
	ventas outbound.VentasRepo,
	clock outbound.Clock,
) *Service {
	return &Service{saldos: saldos, pagos: pagos, ventas: ventas, clock: clock}
}

// PorVenta returns the cached saldo for the given PV document ID.
// Returns domain.ErrSaldoNoEncontrado when no cache row exists.
func (s *Service) PorVenta(ctx context.Context, doctoPVID int) (*domain.Saldo, error) {
	return s.saldos.PorVenta(ctx, doctoPVID)
}

// PorCargo returns the cached saldo for the given cargo (DOCTOS_CC) ID.
// Returns domain.ErrSaldoNoEncontrado when no cache row exists.
func (s *Service) PorCargo(ctx context.Context, doctoCCID int) (*domain.Saldo, error) {
	return s.saldos.PorCargo(ctx, doctoCCID)
}

// EnRutaPorZona returns ventas abiertas for a zona plus saldadas with
// FECHA_ULT_PAGO >= the resolved cutoff date.
//
// Exactly one of desde or ventanaDias may be non-nil:
//   - desde: explicit RFC3339 cutoff (deterministic across calls); the time
//     component is preserved on the way in but truncated to DATE precision
//     by the underlying column.
//   - ventanaDias: relative window in days, resolved at call time via the
//     injected clock. Must be in [0, MaxVentanaDias].
//   - both nil: defaults to ventanaDias=DefaultVentanaDias (7).
//   - both non-nil: returns ErrParametrosExcluyentes.
//
// When the resolved cutoff is zero-valued, the repo returns only abiertas
// (no UNION branch — faster).
func (s *Service) EnRutaPorZona(ctx context.Context, zonaID int, desde *time.Time, ventanaDias *int) ([]domain.Saldo, error) {
	cutoff, err := resolveCutoff(desde, ventanaDias, s.clock)
	if err != nil {
		return nil, err
	}
	return s.saldos.EnRutaPorZona(ctx, zonaID, cutoff)
}

// AbiertasPorCliente returns all open saldos (positive balance, not cancelled)
// for the given cliente.
func (s *Service) AbiertasPorCliente(ctx context.Context, clienteID int) ([]domain.Saldo, error) {
	return s.saldos.AbiertasPorCliente(ctx, clienteID)
}

// ResumenZonas returns an aggregated view of open saldos grouped by zona.
func (s *Service) ResumenZonas(ctx context.Context) ([]domain.ResumenZona, error) {
	return s.saldos.ResumenZonas(ctx)
}

// SyncSaldosPorZona returns a page of saldos for incremental sync.
func (s *Service) SyncSaldosPorZona(
	ctx context.Context, zonaID int, cursor time.Time, afterID, limit int,
) (outbound.SyncPage[domain.Saldo], error) {
	limit, err := clampSyncLimit(limit)
	if err != nil {
		return outbound.SyncPage[domain.Saldo]{}, err
	}
	return s.saldos.SyncPorZona(ctx, zonaID, cursor, afterID, limit)
}

// PagosPorVenta returns every pago acreditado al cargo doctoCCID, ordered by
// FECHA ascending.
func (s *Service) PagosPorVenta(ctx context.Context, doctoCCID int) ([]domain.Pago, error) {
	return s.pagos.PorVenta(ctx, doctoCCID)
}

// PagosPorCliente returns every pago made by the given cliente, ordered by
// FECHA descending.
func (s *Service) PagosPorCliente(ctx context.Context, clienteID int) ([]domain.Pago, error) {
	return s.pagos.PorCliente(ctx, clienteID)
}

// PagosEnRutaPorZona returns pagos hechos en la zona con FECHA >= cutoff,
// resolved from desde / ventanaDias the same way as EnRutaPorZona.
func (s *Service) PagosEnRutaPorZona(
	ctx context.Context, zonaID int, desde *time.Time, ventanaDias *int,
) ([]domain.Pago, error) {
	cutoff, err := resolveCutoff(desde, ventanaDias, s.clock)
	if err != nil {
		return nil, err
	}
	return s.pagos.EnRutaPorZona(ctx, zonaID, cutoff)
}

// SyncPagosPorZona returns a page of pagos for incremental sync.
//
// desde acota la ventana de saldados en el sync inicial (cursor zero): nil
// devuelve solo pagos de cargos con saldo activo (legacy), set incluye
// además pagos con FECHA >= desde aunque la venta ya esté saldada. Ignorado
// cuando cursor != zero. Mismo contrato wire que /cobranza/pagos/zona.
func (s *Service) SyncPagosPorZona(
	ctx context.Context, zonaID int, cursor time.Time, afterID, limit int, desde *time.Time,
) (outbound.SyncPage[domain.Pago], error) {
	limit, err := clampSyncLimit(limit)
	if err != nil {
		return outbound.SyncPage[domain.Pago]{}, err
	}
	return s.pagos.SyncPorZona(ctx, zonaID, cursor, afterID, limit, optionalDesdeOrZero(desde))
}

// SyncVentasPorZona returns a page of enriched ventas for incremental sync.
// Each item carries the saldo row plus the static cliente/direccion/contrato
// fields needed to render the mobile cobranza UI without a follow-up call.
//
// desde acota la ventana de saldadas en el sync inicial (cursor zero): nil
// devuelve solo activas + tombstones (legacy), set incluye además ventas
// saldadas con FECHA_ULT_PAGO >= desde para que la app del cobrador las
// conserve dentro de su ventana visible. Ignorado cuando cursor != zero.
func (s *Service) SyncVentasPorZona(
	ctx context.Context, zonaID int, cursor time.Time, afterID, limit int, desde *time.Time,
) (outbound.SyncPage[domain.Venta], error) {
	limit, err := clampSyncLimit(limit)
	if err != nil {
		return outbound.SyncPage[domain.Venta]{}, err
	}
	return s.ventas.SyncPorZona(ctx, zonaID, cursor, afterID, limit, optionalDesdeOrZero(desde))
}

// optionalDesdeOrZero unwraps a nullable desde for the sync repos, which take
// time.Time{} as the "no cutoff" sentinel. Sync no honra `ventana_dias` (la
// app móvil pasa siempre la fecha absoluta de FECHA_CARGA_INICIAL), por eso
// no reutilizamos resolveCutoff aquí — su default de 7 días no aplica.
func optionalDesdeOrZero(desde *time.Time) time.Time {
	if desde == nil {
		return time.Time{}
	}
	return *desde
}

// resolveCutoff applies the desde / ventanaDias contract used by saldos and
// pagos zone queries. Returns the zero time when the caller wants no cutoff.
func resolveCutoff(desde *time.Time, ventanaDias *int, clock outbound.Clock) (time.Time, error) {
	if desde != nil && ventanaDias != nil {
		return time.Time{}, domain.ErrParametrosExcluyentes
	}
	switch {
	case desde != nil:
		return *desde, nil
	case ventanaDias != nil:
		if *ventanaDias < 0 || *ventanaDias > MaxVentanaDias {
			return time.Time{}, domain.ErrVentanaDiasInvalida
		}
		if *ventanaDias == 0 {
			return time.Time{}, nil
		}
		return clock.Now().AddDate(0, 0, -*ventanaDias), nil
	default:
		return clock.Now().AddDate(0, 0, -DefaultVentanaDias), nil
	}
}

// clampSyncLimit applies the default / maximum limit for sync endpoints.
// Returns ErrParametrosExcluyentes when limit is negative.
func clampSyncLimit(limit int) (int, error) {
	switch {
	case limit < 0:
		return 0, domain.ErrParametrosExcluyentes
	case limit == 0:
		return DefaultSyncLimit, nil
	case limit > MaxSyncLimit:
		return MaxSyncLimit, nil
	default:
		return limit, nil
	}
}
