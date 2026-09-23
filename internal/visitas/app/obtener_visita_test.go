package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// TestObtenerVisita_Delegates verifies the query returns the stored aggregate
// and consults the repo exactly once — it must not cache or re-query.
func TestObtenerVisita_Delegates(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	in := validInput()
	created, err := svc.RegistrarVisita(context.Background(), in, uuid.New())
	require.NoError(t, err)

	got, err := svc.ObtenerVisita(context.Background(), in.ID)
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, created.ID(), got.ID())
	assert.Equal(t, in.ClienteID, got.ClienteID())
	assert.Equal(t, in.Cobrador, got.Cobrador())
	assert.Equal(t, in.TipoVisita, got.TipoVisita().String())
	assert.Equal(t, 1, repo.findByIDCalls, "ObtenerVisita must hit the repo exactly once")
}

// TestObtenerVisita_NotFound verifies a miss surfaces the domain's typed
// not-found sentinel, which the HTTP layer maps to 404 (never 500).
func TestObtenerVisita_NotFound(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	got, err := svc.ObtenerVisita(context.Background(), uuid.New())

	require.Error(t, err)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, domain.ErrVisitaNoEncontrada)
}

// TestObtenerVisita_RepoErrorPropagates verifies an infrastructure failure is
// passed through untouched rather than being flattened into "not found".
func TestObtenerVisita_RepoErrorPropagates(t *testing.T) {
	t.Parallel()

	boom := errors.New("firebird connection reset")
	repo := newFakeVisitasRepo()
	repo.findByIDErr = boom
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	got, err := svc.ObtenerVisita(context.Background(), uuid.New())

	require.Error(t, err)
	assert.Nil(t, got)
	require.ErrorIs(t, err, boom)
	assert.NotErrorIs(t, err, domain.ErrVisitaNoEncontrada,
		"an infra failure must not masquerade as a missing row")
}

// TestObtenerVisita_LeeFilasLegacyFueraDelCatalogo is the read-path
// guarantee at the application layer, and the reason this endpoint could ship
// at all.
//
// The repo rehydrates rows written years before the TipoVisita catalogue
// existed. ~59k rows in MSP_VISITAS carry retired literals and 200 carry the
// empty string. If ObtenerVisita validated what it read, GET /v2/visitas/{id}
// would fail on every one of them. The values below are seeded straight
// through domain.RehydrateVisita — the exact path visitasfb takes — and each
// must come back verbatim.
func TestObtenerVisita_LeeFilasLegacyFueraDelCatalogo(t *testing.T) {
	t.Parallel()

	legacy := []struct {
		name string
		tipo string
	}{
		{"empty string (200 real rows)", ""},
		{"retired literal (38,855 real rows)", "No va a dar pago"},
		{"retired literal (16,925 real rows)", "Dijo que no va a pagar"},
		{"retired literal (3,682 real rows)", "Se esconde y no sale"},
		{"a value no app build ever sent", "cobro"},
	}

	for _, tc := range legacy {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			id := uuid.New()
			repo := newFakeVisitasRepo()
			repo.byID[id] = domain.RehydrateVisita(domain.RehydrateVisitaParams{
				ID:            id,
				Cobrador:      "Ramírez García, Jorge",
				CobradorID:    42,
				Fecha:         time.Date(2024, 9, 15, 9, 59, 46, 0, time.UTC),
				FormaCobroID:  87327,
				Lat:           19.432608,
				Lng:           -99.133209,
				TipoVisita:    tc.tipo,
				ZonaClienteID: 7,
				ClienteID:     11486,
			})
			svc := app.NewService(repo, &fakeClock{t: fixedNow})

			got, err := svc.ObtenerVisita(context.Background(), id)

			require.NoError(t, err, "a legacy row must never fail to read")
			require.NotNil(t, got)
			assert.Equal(t, tc.tipo, got.TipoVisita().String(), "the stored value must come back verbatim")
		})
	}
}

// TestRegistrarVisita_RechazaTipoFueraDelCatalogo is the write-path half of
// the pair above: the same values the read path returns happily are refused
// on write, with the distinguishable catalogue error.
func TestRegistrarVisita_RechazaTipoFueraDelCatalogo(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	in := validInput()
	in.TipoVisita = "cobro"

	got, err := svc.RegistrarVisita(context.Background(), in, uuid.New())

	require.Error(t, err)
	assert.Nil(t, got)
	require.ErrorIs(t, err, domain.ErrVisitaTipoInvalido)
	assert.Equal(t, 0, repo.insertCalls, "an invalid tipo must be refused before touching the repo")
}

// TestRegistrarVisita_AceptaLiteralRetirado proves the write path still takes
// what unupdated phones send. "Dijo que no va a pagar" was renamed away in
// the app on 2025-09-02 and was still arriving on 2026-02-18.
func TestRegistrarVisita_AceptaLiteralRetirado(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	in := validInput()
	in.TipoVisita = string(domain.TipoVisitaLegacyDijoQueNoVaAPagar)

	got, err := svc.RegistrarVisita(context.Background(), in, uuid.New())

	require.NoError(t, err, "a phone still running the old build must not be rejected")
	require.NotNil(t, got)
	assert.Equal(t, domain.TipoVisitaLegacyDijoQueNoVaAPagar, got.TipoVisita())
	assert.Equal(t, 1, repo.insertCalls)
}
