//nolint:misspell // Spanish domain vocabulary by project convention.
package clientesfb

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	minPageSize     = 10
	maxPageSize     = 200
	defaultPageSize = 50
)

// clampPageSize returns a page size within [minPageSize, maxPageSize].
// Zero or negative values return defaultPageSize.
func clampPageSize(requested int) int {
	if requested <= 0 {
		return defaultPageSize
	}
	if requested < minPageSize {
		return minPageSize
	}
	if requested > maxPageSize {
		return maxPageSize
	}
	return requested
}

// ─── Directory cursor (nombre, clienteID) ─────────────────────────────────────

// encodeCursorDir encodes a (nombre, clienteID) directory cursor as a
// base64url-encoded "nombre\x00clienteID" string. Callers supply the values
// from the LAST row of the current page.
func encodeCursorDir(nombre string, clienteID int) string {
	raw := nombre + "\x00" + strconv.Itoa(clienteID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursorDir decodes a directory cursor back to (nombre, clienteID).
// Returns errInvalidCursor on malformed input so the caller can return an
// empty page rather than a 500.
func decodeCursorDir(encoded string) (string, int, error) {
	if encoded == "" {
		return "", 0, nil
	}
	raw, decErr := base64.RawURLEncoding.DecodeString(encoded)
	if decErr != nil {
		return "", 0, errInvalidCursor
	}
	parts := strings.SplitN(string(raw), "\x00", 2)
	if len(parts) != 2 {
		return "", 0, errInvalidCursor
	}
	id, atoiErr := strconv.Atoi(parts[1])
	if atoiErr != nil {
		return "", 0, errInvalidCursor
	}
	nombre := parts[0]
	return nombre, id, nil
}

// ─── VentaCliente cursor (fecha string, doctoPVID) ────────────────────────────

// encodeCursorVentas encodes a (fecha RFC3339, doctoPVID) ventas cursor.
func encodeCursorVentas(fechaStr string, doctoPVID int) string {
	raw := fechaStr + "\x00" + strconv.Itoa(doctoPVID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursorVentas decodes a ventas cursor back to (fechaStr, doctoPVID).
func decodeCursorVentas(encoded string) (string, int, error) {
	if encoded == "" {
		return "", 0, nil
	}
	raw, decErr := base64.RawURLEncoding.DecodeString(encoded)
	if decErr != nil {
		return "", 0, errInvalidCursor
	}
	parts := strings.SplitN(string(raw), "\x00", 2)
	if len(parts) != 2 {
		return "", 0, errInvalidCursor
	}
	id, atoiErr := strconv.Atoi(parts[1])
	if atoiErr != nil {
		return "", 0, errInvalidCursor
	}
	fechaStr := parts[0]
	return fechaStr, id, nil
}

// errInvalidCursor is an internal sentinel used by the decode functions.
// It is NOT a domain apperror — it stays at the infra layer. Callers that
// receive it should treat the cursor as absent (first page) and log a warning.
var errInvalidCursor = errors.New("clientesfb: invalid cursor encoding")

// buildInPlaceholders returns a "(?,?,?)" style IN clause for n items.
// Panics if n < 1 — callers must guard.
func buildInPlaceholders(n int) string {
	if n <= 0 {
		panic(fmt.Sprintf("clientesfb: buildInPlaceholders called with n=%d", n))
	}
	return "(" + strings.TrimRight(strings.Repeat("?,", n), ",") + ")"
}
