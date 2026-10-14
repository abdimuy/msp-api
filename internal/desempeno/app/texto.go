//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// normalizar quita acentos y mayúsculas para buscar "sin acentos ni
// mayúsculas" (contrato §4 `q`): "MÉNDEZ" y "mendez" son iguales.
func normalizar(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		_, _ = b.WriteRune(unicode.ToLower(r))
	}
	return strings.TrimSpace(b.String())
}
