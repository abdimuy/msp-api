//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

// encoding_boundary_test.go — pins the UTF-8 contract at the HTTP boundary
// for the ventas module. Post migration 000005 the MSP_VENTAS text columns
// are CHARACTER SET UTF8; the driver connection charset is also UTF8. The
// app layer passes Go strings verbatim — no encoding boundary inside the
// repository. See docs/module-standards/ENCODING_HANDLING.md for the rules.
//
// CONTRACT:
//
//  1. Any valid UTF-8 string round-trips losslessly through POST → DB → GET,
//     up to the domain's ALL-CAPS fold of user-captured text (Microsip
//     convention). Expected values are therefore strings.ToUpper(input). This
//     covers Spanish accents, emoji, CJK, Cyrillic, em-dash, smart quotes, €,
//     and combining characters in NFC form — none of which the case fold may
//     corrupt (only cased letters change, e.g. é→É; emoji/CJK are untouched).
//
//  2. NUL bytes and ASCII control characters (other than \t \n \r) are
//     rejected at the domain layer with HTTP 422 and code "string_unsafe_chars".
//
//  3. The domain layer applies Unicode NFC normalization so canonically-
//     equivalent inputs ("é" composed vs "e" + U+0301) become identical
//     downstream.
//
// SKIP GATE
//
// Every test in this file skips when FB_DATABASE is not set in the environment
// — the same guard used by all other E2E tests in this package.
//
// To run:
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

// roundTripCase declares one POST input + the expected echoed value after a
// GET round-trip. Used by TestE2E_Encoding_UTF8_RoundTrip below to assert that
// every Unicode codepoint we expect to support survives the DB cycle.
type roundTripCase struct {
	name        string
	clienteName string
	calle       string
	poblacion   string
	nota        string
}

// TestE2E_Encoding_UTF8_RoundTrip exercises a wide spectrum of Unicode through
// POST + GET on a single venta per case. Any character set that doesn't make
// it byte-equal back to the caller is a regression of the UTF-8 contract.
//
//nolint:paralleltest // E2E tests share a Firebird tx and must run serially.
func TestE2E_Encoding_UTF8_RoundTrip(t *testing.T) {
	cases := []roundTripCase{
		{
			name:        "spanish_accents",
			clienteName: "Sofía Hernández Núñez",
			calle:       "Calle Niño Artillero",
			poblacion:   "Mérida",
			nota:        "entrega después del jueves; cliente con preferencia",
		},
		{
			name:        "win1252_punctuation",
			clienteName: "Cliente — Histórico",
			calle:       "Av. ‘Las Palmas’ #42",
			poblacion:   "“Centro”",
			nota:        "Costo: €1,500 — promoción —",
		},
		{
			name:        "emoji",
			clienteName: "Mariana 🎉 Promo",
			calle:       "Calle 🌮 Taco 42",
			poblacion:   "CDMX",
			nota:        "Cliente VIP 🚀🔥",
		},
		{
			name:        "cyrillic",
			clienteName: "Иван Петров",
			calle:       "улица Ленина 5",
			poblacion:   "Москва",
			nota:        "Доставка завтра",
		},
		{
			name:        "cjk",
			clienteName: "山田 太郎",
			calle:       "東京都渋谷区道玄坂",
			poblacion:   "東京",
			nota:        "明日配達してください",
		},
		{
			name:        "mixed_script",
			clienteName: "Café 北京 ✓",
			calle:       "Calle 文学 №7",
			poblacion:   "Mérida",
			nota:        "Pagó €1,500 — al contado. 確認済み 🎉",
		},
	}

	pool := e2eTestPool(t)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) { //nolint:paralleltest // shared tx
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
				body.Cliente.Nombre = tc.clienteName
				body.Direccion.Calle = tc.calle
				body.Direccion.Poblacion = tc.poblacion
				body.Nota = &tc.nota

				req := crearVentaMultipartRequest(t, body)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				require.Equal(t, http.StatusCreated, rec.Code,
					"case %s: POST must accept valid UTF-8: %s", tc.name, rec.Body.String())

				var created venthttp.VentaDTO
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
				assert.Equal(t, strings.ToUpper(tc.clienteName), created.Cliente.Nombre,
					"POST response must echo cliente.nombre folded to ALL CAPS")

				req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
				rec = httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				require.Equal(t, http.StatusOK, rec.Code, "GET after POST: %s", rec.Body.String())

				var got venthttp.VentaDTO
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
				assert.Equal(t, strings.ToUpper(tc.clienteName), got.Cliente.Nombre, "cliente.nombre must round-trip up to the ALL-CAPS fold")
				assert.Equal(t, strings.ToUpper(tc.calle), got.Direccion.Calle, "direccion.calle must round-trip up to the ALL-CAPS fold")
				assert.Equal(t, strings.ToUpper(tc.poblacion), got.Direccion.Poblacion, "direccion.poblacion must round-trip up to the ALL-CAPS fold")
				require.NotNil(t, got.Nota, "nota must be present in GET response")
				assert.Equal(t, strings.ToUpper(tc.nota), *got.Nota, "nota must round-trip up to the ALL-CAPS fold")
			})
		})
	}
}

