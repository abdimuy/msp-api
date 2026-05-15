package firebird

// Win1252 encoding boundary between Go's UTF-8 strings and the Windows-1252
// bytes that Microsip's Firebird DB expects.
//
// Microsip stores text in DB columns whose CHARACTER SET is NONE (raw bytes)
// or ISO8859_1, and its Delphi/Windows client writes those bytes already
// encoded in Windows-1252. Our Go domain layer is UTF-8 native; without this
// boundary, accented characters round-trip as broken bytes or trigger "cannot
// transliterate" errors at the Firebird wire layer.
//
// Win1252 implements driver.Valuer (Go→DB) and sql.Scanner (DB→Go),
// transparently translating between encodings. The domain layer never sees
// Win1252 — repos convert at the SQL boundary.

import (
	"database/sql/driver"
	"fmt"

	"golang.org/x/text/encoding/charmap"
)

// Win1252 wraps a UTF-8 Go string for Firebird I/O.
//
//   - Value() returns the string re-encoded as Windows-1252 bytes.
//   - Scan() reads Windows-1252 bytes from the driver and decodes to UTF-8.
//
// Characters outside Windows-1252 (e.g. emoji, Cyrillic, CJK) cause Value
// to return an error; the caller treats this as a validation failure.
//
// The type intentionally uses a non-pointer receiver for Value (driver.Valuer
// interface) and a pointer receiver for Scan (sql.Scanner interface); both are
// required by their respective interfaces.
//
//nolint:recvcheck // non-pointer Value + pointer Scan required by driver interfaces.
type Win1252 string

// Compile-time interface checks.
var (
	_ driver.Valuer                    = Win1252("")
	_ interface{ Scan(src any) error } = (*Win1252)(nil)
)

// Value encodes the UTF-8 string to Windows-1252 bytes for the Firebird driver.
//
// Returning []byte (not string) is critical: the firebirdsql driver renders
// string values as UTF-8 bytes on the wire regardless of the connection
// charset. Returning []byte forces the driver to send the payload verbatim,
// which Firebird then interprets according to the FB_CHARSET=WIN1252
// connection declaration.
//
// An empty string returns ([]byte(nil), nil) — treated as an empty column
// value, not NULL.
func (w Win1252) Value() (driver.Value, error) {
	if w == "" {
		return []byte(nil), nil
	}
	enc, err := charmap.Windows1252.NewEncoder().Bytes([]byte(string(w)))
	if err != nil {
		return nil, fmt.Errorf("firebird: cannot encode %q to windows-1252: %w", string(w), err)
	}
	return enc, nil
}

// Scan accepts []byte or string from the Firebird driver and decodes
// Windows-1252 bytes to UTF-8, storing the result in *w.
//
// A nil src sets *w to an empty string with no error. An unexpected type
// returns an error with the actual type in the message.
func (w *Win1252) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*w = ""
		return nil
	case []byte:
		return w.decodeBytes(v)
	case string:
		return w.decodeBytes([]byte(v))
	default:
		//nolint:err113 // dynamic %T format is intentional for diagnostic clarity.
		return fmt.Errorf("firebird: Win1252.Scan: unsupported source type %T", src)
	}
}

// decodeBytes converts Windows-1252 encoded bytes to UTF-8 and stores the
// result. The zero-length case short-circuits to avoid an allocating decode.
func (w *Win1252) decodeBytes(b []byte) error {
	if len(b) == 0 {
		*w = ""
		return nil
	}
	decoded, err := charmap.Windows1252.NewDecoder().Bytes(b)
	if err != nil {
		return fmt.Errorf("firebird: Win1252.Scan: cannot decode windows-1252 bytes: %w", err)
	}
	*w = Win1252(decoded)
	return nil
}

// EncodeWin1252 is the explicit form callers use when building arg slices for
// sql.Exec. It is equivalent to Win1252(s).Value().
func EncodeWin1252(s string) (driver.Value, error) {
	return Win1252(s).Value()
}

// EncodeWin1252Ptr returns driver.Value(nil) when s is nil; otherwise it
// encodes the pointed-to string. Used for nullable string columns.
func EncodeWin1252Ptr(s *string) (driver.Value, error) {
	if s == nil {
		return nil, nil //nolint:nilnil // SQL NULL is represented as (nil, nil) by convention.
	}
	return Win1252(*s).Value()
}

// MustEncodeWin1252 is the panic-on-error form used in tests. Production
// callers always use EncodeWin1252.
func MustEncodeWin1252(s string) []byte {
	v, err := Win1252(s).Value()
	if err != nil {
		panic(fmt.Sprintf("firebird: MustEncodeWin1252(%q): %v", s, err))
	}
	if v == nil {
		return []byte(nil)
	}
	b, ok := v.([]byte)
	if !ok {
		panic(fmt.Sprintf("firebird: MustEncodeWin1252(%q): unexpected type %T", s, v))
	}
	return b
}
