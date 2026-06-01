//nolint:misspell // Spanish vocabulary by convention.
package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// validParams returns a base CrearPagoRecibidoParams whose every field is
// legal. Individual tests mutate one field at a time to hit validation paths.
func validParams(t *testing.T) domain.CrearPagoRecibidoParams {
	t.Helper()
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

// validStorage returns a well-formed ImagenStorage for imagen tests.
func validStorage(t *testing.T) domain.ImagenStorage {
	t.Helper()
	st, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "pagos/recibos/img001.jpg")
	require.NoError(t, err)
	return st
}

// ─── NewPagoRecibido ──────────────────────────────────────────────────────────

func TestNewPagoRecibido_HappyPath(t *testing.T) {
	t.Parallel()

	p := validParams(t)
	pago, err := domain.NewPagoRecibido(p)

	require.NoError(t, err)
	require.NotNil(t, pago)

	assert.Equal(t, p.ID, pago.ID())
	assert.Equal(t, p.CargoDoctoCCID, pago.CargoDoctoCCID())
	assert.Equal(t, p.ClienteID, pago.ClienteID())
	assert.Equal(t, p.CobradorID, pago.CobradorID())
	assert.Equal(t, p.Cobrador, pago.Cobrador())
	assert.True(t, p.Importe.Equal(pago.Importe()))
	assert.Equal(t, p.FormaCobroID, pago.FormaCobroID())

	// State machine starts at pendiente with zero attempts.
	assert.True(t, pago.IsPendiente())
	assert.False(t, pago.IsAplicada())
	assert.Equal(t, 0, pago.Intentos())
	assert.Nil(t, pago.UltimoError())
	assert.Nil(t, pago.AplicadoAt())
	assert.Equal(t, 0, pago.ImagenesCount())
}

func TestNewPagoRecibido_ConceptoDerivation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		formaCobroID     int
		wantConceptoCCID int
	}{
		{
			name:             "abono_mostrador",
			formaCobroID:     137026,
			wantConceptoCCID: 27969,
		},
		{
			name:             "efectivo_cobranza_ruta",
			formaCobroID:     1,
			wantConceptoCCID: 87327,
		},
		{
			name:             "forma_desconocida_cobranza_ruta",
			formaCobroID:     999,
			wantConceptoCCID: 87327,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validParams(t)
			p.FormaCobroID = tc.formaCobroID

			pago, err := domain.NewPagoRecibido(p)
			require.NoError(t, err)
			assert.Equal(t, tc.wantConceptoCCID, pago.ConceptoCCID())
		})
	}
}

