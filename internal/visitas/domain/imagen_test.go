//nolint:misspell // visitas vocabulary is Spanish per project convention.
package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

var imagenNow = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// validVisitaForImagen builds a Visita through the real constructor so the
// image tests exercise the aggregate root, not a hand-assembled struct.
func validVisitaForImagen(t *testing.T) *domain.Visita {
	t.Helper()
	v, err := domain.NewVisita(domain.CrearVisitaParams{
		ID:         uuid.New(),
		Cobrador:   "Ana Ruiz",
		ClienteID:  4321,
		Fecha:      imagenNow.Add(-2 * time.Hour),
		TipoVisita: string(domain.TipoVisitaNoSeEncontraba),
		CreatedBy:  uuid.New(),
		Now:        imagenNow,
	})
	require.NoError(t, err)
	return v
}

func validStorage(t *testing.T, key string) domain.ImagenStorage {
	t.Helper()
	s, err := domain.NewImagenStorage(domain.StorageKindFilesystem, key)
	require.NoError(t, err)
	return s
}

func ptr(s string) *string { return &s }

// ─── AdjuntarImagen: happy path ───────────────────────────────────────────────

func TestAdjuntarImagen_HappyPath(t *testing.T) {
	t.Parallel()

	v := validVisitaForImagen(t)
	imgID := uuid.New()
	by := uuid.New()

	img, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:          imgID,
		Storage:     validStorage(t, "visitas/v/i.jpg"),
		Mime:        domain.MimeJPEG,
		SizeBytes:   1024,
		Descripcion: ptr("puerta"),
		By:          by,
		Now:         imagenNow,
	})
	require.NoError(t, err)

	assert.Equal(t, imgID, img.ID())
	assert.Equal(t, "image/jpeg", img.Mime())
	assert.Equal(t, int64(1024), img.SizeBytes())
	assert.Equal(t, "visitas/v/i.jpg", img.Storage().Key())
	assert.Equal(t, domain.StorageKindFilesystem, img.Storage().Kind())
	require.NotNil(t, img.Descripcion())
	assert.Equal(t, "puerta", *img.Descripcion())

	aud := img.Audit()
	assert.Equal(t, imagenNow, aud.CreatedAt(), "the imagen DOES persist audit, unlike its parent")
	assert.Equal(t, imagenNow, aud.UpdatedAt())
	assert.Equal(t, by, aud.CreatedBy())
}

func TestAdjuntarImagen_AppendsToTheAggregateInOrder(t *testing.T) {
	t.Parallel()

	v := validVisitaForImagen(t)
	assert.Empty(t, v.Imagenes(), "a fresh visita carries no images")

	first := uuid.New()
	second := uuid.New()
	for i, id := range []uuid.UUID{first, second} {
		_, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
			ID:        id,
			Storage:   validStorage(t, "visitas/v/"+id.String()+".jpg"),
			Mime:      domain.MimeJPEG,
			SizeBytes: int64(100 * (i + 1)),
			By:        uuid.New(),
			Now:       imagenNow,
		})
		require.NoError(t, err)
	}

	imgs := v.Imagenes()
	require.Len(t, imgs, 2)
	assert.Equal(t, first, imgs[0].ID())
	assert.Equal(t, second, imgs[1].ID())
}

func TestImagenes_ReturnsACopy(t *testing.T) {
	t.Parallel()

	v := validVisitaForImagen(t)
	_, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:        uuid.New(),
		Storage:   validStorage(t, "visitas/v/a.jpg"),
		Mime:      domain.MimeJPEG,
		SizeBytes: 1,
		By:        uuid.New(),
		Now:       imagenNow,
	})
	require.NoError(t, err)

	got := v.Imagenes()
	require.Len(t, got, 1)
	got[0] = nil // mutate the copy

	assert.NotNil(t, v.Imagenes()[0], "the aggregate's own slice must be untouched")
}

func TestAdjuntarImagen_AcceptsEveryWhitelistedMime(t *testing.T) {
	t.Parallel()

	for _, mime := range []string{
		domain.MimeJPEG, domain.MimePNG, domain.MimeGIF, domain.MimeWebP, domain.MimePDF,
	} {
		t.Run(mime, func(t *testing.T) {
			t.Parallel()
			v := validVisitaForImagen(t)
			img, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
				ID:        uuid.New(),
				Storage:   validStorage(t, "visitas/v/x"),
				Mime:      mime,
				SizeBytes: 10,
				By:        uuid.New(),
				Now:       imagenNow,
			})
			require.NoError(t, err)
			assert.Equal(t, mime, img.Mime())
		})
	}
}

func TestAdjuntarImagen_LowerCasesTheMime(t *testing.T) {
	t.Parallel()

	// A phone that sends "IMAGE/JPEG" must be accepted and stored canonically
	// — the whitelist check itself lower-cases before comparing.
	v := validVisitaForImagen(t)
	img, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:        uuid.New(),
		Storage:   validStorage(t, "visitas/v/x.jpg"),
		Mime:      "IMAGE/JPEG",
		SizeBytes: 10,
		By:        uuid.New(),
		Now:       imagenNow,
	})
	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", img.Mime())
}

