package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestParseFecha(t *testing.T) {
	t.Parallel()
	f, err := domain.ParseFecha("2026-10-14")
	require.NoError(t, err)
	assert.Equal(t, "2026-10-14", f.String())

	for _, malo := range []string{"", "2026-10-32", "14/10/2026", "2026-10-14T00:00:00Z", "2026-2-1"} {
		_, err := domain.ParseFecha(malo)
		require.ErrorIs(t, err, domain.ErrParametroInvalido, "entrada %q", malo)
	}
}

func TestNuevaFecha_RechazaDiasInexistentes(t *testing.T) {
	t.Parallel()
	_, err := domain.NuevaFecha(2026, time.February, 30)
	require.ErrorIs(t, err, domain.ErrParametroInvalido)
	f, err := domain.NuevaFecha(2028, time.February, 29)
	require.NoError(t, err)
	assert.Equal(t, "2028-02-29", f.String())
}

func TestDiaDe_UsaLaZonaDeNegocio(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	// 2026-10-15T05:30Z son las 23:30 del 14 en CDMX (UTC-6).
	assert.Equal(t, "2026-10-14", domain.DiaDe(time.Date(2026, 10, 15, 5, 30, 0, 0, time.UTC), loc).String())
	// 06:00Z ya es medianoche del 15.
	assert.Equal(t, "2026-10-15", domain.DiaDe(time.Date(2026, 10, 15, 6, 0, 0, 0, time.UTC), loc).String())
}

func TestFecha_InicioDelDiaYSiguiente(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	f, err := domain.ParseFecha("2026-12-31")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 12, 31, 6, 0, 0, 0, time.UTC), f.InicioDelDia(loc))
	assert.Equal(t, "2027-01-01", f.Siguiente().String())
	assert.True(t, f.Antes(f.Siguiente()))
	assert.True(t, f.Siguiente().Despues(f))
	assert.False(t, f.Antes(f))
	assert.False(t, f.EsCero())
	assert.True(t, domain.Fecha{}.EsCero())
}

func TestParseHoraDelDia(t *testing.T) {
	t.Parallel()
	h, err := domain.ParseHoraDelDia("08:05")
	require.NoError(t, err)
	assert.Equal(t, 8*60+5, h.Minutos())
	assert.Equal(t, "08:05", h.String())

	for _, malo := range []string{"", "8:05", "24:00", "12:60", "08:05:00", "0805"} {
		_, err := domain.ParseHoraDelDia(malo)
		require.ErrorIs(t, err, domain.ErrParametroInvalido, "entrada %q", malo)
	}
}

func TestHoraDelDia_EnDia(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	f, _ := domain.ParseFecha("2026-10-14")
	h, _ := domain.ParseHoraDelDia("18:00")
	assert.Equal(t, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), h.EnDia(f, loc))
}
