package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

func TestParseSegmento(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    domain.Segmento
		wantErr error
	}{
		{
			name:  "LEAL_POR_LIQUIDAR is valid",
			input: "LEAL_POR_LIQUIDAR",
			want:  domain.SegmentoLealPorLiquidar,
		},
		{
			name:  "DORMIDO_VALIOSO is valid",
			input: "DORMIDO_VALIOSO",
			want:  domain.SegmentoDormidoValioso,
		},
		{
			name:  "ACTIVO is valid",
			input: "ACTIVO",
			want:  domain.SegmentoActivo,
		},
		{
			name:  "NUEVO is valid",
			input: "NUEVO",
			want:  domain.SegmentoNuevo,
		},
		{
			name:  "FRIO is valid",
			input: "FRIO",
			want:  domain.SegmentoFrio,
		},
		{
			name:  "PERDIDO is valid",
			input: "PERDIDO",
			want:  domain.SegmentoPerdido,
		},
		{
			name:    "lowercase activo is invalid",
			input:   "activo",
			wantErr: domain.ErrSegmentoInvalido,
		},
		{
			name:    "empty string is invalid",
			input:   "",
			wantErr: domain.ErrSegmentoInvalido,
		},
		{
			name:    "unknown value is invalid",
			input:   "DESCONOCIDO",
			wantErr: domain.ErrSegmentoInvalido,
		},
		{
			name:    "mixed case is invalid",
			input:   "Activo",
			wantErr: domain.ErrSegmentoInvalido,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.ParseSegmento(tc.input)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
				assert.Empty(t, got.String())
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSegmentoIsValid(t *testing.T) {
	t.Parallel()

	valid := []domain.Segmento{
		domain.SegmentoLealPorLiquidar,
		domain.SegmentoDormidoValioso,
		domain.SegmentoActivo,
		domain.SegmentoNuevo,
		domain.SegmentoFrio,
		domain.SegmentoPerdido,
	}
	for _, s := range valid {
		s := s
		t.Run(string(s)+"_is_valid", func(t *testing.T) {
			t.Parallel()
			assert.True(t, s.IsValid())
		})
	}

	invalid := []domain.Segmento{
		"",
		"activo",
		"UNKNOWN",
		"leal_por_liquidar",
	}
	for _, s := range invalid {
		s := s
		t.Run(string(s)+"_is_invalid", func(t *testing.T) {
			t.Parallel()
			assert.False(t, s.IsValid())
		})
	}
}

func TestSegmentoString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		seg  domain.Segmento
		want string
	}{
		{domain.SegmentoLealPorLiquidar, "LEAL_POR_LIQUIDAR"},
		{domain.SegmentoDormidoValioso, "DORMIDO_VALIOSO"},
		{domain.SegmentoActivo, "ACTIVO"},
		{domain.SegmentoNuevo, "NUEVO"},
		{domain.SegmentoFrio, "FRIO"},
		{domain.SegmentoPerdido, "PERDIDO"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.seg.String())
		})
	}
}
