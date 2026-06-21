//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"strings"
	"testing"
	"unicode"

	"github.com/abdimuy/msp-api/internal/analytics/app"
)

// TestEsRasgoValido verifies that known codes return true and unknown/empty codes return false.
func TestEsRasgoValido(t *testing.T) {
	t.Parallel()

	validCodes := []string{
		"loyal_but_stagnant",
		"churn_risk",
		"steady_reliable",
		"price_sensitive",
		"dormant_valuable",
	}
	for _, code := range validCodes {
		if !app.EsRasgoValido(code) {
			t.Errorf("EsRasgoValido(%q) = false, want true", code)
		}
	}

	invalidCodes := []string{
		"",
		"nonexistent",
		"MOROSO",
		"CRÍTICO",
		"loyal_but_stagnant ", // trailing space
		"LoyalButStagnant",
	}
	for _, code := range invalidCodes {
		if app.EsRasgoValido(code) {
			t.Errorf("EsRasgoValido(%q) = true, want false", code)
		}
	}
}

// TestEtiquetaDe verifies exact Spanish labels for known codes and empty string for unknown.
func TestEtiquetaDe(t *testing.T) {
	t.Parallel()

	cases := []struct {
		codigo   string
		etiqueta string
	}{
		{"loyal_but_stagnant", "Leal pero estancado"},
		{"churn_risk", "Riesgo de fuga"},
		{"steady_reliable", "Cumplido constante"},
		{"high_value_at_risk", "Alto valor en riesgo"},
		{"cash_reliable", "Contado confiable"},
		{"growing_relationship", "Relación en crecimiento"},
		{"price_sensitive", "Sensible al precio"},
		{"recoverable_with_promo", "Recuperable con promoción"},
		{"enganche_sensitive", "Sensible al enganche"},
		{"seasonal_buyer", "Comprador de temporada"},
		{"pays_in_streaks", "Paga en rachas"},
		{"dormant_valuable", "Dormido valioso"},
	}
	for _, tc := range cases {
		got := app.EtiquetaDe(tc.codigo)
		if got != tc.etiqueta {
			t.Errorf("EtiquetaDe(%q) = %q, want %q", tc.codigo, got, tc.etiqueta)
		}
	}

	unknownCodes := []string{"", "nonexistent", "MOROSO"}
	for _, code := range unknownCodes {
		got := app.EtiquetaDe(code)
		if got != "" {
			t.Errorf("EtiquetaDe(%q) = %q, want %q", code, got, "")
		}
	}
}

// TestCatalogoIntegrity verifies structural invariants of the full catalog.
func TestCatalogoIntegrity(t *testing.T) {
	t.Parallel()

	catalog := app.CatalogoRasgos

	// Exactly 12 entries.
	if len(catalog) != 12 {
		t.Fatalf("len(CatalogoRasgos) = %d, want 12", len(catalog))
	}

	seen := make(map[string]bool, len(catalog))
	for i, r := range catalog {
		// Code must be non-empty.
		if r.Codigo == "" {
			t.Errorf("entry %d: Codigo is empty", i)
		}

		// Code must be lowercase snake_case (letters, digits, underscores; no spaces or uppercase).
		if !isSnakeCase(r.Codigo) {
			t.Errorf("entry %d: Codigo %q is not lowercase snake_case", i, r.Codigo)
		}

		// Codes must be unique.
		if seen[r.Codigo] {
			t.Errorf("entry %d: duplicate Codigo %q", i, r.Codigo)
		}
		seen[r.Codigo] = true

		// Etiqueta must be non-empty.
		if strings.TrimSpace(r.Etiqueta) == "" {
			t.Errorf("entry %d (%s): Etiqueta is empty", i, r.Codigo)
		}

		// Definicion must be non-empty.
		if strings.TrimSpace(r.Definicion) == "" {
			t.Errorf("entry %d (%s): Definicion is empty", i, r.Codigo)
		}
	}
}

// isSnakeCase returns true if s consists only of lowercase letters, digits, and underscores.
func isSnakeCase(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if !unicode.IsLower(ch) && !unicode.IsDigit(ch) && ch != '_' {
			return false
		}
	}
	return true
}
