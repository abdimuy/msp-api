//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// asegurarContextoNota lazily (re-)distills the cobrador's free-text note for
// conv's cliente into conv's cached ContextoNota/Banderas, ONLY when the note
// has actually changed since the last distillation (NotaHash mismatch) —
// avoiding an LLM call on every single inbound turn.
//
// This is best-effort and NEVER fails the caller: every failure path
// (NotaReader error, empty nota, LLM error) logs and returns so a nota/LLM
// problem never fails the whole ProcesarMensajeEntrante flow — the copiloto
// simply proceeds without fresh nota context. An LLM error deliberately does
// NOT update NotaHash, so the NEXT inbound turn retries the distillation
// instead of silently giving up forever (e.g. once LLM_ENABLED flips back on).
//
// conv is mutated in place; the caller is responsible for persisting it
// (Upsert) inside its own transaction. now is threaded in by the caller
// (rather than read via s.clock.Now() here) so the whole
// ProcesarMensajeEntrante call uses a single point-in-time snapshot instead
// of two independent clock reads.
func (s *Service) asegurarContextoNota(ctx context.Context, conv *domain.Conversacion, nombre, segmento string, now time.Time) {
	clienteID := conv.ClienteID()

	nota, err := s.notaReader.GetNotaCliente(ctx, clienteID)
	if err != nil {
		s.logger.WarnContext(ctx, "reactivacion_copiloto.nota_reader_failed",
			slog.Int("cliente_id", clienteID), slog.String("error", err.Error()))
		return
	}
	if nota == "" {
		return
	}

	hash := hashNota(nota)
	if hash == conv.NotaHash() {
		// Lazy cache hit — the note has not changed since the last distillation.
		return
	}

	out, err := s.copilotoLLM.DestilarNota(ctx, outbound.NotaInput{Nota: nota, Nombre: nombre, Segmento: segmento})
	if err != nil {
		s.logger.WarnContext(ctx, "reactivacion_copiloto.destilar_nota_failed",
			slog.Int("cliente_id", clienteID), slog.String("error", err.Error()))
		return
	}

	conv.SetContextoNota(out.Contexto, out.Banderas, hash, now)
}

// hashNota returns the SHA-256 hex digest of nota — the invalidation key
// asegurarContextoNota compares against Conversacion.NotaHash to detect a
// stale distillation cache. Calques
// internal/analytics/app/narrativa_hash.go's NarrativaInputHash pattern.
func hashNota(nota string) string {
	sum := sha256.Sum256([]byte(nota))
	return hex.EncodeToString(sum[:])
}
