//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// buildFuzzRouter returns a fresh router backed by clean fakes for each fuzz
// iteration. Shared state across iterations would cause false failures under
// parallel fuzzing, so we always create fresh fakes here.
func buildFuzzRouter(t *testing.T) http.Handler {
	t.Helper()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	saldos := newFakeSaldosRepoHTTP()
	// Seed a fixed cargo so valid payloads can actually pass cargo validation.
	s := makeSaldoHTTP(5000, decimal.NewFromInt(9999))
	saldos.byCargo[5000] = &s

	pagosRepo := newFakePagosRecibidosRepo()
	imagenesRepo := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()

	writer := &fakeMicrosipPagoWriter{
		result: outbound.MicrosipPagoResult{DoctoCCID: 1, ImpteDoctoCCID: 2, Folio: "ZF01"},
	}

	svc := buildTestService(now, saldos, pagosRepo, imagenesRepo, writer, storage, nil, fakeTxRunner{})
	return mountReadWithUser(pagoUser(), svc)
}

// buildFuzzRouterWithPago returns a fresh router backed by clean fakes and a
// pre-seeded pago for the given pagoID. Used by FuzzAdjuntarImagenPagoMultipart
// so the handler can reach the multipart-parsing stage.
func buildFuzzRouterWithPago(t *testing.T, pagoID uuid.UUID) (http.Handler, *fakePagosRecibidosRepo) {
	t.Helper()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	saldos := newFakeSaldosRepoHTTP()
	s := makeSaldoHTTP(5000, decimal.NewFromInt(9999))
	saldos.byCargo[5000] = &s

	pagosRepo := newFakePagosRecibidosRepo()
	imagenesRepo := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()

	// Pre-seed the pago so the upload handler can find it.
	pago, err := domain.NewPagoRecibido(domain.CrearPagoRecibidoParams{
		ID:             pagoID,
		CargoDoctoCCID: 5000,
		ClienteID:      11486,
		CobradorID:     200,
		Cobrador:       "Mendoza Torres, Ana",
		Importe:        decimal.NewFromInt(1500),
		FormaCobroID:   1,
		FechaHoraPago:  now.Add(-30 * time.Minute),
		CreatedBy:      uuid.New(),
		Now:            now,
	})
	if err == nil {
		_ = pagosRepo.Insert(context.Background(), pago)
	}

	svc := buildTestService(now, saldos, pagosRepo, imagenesRepo, nil, storage, nil, fakeTxRunner{})
	router := mountReadWithUser(pagoUser(), svc)
	return router, pagosRepo
}

// buildSeedMultipart returns a raw multipart body as bytes, using a fixed
// boundary so it can be supplied as a fuzz seed.
//
// bytes.Buffer.Write and fmt.Fprintf to an in-memory buffer never return an
// error — the nolint suppresses the revive unhandled-error warning.
//
//nolint:revive // writing to bytes.Buffer always succeeds.
func buildSeedMultipart(mimeType string, content []byte, filename string) []byte {
	buf := &bytes.Buffer{}
	// Use a fixed boundary for seeds so they are reproducible.
	boundary := "fuzzSeedBoundary"
	_, _ = fmt.Fprintf(buf, "--%s\r\n", boundary)
	_, _ = fmt.Fprintf(buf, "Content-Disposition: form-data; name=\"file\"; filename=\"%s\"\r\n", filename)
	_, _ = fmt.Fprintf(buf, "Content-Type: %s\r\n", mimeType)
	_, _ = fmt.Fprintf(buf, "\r\n")
	_, _ = buf.Write(content)
	_, _ = fmt.Fprintf(buf, "\r\n--%s--\r\n", boundary)
	return buf.Bytes()
}

