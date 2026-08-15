//nolint:misspell // Spanish vocabulary (ciudad, catálogo) per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// fakeCiudadCatalogo resolves by exact normalized key.
type fakeCiudadCatalogo struct {
	byKey map[string]outbound.CiudadResuelta
	err   error
	calls int
}

func (f *fakeCiudadCatalogo) Resolver(_ context.Context, nombre string) (*outbound.CiudadResuelta, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if c, ok := f.byKey[domain.NormalizeCiudad(nombre)]; ok {
		return &c, nil
	}
	return nil, nil
}

// seedAprobadaSinClienteMicrosip builds an approved CONTADO venta with NO
// ClienteID, so AplicarVenta takes the cliente auto-create branch.
func seedAprobadaSinClienteMicrosip(t *testing.T, h *testHarness, ciudad string) uuid.UUID {
	t.Helper()
	in := validContadoInput()
	in.ClienteID = nil
	zona := 21563
	in.ZonaClienteID = &zona
	in.Ciudad = ciudad
	by := uuid.New()

	v, err := h.svc.CrearVenta(t.Context(), in, by)
	require.NoError(t, err)
	seedOneEvidencia(t, h, v.ID(), by)
	_, err = h.svc.EnviarARevision(t.Context(), v.ID(), by)
	require.NoError(t, err)
	_, err = h.svc.Aprobar(t.Context(), v.ID(), by)
	require.NoError(t, err)
	return v.ID()
}

// Flag OFF: the fixed Tehuacán/Puebla defaults are written, as before. The
// catalog is never consulted.
func TestAplicarVenta_CiudadCatalogoOff_EscribeLosDefaults(t *testing.T) {
	t.Parallel()
	h, _, _, clienteWriter := newAplicarHarness(t)
	cat := &fakeCiudadCatalogo{}
	h.svc.WithCiudadCatalogo(cat, false)

	id := seedAprobadaSinClienteMicrosip(t, h, "Coyomeapan")
	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())
	require.NoError(t, err)

	in := clienteWriter.LastIn
	assert.Equal(t, outbound.DefaultCiudadID, in.CiudadID)
	assert.Equal(t, outbound.DefaultEstadoID, in.EstadoID)
	assert.Zero(t, cat.calls, "the catalog must not be consulted while the flag is off")
}

// Flag ON: the captured ciudad is resolved, and the estado comes from the SAME
// catalog row — not from the fixed default.
func TestAplicarVenta_CiudadCatalogoOn_ResuelveCiudadYEstado(t *testing.T) {
	t.Parallel()
	h, _, _, clienteWriter := newAplicarHarness(t)
	h.svc.WithCiudadCatalogo(&fakeCiudadCatalogo{
		byKey: map[string]outbound.CiudadResuelta{
			// A ciudad in Oaxaca, deliberately not Puebla.
			"SANTIAGO CHAZUMBA": {CiudadID: 11827, EstadoID: 11523},
		},
	}, true)

	id := seedAprobadaSinClienteMicrosip(t, h, "Santiago Chazumba")
	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())
	require.NoError(t, err)

	in := clienteWriter.LastIn
	assert.Equal(t, 11827, in.CiudadID, "must use the captured ciudad")
	assert.Equal(t, 11523, in.EstadoID, "estado must come from the ciudad's own row")
	assert.NotEqual(t, outbound.DefaultEstadoID, in.EstadoID, "must NOT stamp Puebla on an Oaxaca ciudad")
}

// Accents, case and the catalog's trailing spaces must not cause a miss.
func TestAplicarVenta_CiudadCatalogoOn_ToleraAcentosYEspacios(t *testing.T) {
	t.Parallel()
	h, _, _, clienteWriter := newAplicarHarness(t)
	h.svc.WithCiudadCatalogo(&fakeCiudadCatalogo{
		byKey: map[string]outbound.CiudadResuelta{
			domain.NormalizeCiudad("COYOMEAPAN "): {CiudadID: 25361, EstadoID: 337},
		},
	}, true)

	id := seedAprobadaSinClienteMicrosip(t, h, "  coyomeapán ")
	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())
	require.NoError(t, err)

	assert.Equal(t, 25361, clienteWriter.LastIn.CiudadID)
}

// A ciudad that is not in the catalog BLOCKS the apply — it must never fall
// back to writing Tehuacán over what the vendor captured.
func TestAplicarVenta_CiudadCatalogoOn_SinCoincidenciaBloquea(t *testing.T) {
	t.Parallel()
	h, _, writer, clienteWriter := newAplicarHarness(t)
	h.svc.WithCiudadCatalogo(&fakeCiudadCatalogo{
		byKey: map[string]outbound.CiudadResuelta{"TEHUACAN": {CiudadID: 338, EstadoID: 337}},
	}, true)

	id := seedAprobadaSinClienteMicrosip(t, h, "Pueblo Que No Existe")
	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.ErrorIs(t, err, domain.ErrCiudadNoEnCatalogo)
	assert.Equal(t, 0, writer.callsCount(), "no venta may be materialized")
	assert.Equal(t, 0, clienteWriter.callsCount(), "no cliente may be created")
}

// A blank ciudad never reaches the apply: the domain rejects it at capture.
// Pinning it here documents why resolveCiudad's empty-key branch is a
// belt-and-braces guard rather than the primary defense.
func TestCrearVenta_CiudadEnBlancoRechazadaEnLaCaptura(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	in := validContadoInput()
	in.Ciudad = "   "

	_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())

	require.Error(t, err, "una ciudad en blanco no puede capturarse")
	assert.Zero(t, h.ventas.SaveCalls)
}

// A catalog lookup failure propagates instead of falling back to the defaults.
func TestAplicarVenta_CiudadCatalogoOn_ErrorDelCatalogoSePropaga(t *testing.T) {
	t.Parallel()
	h, _, writer, _ := newAplicarHarness(t)
	boom := errors.New("firebird pool wedged")
	h.svc.WithCiudadCatalogo(&fakeCiudadCatalogo{err: boom}, true)

	id := seedAprobadaSinClienteMicrosip(t, h, "Tehuacán")
	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.ErrorIs(t, err, boom)
	assert.Equal(t, 0, writer.callsCount())
}

// A nil catalog disables the feature even if enabled is true — no nil deref.
func TestAplicarVenta_CiudadCatalogoNil_NoRompe(t *testing.T) {
	t.Parallel()
	h, _, _, clienteWriter := newAplicarHarness(t)
	h.svc.WithCiudadCatalogo(nil, true)

	id := seedAprobadaSinClienteMicrosip(t, h, "Cualquiera")
	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())
	require.NoError(t, err)

	assert.Equal(t, outbound.DefaultCiudadID, clienteWriter.LastIn.CiudadID)
}
