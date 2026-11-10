package app

import (
	"testing"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestFirmaIDDe_ClaveInvalidaEsNil pins the sentinel of the signature id: a key
// that is not a UUID cannot identify an image row, so firmaIDDe must answer
// uuid.Nil instead of inventing an id. The caller stores the blob anyway and the
// cleanup then deletes it; a random id here would be indistinguishable from a
// real row to any later reader. Mutante T10: devolver uuid.New().
func TestFirmaIDDe_ClaveInvalidaEsNil(t *testing.T) {
	t.Parallel()
	for _, clave := range []string{"", "   ", "no-es-un-uuid", "{no-es-un-uuid}"} {
		t.Run("clave="+clave, func(t *testing.T) {
			t.Parallel()
			if got := firmaIDDe(clave); got != uuid.Nil {
				t.Fatalf("firmaIDDe(%q) = %s, want uuid.Nil", clave, got)
			}
		})
	}
}

// TestFirmaIDDe_ClaveValidaDerivaLaImagen keeps the other half honest: a valid
// key still maps to the id the domain derives, so the sentinel did not replace
// the normal path.
func TestFirmaIDDe_ClaveValidaDerivaLaImagen(t *testing.T) {
	t.Parallel()
	clave := uuid.NewString()

	if got, want := firmaIDDe(clave), domain.FirmaImagenID(clave); got != want {
		t.Fatalf("firmaIDDe(%q) = %s, want %s", clave, got, want)
	}
}