// ─── AdjuntarImagen: rejections ───────────────────────────────────────────────

func TestAdjuntarImagen_RejectsZeroID(t *testing.T) {
	t.Parallel()

	v := validVisitaForImagen(t)
	_, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:        uuid.Nil,
		Storage:   validStorage(t, "visitas/v/x.jpg"),
		Mime:      domain.MimeJPEG,
		SizeBytes: 1,
		By:        uuid.New(),
		Now:       imagenNow,
	})
	require.ErrorIs(t, err, domain.ErrImagenIDRequerido)
	assert.Empty(t, v.Imagenes(), "a rejected imagen must not reach the aggregate")
}

func TestAdjuntarImagen_RejectsDisallowedMime(t *testing.T) {
	t.Parallel()

	for _, mime := range []string{"", "image/bmp", "image/svg+xml", "text/html", "application/zip"} {
		t.Run(mime, func(t *testing.T) {
			t.Parallel()
			v := validVisitaForImagen(t)
			_, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
				ID:        uuid.New(),
				Storage:   validStorage(t, "visitas/v/x"),
				Mime:      mime,
				SizeBytes: 1,
				By:        uuid.New(),
				Now:       imagenNow,
			})
			require.ErrorIs(t, err, domain.ErrImagenMimeNoPermitido)
			assert.Empty(t, v.Imagenes())
		})
	}
}

func TestAdjuntarImagen_RejectsNegativeSize(t *testing.T) {
	t.Parallel()

	v := validVisitaForImagen(t)
	_, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:        uuid.New(),
		Storage:   validStorage(t, "visitas/v/x.jpg"),
		Mime:      domain.MimeJPEG,
		SizeBytes: -1,
		By:        uuid.New(),
		Now:       imagenNow,
	})
	require.ErrorIs(t, err, domain.ErrImagenSizeBytesNegativo)
}

func TestAdjuntarImagen_AcceptsZeroSize(t *testing.T) {
	t.Parallel()

	// Zero is not negative. An empty part is a client bug, not a domain
	// violation, and rejecting it here would lose the whole visita.
	v := validVisitaForImagen(t)
	img, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:        uuid.New(),
		Storage:   validStorage(t, "visitas/v/x.jpg"),
		Mime:      domain.MimeJPEG,
		SizeBytes: 0,
		By:        uuid.New(),
		Now:       imagenNow,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), img.SizeBytes())
}

func TestAdjuntarImagen_AcceptsAVeryLargeSize(t *testing.T) {
	t.Parallel()

	// There is NO per-image cap in this module, by design: the only ceiling
	// is the global HTTP BodyLimit. If someone ever adds one, this test goes
	// red and they have to justify it.
	v := validVisitaForImagen(t)
	img, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:        uuid.New(),
		Storage:   validStorage(t, "visitas/v/x.jpg"),
		Mime:      domain.MimeJPEG,
		SizeBytes: 500 << 20, // 500 MB
		By:        uuid.New(),
		Now:       imagenNow,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(500<<20), img.SizeBytes())
}

func TestAdjuntarImagen_RejectsUnvalidatedStorage(t *testing.T) {
	t.Parallel()

	cases := map[string]domain.ImagenStorage{
		"zero value": {},
		"empty key":  domain.HydrateImagenStorage(domain.StorageKindFilesystem, ""),
		"bad kind":   domain.HydrateImagenStorage(domain.StorageKind("R2"), "visitas/v/x.jpg"),
	}
	for name, st := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v := validVisitaForImagen(t)
			_, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
				ID:        uuid.New(),
				Storage:   st,
				Mime:      domain.MimeJPEG,
				SizeBytes: 1,
				By:        uuid.New(),
				Now:       imagenNow,
			})
			require.ErrorIs(t, err, domain.ErrImagenStorageKeyInvalida,
				"a storage value that skipped NewImagenStorage must not slip through the root")
		})
	}
}

// ─── Descripcion handling ─────────────────────────────────────────────────────

func TestAdjuntarImagen_DescripcionNilStaysNil(t *testing.T) {
	t.Parallel()

	v := validVisitaForImagen(t)
	img, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:        uuid.New(),
		Storage:   validStorage(t, "visitas/v/x.jpg"),
		Mime:      domain.MimeJPEG,
		SizeBytes: 1,
		By:        uuid.New(),
		Now:       imagenNow,
	})
	require.NoError(t, err)
	assert.Nil(t, img.Descripcion())
}

func TestAdjuntarImagen_DescripcionBlankBecomesNil(t *testing.T) {
	t.Parallel()

	v := validVisitaForImagen(t)
	img, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:          uuid.New(),
		Storage:     validStorage(t, "visitas/v/x.jpg"),
		Mime:        domain.MimeJPEG,
		SizeBytes:   1,
		Descripcion: ptr("   \t "),
		By:          uuid.New(),
		Now:         imagenNow,
	})
	require.NoError(t, err)
	assert.Nil(t, img.Descripcion(), "blank means 'not provided', i.e. SQL NULL")
}

