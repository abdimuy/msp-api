//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp_test

import (
	"bytes"
	"mime/multipart"
	"net/textproto"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// crearPagoImagen describes a single comprobante to add to a multipart
// CrearPago request. Mime + filename feed the Content-Type header so Huma
// accepts the part; ID/Descripcion become the positional id_<n>/descripcion_<n>
// text fields. Body is the raw bytes the server stores.
type crearPagoImagen struct {
	Filename    string
	Mime        string
	Body        []byte
	ID          string // optional; "" means do not send id_<n>
	Descripcion string // optional; "" means do not send descripcion_<n>
}

// buildCrearPagoMultipart serializes the multipart/form-data body the new
// POST /pagos endpoint expects. Returns the body bytes + Content-Type header
// the caller must set on the request.
//
// datos becomes the `datos` JSON field; each imagen becomes one repeated
// `imagen` file part plus optional `id_<n>` / `descripcion_<n>` text fields.
func buildCrearPagoMultipart(t *testing.T, datos string, imagenes []crearPagoImagen) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// datos field — plain text JSON.
	require.NoError(t, mw.WriteField("datos", datos))

	// imagen files — one part per upload.
	for i, img := range imagenes {
		hdr := make(textproto.MIMEHeader)
		hdr.Set("Content-Disposition",
			`form-data; name="imagen"; filename="`+img.Filename+`"`)
		hdr.Set("Content-Type", img.Mime)
		fw, err := mw.CreatePart(hdr)
		require.NoError(t, err)
		_, err = fw.Write(img.Body)
		require.NoError(t, err)

		if img.ID != "" {
			require.NoError(t, mw.WriteField("id_"+strconv.Itoa(i), img.ID))
		}
		if img.Descripcion != "" {
			require.NoError(t, mw.WriteField("descripcion_"+strconv.Itoa(i), img.Descripcion))
		}
	}

	require.NoError(t, mw.Close())
	return &buf, mw.FormDataContentType()
}

// crearPagoDatosJSON builds the JSON payload for the `datos` field with the
// given pago_id and fecha_hora_pago. Other fields use happy-path defaults
// consistent with the test fixtures (cargo 5000, saldo 2000, importe 1500).
func crearPagoDatosJSON(pagoID, fechaRFC3339 string) string {
	return `{"id":"` + pagoID + `",` +
		`"cargo_docto_cc_id":5000,` +
		`"cliente_id":11486,` +
		`"cobrador_id":200,` +
		`"cobrador":"Mendoza Torres, Ana",` +
		`"importe":"1500.00",` +
		`"forma_cobro_id":1,` +
		`"fecha_hora_pago":"` + fechaRFC3339 + `"}`
}
