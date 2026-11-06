package domain

import "github.com/google/uuid"

// FirmaImagenID returns the image id the delivery signature must use. It is
// derived from the command's idempotency key so a retry of the same Entregar
// lands on the same primary key instead of creating a second signature row.
func FirmaImagenID(clave string) uuid.UUID {
	return uuid.MustParse(claveDerivada(clave, "firma"))
}