func TestNewPagoRecibido_Validations(t *testing.T) {
	t.Parallel()

	lat21 := strings.Repeat("1", 21)
	lon21 := strings.Repeat("2", 21)
	cobrador101 := strings.Repeat("x", 101)

	tests := []struct {
		name    string
		mutate  func(*domain.CrearPagoRecibidoParams)
		wantErr error
	}{
		{
			name:    "id_zero",
			mutate:  func(p *domain.CrearPagoRecibidoParams) { p.ID = uuid.Nil },
			wantErr: domain.ErrPagoIDRequerido,
		},
		{
			name:    "cargo_docto_cc_id_zero",
			mutate:  func(p *domain.CrearPagoRecibidoParams) { p.CargoDoctoCCID = 0 },
			wantErr: domain.ErrPagoCargoIDInvalido,
		},
		{
			name:    "cargo_docto_cc_id_negative",
			mutate:  func(p *domain.CrearPagoRecibidoParams) { p.CargoDoctoCCID = -1 },
			wantErr: domain.ErrPagoCargoIDInvalido,
		},
		{
			name:    "cliente_id_zero",
			mutate:  func(p *domain.CrearPagoRecibidoParams) { p.ClienteID = 0 },
			wantErr: domain.ErrPagoClienteIDInvalido,
		},
		{
			name:    "cliente_id_negative",
			mutate:  func(p *domain.CrearPagoRecibidoParams) { p.ClienteID = -5 },
			wantErr: domain.ErrPagoClienteIDInvalido,
		},
		{
			name:    "cobrador_id_zero",
			mutate:  func(p *domain.CrearPagoRecibidoParams) { p.CobradorID = 0 },
			wantErr: domain.ErrPagoCobradorIDInvalido,
		},
		{
			name:    "cobrador_id_negative",
			mutate:  func(p *domain.CrearPagoRecibidoParams) { p.CobradorID = -1 },
			wantErr: domain.ErrPagoCobradorIDInvalido,
		},
		{
			name:    "forma_cobro_id_zero",
			mutate:  func(p *domain.CrearPagoRecibidoParams) { p.FormaCobroID = 0 },
			wantErr: domain.ErrPagoFormaCobroInvalida,
		},
		{
			name:    "forma_cobro_id_negative",
			mutate:  func(p *domain.CrearPagoRecibidoParams) { p.FormaCobroID = -3 },
			wantErr: domain.ErrPagoFormaCobroInvalida,
		},
		{
			name:    "importe_zero",
			mutate:  func(p *domain.CrearPagoRecibidoParams) { p.Importe = decimal.Zero },
			wantErr: domain.ErrPagoImporteInvalido,
		},
		{
			name:    "importe_negative",
			mutate:  func(p *domain.CrearPagoRecibidoParams) { p.Importe = decimal.NewFromInt(-1) },
			wantErr: domain.ErrPagoImporteInvalido,
		},
		{
			name:    "cobrador_empty",
			mutate:  func(p *domain.CrearPagoRecibidoParams) { p.Cobrador = "" },
			wantErr: domain.ErrPagoCobradorRequerido,
		},
		{
			name:    "cobrador_whitespace_only",
			mutate:  func(p *domain.CrearPagoRecibidoParams) { p.Cobrador = "   " },
			wantErr: domain.ErrPagoCobradorRequerido,
		},
		{
			name:    "cobrador_101_chars",
			mutate:  func(p *domain.CrearPagoRecibidoParams) { p.Cobrador = cobrador101 },
			wantErr: domain.ErrPagoCobradorDemasiadoLargo,
		},
		{
			name: "lat_too_long",
			mutate: func(p *domain.CrearPagoRecibidoParams) {
				p.Lat = &lat21
			},
			wantErr: domain.ErrPagoLatLonInvalida,
		},
		{
			name: "lon_too_long",
			mutate: func(p *domain.CrearPagoRecibidoParams) {
				p.Lon = &lon21
			},
			wantErr: domain.ErrPagoLatLonInvalida,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validParams(t)
			tc.mutate(&p)

			pago, err := domain.NewPagoRecibido(p)
			assert.Nil(t, pago)
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// ─── MarcarAplicada ───────────────────────────────────────────────────────────

func TestPagoRecibido_MarcarAplicada_HappyPath(t *testing.T) {
	t.Parallel()

	pago, err := domain.NewPagoRecibido(validParams(t))
	require.NoError(t, err)

	now := time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC)
	by := uuid.New()
	err = pago.MarcarAplicada(9001, 9002, "Z00042", now, by)

	require.NoError(t, err)
	assert.True(t, pago.IsAplicada())
	assert.False(t, pago.IsPendiente())

	require.NotNil(t, pago.DoctoCCID())
	assert.Equal(t, 9001, *pago.DoctoCCID())

	require.NotNil(t, pago.ImpteDoctoCCID())
	assert.Equal(t, 9002, *pago.ImpteDoctoCCID())

	require.NotNil(t, pago.Folio())
	assert.Equal(t, "Z00042", *pago.Folio())

	require.NotNil(t, pago.AplicadoAt())
	assert.Equal(t, now, *pago.AplicadoAt())

	// UltimoError cleared on successful apply.
	assert.Nil(t, pago.UltimoError())
}

func TestPagoRecibido_MarcarAplicada_Idempotent(t *testing.T) {
	t.Parallel()

	pago, err := domain.NewPagoRecibido(validParams(t))
	require.NoError(t, err)

	now := time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC)
	by := uuid.New()

	// First call must succeed.
	require.NoError(t, pago.MarcarAplicada(1, 2, "Z001", now, by))

	// Second call on the same aplicada pago must return ErrPagoYaAplicado.
	err = pago.MarcarAplicada(3, 4, "Z002", now, by)
	assert.ErrorIs(t, err, domain.ErrPagoYaAplicado)
}

func TestPagoRecibido_MarcarAplicada_Validation(t *testing.T) {
	t.Parallel()

	folio21 := strings.Repeat("F", 21)

	tests := []struct {
		name           string
		doctoCCID      int
		impteDoctoCCID int
		folio          string
		wantErr        error
	}{
		{
			name:           "docto_cc_id_zero",
			doctoCCID:      0,
			impteDoctoCCID: 1,
			folio:          "Z001",
			wantErr:        domain.ErrPagoDoctoCCIDInvalido,
		},
		{
			name:           "docto_cc_id_negative",
			doctoCCID:      -1,
			impteDoctoCCID: 1,
			folio:          "Z001",
			wantErr:        domain.ErrPagoDoctoCCIDInvalido,
		},
		{
			name:           "impte_docto_cc_id_zero",
			doctoCCID:      1,
			impteDoctoCCID: 0,
			folio:          "Z001",
			wantErr:        domain.ErrPagoImpteDoctoCCIDInvalido,
		},
		{
			name:           "impte_docto_cc_id_negative",
			doctoCCID:      1,
			impteDoctoCCID: -5,
			folio:          "Z001",
			wantErr:        domain.ErrPagoImpteDoctoCCIDInvalido,
		},
		{
			name:           "folio_empty",
			doctoCCID:      1,
			impteDoctoCCID: 2,
			folio:          "",
			wantErr:        domain.ErrPagoFolioRequerido,
		},
		{
			name:           "folio_whitespace_only",
			doctoCCID:      1,
			impteDoctoCCID: 2,
			folio:          "   ",
			wantErr:        domain.ErrPagoFolioRequerido,
		},
		{
			name:           "folio_21_chars",
			doctoCCID:      1,
			impteDoctoCCID: 2,
			folio:          folio21,
			wantErr:        domain.ErrPagoFolioDemasiadoLargo,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pago, err := domain.NewPagoRecibido(validParams(t))
			require.NoError(t, err)

			now := time.Now().UTC()
			err = pago.MarcarAplicada(tc.doctoCCID, tc.impteDoctoCCID, tc.folio, now, uuid.New())
			require.ErrorIs(t, err, tc.wantErr)

			// Pago must remain pendiente after a validation failure.
			assert.True(t, pago.IsPendiente())
		})
	}
}

