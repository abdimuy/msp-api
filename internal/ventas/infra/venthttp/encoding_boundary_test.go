//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

// encoding_boundary_test.go — documents the Win1252 encoding boundary at the
// HTTP layer for the ventas module.
//
// BACKGROUND
//
// Microsip's Firebird database stores text in columns declared as CHARACTER SET
// ISO8859_1 (or NONE), but the Delphi client wrote Windows-1252 bytes.
// Our Go layer converts UTF-8 ↔ Win1252 at two barriers:
//
//  1. Domain layer (safe_string.go) — validateSafeChars rejects any rune that
//     cannot be encoded as Windows-1252 (emoji, CJK, Cyrillic, …) with a
//     typed apperror code "string_unsafe_chars", which the HTTP middleware
//     surfaces as a 422 Unprocessable Entity.
//
//  2. Repo layer (ventfb/venta_repo.go) — EncodeWin1252 / EncodeWin1252Ptr
//     convert the already-validated UTF-8 string to raw bytes before binding
//     them to the Firebird driver.
//
// Because domain validation runs first, the repo's Win1252 encoding for text
// fields is a defence-in-depth step rather than the first rejection point. The
// tests below pin this behaviour end-to-end through the full HTTP → app →
// ventfb → Firebird stack inside a rollback-only test transaction.
//
// SKIP GATE
//
// Every test in this file skips when FB_DATABASE is not set in the environment
// — the same guard used by all other E2E tests in this package (see
// e2eTestPool in e2e_firebird_test.go).
//
// To run the encoding E2E tests:
//
//	FB_DATABASE=/path/to/microsip.fdb \
//	  go test -count=1 -v -run TestE2E_Encoding ./internal/ventas/infra/venthttp/...

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// ─── Test cases ──────────────────────────────────────────────────────────────

// TestE2E_Encoding_SpanishAccentsRoundTrip verifies that the most common
// accented characters used in Mexican customer names and addresses survive the
// full round-trip: HTTP POST → ventfb repo (Win1252 encode) → Firebird →
// ventfb repo (Win1252 decode) → HTTP GET response.
//
// These characters are all in Windows-1252 and must reach the caller
// byte-for-byte identical to what was submitted.
//
//nolint:paralleltest // E2E tests share a Firebird tx and must run serially.
func TestE2E_Encoding_SpanishAccentsRoundTrip(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		cu := e2eFullPermsUser(usuarioID)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		// All of the following are WIN1252-representable:
		// á é í ó ú ü ñ Ñ Á É Í Ó Ú Ü — every accent in everyday Mexican Spanish.
		body.Cliente.Nombre = "Sofía Hernández"
		body.Direccion.Calle = "Calle Niño Artillero"
		body.Direccion.Colonia = "Fracc. Mérida"
		body.Direccion.Poblacion = "Mérida"
		body.Direccion.Ciudad = "Mérida"

		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code,
			"WIN1252-representable Spanish accents must create successfully: %s", rec.Body.String())

		var created venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

		// Round-trip GET — every accent must survive the DB encode/decode cycle.
		req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "GET after create: %s", rec.Body.String())

		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, "Sofía Hernández", got.Cliente.Nombre,
			"accented nombre must round-trip exactly through Firebird")
		assert.Equal(t, "Calle Niño Artillero", got.Direccion.Calle,
			"accented calle must round-trip exactly")
		assert.Equal(t, "Mérida", got.Direccion.Poblacion,
			"accented poblacion must round-trip exactly")
	})
}

// TestE2E_Encoding_EuroSign verifies the € character (U+20AC, WIN1252 byte
// 0x80). The euro sign is in Windows-1252 but NOT in ISO8859-1, so whether
// Firebird accepts or rejects it depends on the actual charset declared on the
// column. On a strict ISO8859_1 column the driver may reject it at write time
// with a transliteration error. The domain validateSafeChars passes it (€ is
// WIN1252-representable) so rejection — if it occurs — surfaces as a 5xx
// firebird_error from the repo layer.
//
// CURRENT BEHAVIOR: TBD — run with FB_DATABASE set to observe.
// If Firebird rejects: expect 500 (no boundary validation for the euro sign at
// the HTTP/domain layer — it is WIN1252-representable). Ideal behavior would
// be to detect ISO8859-1 vs WIN1252 mismatch and reject with 422.
//
//nolint:paralleltest // E2E tests share a Firebird tx and must run serially.
func TestE2E_Encoding_EuroSign(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		cu := e2eFullPermsUser(usuarioID)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		// € is U+20AC, WIN1252 byte 0x80 — NOT representable in ISO8859-1
		// (0x80–0x9F are control characters in ISO8859-1).
		body.Cliente.Nombre = "Pago €1500"

		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		// The domain layer (validateSafeChars via charmap.Windows1252) accepts
		// the euro sign because it is WIN1252-encodable. Whether Firebird's
		// ISO8859_1 column rejects it at write time is DB-specific.
		// We only assert the server did NOT panic: the response must be a
		// well-formed HTTP status code.
		assert.NotEqual(t, 0, rec.Code, "server must respond (no panic)")
		if rec.Code == http.StatusCreated {
			t.Logf("FINDING: euro sign accepted by Firebird column — WIN1252 byte 0x80 persisted")
		} else {
			t.Logf("FINDING: euro sign rejected (status=%d) — "+
				"consider whether the column should accept WIN1252 superset or not. body=%s",
				rec.Code, rec.Body.String())
		}
		// Must not be an unhandled panic.
		assert.Less(t, rec.Code, 600, "response must be a valid HTTP status")
	})
}

