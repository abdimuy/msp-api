package garfb

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/nakagami/firebirdsql"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

type scannerConError struct {
	err error
}

func (s *scannerConError) Scan(dest ...any) error {
	return s.err
}

func TestNormalizarLimite(t *testing.T) {
	t.Parallel()

	require.Equal(t, 20, normalizarLimite(0))
	require.Equal(t, 20, normalizarLimite(-1))
	require.Equal(t, 100, normalizarLimite(101))
	require.Equal(t, 50, normalizarLimite(50))
}

func TestEsClaveDuplicada(t *testing.T) {
	t.Parallel()

	require.False(t, esClaveDuplicada(nil))

	require.False(
		t,
		esClaveDuplicada(errors.New(
			"violation of UNIQUE KEY constraint UQ_MSP_GA_EVENTO_CLAVE",
		)),
	)

	require.False(
		t,
		esClaveDuplicada(&firebirdsql.FbError{
			GDSCodes: []int{335544665},
			Message:  "violation of UNIQUE KEY constraint UQ_MSP_GA_GARANTIA_FOLIO",
		}),
	)

	require.True(
		t,
		esClaveDuplicada(&firebirdsql.FbError{
			GDSCodes: []int{335544665},
			Message:  "violation of UNIQUE KEY constraint UQ_MSP_GA_EVENTO_CLAVE",
		}),
	)
}

func TestHydrateRows_UUIDInvalido(t *testing.T) {
	t.Parallel()

	tests := []struct {
		nombre string
		fn     func() error
	}{
		{
			nombre: "garantia",
			fn: func() error {
				_, err := hydrateGarantia(
					&garantiaRow{id: "uuid-invalido"},
					nil,
				)
				return err
			},
		},
		{
			nombre: "articulo",
			fn: func() error {
				_, err := hydrateArticulo(
					&articuloRow{id: "uuid-invalido"},
				)
				return err
			},
		},
		{
			nombre: "evento",
			fn: func() error {
				_, err := hydrateEvento(
					&eventoRow{id: "uuid-invalido"},
				)
				return err
			},
		},
		{
			nombre: "imagen",
			fn: func() error {
				_, err := hydrateImagen(
					&imagenRow{id: "uuid-invalido"},
				)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.nombre, func(t *testing.T) {
			t.Parallel()

			err := tt.fn()
			require.Error(t, err)

			appErr, ok := apperror.As(err)
			require.True(t, ok)
			require.Equal(t, "warranty_hydration_failed", appErr.Code)
		})
	}
}

func TestScanRows_PropagaError(t *testing.T) {
	t.Parallel()

	errEsperado := errors.New("fallo de scan")
	scanner := scannerConError{err: errEsperado}

	tests := []struct {
		nombre string
		fn     func() error
	}{
		{
			nombre: "garantia",
			fn: func() error {
				_, err := scanGarantia(&scanner)
				return err
			},
		},
		{
			nombre: "articulo",
			fn: func() error {
				_, err := scanArticulo(&scanner)
				return err
			},
		},
		{
			nombre: "evento",
			fn: func() error {
				_, err := scanEvento(&scanner)
				return err
			},
		},
		{
			nombre: "imagen",
			fn: func() error {
				_, err := scanImagen(&scanner)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.nombre, func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, tt.fn(), errEsperado)
		})
	}
}

func TestMapperHelpers_NullEInvalidos(t *testing.T) {
	t.Parallel()

	id, err := uuidPtr(sql.NullString{})
	require.NoError(t, err)
	require.Nil(t, id)

	_, err = uuidPtr(sql.NullString{
		String: "uuid-invalido",
		Valid:  true,
	})
	require.Error(t, err)

	estadoCuenta, err := parseEstadoCuentaPtr(sql.NullString{})
	require.NoError(t, err)
	require.Nil(t, estadoCuenta)

	_, err = parseEstadoCuentaPtr(sql.NullString{
		String: "invalido",
		Valid:  true,
	})
	require.Error(t, err)

	ruta, err := parseRutaPtr(sql.NullString{})
	require.NoError(t, err)
	require.Nil(t, ruta)

	_, err = parseRutaPtr(sql.NullString{
		String: "invalida",
		Valid:  true,
	})
	require.Error(t, err)

	dictamen, err := parseDictamenPtr(sql.NullString{})
	require.NoError(t, err)
	require.Nil(t, dictamen)

	_, err = parseDictamenPtr(sql.NullString{
		String: "invalido",
		Valid:  true,
	})
	require.Error(t, err)

	desenlace, err := parseDesenlacePtr(sql.NullString{})
	require.NoError(t, err)
	require.Nil(t, desenlace)

	_, err = parseDesenlacePtr(sql.NullString{
		String: "invalido",
		Valid:  true,
	})
	require.Error(t, err)

	etapa, err := parseEtapaPtr(sql.NullString{})
	require.NoError(t, err)
	require.Nil(t, etapa)

	_, err = parseEtapaPtr(sql.NullString{
		String: "invalida",
		Valid:  true,
	})
	require.Error(t, err)

	rol, err := parseRolDecisorPtr(sql.NullString{})
	require.NoError(t, err)
	require.Nil(t, rol)

	_, err = parseRolDecisorPtr(sql.NullString{
		String: "invalido",
		Valid:  true,
	})
	require.Error(t, err)
}