// ─── RegistrarFallo ───────────────────────────────────────────────────────────

func TestPagoRecibido_RegistrarFallo(t *testing.T) {
	t.Parallel()

	pago, err := domain.NewPagoRecibido(validParams(t))
	require.NoError(t, err)

	now := time.Now().UTC()
	by := uuid.New()

	pago.RegistrarFallo("timeout al conectar con Firebird", now, by)

	assert.Equal(t, 1, pago.Intentos())
	require.NotNil(t, pago.UltimoError())
	assert.Contains(t, *pago.UltimoError(), "timeout")

	// State machine does NOT change.
	assert.True(t, pago.IsPendiente())
}

func TestPagoRecibido_RegistrarFallo_Truncates(t *testing.T) {
	t.Parallel()

	pago, err := domain.NewPagoRecibido(validParams(t))
	require.NoError(t, err)

	// 600 ASCII characters — exceeds the 500-rune column limit.
	longMsg := strings.Repeat("e", 600)
	pago.RegistrarFallo(longMsg, time.Now().UTC(), uuid.New())

	require.NotNil(t, pago.UltimoError())
	assert.LessOrEqual(t, len([]rune(*pago.UltimoError())), 500)
}

func TestPagoRecibido_RegistrarFallo_Increments(t *testing.T) {
	t.Parallel()

	pago, err := domain.NewPagoRecibido(validParams(t))
	require.NoError(t, err)

	now := time.Now().UTC()
	by := uuid.New()

	pago.RegistrarFallo("primer intento", now, by)
	pago.RegistrarFallo("segundo intento", now, by)
	pago.RegistrarFallo("tercer intento", now, by)

	assert.Equal(t, 3, pago.Intentos())
	require.NotNil(t, pago.UltimoError())
	assert.Contains(t, *pago.UltimoError(), "tercer")
}