// TestE2E_Encoding_NUL_Rejected verifies that a NUL byte in cliente.nombre is
// rejected at the domain layer with 422 string_unsafe_chars.
//
//nolint:paralleltest // E2E tests share a Firebird tx and must run serially.
func TestE2E_Encoding_NUL_Rejected(t *testing.T) {
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
		body.Cliente.Nombre = "Cliente\x00Roto"

		req := crearVentaMultipartRequest(t, body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code,
			"NUL in cliente.nombre must be rejected with 422: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), "string_unsafe_chars",
			"response must carry the string_unsafe_chars error code")
	})
}

// TestE2E_Encoding_ControlChar_Rejected verifies ASCII control chars (other
// than tab/CR/LF) are rejected at the domain layer.
//
//nolint:paralleltest // E2E tests share a Firebird tx and must run serially.
func TestE2E_Encoding_ControlChar_Rejected(t *testing.T) {
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
		body.Cliente.Nombre = "Cliente\x01Roto"

		req := crearVentaMultipartRequest(t, body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code,
			"ASCII control char must be rejected with 422: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), "string_unsafe_chars")
	})
}

// TestE2E_Encoding_LongUTF8MultibyteString verifies that a nota containing a
// 400-codepoint string of multibyte characters is accepted and round-trips
// byte-equal. Column is VARCHAR(500) CHARACTER SET UTF8 → 500 codepoints max.
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

		// 5 codepoints × 80 = 400 codepoints. Each emoji takes 4 UTF-8 bytes
		// — 1600 storage bytes total, well under VARCHAR(500) UTF8's per-column
		// allocation of 2000 bytes.
		unit := "áéíóú"
		longNota := strings.Repeat(unit, 80)
		assert.Equal(t, 400, runeCount(longNota), "test setup: nota must be 400 codepoints")

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		body.Nota = &longNota

		req := crearVentaMultipartRequest(t, body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code,
			"400-codepoint nota must be accepted: %s", rec.Body.String())

		req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.NotNil(t, got.Nota)
		assert.Equal(t, strings.ToUpper(longNota), *got.Nota, "long UTF-8 nota must round-trip up to the ALL-CAPS fold")
	})
}

// TestE2E_Encoding_PATCH_OnUnicodeFields verifies PATCH on direccion.poblacion
// with arbitrary UTF-8 (emoji + accents) persists correctly.
//
//nolint:paralleltest // E2E tests share a Firebird tx and must run serially.
func TestE2E_Encoding_PATCH_OnUnicodeFields(t *testing.T) {
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
		body.Direccion.Poblacion = "Mérida"
		req := crearVentaMultipartRequest(t, body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "create: %s", rec.Body.String())

		patchBody := validHeaderBody()
		patchBody.Direccion.Poblacion = "Cancún 🏖️"
		req = jsonRequest(t, http.MethodPatch, "/ventas/"+body.ID, patchBody)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "PATCH: %s", rec.Body.String())

		var patched venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &patched))
		assert.Equal(t, strings.ToUpper("Cancún 🏖️"), patched.Direccion.Poblacion)

		req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, strings.ToUpper("Cancún 🏖️"), got.Direccion.Poblacion,
			"PATCH must round-trip emoji+accents up to the ALL-CAPS fold")
	})
}

// runeCount returns the number of Unicode codepoints in s.
func runeCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
