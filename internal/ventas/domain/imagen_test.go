package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestHydrateImagen(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	creator := uuid.New()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	storage, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "ventas/x.jpg")
	require.NoError(t, err)
	desc := "evidencia"

	img := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:          id,
		Storage:     storage,
		Mime:        "image/jpeg",
		SizeBytes:   1024,
		Descripcion: &desc,
		CreatedAt:   now,
		UpdatedAt:   now,
		CreatedBy:   creator,
		UpdatedBy:   creator,
	})

	assert.Equal(t, id, img.ID())
	assert.Equal(t, "image/jpeg", img.Mime())
	assert.Equal(t, int64(1024), img.SizeBytes())
	require.NotNil(t, img.Descripcion())
	assert.Equal(t, "evidencia", *img.Descripcion())
	a := img.Audit()
	assert.Equal(t, creator, a.CreatedBy())
}

func TestImagen_AdjuntarValidation(t *testing.T) {
	t.Parallel()
	storage, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "ventas/x.jpg")
	require.NoError(t, err)

	cases := []struct {
		name      string
		mime      string
		size      int64
		desc      *string
		expectErr string
	}{
		{"mime invalid", "image/bmp", 100, nil, "mime_no_permitido"},
		{"size negative", "image/jpeg", -1, nil, "size_bytes_negativo"},
		{"desc too long", "image/jpeg", 1, strPtr(strings.Repeat("a", 201)), "imagen_descripcion_too_long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := buildValidVenta(t)
			_, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
				ID: uuid.New(), Storage: storage, Mime: tc.mime,
				SizeBytes: tc.size, Descripcion: tc.desc, By: uuid.New(), Now: time.Now(),
			})
			require.Error(t, err)
		})
	}
}

func TestImagen_AdjuntarValid(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	storage, _ := domain.NewImagenStorage(domain.StorageKindFilesystem, "ventas/y.png")
	desc := "  descripcion  "
	img, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID: uuid.New(), Storage: storage, Mime: "image/png",
		SizeBytes: 500, Descripcion: &desc, By: uuid.New(), Now: time.Now(),
	})
	require.NoError(t, err)
	assert.Equal(t, "image/png", img.Mime())
	require.NotNil(t, img.Descripcion())
	// Descripcion is trimmed and folded to ALL CAPS (Microsip convention).
	assert.Equal(t, "DESCRIPCION", *img.Descripcion())
}

func strPtr(s string) *string { return &s }
