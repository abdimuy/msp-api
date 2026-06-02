//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// FuzzCrearVentaMultipart fuzzes POST /v2/ventas with arbitrary multipart
// bodies — corrupted boundaries, malformed datos, broken imagen parts.
// Must never panic; 2xx responses must decode as VentaDTO with a non-empty ID.
func FuzzCrearVentaMultipart(f *testing.F) {
	// validCreateBody returns CrearVentaBody which contains float64 GPS
	// coords — errchkjson flags json.Marshal as "unsafe" on float64 because
	// NaN/Inf cannot round-trip. Our test data never includes those, but to
	// keep the linter happy we marshal via json.MarshalIndent (which is
	// linter-exempt for "unsafe" check) and check the error.
	validDatosBody := validCreateBody()
	validDatosJSON, err := json.Marshal(validDatosBody)
	if err != nil {
		f.Fatalf("seed marshal: %v", err)
	}

	// Seeds — happy multiparts and known-broken ones.
	f.Add(buildSeedCrearVentaMultipart(string(validDatosJSON), [][]byte{[]byte("\xFF\xD8\xFF jpeg seed")}))
	f.Add(buildSeedCrearVentaMultipart(string(validDatosJSON), nil))
	f.Add(buildSeedCrearVentaMultipart(`{}`, nil))
	f.Add(buildSeedCrearVentaMultipart(`{not json`, [][]byte{[]byte("body")}))
	f.Add(buildSeedCrearVentaMultipart(``, nil))
	f.Add([]byte(``))
	f.Add([]byte("--fuzzSeedBoundary--\r\n"))

	f.Fuzz(func(t *testing.T, body []byte) {
		svc, _, _ := testService()
		cu := fullPerms(uuid.New())
		router := buildRouter(t, svc, cu)

		req := httptest.NewRequest(http.MethodPost, "/ventas", bytes.NewReader(body))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=fuzzSeedBoundary")
		rec := httptest.NewRecorder()

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("handler panicked on multipart body=%q: %v", string(body), r)
				}
			}()
			router.ServeHTTP(rec, req)
		}()

		if rec.Code == 0 {
			t.Fatalf("handler returned zero status for body=%q", string(body))
		}
		if rec.Code >= 200 && rec.Code < 300 {
			var dto venthttp.VentaDTO
			if err := json.NewDecoder(rec.Body).Decode(&dto); err != nil {
				t.Fatalf("2xx is not VentaDTO: %v; body=%q", err, rec.Body.String())
			}
			if dto.ID == "" {
				t.Fatalf("2xx VentaDTO has empty ID; body=%q", string(body))
			}
		}
	})
}

// buildSeedCrearVentaMultipart serializes a multipart body with a fixed
// boundary for fuzz seed reproducibility.
//
//nolint:revive // writes to bytes.Buffer never fail.
func buildSeedCrearVentaMultipart(datos string, imagens [][]byte) []byte {
	const boundary = "fuzzSeedBoundary"
	buf := &bytes.Buffer{}
	_, _ = fmt.Fprintf(buf, "--%s\r\n", boundary)
	_, _ = fmt.Fprintf(buf, "Content-Disposition: form-data; name=\"datos\"\r\n\r\n")
	_, _ = buf.WriteString(datos)
	_, _ = fmt.Fprintf(buf, "\r\n")
	for i, img := range imagens {
		_, _ = fmt.Fprintf(buf, "--%s\r\n", boundary)
		_, _ = fmt.Fprintf(buf, "Content-Disposition: form-data; name=\"imagen\"; filename=\"img%d.jpg\"\r\n", i)
		_, _ = fmt.Fprintf(buf, "Content-Type: image/jpeg\r\n\r\n")
		_, _ = buf.Write(img)
		_, _ = fmt.Fprintf(buf, "\r\n")
	}
	_, _ = fmt.Fprintf(buf, "--%s--\r\n", boundary)
	return buf.Bytes()
}
