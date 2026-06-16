//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"
	"encoding/binary"
	"errors"
	"hash/fnv"
	"time"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// Job name constants for the refresh state rows.
const (
	jobIncremental = "winback_incr"
	jobFull        = "winback_full"
)

// RefrescarResult summarises the outcome of a [Service.RefrescarCandidatos] run.
type RefrescarResult struct {
	// Procesados is the number of candidatos upserted in this run.
	Procesados int
	// Watermark is the timestamp used as the new watermark (= clock.Now() at
	// the start of the run).
	Watermark time.Time
}

// RefrescarCandidatos refreshes the winback candidatos projection from Microsip
// anchor facts.
//
// When full is true, ALL clients are read from Microsip (full rebuild).
// When full is false, only clients updated since the last incremental watermark
// are read; if no watermark exists (first run) it falls back to a full read.
//
// Decision: if domain.CrearWinbackCandidato returns an error for an ancla, the
// whole run is aborted and the error is surfaced. A bad ancla indicates a
// data/logic bug worth surfacing at run time rather than silently skipping.
//
// Single-transaction guarantee: UpsertCandidatos and SaveRefreshState are
// executed inside a single runInTx call. If either fails, neither is committed
// (the watermark is never advanced on partial failure).
//
// en_control is NEVER overwritten: for existing clients the flag is carried
// forward from ExistingControlFlags; for new clients it is assigned via
// deterministicControl.
func (s *Service) RefrescarCandidatos(ctx context.Context, full bool) (RefrescarResult, error) {
	const source = "analytics.RefrescarCandidatos"

	now := s.clock.Now()

	job, since, err := s.resolveJobAndSince(ctx, full, source)
	if err != nil {
		return RefrescarResult{}, err
	}

	anclas, err := s.micro.LeerAnclasDesde(ctx, since)
	if err != nil {
		return RefrescarResult{}, apperror.NewInternal("microsip_anclas_failed", "error al leer anclas de microsip").
			WithSource(source).WithError(err)
	}

	existing, err := s.repo.ExistingControlFlags(ctx)
	if err != nil {
		return RefrescarResult{}, apperror.NewInternal("control_flags_failed", "error al leer flags de control existentes").
			WithSource(source).WithError(err)
	}

	candidatos, err := buildCandidatos(anclas, existing, now, source)
	if err != nil {
		return RefrescarResult{}, err
	}

	if err := s.persistRefresh(ctx, job, now, candidatos, source); err != nil {
		return RefrescarResult{}, err
	}

	return RefrescarResult{
		Procesados: len(candidatos),
		Watermark:  now,
	}, nil
}

// resolveJobAndSince returns the job name and incremental since pointer for
// the upcoming refresh run.
func (s *Service) resolveJobAndSince(ctx context.Context, full bool, source string) (string, *time.Time, error) {
	if full {
		return jobFull, nil, nil
	}

	st, err := s.repo.GetRefreshState(ctx, jobIncremental)
	if err != nil {
		if !errors.Is(err, domain.ErrRefreshStateNotFound) {
			return "", nil, apperror.NewInternal("refresh_state_get_failed", "error al leer el estado de refresco").
				WithSource(source).WithError(err)
		}
		// First incremental run — treat as full read (since = nil).
		return jobIncremental, nil, nil
	}

	return jobIncremental, st.LastWatermark, nil
}

// buildCandidatos constructs domain.WinbackCandidato entities from Microsip
// anchor facts, merging the existing control-group assignments.
func buildCandidatos(
	anclas []outbound.AnclaCliente,
	existing map[int]bool,
	now time.Time,
	source string,
) ([]*domain.WinbackCandidato, error) {
	candidatos := make([]*domain.WinbackCandidato, 0, len(anclas))
	for _, a := range anclas {
		enControl := deterministicControl(a.ClienteID)
		if flag, ok := existing[a.ClienteID]; ok {
			// Existing client: ALWAYS preserve the stored flag.
			enControl = flag
		}

		c, err := domain.CrearWinbackCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         a.ClienteID,
			Nombre:            a.Nombre,
			Zona:              a.Zona,
			Telefono:          a.Telefono,
			FechaUltimaCompra: a.FechaUltimaCompra,
			Frecuencia:        a.Frecuencia,
			Monetary:          a.Monetary,
			Saldo:             a.Saldo,
			PorLiquidarPct:    a.PorLiquidarPct,
			NextBestProduct:   a.NextBestProduct,
			FechaUltimoPago:   a.FechaUltimoPago,
			NumPagos:          a.NumPagos,
			CadenciaDias:      a.CadenciaDias,
			DiasAtrasoProm:    a.DiasAtrasoProm,
			PctPagosATiempo:   a.PctPagosATiempo,
			FechaProxPago:     a.FechaProxPago,
			MontoProxPago:     a.MontoProxPago,
			EnControl:         enControl,
			CohorteFecha:      now,
			Now:               now,
		})
		if err != nil {
			// Fail fast: a bad ancla is a data/logic bug worth surfacing.
			return nil, apperror.NewInternal("candidato_create_failed", "error al construir candidato winback").
				WithSource(source).WithError(err)
		}
		candidatos = append(candidatos, c)
	}
	return candidatos, nil
}

// persistRefresh writes UpsertCandidatos and SaveRefreshState atomically in a
// single transaction. The order is: upsert first, then advance the watermark.
func (s *Service) persistRefresh(
	ctx context.Context,
	job string,
	now time.Time,
	candidatos []*domain.WinbackCandidato,
	source string,
) error {
	return s.runInTx(ctx, func(ctx context.Context) error {
		if err := s.repo.UpsertCandidatos(ctx, candidatos); err != nil {
			return apperror.NewInternal("candidatos_upsert_failed", "error al guardar candidatos winback").
				WithSource(source).WithError(err)
		}
		newWatermark := now
		if err := s.repo.SaveRefreshState(ctx, outbound.RefreshState{
			Job:           job,
			LastWatermark: &newWatermark,
			LastRunAt:     now,
		}); err != nil {
			return apperror.NewInternal("refresh_state_save_failed", "error al guardar el estado de refresco").
				WithSource(source).WithError(err)
		}
		return nil
	})
}

// deterministicControl returns true when clienteID belongs to the control
// group (15% of clients).
//
// Uses FNV-32a which is stable across process restarts and Go versions — unlike
// Go's built-in map hash (randomly seeded per process). The result is
// deterministic: the same clienteID always maps to the same group.
//
// Note: hash/fnv is safe for this purpose (deterministic group assignment), NOT
// for cryptographic use.
func deterministicControl(clienteID int) bool {
	h := fnv.New32a()
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(clienteID)) //nolint:gosec // clienteID is always >= 0
	_, _ = h.Write(b)
	return h.Sum32()%100 < 15
}
