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
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// baseParams returns a valid CrearPagoRecibidoParams without calling t.Helper()
// so it can be called from inside rapid.Check closures (which receive *rapid.T,
// not *testing.T). Individual tests mutate one field at a time to hit specific
// validation paths. For table-driven tests that need the *testing.T version,
// use validParams from pago_recibido_test.go instead.
func baseParams() domain.CrearPagoRecibidoParams {
	return domain.CrearPagoRecibidoParams{
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
}

// ─── Property 1: NewPagoRecibido accepts valid inputs ────────────────────────

// TestProperty_NewPagoRecibido_AcceptsValidInputs generates random valid params
// and asserts that NewPagoRecibido returns nil error and that all getters
// round-trip the inputs faithfully.
func TestProperty_NewPagoRecibido_AcceptsValidInputs(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// IDs: positive integers for external IDs.
		cargoID := rapid.IntRange(1, 1_000_000).Draw(t, "cargoID")
		clienteID := rapid.IntRange(1, 1_000_000).Draw(t, "clienteID")
		cobradorID := rapid.IntRange(1, 1_000_000).Draw(t, "cobradorID")
		formaCobroID := rapid.IntRange(1, 1_000_000).Draw(t, "formaCobroID")

		// Cobrador name: starts with a letter, optionally followed by letters/spaces.
		// The leading letter guarantees TrimSpace leaves at least one non-space char.
		cobrador := rapid.StringMatching(`[A-Za-záéíóúÁÉÍÓÚñÑ][A-Za-záéíóúÁÉÍÓÚñÑ ]{0,99}`).Draw(t, "cobrador")

		// Importe: positive value from cents [1, 9_999_999_999_999].
		cents := rapid.Int64Range(1, 9_999_999_999_999).Draw(t, "cents")
		importe := decimal.New(cents, -2)

		// FechaHoraPago: any timestamp (domain does not validate date range).
		now := time.Now().UTC()
		fechaHoraPago := now.Add(time.Duration(rapid.IntRange(-86400, 86400).Draw(t, "secOffset")) * time.Second)

		id := uuid.New()
		createdBy := uuid.New()

		p := domain.CrearPagoRecibidoParams{
			ID:             id,
			CargoDoctoCCID: cargoID,
			ClienteID:      clienteID,
			CobradorID:     cobradorID,
			Cobrador:       cobrador,
			Importe:        importe,
			FormaCobroID:   formaCobroID,
			FechaHoraPago:  fechaHoraPago,
			CreatedBy:      createdBy,
			Now:            now,
		}

		pago, err := domain.NewPagoRecibido(p)
		if err != nil {
			t.Fatalf("expected valid inputs to be accepted, got: %v", err)
		}

		// Round-trip assertions.
		if pago.ID() != id {
			t.Fatalf("ID mismatch")
		}
		if pago.CargoDoctoCCID() != cargoID {
			t.Fatalf("CargoDoctoCCID mismatch: got=%d want=%d", pago.CargoDoctoCCID(), cargoID)
		}
		if pago.ClienteID() != clienteID {
			t.Fatalf("ClienteID mismatch")
		}
		if pago.CobradorID() != cobradorID {
			t.Fatalf("CobradorID mismatch")
		}
		if pago.FormaCobroID() != formaCobroID {
			t.Fatalf("FormaCobroID mismatch")
		}
		if !pago.Importe().IsPositive() {
			t.Fatalf("Importe must be positive")
		}
		if !pago.IsPendiente() {
			t.Fatalf("new pago must start pendiente")
		}
		if pago.IsAplicada() {
			t.Fatalf("new pago must not be aplicada")
		}
		if pago.Intentos() != 0 {
			t.Fatalf("new pago must have zero intentos")
		}
	})
}

// ─── Property 2: DerivarConceptoCC always returns one of the two known IDs ──

// TestProperty_NewPagoRecibido_DerivarConcepto asserts that for any formaCobroID,
// the derived conceptoCCID is either 27969 (abono mostrador) or 87327 (cobranza
// ruta), and that 137026 is the only ID that maps to 27969.
func TestProperty_NewPagoRecibido_DerivarConcepto(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		formaCobroID := rapid.IntRange(-1_000_000, 1_000_000).Draw(t, "formaCobroID")
		got := domain.DerivarConceptoCC(formaCobroID)
		if got != 27969 && got != 87327 {
			t.Fatalf("DerivarConceptoCC(%d) = %d, want 27969 or 87327", formaCobroID, got)
		}
		if formaCobroID == 137026 && got != 27969 {
			t.Fatalf("DerivarConceptoCC(137026) = %d, want 27969", got)
		}
		if formaCobroID != 137026 && got != 87327 {
			t.Fatalf("DerivarConceptoCC(%d) = %d, want 87327", formaCobroID, got)
		}
	})
}

// ─── Property 3: MarcarAplicada is idempotent ────────────────────────────────

