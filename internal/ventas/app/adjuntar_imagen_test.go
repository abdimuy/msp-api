//nolint:misspell // domain vocabulary is Spanish (imagen, ventas) per project convention.
package app_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
	"github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// adjuntarInput returns a valid AdjuntarImagenInput. Tests mutate the result.
func adjuntarInput(ventaID uuid.UUID) app.AdjuntarImagenInput {
	return app.AdjuntarImagenInput{
		VentaID:     ventaID,
		ImagenID:    uuid.New(),
		StorageKind: "FILESYSTEM",
		StorageKey:  "ventas/" + ventaID.String() + "/photo.jpg",
		Mime:        domain.MimeJPEG,
		SizeBytes:   1024,
		Body:        strings.NewReader("fake jpeg bytes"),
	}
}

func TestAdjuntarImagen(t *testing.T) {
	t.Parallel()

	t.Run("happy_path_stores_blob_and_emits_event", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		in := adjuntarInput(*ventaID)

		img, err := h.svc.AdjuntarImagen(t.Context(), in, uuid.New())
		require.NoError(t, err)
		require.NotNil(t, img)
		assert.Equal(t, 1, h.storage.StoreCalls)
		assert.Equal(t, 1, h.ventas.InsertImagenCalls)
		assert.Zero(t, h.storage.DeleteCalls, "no rollback on success")
		assert.True(t, h.storage.has(in.StorageKey))
		assert.Equal(t, []string{domain.EventTypeImagenAdjuntada}, h.outbox.eventTypes())
	})

	t.Run("canceled_venta_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		_, err := h.svc.CancelarVenta(t.Context(), *ventaID, "motivo", uuid.New())
		require.NoError(t, err)
		h.outbox.mu.Lock()
		h.outbox.calls = nil
		h.outbox.mu.Unlock()

		_, err = h.svc.AdjuntarImagen(t.Context(), adjuntarInput(*ventaID), uuid.New())
		require.ErrorIs(t, err, domain.ErrVentaCanceladaInmutable)
		assert.Zero(t, h.storage.StoreCalls)
		assert.Zero(t, h.ventas.InsertImagenCalls)
		assert.Empty(t, h.outbox.snapshot())
	})

	t.Run("venta_not_found_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)

		_, err := h.svc.AdjuntarImagen(t.Context(), adjuntarInput(uuid.New()), uuid.New())
		require.ErrorIs(t, err, domain.ErrVentaNotFound)
		assert.Zero(t, h.storage.StoreCalls)
	})

	t.Run("invalid_storage_kind_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		in := adjuntarInput(*ventaID)
		in.StorageKind = "BOGUS"

		_, err := h.svc.AdjuntarImagen(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrStorageKindInvalido)
		assert.Zero(t, h.storage.StoreCalls)
	})

	t.Run("invalid_mime_rolls_back_storage", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		in := adjuntarInput(*ventaID)
		in.Mime = "application/pdf"

		_, err := h.svc.AdjuntarImagen(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrMimeNoPermitido)
		assert.Equal(t, 1, h.storage.StoreCalls, "blob was uploaded before domain validation")
		assert.Equal(t, 1, h.storage.DeleteCalls, "blob is rolled back when domain rejects it")
		assert.False(t, h.storage.has(in.StorageKey))
		assert.Empty(t, h.outbox.snapshot())
	})

	t.Run("storage_store_failure_returns_error", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		boom := errors.New("disk full")
		h.storage.StoreErr = boom

		_, err := h.svc.AdjuntarImagen(t.Context(), adjuntarInput(*ventaID), uuid.New())
		require.ErrorIs(t, err, boom)
		assert.Zero(t, h.ventas.InsertImagenCalls)
		assert.Empty(t, h.outbox.snapshot())
	})

	t.Run("insert_failure_triggers_storage_rollback", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		boom := errors.New("insert failed")
		h.ventas.InsertImagenErr = boom
		in := adjuntarInput(*ventaID)

		_, err := h.svc.AdjuntarImagen(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, boom)
		assert.Equal(t, 1, h.storage.StoreCalls)
		assert.Equal(t, 1, h.storage.DeleteCalls, "blob was rolled back after insert failure")
		assert.False(t, h.storage.has(in.StorageKey))
		assert.Empty(t, h.outbox.snapshot())
	})

	t.Run("storage_rollback_failure_is_logged_not_returned", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		original := errors.New("insert failed")
		h.ventas.InsertImagenErr = original
		h.storage.DeleteErr = errors.New("delete failed too")
		in := adjuntarInput(*ventaID)

		_, err := h.svc.AdjuntarImagen(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, original, "the original error wins; rollback failure is logged only")
	})

	t.Run("image_processor_is_invoked_with_input_metadata", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		in := adjuntarInput(*ventaID)

		_, err := h.svc.AdjuntarImagen(t.Context(), in, uuid.New())
		require.NoError(t, err)
		assert.Equal(t, 1, h.imageProc.callsCount())
		assert.Equal(t, in.Mime, h.imageProc.LastContentType)
		assert.Equal(t, in.SizeBytes, h.imageProc.LastSizeBytes)
	})

	t.Run("image_processor_output_drives_storage_inputs", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		// Simulate a processor that compresses the upload to a smaller JPEG.
		processed := []byte("compressed-jpeg-bytes")
		h.imageProc.OverrideOutput = &outbound.ImageProcessorOutput{
			Body:        bytes.NewReader(processed),
			ContentType: domain.MimeJPEG,
			SizeBytes:   int64(len(processed)),
		}
		in := adjuntarInput(*ventaID)
		in.Mime = domain.MimePNG // declared mime overridden by processor output
		in.SizeBytes = 999

		img, err := h.svc.AdjuntarImagen(t.Context(), in, uuid.New())
		require.NoError(t, err)
		assert.Equal(t, domain.MimeJPEG, img.Mime(), "stored mime reflects processor output")
		assert.Equal(t, int64(len(processed)), img.SizeBytes())
	})

	t.Run("image_processor_too_large_maps_to_domain_error", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		h.imageProc.Err = imageprocessor.ErrInputTooLarge

		_, err := h.svc.AdjuntarImagen(t.Context(), adjuntarInput(*ventaID), uuid.New())
		require.ErrorIs(t, err, domain.ErrImagenDemasiadoGrande)
		assert.Zero(t, h.storage.StoreCalls, "storage must not be touched when the processor rejects the upload")
	})

	t.Run("image_processor_unsupported_mime_maps_to_domain_error", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		h.imageProc.Err = imageprocessor.ErrUnsupportedMIME

		_, err := h.svc.AdjuntarImagen(t.Context(), adjuntarInput(*ventaID), uuid.New())
		require.ErrorIs(t, err, domain.ErrMimeNoPermitido)
	})

	t.Run("image_processor_decode_failure_maps_to_domain_error", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		h.imageProc.Err = imageprocessor.ErrDecodeFailed

		_, err := h.svc.AdjuntarImagen(t.Context(), adjuntarInput(*ventaID), uuid.New())
		require.ErrorIs(t, err, domain.ErrImagenDecodeFallo)
	})

	t.Run("image_processor_unknown_error_passes_through", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		boom := errors.New("disk decoded too slowly")
		h.imageProc.Err = boom

		_, err := h.svc.AdjuntarImagen(t.Context(), adjuntarInput(*ventaID), uuid.New())
		require.ErrorIs(t, err, boom)
	})

	t.Run("processor_runs_before_storage_store", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		// Reject at the processor — storage.Store must not be called.
		h.imageProc.Err = imageprocessor.ErrUnsupportedMIME

		_, err := h.svc.AdjuntarImagen(t.Context(), adjuntarInput(*ventaID), uuid.New())
		require.Error(t, err)
		assert.Equal(t, 1, h.imageProc.callsCount(), "processor must be called")
		assert.Zero(t, h.storage.StoreCalls, "storage.Store must NOT be called when processor rejects")
		assert.Zero(t, h.storage.DeleteCalls, "no rollback when nothing was stored")
	})

	t.Run("storage_receives_processor_normalized_metadata", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		// Simulate a WebP→JPEG transcode that shrinks the payload.
		processed := []byte("normalized-jpeg-payload")
		h.imageProc.OverrideOutput = &outbound.ImageProcessorOutput{
			Body:        bytes.NewReader(processed),
			ContentType: domain.MimeJPEG,
			SizeBytes:   int64(len(processed)),
		}
		in := adjuntarInput(*ventaID)
		in.Mime = domain.MimePNG
		in.SizeBytes = 9999

		_, err := h.svc.AdjuntarImagen(t.Context(), in, uuid.New())
		require.NoError(t, err)
		assert.Equal(t, domain.MimeJPEG, h.storage.LastStoreContentType,
			"storage.Store must receive the processor's MIME, not the original")
		assert.Equal(t, int64(len(processed)), h.storage.LastStoreSizeBytes,
			"storage.Store must receive the processor's SizeBytes, not the original")
		assert.Equal(t, processed, h.storage.LastStoreBody,
			"storage.Store must receive the processor's body bytes verbatim")
	})

	t.Run("storage_store_failure_after_processor_success_no_extra_rollback", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		boom := errors.New("disk full")
		h.storage.StoreErr = boom

		_, err := h.svc.AdjuntarImagen(t.Context(), adjuntarInput(*ventaID), uuid.New())
		require.ErrorIs(t, err, boom)
		assert.Equal(t, 1, h.imageProc.callsCount(), "processor still runs even when storage will fail")
		assert.Equal(t, 1, h.storage.StoreCalls)
		assert.Zero(t, h.storage.DeleteCalls,
			"failed Store leaves nothing on disk; rollback would be wasted work")
	})

	t.Run("processor_invoked_with_propagated_size_zero_does_not_crash", func(t *testing.T) {
		t.Parallel()
		// Some upload paths (chunked HTTP) cannot supply SizeBytes; the
		// processor must accept zero and let downstream stages discover the
		// real size from the body. This pins the contract for those paths.
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		in := adjuntarInput(*ventaID)
		in.SizeBytes = 0

		_, err := h.svc.AdjuntarImagen(t.Context(), in, uuid.New())
		require.NoError(t, err)
		assert.Equal(t, int64(0), h.imageProc.LastSizeBytes)
		assert.Positive(t, h.storage.LastStoreSizeBytes,
			"storage must see a non-zero size after the processor measures the body")
	})
}

// TestAdjuntarImagen_BestEffortStorageDeleteLogsPath lives at the top level
// (not inside TestAdjuntarImagen) because it mutates slog.Default — it must
// run sequentially with respect to anything else that also reads/writes the
// global default. The contract pinned here: when both Insert and the
// rollback Delete fail, the operator gets a log entry with the storage_key
// so they can clean up the orphan blob by hand.
//
//nolint:paralleltest,tparallel // mutates global slog.Default; sequential by necessity.
func TestAdjuntarImagen_BestEffortStorageDeleteLogsPath(t *testing.T) {
	logs := captureLogs(t)
	h := newHarness(t)
	ventaID := h.seedVenta(t)
	h.ventas.InsertImagenErr = errors.New("insert exploded")
	h.storage.DeleteErr = errors.New("delete also exploded")
	in := adjuntarInput(*ventaID)

	_, err := h.svc.AdjuntarImagen(t.Context(), in, uuid.New())
	require.Error(t, err)

	out := logs()
	assert.Contains(t, out, "ventas.storage_rollback_failed",
		"rollback failure must produce a structured log entry")
	assert.Contains(t, out, in.StorageKey,
		"log entry must carry the storage_key so the operator can clean up the orphan blob")
}
