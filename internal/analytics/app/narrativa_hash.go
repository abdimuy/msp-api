// Package app — narrativa_hash.go provides the cache-invalidation key for a
// client's materialized narrativa row.
//
//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/abdimuy/msp-api/internal/analytics"
)

// NarrativaInputHash is the invalidation key for a client's cached narrativa.
// It hashes exactly the facts the narrativa is derived from: the three bands,
// the three Fase-1 quantified titulars, and the cobrador's free-text note. When
// any of these change (including an edit to the note), the hash changes and the
// cached narrativa is considered stale.
//
// Returns a 64-character lowercase hex string (SHA-256), matching the
// CHAR(64) column in MSP_NARRATIVAS.
func NarrativaInputHash(comp analytics.PulsoComputado, nota string) string {
	// Join the input fields with "|". The order is fixed and deterministic. The
	// note is last, so even if it contains "|" the mapping (comp, nota) → hash
	// stays deterministic: same input → same hash, changed note → changed hash.
	payload := strings.Join([]string{
		comp.BandaCredito,
		comp.BandaRecompra,
		comp.BandaCLV,
		comp.CreditoResumen,
		comp.RecompraResumen,
		comp.CLVResumen,
		nota,
	}, "|")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}
