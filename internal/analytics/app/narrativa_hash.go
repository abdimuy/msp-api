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
// It hashes exactly the facts the narrativa is derived from: the three bands
// and the three Fase-1 quantified titulars. When any of these change, the hash
// changes and the cached narrativa is considered stale.
//
// Returns a 64-character lowercase hex string (SHA-256), matching the
// CHAR(64) column in MSP_NARRATIVAS.
func NarrativaInputHash(comp analytics.PulsoComputado) string {
	// Join the six input fields with "|". The order is fixed and deterministic;
	// none of these fields can contain "|", so there is no ambiguity.
	payload := strings.Join([]string{
		comp.BandaCredito,
		comp.BandaRecompra,
		comp.BandaCLV,
		comp.CreditoResumen,
		comp.RecompraResumen,
		comp.CLVResumen,
	}, "|")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}
