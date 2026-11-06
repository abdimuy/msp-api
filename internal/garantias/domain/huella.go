package domain

// largoHuella is the width of MSP_GA_EVENTO.HUELLA and MSP_GA_IMAGEN.HUELLA:
// the lowercase hex of a SHA-256 (migración 000071).
const largoHuella = 64

// huellaValida reports whether h fits the HUELLA column. The fingerprint is
// computed in app from the full content of the command (everything except the
// idempotency key itself); the domain does not know how, it only refuses a
// value that the column would truncate or that no SHA-256 could produce. Empty
// is accepted: a row without a fingerprint never matches a replay, so it can
// only fail closed (409), never answer "ya lo hice" for different content.
func huellaValida(h string) bool {
	if h == "" {
		return true
	}
	if len(h) != largoHuella {
		return false
	}
	for i := range len(h) {
		c := h[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
