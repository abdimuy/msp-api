//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// TestProperty_CrearVentaConImagenes_AtomicityInvariant explores combinations
// of (N imagenes, K-th fails, fault type) and asserts the all-or-nothing
// invariant: every successful run leaves N blobs + 1 venta in the repo; every
// failure path leaves 0 blobs + 0 ventas — no partial state visible.
func TestProperty_CrearVentaConImagenes_AtomicityInvariant(t *testing.T) {
	t.Parallel()

	type fault int
	const (
		faultNone fault = iota
		faultStore
		faultSave
	)

	cases := []struct {
		name      string
		nImgs     int
		failAt    int // 1-indexed (storage call number); 0 = no failure
		faultKind fault
		wantOK    bool
	}{
		{"3img_ok", 3, 0, faultNone, true},
		{"5img_ok", 5, 0, faultNone, true},
		{"1img_storage_fails", 1, 1, faultStore, false},
		{"3img_storage_fails_first", 3, 1, faultStore, false},
		{"3img_storage_fails_middle", 3, 2, faultStore, false},
		{"3img_storage_fails_last", 3, 3, faultStore, false},
		{"3img_save_fails", 3, 0, faultSave, false},
		{"1img_save_fails", 1, 0, faultSave, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			var storeWrap *flakyPropStorage
			if tc.faultKind == faultStore {
				storeWrap = &flakyPropStorage{
					fakeStorage: h.storage,
					failOnCall:  tc.failAt,
					failErr:     errors.New("induced_store_failure"),
				}
				h.svc = app.NewService(h.ventas, nil, nil, storeWrap, h.clock, h.outbox, h.imageProc, nil, nil, nil)
			}
			if tc.faultKind == faultSave {
				h.ventas.SaveErr = errors.New("induced_save_failure")
			}

			in := validContadoInput()
			imgs := make([]app.ImagenUploadInput, tc.nImgs)
			for i := range imgs {
				imgs[i] = makePropertyImg(in.ID)
			}

			_, err := h.svc.CrearVentaConImagenes(t.Context(), in, imgs, uuid.New())

			if tc.wantOK {
				require.NoError(t, err)
				assert.Equal(t, 1, h.ventas.SaveCalls)
				assert.Len(t, h.storage.blobs, tc.nImgs, "every blob persisted")
			} else {
				require.Error(t, err)
				assert.Empty(t, h.storage.blobs, "atomicity: no blobs remain on failure")
				if tc.faultKind == faultSave {
					assert.Equal(t, 1, h.ventas.SaveCalls, "Save was attempted")
				} else {
					assert.Zero(t, h.ventas.SaveCalls, "Save never invoked when blobs failed")
				}
			}
		})
	}
}

func makePropertyImg(ventaID uuid.UUID) app.ImagenUploadInput {
	id := uuid.New()
	return app.ImagenUploadInput{
		ImagenID:    id,
		StorageKind: string(domain.StorageKindFilesystem),
		StorageKey:  "ventas/" + ventaID.String() + "/" + id.String() + ".jpg",
		Mime:        domain.MimeJPEG,
		SizeBytes:   8,
		Body:        bytes.NewReader([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0}),
	}
}

// flakyPropStorage wraps the fake storage with an n-th-call Store failure.
type flakyPropStorage struct {
	*fakeStorage
	failOnCall int
	failErr    error
	storeN     int
}

func (f *flakyPropStorage) Store(ctx context.Context, key, contentType string, sizeBytes int64, body io.Reader) error {
	f.storeN++
	if f.storeN == f.failOnCall {
		return f.failErr
	}
	return f.fakeStorage.Store(ctx, key, contentType, sizeBytes, body)
}
