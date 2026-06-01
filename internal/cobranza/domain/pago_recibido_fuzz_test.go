//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// FuzzNewPagoRecibido_PanicFree exercises NewPagoRecibido with arbitrary
// inputs. The contract is: it must never panic. When it returns a valid pago
// (err == nil), the aggregate invariants must hold.
func FuzzNewPagoRecibido_PanicFree(f *testing.F) {
	// Seeds: cover boundary and pathological cases.
	f.Add("Ramírez García, Jorge", int64(50000), 1001, 2002, 3003, 1)
	f.Add("", int64(50000), 1001, 2002, 3003, 1)
	f.Add(strings.Repeat("x", 10_000), int64(50000), 1001, 2002, 3003, 1)
	f.Add("NUL\x00byte", int64(50000), 1001, 2002, 3003, 1)
	f.Add("valid", int64(0), 1001, 2002, 3003, 1)
	f.Add("valid", int64(-1), 1001, 2002, 3003, 1)
	f.Add("valid", int64(1), 0, 2002, 3003, 1)
	f.Add("valid", int64(1), 1001, 0, 3003, 1)
	f.Add("valid", int64(1), 1001, 2002, 0, 1)
	f.Add("valid", int64(1), 1001, 2002, 3003, 0)

	f.Fuzz(func(t *testing.T, cobrador string, importeCents int64, cargoID, clienteID, cobradorID, formaCobroID int) {
		var importe decimal.Decimal
		if importeCents != 0 {
			importe = decimal.New(importeCents, -2)
		}

		p := domain.CrearPagoRecibidoParams{
			ID:             uuid.New(),
			CargoDoctoCCID: cargoID,
			ClienteID:      clienteID,
			CobradorID:     cobradorID,
			Cobrador:       cobrador,
			Importe:        importe,
			FormaCobroID:   formaCobroID,
			FechaHoraPago:  time.Now().UTC(),
			CreatedBy:      uuid.New(),
			Now:            time.Now().UTC(),
		}

		// Must never panic — if it does, the fuzzer will report it.
		pago, err := domain.NewPagoRecibido(p)
		if err != nil {
			return
		}

		// Invariants for accepted pagos.
		if pago.CargoDoctoCCID() <= 0 {
			t.Fatalf("CargoDoctoCCID must be positive when accepted: got=%d", pago.CargoDoctoCCID())
		}
		if pago.ClienteID() <= 0 {
			t.Fatalf("ClienteID must be positive when accepted: got=%d", pago.ClienteID())
		}
		if pago.CobradorID() <= 0 {
			t.Fatalf("CobradorID must be positive when accepted: got=%d", pago.CobradorID())
		}
		if pago.FormaCobroID() <= 0 {
			t.Fatalf("FormaCobroID must be positive when accepted: got=%d", pago.FormaCobroID())
		}
		if !pago.Importe().IsPositive() {
			t.Fatalf("Importe must be positive when accepted: got=%s", pago.Importe())
		}
		cobTrimmed := strings.TrimSpace(cobrador)
		if cobTrimmed == "" {
			t.Fatalf("accepted empty cobrador name")
		}
		if utf8.RuneCountInString(pago.Cobrador()) > 100 {
			t.Fatalf("accepted cobrador exceeds 100 runes: %d", utf8.RuneCountInString(pago.Cobrador()))
		}
		if !pago.IsPendiente() {
			t.Fatalf("new pago must start pendiente")
		}
		if pago.Intentos() != 0 {
			t.Fatalf("new pago must have zero intentos")
		}
		// ConceptoCCID must be one of the two known values.
		cc := pago.ConceptoCCID()
		if cc != 27969 && cc != 87327 {
			t.Fatalf("unexpected conceptoCCID: %d", cc)
		}
	})
}

