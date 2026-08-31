//nolint:misspell // Spanish domain vocabulary by project convention.
package analyticsfb

// Encoding contract for the legacy Microsip text columns in the ancla read path.
//
// THE PREMISE THIS FILE USED TO STATE WAS WRONG, and it mattered.
//
// The old comment claimed "Firebird server-side transliterates CHARACTER SET
// NONE columns to UTF-8 before sending bytes on the wire". It does not. NONE
// means "no character set"; the server has nothing to transliterate FROM, so it
// ships the stored bytes verbatim. Acting on the false premise would push every
// NONE column onto a plain scan target and hand Go invalid UTF-8 — which is the
// opposite defect from the one this file was written to prevent.
//
// What is actually true, measured against the live DB over a charset=UTF8
// connection (FB_CHARSET=UTF8 in every environment):
//
//	ARTICULOS.NOMBRE    ISO8859_1  plain → "NIÑO"        Win1252 → "NIÃ‘O"
//	CLIENTES.NOMBRE     ISO8859_1  plain → "FARIÑO"      Win1252 → "FARIÃ‘O"
//	DIRS_CLIENTES.CALLE NONE       plain → invalid UTF-8 Win1252 → "SEÑORES"
//	CONCEPTOS_CC.NOMBRE NONE       plain → invalid UTF-8 Win1252 → "Interés"
//
// So the rule is per-column, and the charset is not derivable from the name:
// CIUDADES.NOMBRE is ISO8859_1 while ZONAS_CLIENTES.NOMBRE is NONE.
//
//	ISO8859_1 / UTF8 → Firebird transliterates → scan as string / sql.NullString.
//	                   A firebird.Win1252 target decodes a SECOND time → mojibake.
//	NONE             → Firebird transliterates nothing → scan as firebird.Win1252.
//	                   A plain target yields raw Windows-1252 bytes in a Go string.
//
// anclaRowRaw follows that split: CLIENTES.NOMBRE and ARTICULOS.NOMBRE (both
// ISO8859_1) are plain; ZONAS_CLIENTES.NOMBRE and DIRS_CLIENTES.TELEFONO1 (both
// NONE) go through firebird.Win1252.
//
// The two tests below pin the two halves of that rule. The classification
// itself — which column is in which family — is pinned by
// TestCatalogoCharsets_ClasificacionDeColumnas in internal/platform/fbcharset,
// which reads RDB$FIELDS instead of trusting a comment like this one.

import (
	"testing"
	"unicode/utf8"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// TestWin1252DoubleDecodeProducedMojibake proves the ISO8859_1 half of the rule:
// applying Win1252.Scan to the UTF-8 bytes Firebird delivers for an ISO8859_1
// column yields mojibake, which is why anclaRowRaw scans CLIENTES.NOMBRE plain.
func TestWin1252DoubleDecodeProducedMojibake(t *testing.T) {
	t.Parallel()

	// "MUÑOZ" as UTF-8 bytes — what Firebird delivers on the wire for an
	// ISO8859_1 column (e.g. CLIENTES.NOMBRE) when the connection is UTF8.
	// Ñ = U+00D1 → UTF-8 bytes: 0xC3 0x91
	utfBytes := []byte("MU\xC3\x91OZ")

	// Applying Win1252.Scan on these already-UTF-8 bytes decodes each byte as
	// Windows-1252: 0xC3 = 'Ã', 0x91 = '‘' (left single quotation mark).
	// Result: "MUÃ‘OZ" — the classic mojibake.
	var w firebird.Win1252
	if err := w.Scan(utfBytes); err != nil {
		t.Fatalf("Win1252.Scan unexpected error: %v", err)
	}
	got := string(w)
	if got == "MUÑOZ" {
		t.Fatal("Win1252.Scan on UTF-8 bytes should NOT produce correct output — test premise is wrong")
	}
	// Confirm the mojibake is present (C3 decoded as Windows-1252 'Ã').
	if len(got) == 0 || got[0:2] != "MU" {
		t.Fatalf("unexpected Win1252 decode result: %q", got)
	}
	// The 3rd character must be 'Ã' (U+00C3), not 'Ñ' (U+00D1).
	runes := []rune(got)
	if len(runes) < 3 || runes[2] == 'Ñ' {
		t.Fatalf("expected mojibake at position 2, got %q (rune U+%04X)", string(runes[2:3]), runes[2])
	}

	// The correct approach for an ISO8859_1 column: scan as a plain string.
	// The driver delivers UTF-8 bytes, which a Go string stores verbatim.
	correctResult := string(utfBytes)
	if correctResult != "MUÑOZ" {
		t.Fatalf("plain string scan should give correct UTF-8: got %q", correctResult)
	}
}

// TestPlainScanOnNoneColumnYieldsInvalidUTF8 proves the OTHER half of the rule —
// the half the old premise denied. A CHARACTER SET NONE column is not
// transliterated, so the driver hands Go the stored Windows-1252 bytes. Scanning
// those into a plain string produces invalid UTF-8; only firebird.Win1252
// recovers the text. This is why anclaRowRaw keeps ZONAS_CLIENTES.NOMBRE and
// DIRS_CLIENTES.TELEFONO1 on a Win1252 target.
func TestPlainScanOnNoneColumnYieldsInvalidUTF8(t *testing.T) {
	t.Parallel()

	// "SEÑORES" as Windows-1252 bytes — what Firebird delivers verbatim for a
	// CHARACTER SET NONE column such as DIRS_CLIENTES.NOMBRE_CALLE.
	// Ñ = 0xD1 in Windows-1252, a single byte.
	rawBytes := []byte("SE\xD1ORES")

	// A plain string target keeps those bytes as-is: not valid UTF-8.
	plain := string(rawBytes)
	if utf8.ValidString(plain) {
		t.Fatalf("premise wrong: raw Windows-1252 bytes should not be valid UTF-8, got %q", plain)
	}
	if plain == "SEÑORES" {
		t.Fatal("a plain scan of a NONE column must NOT already be the correct text")
	}

	// firebird.Win1252 is the correct target for a NONE column.
	var w firebird.Win1252
	if err := w.Scan(rawBytes); err != nil {
		t.Fatalf("Win1252.Scan unexpected error: %v", err)
	}
	if got := string(w); got != "SEÑORES" {
		t.Fatalf("Win1252.Scan on NONE-column bytes = %q, want %q", got, "SEÑORES")
	}
}