func TestAdjuntarImagen_DescripcionIsTrimmedAndNFCNormalized(t *testing.T) {
	t.Parallel()

	// "e" + combining acute must come back as the single composed rune, or
	// two visually identical descriptions would compare unequal.
	v := validVisitaForImagen(t)
	img, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:          uuid.New(),
		Storage:     validStorage(t, "visitas/v/x.jpg"),
		Mime:        domain.MimeJPEG,
		SizeBytes:   1,
		Descripcion: ptr("  cliente no está  "),
		By:          uuid.New(),
		Now:         imagenNow,
	})
	require.NoError(t, err)
	require.NotNil(t, img.Descripcion())
	assert.Equal(t, "cliente no está", *img.Descripcion())
}

func TestAdjuntarImagen_DescripcionAtTheColumnWidthPasses(t *testing.T) {
	t.Parallel()

	v := validVisitaForImagen(t)
	img, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:          uuid.New(),
		Storage:     validStorage(t, "visitas/v/x.jpg"),
		Mime:        domain.MimeJPEG,
		SizeBytes:   1,
		Descripcion: ptr(strings.Repeat("á", 200)),
		By:          uuid.New(),
		Now:         imagenNow,
	})
	require.NoError(t, err)
	require.NotNil(t, img.Descripcion())
	assert.Len(t, []rune(*img.Descripcion()), 200,
		"the cap counts runes, not bytes — 200 accented chars fit")
}

func TestAdjuntarImagen_RejectsOverlongDescripcion(t *testing.T) {
	t.Parallel()

	v := validVisitaForImagen(t)
	_, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:          uuid.New(),
		Storage:     validStorage(t, "visitas/v/x.jpg"),
		Mime:        domain.MimeJPEG,
		SizeBytes:   1,
		Descripcion: ptr(strings.Repeat("a", 201)),
		By:          uuid.New(),
		Now:         imagenNow,
	})
	require.ErrorIs(t, err, domain.ErrImagenDescripcionDemasiadoLarga)
}

func TestAdjuntarImagen_RejectsUnsafeCharsInDescripcion(t *testing.T) {
	t.Parallel()

	v := validVisitaForImagen(t)
	_, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:          uuid.New(),
		Storage:     validStorage(t, "visitas/v/x.jpg"),
		Mime:        domain.MimeJPEG,
		SizeBytes:   1,
		Descripcion: ptr("foto\x00rota"),
		By:          uuid.New(),
		Now:         imagenNow,
	})
	require.ErrorIs(t, err, domain.ErrVisitaStringCaracteresInvalidos)
}

// ─── HydrateImagen ────────────────────────────────────────────────────────────

func TestHydrateImagen_DoesNotValidate(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	createdBy := uuid.New()
	updatedBy := uuid.New()
	created := imagenNow
	updated := imagenNow.Add(time.Hour)

	// A MIME that is NOT on the whitelist and a storage value that would be
	// rejected on write both hydrate cleanly: the read path never rejects a
	// stored row.
	img := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:          id,
		Storage:     domain.HydrateImagenStorage(domain.StorageKind("R2"), "/unsafe/../key"),
		Mime:        "image/bmp",
		SizeBytes:   77,
		Descripcion: ptr("cualquier cosa"),
		CreatedAt:   created,
		UpdatedAt:   updated,
		CreatedBy:   createdBy,
		UpdatedBy:   updatedBy,
	})

	assert.Equal(t, id, img.ID())
	assert.Equal(t, "image/bmp", img.Mime(), "hydration does not re-check the whitelist")
	assert.Equal(t, int64(77), img.SizeBytes())
	assert.Equal(t, "/unsafe/../key", img.Storage().Key())
	require.NotNil(t, img.Descripcion())
	assert.Equal(t, "cualquier cosa", *img.Descripcion())

	aud := img.Audit()
	assert.Equal(t, created, aud.CreatedAt())
	assert.Equal(t, updated, aud.UpdatedAt())
	assert.Equal(t, createdBy, aud.CreatedBy())
	assert.Equal(t, updatedBy, aud.UpdatedBy())
}

func TestHydrateImagen_KeepsMimeVerbatim(t *testing.T) {
	t.Parallel()

	// Not lower-cased on the way in either — whatever the column holds is
	// what comes back.
	img := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:   uuid.New(),
		Mime: "IMAGE/JPEG",
	})
	assert.Equal(t, "IMAGE/JPEG", img.Mime())
}

// ─── The parent stays unaffected ──────────────────────────────────────────────

func TestRehydrateVisita_HasNoImagenes(t *testing.T) {
	t.Parallel()

	// GET /v2/visitas/{id} does not project images, so the read-path
	// constructor must leave the collection empty rather than fabricate one.
	v := domain.RehydrateVisita(domain.RehydrateVisitaParams{
		ID:         uuid.New(),
		Cobrador:   "Legacy",
		ClienteID:  1,
		Fecha:      imagenNow,
		TipoVisita: "lo que sea",
	})
	assert.Empty(t, v.Imagenes())
}