// TestProperty_MarcarAplicada_Idempotent builds a pendiente pago, applies it,
// then calls MarcarAplicada a second time and asserts ErrPagoYaAplicado is
// returned and state is unchanged.
func TestProperty_MarcarAplicada_Idempotent(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		pago, err := domain.NewPagoRecibido(baseParams())
		if err != nil {
			rt.Fatalf("setup failed: %v", err)
		}

		doctoCCID := rapid.IntRange(1, 9_999_999).Draw(rt, "doctoCCID")
		impteDoctoCCID := rapid.IntRange(1, 9_999_999).Draw(rt, "impteDoctoCCID")
		folio := rapid.StringMatching(`[A-Z][0-9]{1,10}`).Draw(rt, "folio")
		now := time.Now().UTC()
		by := uuid.New()

		// First apply must succeed.
		err = pago.MarcarAplicada(doctoCCID, impteDoctoCCID, folio, now, by)
		if err != nil {
			rt.Fatalf("first MarcarAplicada failed: %v", err)
		}
		if !pago.IsAplicada() {
			rt.Fatalf("pago must be aplicada after first MarcarAplicada")
		}
		if pago.Folio() == nil {
			rt.Fatalf("Folio must be non-nil after first MarcarAplicada")
		}
		firstFolio := *pago.Folio()

		// Second apply must return ErrPagoYaAplicado.
		err = pago.MarcarAplicada(doctoCCID+1, impteDoctoCCID+1, folio+"X", now, by)
		if err == nil {
			rt.Fatalf("second MarcarAplicada must return an error")
		}
		require.ErrorIs(t, err, domain.ErrPagoYaAplicado)

		// State and folio must be unchanged.
		if !pago.IsAplicada() {
			rt.Fatalf("pago must still be aplicada after second MarcarAplicada")
		}
		if pago.Folio() == nil || *pago.Folio() != firstFolio {
			rt.Fatalf("folio changed after failed second MarcarAplicada")
		}
	})
}

// ─── Property 4: RegistrarFallo increments intentos monotonically ───────────

// TestProperty_RegistrarFallo_MonotonicIntentos calls RegistrarFallo N times
// and asserts that intentos increases by exactly 1 after each call, and that
// the pago remains pendiente throughout.
func TestProperty_RegistrarFallo_MonotonicIntentos(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		pago, err := domain.NewPagoRecibido(baseParams())
		if err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		n := rapid.IntRange(1, 50).Draw(t, "n")
		now := time.Now().UTC()
		by := uuid.New()

		for i := range n {
			before := pago.Intentos()
			msg := rapid.StringMatching(`[a-z ]{1,100}`).Draw(t, "msg")
			pago.RegistrarFallo(msg, now, by)
			after := pago.Intentos()
			if after != before+1 {
				t.Fatalf("intentos did not increase by 1 at step %d: before=%d after=%d", i, before, after)
			}
			if !pago.IsPendiente() {
				t.Fatalf("pago must remain pendiente after RegistrarFallo at step %d", i)
			}
			if pago.UltimoError() == nil {
				t.Fatalf("UltimoError must be non-nil after RegistrarFallo at step %d", i)
			}
		}

		if pago.Intentos() != n {
			t.Fatalf("total intentos mismatch: got=%d want=%d", pago.Intentos(), n)
		}
	})
}

// ─── Property 5: RegistrarFallo truncates UltimoError at 500 runes ──────────

// TestProperty_RegistrarFallo_TruncatesError verifies that regardless of the
// input length, UltimoError() never exceeds 500 runes after RegistrarFallo.
func TestProperty_RegistrarFallo_TruncatesError(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		pago, err := domain.NewPagoRecibido(baseParams())
		if err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		// Draw strings of arbitrary length (0..2000 runes) using StringN.
		msg := rapid.StringN(0, 2000, 2000).Draw(t, "msg")
		pago.RegistrarFallo(msg, time.Now().UTC(), uuid.New())

		if pago.UltimoError() == nil {
			t.Fatalf("UltimoError must be non-nil after RegistrarFallo")
		}
		runeCount := utf8.RuneCountInString(*pago.UltimoError())
		if runeCount > 500 {
			t.Fatalf("UltimoError exceeds 500 runes: got %d", runeCount)
		}
	})
}

// ─── Property 6: AdjuntarImagen rejects bad MIME types ──────────────────────

// TestProperty_AdjuntarImagen_RejectsBadMime draws MIME strings that do NOT
// match the whitelist and asserts ErrMimeNoPermitido is returned.
func TestProperty_AdjuntarImagen_RejectsBadMime(t *testing.T) {
	t.Parallel()

	allowed := map[string]bool{
		"image/jpeg":      true,
		"image/png":       true,
		"image/gif":       true,
		"image/webp":      true,
		"application/pdf": true,
	}

	rapid.Check(t, func(rt *rapid.T) {
		mime := rapid.StringMatching(`[a-z]+/[a-z][a-z0-9]*`).Draw(rt, "mime")
		if allowed[mime] {
			rt.Skip() // happened to draw a valid MIME — skip iteration.
		}

		pago, err := domain.NewPagoRecibido(baseParams())
		if err != nil {
			rt.Fatalf("setup failed: %v", err)
		}
		st, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "pagos/test/img.jpg")
		if err != nil {
			rt.Fatalf("storage setup failed: %v", err)
		}

		_, err = pago.AdjuntarImagen(domain.AdjuntarImagenParams{
			ID:        uuid.New(),
			Storage:   st,
			Mime:      mime,
			SizeBytes: 1024,
			By:        uuid.New(),
			Now:       time.Now().UTC(),
		})
		require.ErrorIs(t, err, domain.ErrMimeNoPermitido)
	})
}