// TestE2E_Encoding_EmojiRejected verifies that a cliente nombre containing an
// emoji (🎉) is rejected before reaching the Firebird repo.
//
// CURRENT BEHAVIOR: 422 Unprocessable Entity with code "string_unsafe_chars".
// The domain layer (validateSafeChars) rejects any rune outside WIN1252 at
// venta creation time — the repo's EncodeWin1252 is never reached.
// This IS the ideal behavior.
//
//nolint:paralleltest // E2E tests share a Firebird tx and must run serially.
func TestE2E_Encoding_EmojiRejected(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		cu := e2eFullPermsUser(usuarioID)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		body.Cliente.Nombre = "Cliente 🎉"

		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		// Domain validation rejects the emoji with 422 string_unsafe_chars
		// — this is the correct, ideal behavior.
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code,
			"emoji in cliente.nombre must be rejected with 422 (not WIN1252-representable): %s",
			rec.Body.String())
		assert.Contains(t, rec.Body.String(), "string_unsafe_chars",
			"response must carry the string_unsafe_chars error code")
		t.Logf("CONFIRMED: emoji surfaces as 422 string_unsafe_chars — boundary validation works correctly")
	})
}

// TestE2E_Encoding_CyrillicRejected verifies that Cyrillic text in cliente
// nombre (e.g., "Иван Петров") is rejected before reaching Firebird.
//
// CURRENT BEHAVIOR: 422 Unprocessable Entity with code "string_unsafe_chars".
// Cyrillic runes (U+0410–U+044F) are outside Windows-1252; the domain layer
// rejects them at the string VO constructor before any DB interaction.
//
//nolint:paralleltest // E2E tests share a Firebird tx and must run serially.
func TestE2E_Encoding_CyrillicRejected(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		cu := e2eFullPermsUser(usuarioID)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		body.Cliente.Nombre = "Иван Петров"

		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		// Must not succeed — Cyrillic has no mapping in Windows-1252.
		assert.NotEqual(t, http.StatusCreated, rec.Code,
			"Cyrillic text must NOT be accepted as cliente.nombre")
		assert.Less(t, rec.Code, 500,
			"Cyrillic rejection must be a clean 4xx (not a 5xx panic): body=%s", rec.Body.String())
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code,
			"Cyrillic must be rejected at domain layer with 422: body=%s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), "string_unsafe_chars",
			"response body must carry the string_unsafe_chars error code")
	})
}

// TestE2E_Encoding_ChineseRejected verifies that CJK text (e.g., "山田太郎")
// in cliente nombre is rejected before reaching Firebird.
//
// CURRENT BEHAVIOR: 422 Unprocessable Entity with code "string_unsafe_chars".
// CJK characters (U+4E00–U+9FFF) are outside Windows-1252; domain validation
// rejects them at the string VO level.
//
//nolint:paralleltest // E2E tests share a Firebird tx and must run serially.
func TestE2E_Encoding_ChineseRejected(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		cu := e2eFullPermsUser(usuarioID)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		body.Cliente.Nombre = "山田太郎"

		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		// Must not succeed — CJK has no mapping in Windows-1252.
		assert.NotEqual(t, http.StatusCreated, rec.Code,
			"CJK text must NOT be accepted as cliente.nombre")
		assert.Less(t, rec.Code, 500,
			"CJK rejection must be a clean 4xx (not a 5xx panic): body=%s", rec.Body.String())
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code,
			"CJK text must be rejected at domain layer with 422: body=%s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), "string_unsafe_chars",
			"response body must carry the string_unsafe_chars error code")
	})
}