// FuzzAdjuntarImagen_PanicFree exercises AdjuntarImagen with arbitrary inputs.
// Builds one valid PagoRecibido (fixed), then calls AdjuntarImagen with fuzzed
// (mime, path, size, descripcion). Must never panic.
func FuzzAdjuntarImagen_PanicFree(f *testing.F) {
	// Seeds: boundary and pathological cases.
	f.Add("image/jpeg", "pagos/receipts/img001.jpg", int64(12345), "recibo de transferencia")
	f.Add("", "pagos/receipts/img001.jpg", int64(0), "")
	f.Add("application/pdf", "", int64(0), "")
	f.Add("image/jpeg", "/absolute/path.jpg", int64(1024), "")
	f.Add("image/jpeg", "a/../b/img.jpg", int64(1024), "")
	f.Add("image/jpeg", "path\x00with\x00nul.jpg", int64(1024), "")
	f.Add("image/jpeg", strings.Repeat("x", 1000)+".jpg", int64(1024), "")
	f.Add("text/html", "pagos/img.jpg", int64(-1), "negative size")
	f.Add("image/png", "pagos/img.png", int64(0), strings.Repeat("d", 300))

	f.Fuzz(func(t *testing.T, mime, path string, sizeBytes int64, descripcion string) {
		// Build a valid base pago (fixed, not fuzzed).
		base := domain.CrearPagoRecibidoParams{
			ID:             uuid.New(),
			CargoDoctoCCID: 1001,
			ClienteID:      2002,
			CobradorID:     3003,
			Cobrador:       "Ramírez García, Jorge",
			Importe:        decimal.NewFromInt(500),
			FormaCobroID:   1,
			FechaHoraPago:  time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
			CreatedBy:      uuid.New(),
			Now:            time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		}
		pago, err := domain.NewPagoRecibido(base)
		if err != nil {
			t.Fatalf("base pago setup failed: %v", err)
		}

		// Storage may fail — that's fine, we check AdjuntarImagen separately.
		st, stErr := domain.NewImagenStorage(domain.StorageKindFilesystem, path)
		if stErr != nil {
			// Even with invalid storage, AdjuntarImagen must not panic when
			// called with the zero-value ImagenStorage.
			var zero domain.ImagenStorage
			var desc *string
			if descripcion != "" {
				desc = &descripcion
			}
			// Must not panic.
			_, _ = pago.AdjuntarImagen(domain.AdjuntarImagenParams{
				ID:          uuid.New(),
				Storage:     zero,
				Mime:        mime,
				SizeBytes:   sizeBytes,
				Descripcion: desc,
				By:          uuid.New(),
				Now:         time.Now().UTC(),
			})
			return
		}

		before := pago.ImagenesCount()
		var desc *string
		if descripcion != "" {
			desc = &descripcion
		}

		// Must not panic.
		_, err = pago.AdjuntarImagen(domain.AdjuntarImagenParams{
			ID:          uuid.New(),
			Storage:     st,
			Mime:        mime,
			SizeBytes:   sizeBytes,
			Descripcion: desc,
			By:          uuid.New(),
			Now:         time.Now().UTC(),
		})
		if err != nil {
			// Rejection is fine; pago count must not have changed.
			if pago.ImagenesCount() != before {
				t.Fatalf("image count changed after rejected AdjuntarImagen: before=%d after=%d", before, pago.ImagenesCount())
			}
			return
		}

		// Acceptance: count must have grown by exactly 1.
		after := pago.ImagenesCount()
		if after != before+1 {
			t.Fatalf("image count did not grow by 1: before=%d after=%d", before, after)
		}
		// New image must be reachable via the iterator.
		collected := slices.Collect(pago.Imagenes())
		if len(collected) != after {
			t.Fatalf("iterator length mismatch: got=%d want=%d", len(collected), after)
		}
	})
}

// FuzzMarcarAplicada_PanicFree exercises MarcarAplicada with arbitrary inputs.
// Builds one valid PagoRecibido (fixed), then calls MarcarAplicada with fuzzed
// (doctoCCID, impteDoctoCCID, folio). Must never panic.
func FuzzMarcarAplicada_PanicFree(f *testing.F) {
	// Seeds: boundary and pathological cases.
	f.Add(9001, 9002, "Z00042")
	f.Add(0, 9002, "Z00042")
	f.Add(-1, 9002, "Z00042")
	f.Add(9001, 0, "Z00042")
	f.Add(9001, -5, "Z00042")
	f.Add(9001, 9002, "")
	f.Add(9001, 9002, "   ")
	f.Add(9001, 9002, strings.Repeat("F", 1000))
	f.Add(1, 1, "F")

	f.Fuzz(func(t *testing.T, doctoCCID, impteDoctoCCID int, folio string) {
		// Build a valid base pago.
		base := domain.CrearPagoRecibidoParams{
			ID:             uuid.New(),
			CargoDoctoCCID: 1001,
			ClienteID:      2002,
			CobradorID:     3003,
			Cobrador:       "Ramírez García, Jorge",
			Importe:        decimal.NewFromInt(500),
			FormaCobroID:   1,
			FechaHoraPago:  time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
			CreatedBy:      uuid.New(),
			Now:            time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		}
		pago, err := domain.NewPagoRecibido(base)
		if err != nil {
			t.Fatalf("base pago setup failed: %v", err)
		}

		// Must never panic.
		err = pago.MarcarAplicada(doctoCCID, impteDoctoCCID, folio, time.Now().UTC(), uuid.New())
		if err != nil {
			// Rejection must leave pago pendiente.
			if !pago.IsPendiente() {
				t.Fatalf("pago must stay pendiente after failed MarcarAplicada")
			}
			return
		}

		// Acceptance invariants.
		if !pago.IsAplicada() {
			t.Fatalf("pago must be aplicada after successful MarcarAplicada")
		}
		if pago.DoctoCCID() == nil || *pago.DoctoCCID() != doctoCCID {
			t.Fatalf("DoctoCCID mismatch after MarcarAplicada")
		}
		if pago.ImpteDoctoCCID() == nil || *pago.ImpteDoctoCCID() != impteDoctoCCID {
			t.Fatalf("ImpteDoctoCCID mismatch after MarcarAplicada")
		}
		if pago.Folio() == nil || strings.TrimSpace(*pago.Folio()) == "" {
			t.Fatalf("Folio must be non-empty after successful MarcarAplicada")
		}
		if pago.AplicadoAt() == nil {
			t.Fatalf("AplicadoAt must be set after successful MarcarAplicada")
		}
	})
}