// FuzzCrearPagoJSON fuzzes the CrearPago handler with arbitrary JSON bodies.
// It must never panic; for 2xx responses the body must decode as PagoRecibidoDTO.
func FuzzCrearPagoJSON(f *testing.F) {
	// Corpus seeds — valid and invalid examples.
	f.Add([]byte(`{"id":"550e8400-e29b-41d4-a716-446655440000","cargo_docto_cc_id":5000,"cliente_id":11486,"cobrador_id":200,"cobrador":"Mendoza Torres, Ana","importe":"1500.00","forma_cobro_id":1,"fecha_hora_pago":"2026-06-01T09:30:00Z"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(``))
	f.Add([]byte(`{"id":"not-a-uuid"}`))
	f.Add([]byte(`{"id":" "}`))
	f.Add([]byte(`{`))
	f.Add([]byte(`{"id":"550e8400-e29b-41d4-a716-446655440000","cargo_docto_cc_id":5000,"cliente_id":11486,"cobrador_id":200,"cobrador":"Test","importe":"not-a-decimal","forma_cobro_id":1,"fecha_hora_pago":"2026-06-01T09:30:00Z"}`))
	f.Add([]byte(`{"id":"550e8400-e29b-41d4-a716-446655440000","cargo_docto_cc_id":5000,"cliente_id":11486,"cobrador_id":200,"cobrador":"Test","importe":"1.00","forma_cobro_id":1,"fecha_hora_pago":"not-a-date"}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`[]`))

	f.Fuzz(func(t *testing.T, body []byte) {
		// Build a fresh router for each fuzz iteration — no shared state.
		router := buildFuzzRouter(t)

		req := httptest.NewRequest(http.MethodPost, "/pagos", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		// Catch any panics — they indicate a real bug in the handler.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("handler panicked on body=%q: %v", string(body), r)
				}
			}()
			router.ServeHTTP(rec, req)
		}()

		// The status code must always be set (never zero).
		if rec.Code == 0 {
			t.Fatalf("handler returned zero status code for body=%q", string(body))
		}

		// For 2xx responses the body must decode as a valid PagoRecibidoDTO.
		if rec.Code >= 200 && rec.Code < 300 {
			var dto cobranzahttp.PagoRecibidoDTO
			if err := json.NewDecoder(rec.Body).Decode(&dto); err != nil {
				t.Fatalf("2xx response body is not valid PagoRecibidoDTO: %v; body=%q", err, rec.Body.String())
			}
			if dto.ID == "" {
				t.Fatalf("2xx response DTO has empty ID; body=%q", string(body))
			}
		}
	})
}

// FuzzAdjuntarImagenPagoMultipart fuzzes the AdjuntarImagenPago handler with
// arbitrary multipart bodies. It must never panic.
func FuzzAdjuntarImagenPagoMultipart(f *testing.F) {
	// Use a stable seed pago ID so seeds reference a real pago.
	seedPagoID := uuid.MustParse("650e8400-e29b-41d4-a716-446655440001")

	// Corpus seeds — valid PDF, empty body, malformed multipart.
	f.Add(buildSeedMultipart("application/pdf", []byte("%PDF-1.4 fuzz test content"), "recibo.pdf"))
	f.Add([]byte(``))
	f.Add([]byte("--boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"x\"\r\n\r\n--boundary--\r\n"))
	f.Add(buildSeedMultipart("image/jpeg", []byte("\xFF\xD8\xFF fake jpeg"), "foto.jpg"))
	f.Add(buildSeedMultipart("text/plain", []byte("not allowed"), "notes.txt"))
	f.Add(buildSeedMultipart("image/bmp", []byte("BM fake bmp"), "foto.bmp"))

	f.Fuzz(func(t *testing.T, body []byte) {
		// Build a fresh router with the pre-seeded pago for each iteration.
		router, _ := buildFuzzRouterWithPago(t, seedPagoID)

		req := httptest.NewRequest(
			http.MethodPost,
			"/pagos/"+seedPagoID.String()+"/imagenes",
			bytes.NewReader(body),
		)
		// Always use the fuzzSeedBoundary boundary so the seeds are parseable.
		req.Header.Set("Content-Type", "multipart/form-data; boundary=fuzzSeedBoundary")

		rec := httptest.NewRecorder()

		// Catch any panics — they indicate a real bug in the handler.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("handler panicked on multipart body=%q: %v", string(body), r)
				}
			}()
			router.ServeHTTP(rec, req)
		}()

		// Status code must be set.
		if rec.Code == 0 {
			t.Fatalf("handler returned zero status code for multipart body=%q", string(body))
		}
	})
}

// ─── Seed helpers (referenced but not exported) ───────────────────────────────

// makeSaldoHTTP is defined in handlers_pago_recibido_test.go (same package).
// Declared here only for documentation; do not re-declare.
var _ = makeSaldoHTTP

// cobranzaapp is imported above; this ensures the import is used for
// buildFuzzRouterWithPago.
var _ *cobranzaapp.Service