// TestE2E_Encoding_LongUTF8MultibyteString verifies that a nota containing
// 400 valid Latin-1 / WIN1252 characters (áéíóú repeated) is accepted and
// persists correctly. This is close to — but below — a typical VARCHAR(500)
// limit and tests that multibyte UTF-8 encoding does NOT corrupt the byte-count
// check (each of those chars is 2 bytes in UTF-8 but 1 byte in WIN1252).
//
// CURRENT BEHAVIOR: 201 Created; nota round-trips exactly.
//
//nolint:paralleltest // E2E tests share a Firebird tx and must run serially.
func TestE2E_Encoding_LongUTF8MultibyteString(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		cu := e2eFullPermsUser(usuarioID)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		// Build a 400-rune nota from accented Latin chars.
		// Each of áéíóú is one rune (one WIN1252 byte), so 400 runes = 400 DB bytes.
		unit := "áéíóú" // 5 runes × 80 = 400 runes total
		longNota := strings.Repeat(unit, 80)
		assert.Len(t, []rune(longNota), 400, "test setup: nota must be exactly 400 runes")

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		body.Nota = &longNota

		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code,
			"400-char accented nota must be accepted: %s", rec.Body.String())

		// Round-trip GET — the full string must survive the Win1252 encode/decode.
		req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.NotNil(t, got.Nota, "nota must be present in the GET response")
		assert.Equal(t, longNota, *got.Nota,
			"400-char accented nota must round-trip byte-for-byte through Firebird")
	})
}

// TestE2E_Encoding_PATCH_OnAccentedFields verifies that PATCH /ventas/{id}
// updating direccion.poblacion from "Mérida" to "Cancún" persists correctly
// and the new value round-trips through the HTTP GET response.
//
// CURRENT BEHAVIOR: 200 OK; updated poblacion survives the Win1252 cycle.
//
//nolint:paralleltest // E2E tests share a Firebird tx and must run serially.
func TestE2E_Encoding_PATCH_OnAccentedFields(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		cu := e2eFullPermsUser(usuarioID)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		// Create with "Mérida".
		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		body.Direccion.Poblacion = "Mérida"
		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "create: %s", rec.Body.String())

		// PATCH — update poblacion to "Cancún".
		patchBody := validHeaderBody()
		patchBody.Direccion.Poblacion = "Cancún"
		req = jsonRequest(t, http.MethodPatch, "/ventas/"+body.ID, patchBody)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code,
			"PATCH with accented poblacion must succeed: %s", rec.Body.String())

		var patched venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &patched))
		assert.Equal(t, "Cancún", patched.Direccion.Poblacion,
			"PATCH response must carry the new accented poblacion immediately")

		// GET round-trip — value must persist to the DB.
		req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "GET after PATCH: %s", rec.Body.String())

		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, "Cancún", got.Direccion.Poblacion,
			"PATCH accented poblacion must round-trip from Firebird exactly")
	})
}

// TestE2E_Encoding_UnencodableInPATCH verifies that PATCH /ventas/{id}/cliente
// with a rocket emoji in nombre is rejected cleanly — the same domain
// validation that guards POST also guards PATCH.
//
// CURRENT BEHAVIOR: 422 Unprocessable Entity with code "string_unsafe_chars".
// The domain's ActualizarCliente call runs validateSafeChars on the new nombre
// before any SQL is executed, so the rejection is clean and no partial write
// occurs.
//
//nolint:paralleltest // E2E tests share a Firebird tx and must run serially.
func TestE2E_Encoding_UnencodableInPATCH(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		cu := e2eFullPermsUser(usuarioID)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		// Seed a valid venta first.
		id := e2eSeedVenta(t, r, usuarioID)

		// PATCH /ventas/{id}/cliente — nombre contains a rocket emoji.
		patchBody := venthttp.ActualizarClienteBody{
			Cliente: venthttp.ClienteSnapshotDTO{Nombre: "Cliente 🚀"},
		}
		req := jsonRequest(t, http.MethodPatch, "/ventas/"+id+"/cliente", patchBody)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		// Must NOT succeed — emoji is outside WIN1252.
		assert.NotEqual(t, http.StatusOK, rec.Code,
			"PATCH with emoji nombre must NOT return 200")
		assert.Less(t, rec.Code, 500,
			"PATCH emoji rejection must be 4xx (not 5xx panic): body=%s", rec.Body.String())
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code,
			"PATCH emoji must be rejected at domain layer with 422: body=%s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), "string_unsafe_chars",
			"response must carry string_unsafe_chars error code")
		t.Logf(
			"CONFIRMED: PATCH with emoji surfaces as 422 string_unsafe_chars — " +
				"domain validation guards both POST and PATCH paths correctly",
		)
	})
}
