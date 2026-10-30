package domain

import "testing"

// TestClaveDerivada_NormalizaNFC pins the NFC rule on the two pieces that feed
// the UUIDv5: the same retry must mint the same derived key whether its text
// arrives composed or decomposed. For real callers both pieces are canonical
// UUIDs, so this is the boundary that keeps the guarantee honest.
func TestClaveDerivada_NormalizaNFC(t *testing.T) {
	t.Parallel()
	compuesta := claveDerivada("jos\u00e9", "art\u00edculo/reemplazo")
	descompuesta := claveDerivada("jose\u0301", "arti\u0301culo/reemplazo")
	if compuesta != descompuesta {
		t.Errorf("claveDerivada NFD = %q, want %q", descompuesta, compuesta)
	}
}

// TestClaveDerivada_DistingueElDiscriminador keeps the previous test from
// passing trivially: NFC must not collapse two different inputs into one key.
func TestClaveDerivada_DistingueElDiscriminador(t *testing.T) {
	t.Parallel()
	clave := "6f6b0a7c-1f2e-5a3d-9b4c-7d8e9f0a1b2c"
	if claveDerivada(clave, "articulo-a") == claveDerivada(clave, "articulo-b") {
		t.Error("claves derivadas iguales para discriminadores distintos")
	}
}
