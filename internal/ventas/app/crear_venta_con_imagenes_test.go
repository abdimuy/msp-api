//nolint:misspell // ventas vocabulary is Spanish (imagenes, comprobantes) per convention.
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
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// baseImagenUpload returns a valid ImagenUploadInput with a unique ImagenID
// and StorageKey for the given ventaID.
func baseImagenUpload(ventaID uuid.UUID, payload []byte) app.ImagenUploadInput {
	id := uuid.New()
	return app.ImagenUploadInput{
		ImagenID:    id,
		StorageKind: string(domain.StorageKindFilesystem),
		StorageKey:  "ventas/" + ventaID.String() + "/" + id.String() + ".jpg",
		Mime:        domain.MimeJPEG,
		SizeBytes:   int64(len(payload)),
		Body:        bytes.NewReader(payload),
	}
}

// (The default fakeImageProcessor in fakes_test.go is already a passthrough:
// it reads the input body and returns the same bytes with the same MIME. No
// helper needed.)

// ─── happy paths ─────────────────────────────────────────────────────────────

func TestCrearVentaConImagenes_HappyPath_1Imagen(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	in := validContadoInput()
	img := baseImagenUpload(in.ID, []byte("photo bytes"))

	venta, err := h.svc.CrearVentaConImagenes(t.Context(), in, []app.ImagenUploadInput{img}, uuid.New())

	require.NoError(t, err)
	require.NotNil(t, venta)
	assert.Equal(t, 1, h.ventas.SaveCalls, "ventas.Save called exactly once (atomic)")
	assert.Equal(t, 1, venta.ImagenesCount(), "venta has one imagen attached")
	assert.Equal(t, 1, h.storage.StoreCalls, "one blob stored")
	assert.Equal(t, 0, h.storage.DeleteCalls, "no cleanup on happy path")
	assert.True(t, h.storage.has(img.StorageKey))
}

func TestCrearVentaConImagenes_HappyPath_3Imagenes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	in := validContadoInput()
	imgs := []app.ImagenUploadInput{
		baseImagenUpload(in.ID, []byte("AAA")),
		baseImagenUpload(in.ID, []byte("BBB")),
		baseImagenUpload(in.ID, []byte("CCC")),
	}

	venta, err := h.svc.CrearVentaConImagenes(t.Context(), in, imgs, uuid.New())

	require.NoError(t, err)
	assert.Equal(t, 3, venta.ImagenesCount())
	assert.Equal(t, 1, h.ventas.SaveCalls, "single Save covers header + 3 imagenes atómicamente")
	assert.Equal(t, 3, h.storage.StoreCalls)
	assert.Equal(t, 0, h.storage.DeleteCalls)
}

// ─── mandatory evidence ─────────────────────────────────────────────────────

func TestCrearVentaConImagenes_NoImagenes_Rejected(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	_, err := h.svc.CrearVentaConImagenes(t.Context(), validContadoInput(), nil, uuid.New())

	require.ErrorIs(t, err, domain.ErrVentaEvidenciaRequerida)
	assert.Zero(t, h.ventas.SaveCalls)
	assert.Equal(t, 0, h.storage.StoreCalls, "no side effects when evidence missing")
}

func TestCrearVentaConImagenes_EmptyImagenes_Rejected(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	_, err := h.svc.CrearVentaConImagenes(t.Context(), validContadoInput(), []app.ImagenUploadInput{}, uuid.New())

	require.ErrorIs(t, err, domain.ErrVentaEvidenciaRequerida)
	assert.Zero(t, h.ventas.SaveCalls)
}

// ─── rollback scenarios ─────────────────────────────────────────────────────

func TestCrearVentaConImagenes_RollbackOnSaveFails(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.ventas.SaveErr = errors.New("constraint_violation")

	in := validContadoInput()
	imgs := []app.ImagenUploadInput{
		baseImagenUpload(in.ID, []byte("A")),
		baseImagenUpload(in.ID, []byte("B")),
	}

	_, err := h.svc.CrearVentaConImagenes(t.Context(), in, imgs, uuid.New())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "constraint_violation")
	// Atomicity: 2 blobs were stored, both must be cleaned up.
	assert.Equal(t, 2, h.storage.StoreCalls)
	assert.Equal(t, 2, h.storage.DeleteCalls)
}

func TestCrearVentaConImagenes_StorageStoreFails_NoSave(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	// Image processor passes through; we use a counting storage that fails
	// on the 2nd call (1st succeeded, so cleanup must fire for it).
	flaky := &flakyVentasStorage{fakeStorage: h.storage, failOnCall: 2, failErr: errors.New("disk_full")}
	// Swap in the flaky storage by rebuilding the service.
	svc := app.NewService(h.ventas, nil, nil, flaky, h.clock, h.outbox, h.imageProc, nil, nil, nil, nil)

	in := validContadoInput()
	imgs := []app.ImagenUploadInput{
		baseImagenUpload(in.ID, []byte("A")),
		baseImagenUpload(in.ID, []byte("B")),
	}

	_, err := svc.CrearVentaConImagenes(t.Context(), in, imgs, uuid.New())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk_full")
	assert.Zero(t, h.ventas.SaveCalls, "tx never started")
	assert.Equal(t, 2, h.storage.StoreCalls, "1 success + 1 attempted fail")
	assert.Equal(t, 1, h.storage.DeleteCalls, "only the successful blob is cleaned up")
}