// ─── PreconditionForAplicar ───────────────────────────────────────────────────

func TestPagoRecibido_PreconditionForAplicar(t *testing.T) {
	t.Parallel()

	t.Run("pendiente_ok", func(t *testing.T) {
		t.Parallel()
		pago, err := domain.NewPagoRecibido(validParams(t))
		require.NoError(t, err)
		assert.NoError(t, pago.PreconditionForAplicar())
	})

	t.Run("aplicada_rejected", func(t *testing.T) {
		t.Parallel()
		pago, err := domain.NewPagoRecibido(validParams(t))
		require.NoError(t, err)

		require.NoError(t, pago.MarcarAplicada(1, 2, "Z001", time.Now().UTC(), uuid.New()))

		err = pago.PreconditionForAplicar()
		assert.ErrorIs(t, err, domain.ErrPagoYaAplicado)
	})
}

// ─── Imagen mutations ─────────────────────────────────────────────────────────

func TestPagoRecibido_AdjuntarImagen_HappyPath(t *testing.T) {
	t.Parallel()

	pago, err := domain.NewPagoRecibido(validParams(t))
	require.NoError(t, err)

	imgID := uuid.New()
	storage := validStorage(t)
	by := uuid.New()
	now := time.Now().UTC()

	img, err := pago.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:        imgID,
		Storage:   storage,
		Mime:      domain.MimeJPEG,
		SizeBytes: 12345,
		By:        by,
		Now:       now,
	})

	require.NoError(t, err)
	require.NotNil(t, img)
	assert.Equal(t, imgID, img.ID())
	assert.Equal(t, 1, pago.ImagenesCount())

	// Verify the image is accessible via the iterator.
	var collected []*domain.Imagen
	for i := range pago.Imagenes() {
		collected = append(collected, i)
	}
	require.Len(t, collected, 1)
	assert.Equal(t, imgID, collected[0].ID())
}

func TestPagoRecibido_EliminarImagen(t *testing.T) {
	t.Parallel()

	t.Run("non_existent_id", func(t *testing.T) {
		t.Parallel()
		pago, err := domain.NewPagoRecibido(validParams(t))
		require.NoError(t, err)

		err = pago.EliminarImagen(uuid.New(), uuid.New(), time.Now().UTC())
		assert.ErrorIs(t, err, domain.ErrImagenNoEncontrada)
	})

	t.Run("existing_id_removed", func(t *testing.T) {
		t.Parallel()
		pago, err := domain.NewPagoRecibido(validParams(t))
		require.NoError(t, err)

		imgID := uuid.New()
		_, err = pago.AdjuntarImagen(domain.AdjuntarImagenParams{
			ID:        imgID,
			Storage:   validStorage(t),
			Mime:      domain.MimePNG,
			SizeBytes: 4096,
			By:        uuid.New(),
			Now:       time.Now().UTC(),
		})
		require.NoError(t, err)
		assert.Equal(t, 1, pago.ImagenesCount())

		err = pago.EliminarImagen(imgID, uuid.New(), time.Now().UTC())
		require.NoError(t, err)
		assert.Equal(t, 0, pago.ImagenesCount())
	})
}

// ─── DerivarConceptoCC ────────────────────────────────────────────────────────

func TestDerivarConceptoCC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		formaCobroID     int
		wantConceptoCCID int
	}{
		{137026, 27969},
		{1, 87327},
		{999, 87327},
		{0, 87327},
		{-1, 87327},
	}

	for _, tc := range tests {
		got := domain.DerivarConceptoCC(tc.formaCobroID)
		assert.Equal(t, tc.wantConceptoCCID, got,
			"formaCobroID=%d", tc.formaCobroID)
	}
}
