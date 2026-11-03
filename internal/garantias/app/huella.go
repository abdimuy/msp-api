package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
)

// huellaDe returns the fingerprint of a command: the SHA-256, in lowercase
// hex, of the command's type name, a NUL separator and the JSON of the command
// with ClaveIdempotencia blanked. Every other field counts, including the ids
// that come from the route, the device timestamp and the GPS, so a key reused
// with different content is a mismatch instead of a silent "already done".
//
// The key is blanked on a copy: the caller's command is never mutated. A
// non-struct value, a nil pointer or a marshal failure yields the empty string,
// which never matches a stored fingerprint, so the check fails closed.
func huellaDe(cmd any) string {
	v := reflect.ValueOf(cmd)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	copia := reflect.New(v.Type()).Elem()
	copia.Set(v)
	if campo := copia.FieldByName("ClaveIdempotencia"); campo.IsValid() &&
		campo.Kind() == reflect.String && campo.CanSet() {
		campo.SetString("")
	}
	contenido, err := json.Marshal(copia.Interface())
	if err != nil {
		return ""
	}
	h := sha256.New()
	// hash.Hash never returns an error from Write; it is here to keep the
	// hash explicit instead of concatenating a string.
	_, _ = h.Write([]byte(v.Type().Name()))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(contenido)
	return hex.EncodeToString(h.Sum(nil))
}
