// Package app — narrativa_read.go wires the read-path narrativa serve + lazy
// enqueue into ObtenerPulsoCliente. The LIST path (ObtenerPulsosClientes) is
// never touched.
//
//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"
	"log/slog"

	"github.com/abdimuy/msp-api/internal/analytics"
)

// aplicarNarrativa serves the cached narrativa for clienteID into comp when the
// cache is fresh (InputHash matches the current facts), or lazily enqueues
// generation on miss/stale when the LLM is enabled. All failures degrade
// silently — the ficha simply shows no AI reading. Never returns an error.
func (s *Service) aplicarNarrativa(ctx context.Context, clienteID int, comp *analytics.PulsoComputado) {
	if s.narrativaRepo == nil {
		return
	}

	// The note is part of the invalidation key: editing it in Microsip regenerates
	// the narrativa on the next view. A note read failure degrades to "".
	nota := s.notaCliente(ctx, clienteID)
	hash := NarrativaInputHash(*comp, nota)

	row, err := s.narrativaRepo.GetNarrativa(ctx, clienteID)
	if err != nil {
		s.logger.WarnContext(ctx, "analytics.narrativa_get_failed",
			slog.Int("cliente_id", clienteID),
			slog.String("error", err.Error()),
		)
		return
	}

	if row != nil && row.InputHash == hash {
		// Fresh hit: serve the cached reading. When row is a negative-cache
		// fallback (Texto=="" and Rasgos empty), comp fields stay effectively
		// empty — no IA section, no re-enqueue.
		comp.Narrativa = row.Texto
		comp.RasgosIA = etiquetasDe(row.Rasgos)
		comp.ContextoOperativo = row.ContextoOperativo
		return
	}

	// Miss or stale: lazily enqueue when LLM is enabled.
	if s.llmEnabled {
		if err := s.narrativaRepo.Encolar(ctx, clienteID, hash); err != nil {
			s.logger.WarnContext(ctx, "analytics.narrativa_encolar_failed",
				slog.Int("cliente_id", clienteID),
				slog.String("error", err.Error()),
			)
		}
	}
}

// etiquetasDe resolves validated trait codes to their Spanish display labels,
// dropping any code no longer in the catalog. Returns nil for empty input.
func etiquetasDe(codes []string) []string {
	if len(codes) == 0 {
		return nil
	}
	out := make([]string, 0, len(codes))
	for _, code := range codes {
		label := EtiquetaDe(code)
		if label != "" {
			out = append(out, label)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
