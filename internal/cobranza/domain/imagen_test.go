//nolint:misspell // Spanish vocabulary by convention.
package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// adjuntarParams returns AdjuntarImagenParams with every field valid.
func adjuntarParams(t *testing.T) domain.AdjuntarImagenParams {
	t.Helper()
	return domain.AdjuntarImagenParams{
		ID:      uuid.New(),
		Storage: validStorage(t),
		Mime:    domain.MimeJPEG,
		By:      uuid.New(),
		Now:     time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
}

// newValidPago builds a PagoRecibido from the shared helper.
func newValidPago(t *testing.T) *domain.PagoRecibido {
	t.Helper()
	pago, err := domain.NewPagoRecibido(validParams(t))
	require.NoError(t, err)
	return pago
}

// ─── IsAllowedMime ────────────────────────────────────────────────────────────

func TestIsAllowedMime_AllowsWhitelistedTypes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		mime string
	}{
		{domain.MimeJPEG},
		{domain.MimePNG},
		{domain.MimeGIF},
		{domain.MimeWebP},
		{domain.MimePDF},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.mime, func(t *testing.T) {
			t.Parallel()
			assert.True(t, domain.IsAllowedMime(tc.mime))
		})
	}
}

func TestIsAllowedMime_RejectsUnlistedTypes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		mime string
	}{
		{"text_plain", "text/plain"},
		{"image_bmp", "image/bmp"},
		{"empty", ""},
		{"uppercase_jpeg", "IMAGE/JPEG"},
		{"image_jpg", "image/jpg"},
		{"application_octet", "application/octet-stream"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.False(t, domain.IsAllowedMime(tc.mime))
		})
	}
}

// IsAllowedMime does not lowercase input — uppercase variants must be rejected.
func TestIsAllowedMime_CaseSensitive(t *testing.T) {
	t.Parallel()

	assert.False(t, domain.IsAllowedMime("Image/Jpeg"))
	assert.False(t, domain.IsAllowedMime("image/JPEG"))
	assert.False(t, domain.IsAllowedMime("APPLICATION/PDF"))
}

// ─── AdjuntarImagen (exercises newImagen indirectly) ─────────────────────────

func TestAdjuntarImagen_HappyPath(t *testing.T) {
	t.Parallel()

	pago := newValidPago(t)
	params := adjuntarParams(t)
	desc := "recibo de transferencia"
	params.Descripcion = &desc

	img, err := pago.AdjuntarImagen(params)

	require.NoError(t, err)
	require.NotNil(t, img)
	assert.Equal(t, params.Mime, img.Mime())
	assert.Equal(t, int64(0), img.SizeBytes())
	require.NotNil(t, img.Descripcion())
	assert.Equal(t, desc, *img.Descripcion())
	assert.Equal(t, 1, pago.ImagenesCount())
}

func TestAdjuntarImagen_RejectsBadMime(t *testing.T) {
	t.Parallel()

	pago := newValidPago(t)
	params := adjuntarParams(t)
	params.Mime = "text/plain"

	img, err := pago.AdjuntarImagen(params)

	assert.Nil(t, img)
	require.ErrorIs(t, err, domain.ErrMimeNoPermitido)
}

func TestAdjuntarImagen_RejectsNegativeSizeBytes(t *testing.T) {
	t.Parallel()

	pago := newValidPago(t)
	params := adjuntarParams(t)
	params.SizeBytes = -1

	img, err := pago.AdjuntarImagen(params)

	assert.Nil(t, img)
	require.ErrorIs(t, err, domain.ErrSizeBytesNegativo)
}

func TestAdjuntarImagen_RejectsDescripcionTooLong(t *testing.T) {
	t.Parallel()

	// 201 multi-byte runes ("ñ" = 2 bytes each) — exceeds the 200-rune column limit.
	desc := strings.Repeat("ñ", 201)
	pago := newValidPago(t)
	params := adjuntarParams(t)
	params.Descripcion = &desc

	img, err := pago.AdjuntarImagen(params)

	assert.Nil(t, img)
	require.ErrorIs(t, err, domain.ErrImagenDescripcionDemasiadoLarga)
}

// The 200-rune boundary is rune-counted, not byte-counted.
func TestAdjuntarImagen_AcceptsDescripcionAt200Runes(t *testing.T) {
	t.Parallel()

	desc := strings.Repeat("ñ", 200)
	pago := newValidPago(t)
	params := adjuntarParams(t)
	params.Descripcion = &desc

	img, err := pago.AdjuntarImagen(params)

	require.NoError(t, err)
	require.NotNil(t, img)
}

// ─── HydrateImagen ────────────────────────────────────────────────────────────

func TestHydrateImagen_PreservesPersistedFields(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	storage := domain.HydrateImagenStorage(domain.StorageKindFilesystem, "pagos/2026/05/receipt.jpg")
	mime := "image/jpeg"
	sizeBytes := int64(204800)
	desc := "comprobante de pago"
	createdAt := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	createdBy := uuid.New()
	updatedBy := uuid.New()

	img := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:          id,
		Storage:     storage,
		Mime:        mime,
		SizeBytes:   sizeBytes,
		Descripcion: &desc,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
		CreatedBy:   createdBy,
		UpdatedBy:   updatedBy,
	})

	assert.Equal(t, id, img.ID())
	assert.Equal(t, storage.Key(), img.Storage().Key())
	assert.Equal(t, mime, img.Mime())
	assert.Equal(t, sizeBytes, img.SizeBytes())
	require.NotNil(t, img.Descripcion())
	assert.Equal(t, desc, *img.Descripcion())
	aud := img.Audit()
	assert.Equal(t, createdAt, aud.CreatedAt())
	assert.Equal(t, updatedAt, aud.UpdatedAt())
	assert.Equal(t, createdBy, aud.CreatedBy())
	assert.Equal(t, updatedBy, aud.UpdatedBy())
}

// HydrateImagen stores the mime verbatim without downcasing (no validation).
func TestHydrateImagen_StoresMimeVerbatim(t *testing.T) {
	t.Parallel()

	storage := domain.HydrateImagenStorage(domain.StorageKindFilesystem, "pagos/img.jpg")
	img := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:      uuid.New(),
		Storage: storage,
		Mime:    "IMAGE/JPEG",
	})

	assert.Equal(t, "IMAGE/JPEG", img.Mime())
}

// ─── Audit returns a value copy ───────────────────────────────────────────────

func TestImagen_AuditReturnsValueNotPointer(t *testing.T) {
	t.Parallel()

	pago := newValidPago(t)
	params := adjuntarParams(t)

	img, err := pago.AdjuntarImagen(params)
	require.NoError(t, err)

	audit1 := img.Audit()
	audit2 := img.Audit()

	// Both copies carry the same timestamps — mutations to one do not affect the other.
	assert.Equal(t, audit1.CreatedAt(), audit2.CreatedAt())
	assert.Equal(t, audit1.CreatedBy(), audit2.CreatedBy())
}
