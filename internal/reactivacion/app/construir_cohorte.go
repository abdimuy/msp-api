//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"context"
	"encoding/binary"
	"hash/fnv"
	"log/slog"
	"time"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// ConstruirResult summarises the outcome of a [Service.ConstruirCohorte] run.
type ConstruirResult struct {
	// Procesados is the number of cohorte rows upserted in this run.
	Procesados int
	// CohorteFecha is the timestamp assigned to newly enrolled clientes
	// (= clock.Now() at the start of the run).
	CohorteFecha time.Time
}

// ConstruirCohorte (re)builds the MSP_RX_COHORTE snapshot from the Tehuacán
// tratable universe.
//
// The control assignment, contact flag and cohort date of clientes already in
// the table are ALWAYS preserved: EN_CONTROL and FUE_CONTACTADO are carried
// forward from ExistingControlFlags / ExistingContactadoFlags; COHORTE_FECHA is
// preserved by the upsert (its UPDATE branch omits the column). New clientes get
// a deterministic control assignment and the current time as their cohort date.
//
// Single-transaction guarantee: UpsertCohorte runs inside a single runInTx call.
//
// Decision: if domain.CrearCohorteCliente returns an error for a universe row,
// the whole run is aborted and the error is surfaced — a bad row indicates a
// data/logic bug worth surfacing at run time rather than silently skipping.
func (s *Service) ConstruirCohorte(ctx context.Context) (ConstruirResult, error) {
	const source = "reactivacion.ConstruirCohorte"

	now := s.clock.Now()

	universo, err := s.reader.LeerUniversoTehuacan(ctx)
	if err != nil {
		return ConstruirResult{}, apperror.NewInternal("universo_read_failed", "error al leer el universo de tehuacán").
			WithSource(source).WithError(err)
	}

	existingControl, err := s.repo.ExistingControlFlags(ctx)
	if err != nil {
		return ConstruirResult{}, apperror.NewInternal("control_flags_failed", "error al leer flags de control existentes").
			WithSource(source).WithError(err)
	}

	existingContactado, err := s.repo.ExistingContactadoFlags(ctx)
	if err != nil {
		return ConstruirResult{}, apperror.NewInternal("contactado_flags_failed", "error al leer flags de contacto existentes").
			WithSource(source).WithError(err)
	}

	cohorte, err := s.buildCohorte(universo, existingControl, existingContactado, now, source)
	if err != nil {
		return ConstruirResult{}, err
	}

	if err := s.runInTx(ctx, func(ctx context.Context) error {
		if uerr := s.repo.UpsertCohorte(ctx, cohorte); uerr != nil {
			return apperror.NewInternal("cohorte_upsert_failed", "error al guardar la cohorte de reactivación").
				WithSource(source).WithError(uerr)
		}
		return nil
	}); err != nil {
		return ConstruirResult{}, err
	}

	return ConstruirResult{Procesados: len(cohorte), CohorteFecha: now}, nil
}

// buildCohorte constructs domain.CohorteCliente entities from the universe,
// merging the existing control and contact assignments.
func (s *Service) buildCohorte(
	universo []outbound.ClienteUniverso,
	existingControl, existingContactado map[int]bool,
	now time.Time,
	source string,
) ([]*domain.CohorteCliente, error) {
	pct := s.cfg.controlPct()
	cohorte := make([]*domain.CohorteCliente, 0, len(universo))
	for _, u := range universo {
		enControl := deterministicControl(u.ClienteID, pct)
		if flag, ok := existingControl[u.ClienteID]; ok {
			// Existing cliente: ALWAYS preserve the stored A/B flag.
			enControl = flag
		}
		fueContactado := existingContactado[u.ClienteID] // false when absent

		c, err := domain.CrearCohorteCliente(domain.CrearCohorteClienteParams{
			ClienteID:             u.ClienteID,
			Nombre:                u.Nombre,
			Telefono:              u.Telefono,
			Segmento:              u.Segmento,
			EnControl:             enControl,
			FueContactado:         fueContactado,
			CohorteFecha:          now,
			FechaUltimaCompraBase: u.FechaUltimaCompra,
			Saldo:                 u.Saldo,
			PorLiquidarPct:        u.PorLiquidarPct,
			Now:                   now,
		})
		if err != nil {
			return nil, apperror.NewInternal("cohorte_create_failed", "error al construir la cohorte de reactivación").
				WithSource(source).WithError(err)
		}
		cohorte = append(cohorte, c)
	}
	return cohorte, nil
}

// ConstruirEnSegundoPlano launches ConstruirCohorte in a detached goroutine so
// the HTTP handler can return 202 immediately without waiting for the full
// rebuild.
//
// Single-flight guard: if a build is already running, the method returns false
// immediately without starting a second goroutine. The goroutine runs with
// context.Background() so a client disconnect does not cancel the work mid-write.
func (s *Service) ConstruirEnSegundoPlano() bool {
	if !s.construirRunning.CompareAndSwap(false, true) {
		return false
	}

	go func() {
		defer s.construirRunning.Store(false)

		ctx := context.Background()
		s.logger.InfoContext(ctx, "reactivacion_cohorte.background_start")

		result, err := s.ConstruirCohorte(ctx)
		if err != nil {
			s.logger.ErrorContext(
				ctx, "reactivacion_cohorte.background_failed",
				slog.String("error", err.Error()),
			)
			return
		}

		s.logger.InfoContext(
			ctx, "reactivacion_cohorte.background_done",
			slog.Int("procesados", result.Procesados),
			slog.Time("cohorte_fecha", result.CohorteFecha),
		)
	}()

	return true
}

// deterministicControl returns true when clienteID belongs to the control group
// (pct% of clientes).
//
// Uses FNV-32a which is stable across process restarts and Go versions — unlike
// Go's built-in map hash (randomly seeded per process). The result is
// deterministic: the same clienteID always maps to the same group.
//
// Note: hash/fnv is safe for this purpose (deterministic group assignment), NOT
// for cryptographic use.
func deterministicControl(clienteID, pct int) bool {
	h := fnv.New32a()
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(clienteID)) //nolint:gosec // clienteID is always >= 0
	_, _ = h.Write(b)
	return int(h.Sum32()%100) < pct
}
