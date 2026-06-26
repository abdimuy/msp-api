// Package domain_test tests CarteraSnapshot construction and hydration.
//
//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// carteraTestNow is a deterministic timestamp for CarteraSnapshot tests.
// Uses a name distinct from the package-level "now" declared in winback_candidato_test.go.
var carteraTestNow = time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)

func intPtr(v int) *int { return &v }

// validSnapshotParams returns a fully-valid NewCarteraSnapshotParams.
// Tests that need to violate an invariant copy and mutate the result.
func validSnapshotParams() domain.NewCarteraSnapshotParams {
	return domain.NewCarteraSnapshotParams{
		FechaCorte:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		ZonaClienteID: 1,
		CobradorID:    intPtr(42),
		Bucket:        domain.BucketAgingDias0_30,
		Saldo:         decimal.NewFromFloat(15000.50),
		Conteo:        8,
		Now:           carteraTestNow,
	}
}

func TestNewCarteraSnapshot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*domain.NewCarteraSnapshotParams)
		wantErr error
	}{
		{
			name:   "valid input returns entity without error",
			mutate: nil,
		},
		{
			name: "saldo zero is valid",
			mutate: func(p *domain.NewCarteraSnapshotParams) {
				p.Saldo = decimal.Zero
			},
		},
		{
			name: "conteo zero is valid",
			mutate: func(p *domain.NewCarteraSnapshotParams) {
				p.Conteo = 0
			},
		},
		{
			name: "nil cobrador is valid (zona-level aggregate)",
			mutate: func(p *domain.NewCarteraSnapshotParams) {
				p.CobradorID = nil
			},
		},
		{
			name: "bucket 31-60 is valid",
			mutate: func(p *domain.NewCarteraSnapshotParams) {
				p.Bucket = domain.BucketAgingDias31_60
			},
		},
		{
			name: "bucket 61-90 is valid",
			mutate: func(p *domain.NewCarteraSnapshotParams) {
				p.Bucket = domain.BucketAgingDias61_90
			},
		},
		{
			name: "bucket 90+ is valid",
			mutate: func(p *domain.NewCarteraSnapshotParams) {
				p.Bucket = domain.BucketAgingDias90Plus
			},
		},
		{
			name: "empty bucket returns bucket sentinel",
			mutate: func(p *domain.NewCarteraSnapshotParams) {
				p.Bucket = ""
			},
			wantErr: domain.ErrCarteraSnapshotBucketInvalido,
		},
		{
			name: "unrecognized bucket string returns bucket sentinel",
			mutate: func(p *domain.NewCarteraSnapshotParams) {
				p.Bucket = "90-120"
			},
			wantErr: domain.ErrCarteraSnapshotBucketInvalido,
		},
		{
			name: "negative saldo returns saldo sentinel",
			mutate: func(p *domain.NewCarteraSnapshotParams) {
				p.Saldo = decimal.NewFromFloat(-0.01)
			},
			wantErr: domain.ErrCarteraSnapshotSaldoInvalido,
		},
		{
			name: "negative conteo returns conteo sentinel",
			mutate: func(p *domain.NewCarteraSnapshotParams) {
				p.Conteo = -1
			},
			wantErr: domain.ErrCarteraSnapshotConteoInvalido,
		},
		{
			name: "zona zero returns zona sentinel",
			mutate: func(p *domain.NewCarteraSnapshotParams) {
				p.ZonaClienteID = 0
			},
			wantErr: domain.ErrCarteraSnapshotZonaInvalida,
		},
		{
			name: "zona negative returns zona sentinel",
			mutate: func(p *domain.NewCarteraSnapshotParams) {
				p.ZonaClienteID = -5
			},
			wantErr: domain.ErrCarteraSnapshotZonaInvalida,
		},
		{
			name: "zero fechaCorte returns fecha sentinel",
			mutate: func(p *domain.NewCarteraSnapshotParams) {
				p.FechaCorte = time.Time{}
			},
			wantErr: domain.ErrCarteraSnapshotFechaCorteInvalida,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := validSnapshotParams()
			if tc.mutate != nil {
				tc.mutate(&p)
			}

			got, err := domain.NewCarteraSnapshot(p)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
				assert.Nil(t, got)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)

			// UUID is always populated.
			assert.NotEqual(t, uuid.Nil, got.ID(), "id must be non-nil uuid")

			// Fields round-trip correctly.
			assert.Equal(t, p.FechaCorte.UTC(), got.FechaCorte())
			assert.Equal(t, p.ZonaClienteID, got.ZonaClienteID())
			assert.Equal(t, p.Bucket, got.Bucket())
			assert.True(t, p.Saldo.Equal(got.Saldo()), "saldo mismatch: got %s want %s", got.Saldo(), p.Saldo)
			assert.Equal(t, p.Conteo, got.Conteo())

			// CobradorID pointer is preserved.
			if p.CobradorID == nil {
				assert.Nil(t, got.CobradorID())
			} else {
				require.NotNil(t, got.CobradorID())
				assert.Equal(t, *p.CobradorID, *got.CobradorID())
			}

			// Audit timestamps come from p.Now.
			assert.Equal(t, carteraTestNow, got.CreatedAt())
			assert.Equal(t, carteraTestNow, got.UpdatedAt())
		})
	}
}

func TestHydrateCarteraSnapshot(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 6, 15, 9, 30, 0, 0, time.UTC)
	id := uuid.New()
	cobradorID := 7
	fechaCorte := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	p := domain.HydrateCarteraSnapshotParams{
		ID:            id,
		FechaCorte:    fechaCorte,
		ZonaClienteID: 3,
		CobradorID:    &cobradorID,
		Bucket:        domain.BucketAgingDias31_60,
		Saldo:         decimal.NewFromFloat(8750.25),
		Conteo:        12,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}

	got := domain.HydrateCarteraSnapshot(p)
	require.NotNil(t, got)

	assert.Equal(t, id, got.ID())
	assert.Equal(t, fechaCorte, got.FechaCorte())
	assert.Equal(t, 3, got.ZonaClienteID())
	require.NotNil(t, got.CobradorID())
	assert.Equal(t, cobradorID, *got.CobradorID())
	assert.Equal(t, domain.BucketAgingDias31_60, got.Bucket())
	assert.True(t, p.Saldo.Equal(got.Saldo()), "saldo mismatch")
	assert.Equal(t, 12, got.Conteo())
	assert.Equal(t, createdAt, got.CreatedAt())
	assert.Equal(t, updatedAt, got.UpdatedAt())
}

func TestHydrateCarteraSnapshot_NilCobradorID(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	p := domain.HydrateCarteraSnapshotParams{
		ID:            id,
		FechaCorte:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		ZonaClienteID: 2,
		CobradorID:    nil,
		Bucket:        domain.BucketAgingDias90Plus,
		Saldo:         decimal.NewFromFloat(25000),
		Conteo:        30,
		CreatedAt:     carteraTestNow,
		UpdatedAt:     carteraTestNow,
	}

	got := domain.HydrateCarteraSnapshot(p)
	require.NotNil(t, got)
	assert.Nil(t, got.CobradorID())
}

func TestCarteraSnapshotBucketConstants(t *testing.T) {
	t.Parallel()

	// Ensure canonical bucket strings have not changed. Task 2 MUST use these constants.
	assert.Equal(t, "0-30", domain.BucketAgingDias0_30)
	assert.Equal(t, "31-60", domain.BucketAgingDias31_60)
	assert.Equal(t, "61-90", domain.BucketAgingDias61_90)
	assert.Equal(t, "90+", domain.BucketAgingDias90Plus)
}
