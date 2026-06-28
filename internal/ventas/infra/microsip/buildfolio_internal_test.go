//nolint:misspell // Spanish vocabulary (serie, folio, caja) by convention.
package microsip

import "testing"

// TestBuildFolio verifies the FOLIO always fits Microsip's DOCTOS_PV.FOLIO
// CHAR(9): the consecutivo is zero-padded to (9 - len(serie)) digits so the
// total is exactly 9. Multi-char series (2 and 3 chars) are common across
// cajas; the old fixed %08d overflowed CHAR(9) for them (-303).
func TestBuildFolio(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		serie       string
		consecutivo int
		want        string
	}{
		{"serie 1 char", "Y", 2262, "Y00002262"},
		{"serie 1 char min", "A", 1, "A00000001"},
		{"serie 2 chars", "AI", 2412, "AI0002412"},
		{"serie 2 chars other", "AA", 2046, "AA0002046"},
		{"serie 3 chars", "ABC", 1, "ABC000001"},
		{"serie con espacios", " AI ", 2412, "AI0002412"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildFolio(tc.serie, tc.consecutivo)
			if got != tc.want {
				t.Errorf("buildFolio(%q, %d) = %q, want %q", tc.serie, tc.consecutivo, got, tc.want)
			}
			if len(got) > folioMaxLen {
				t.Errorf("buildFolio(%q, %d) = %q (len %d) exceeds DOCTOS_PV.FOLIO CHAR(%d)",
					tc.serie, tc.consecutivo, got, len(got), folioMaxLen)
			}
		})
	}
}
