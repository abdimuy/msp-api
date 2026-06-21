//nolint:misspell // Spanish domain vocabulary by project convention.
package analyticsfb

// TestAnclaEncodingContract documents the encoding rule for legacy Microsip text
// columns (CLIENTES.NOMBRE, ZONAS_CLIENTES.NOMBRE, DIRS_CLIENTES.TELEFONO1,
// ARTICULOS.NOMBRE) in the ancla read path.
//
// Root cause of the mojibake bug fixed in this commit:
//
//  1. The Firebird connection uses charset=UTF8.
//  2. Firebird server-side transliterates CHARACTER SET NONE columns to UTF-8
//     before sending bytes on the wire.
//  3. The driver delivers UTF-8 bytes to Go.
//  4. If firebird.Win1252.Scan() is used as the scan target it applies a second
//     Windows-1252→UTF-8 decode on those already-UTF-8 bytes → double-encoding
//     mojibake (e.g. ñ U+00F1 → "Ã'" U+00C3 U+2019).
//
// Fix: scan Microsip text columns as plain string / sql.NullString.
// The server-side transliteration is sufficient; no Go-side decode is needed.

import (
	"testing"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// TestWin1252DoubleDecodeProducedMojibake proves that applying Win1252.Scan on
// already-UTF-8 bytes (i.e. what the driver delivers when connection=UTF8) yields
// mojibake — confirming why the fix removes Win1252 scan targets from anclaRowRaw.
func TestWin1252DoubleDecodeProducedMojibake(t *testing.T) {
	t.Parallel()

	// "MUÑOZ" as UTF-8 bytes — this is what Firebird delivers on the wire when
	// the connection charset is UTF8 (server transliterates NONE→UTF8).
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

	// The correct approach: scan as plain string (Go string is UTF-8).
	// The driver delivers UTF-8 bytes, which Go string stores verbatim.
	correctResult := string(utfBytes)
	if correctResult != "MUÑOZ" {
		t.Fatalf("plain string scan should give correct UTF-8: got %q", correctResult)
	}
}