// flakyVentasStorage fails the n-th Store call. Counts towards the underlying
// fakeStorage.StoreCalls so the test can assert about total attempts.
type flakyVentasStorage struct {
	*fakeStorage
	failOnCall int
	failErr    error
	storeN     int
}

func (f *flakyVentasStorage) Store(ctx context.Context, key, contentType string, sizeBytes int64, body io.Reader) error {
	f.storeN++
	if f.storeN == f.failOnCall {
		f.StoreCalls++ // count the attempt
		return f.failErr
	}
	return f.fakeStorage.Store(ctx, key, contentType, sizeBytes, body)
}

// ─── PDF rejected (ventas no acepta PDF) ────────────────────────────────────

func TestCrearVentaConImagenes_PDF_RejectedByDomain(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	// Image processor doesn't actually validate MIME (just returns out),
	// so we use a processor that produces a PDF mime — the domain rejects.
	h.imageProc.OverrideOutput = &outbound.ImageProcessorOutput{
		ContentType: "application/pdf",
		SizeBytes:   10,
		Body:        bytes.NewReader([]byte("%PDF-1.4")),
	}

	in := validContadoInput()
	img := baseImagenUpload(in.ID, []byte("data"))

	_, err := h.svc.CrearVentaConImagenes(t.Context(), in, []app.ImagenUploadInput{img}, uuid.New())

	require.ErrorIs(t, err, domain.ErrMimeNoPermitido)
	// Blob was stored before domain rejected — must be cleaned up.
	assert.Equal(t, 1, h.storage.StoreCalls)
	assert.Equal(t, 1, h.storage.DeleteCalls)
}

// ─── processor failure ──────────────────────────────────────────────────────

func TestCrearVentaConImagenes_ProcessorFails_NoStorage(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.imageProc.Err = errors.New("decode_failed")

	in := validContadoInput()
	img := baseImagenUpload(in.ID, []byte("data"))

	_, err := h.svc.CrearVentaConImagenes(t.Context(), in, []app.ImagenUploadInput{img}, uuid.New())

	require.Error(t, err)
	assert.Equal(t, 0, h.storage.StoreCalls)
	assert.Zero(t, h.ventas.SaveCalls)
}

// ─── duplicate detection ────────────────────────────────────────────────────

func TestCrearVentaConImagenes_DuplicateImagenIDs_Rejected(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	in := validContadoInput()
	dup := uuid.New()
	img1 := baseImagenUpload(in.ID, []byte("A"))
	img1.ImagenID = dup
	img2 := baseImagenUpload(in.ID, []byte("B"))
	img2.ImagenID = dup

	_, err := h.svc.CrearVentaConImagenes(t.Context(), in, []app.ImagenUploadInput{img1, img2}, uuid.New())

	require.ErrorIs(t, err, domain.ErrImagenIDDuplicado)
	assert.Equal(t, 0, h.storage.StoreCalls, "duplicates detected before any side effect")
	assert.Zero(t, h.ventas.SaveCalls)
}

// ─── validation propagation ─────────────────────────────────────────────────

func TestCrearVentaConImagenes_InvalidTipoVenta_RejectedBeforeBlobs(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	in := validContadoInput()
	in.TipoVenta = "INVALIDO"

	img := baseImagenUpload(in.ID, []byte("A"))

	_, err := h.svc.CrearVentaConImagenes(t.Context(), in, []app.ImagenUploadInput{img}, uuid.New())

	require.ErrorIs(t, err, domain.ErrTipoVentaInvalido)
	assert.Equal(t, 0, h.storage.StoreCalls, "venta validation runs before blob storage")
}

func TestCrearVentaConImagenes_StorageKeyInvalid_FirstImg_NoBlobs(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	in := validContadoInput()
	img := baseImagenUpload(in.ID, []byte("A"))
	img.StorageKey = "../etc/passwd"

	_, err := h.svc.CrearVentaConImagenes(t.Context(), in, []app.ImagenUploadInput{img}, uuid.New())

	require.Error(t, err)
	assert.Equal(t, 0, h.storage.StoreCalls)
}

// ─── outbox draining ────────────────────────────────────────────────────────

func TestCrearVentaConImagenes_DrainsEvents(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	in := validContadoInput()
	img := baseImagenUpload(in.ID, []byte("A"))

	venta, err := h.svc.CrearVentaConImagenes(t.Context(), in, []app.ImagenUploadInput{img}, uuid.New())
	require.NoError(t, err)

	// VentaCreada + ImagenAdjuntada both buffered, both drained.
	types := h.outbox.eventTypes()
	assert.Contains(t, types, domain.EventTypeVentaCreada)
	assert.Contains(t, types, domain.EventTypeImagenAdjuntada)
	assert.Empty(t, venta.PendingEvents(), "events drained after commit")
}