// ─── Property 7: Importe decimal precision round-trips bit-exact ─────────────

// TestProperty_Importe_DecimalPrecision builds a PagoRecibido with an importe
// derived from random cents and asserts that the getter returns the exact
// decimal.Decimal value with exponent >= -2.
func TestProperty_Importe_DecimalPrecision(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// NUMERIC(14,2) — max 99_999_999_999_999 cents.
		cents := rapid.Int64Range(1, 99_999_999_999_999).Draw(t, "cents")
		importe := decimal.New(cents, -2)

		p := baseParams()
		p.Importe = importe

		pago, err := domain.NewPagoRecibido(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !pago.Importe().Equal(importe) {
			t.Fatalf("Importe round-trip failed: in=%s out=%s", importe, pago.Importe())
		}
		if pago.Importe().Exponent() < -2 {
			t.Fatalf("Importe exponent too small: %d", pago.Importe().Exponent())
		}
	})
}

// ─── Property 8: AdjuntarImagen assigns unique IDs ───────────────────────────

// TestProperty_AdjuntarImagen_AssignsUniqueIDs adjuntas N images (N drawn 1-10)
// to the same pago and verifies all image IDs are distinct.
func TestProperty_AdjuntarImagen_AssignsUniqueIDs(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		pago, err := domain.NewPagoRecibido(baseParams())
		if err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		n := rapid.IntRange(1, 10).Draw(t, "n")
		st, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "pagos/test/img.jpg")
		if err != nil {
			t.Fatalf("storage setup failed: %v", err)
		}

		for range n {
			_, err := pago.AdjuntarImagen(domain.AdjuntarImagenParams{
				ID:        uuid.New(),
				Storage:   st,
				Mime:      domain.MimeJPEG,
				SizeBytes: 1024,
				By:        uuid.New(),
				Now:       time.Now().UTC(),
			})
			if err != nil {
				t.Fatalf("AdjuntarImagen failed: %v", err)
			}
		}

		// Collect IDs via the iter.Seq iterator.
		collected := slices.Collect(pago.Imagenes())
		if len(collected) != n {
			t.Fatalf("image count mismatch: got=%d want=%d", len(collected), n)
		}

		seen := make(map[uuid.UUID]bool, n)
		for _, img := range collected {
			if seen[img.ID()] {
				t.Fatalf("duplicate image ID: %s", img.ID())
			}
			seen[img.ID()] = true
		}
	})
}

// ─── Property 9: NewPagoRecibido rejects non-positive importe ────────────────

// TestProperty_NewPagoRecibido_ImporteNonPositive_Rejected generates decimal
// values <= 0 and asserts that NewPagoRecibido returns ErrPagoImporteInvalido.
func TestProperty_NewPagoRecibido_ImporteNonPositive_Rejected(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		cents := rapid.Int64Range(-1_000_000, 0).Draw(rt, "cents")
		importe := decimal.New(cents, -2)

		p := baseParams()
		p.Importe = importe

		pago, err := domain.NewPagoRecibido(p)
		if pago != nil {
			rt.Fatalf("expected nil pago for non-positive importe")
		}
		require.ErrorIs(t, err, domain.ErrPagoImporteInvalido)
	})
}

// ─── Property 10: NewImagenStorage rejects unsafe path patterns ──────────────

// TestProperty_StorageKey_NoTraversal generates path-like strings and verifies
// that keys containing "..", leading "/", or NUL bytes are rejected by
// NewImagenStorage, while clean relative paths are accepted.
func TestProperty_StorageKey_NoTraversal(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Build a path from 1-10 segments.
		segments := rapid.SliceOfN(rapid.StringMatching(`[A-Za-z0-9_.]{1,20}`), 1, 10).Draw(t, "segments")
		key := strings.Join(segments, "/")

		// Determine whether the key is expected to be safe.
		containsDotDot := strings.Contains(key, "..")
		startsWithSlash := strings.HasPrefix(key, "/")
		containsNUL := strings.ContainsRune(key, 0)

		_, err := domain.NewImagenStorage(domain.StorageKindFilesystem, key)
		if containsDotDot || startsWithSlash || containsNUL {
			if err == nil {
				t.Fatalf("expected error for unsafe key %q", key)
			}
		} else {
			if err != nil {
				t.Fatalf("expected no error for safe key %q: %v", key, err)
			}
		}
	})
}
