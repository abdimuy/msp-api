package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// seedRepoVisita inserts a minimal Visita directly into the fake repo's
// store, bypassing NewVisita — VisitasExistentesPorIDs only cares whether an
// id is present, not the shape of the row behind it.
func seedRepoVisita(t *testing.T, repo *fakeVisitasRepo, id uuid.UUID) {
	t.Helper()
	repo.byID[id] = domain.RehydrateVisita(domain.RehydrateVisitaParams{
		ID:         id,
		Cobrador:   "Ramírez García, Jorge",
		ClienteID:  11486,
		TipoVisita: "No se encontraba",
	})
}

func TestVisitasExistentesPorIDs_MixOfKnownAndUnknown(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	known1, known2 := uuid.New(), uuid.New()
	unknown := uuid.New()
	seedRepoVisita(t, repo, known1)
	seedRepoVisita(t, repo, known2)

	got, err := svc.VisitasExistentesPorIDs(context.Background(), []uuid.UUID{known1, unknown, known2})

	require.NoError(t, err)
	assert.ElementsMatch(t, []uuid.UUID{known1, known2}, got,
		"an id the repo has never seen must be simply absent, not an error")
}

func TestVisitasExistentesPorIDs_NoneKnown_ReturnsEmptyNotError(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	got, err := svc.VisitasExistentesPorIDs(context.Background(), []uuid.UUID{uuid.New(), uuid.New()})

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestVisitasExistentesPorIDs_EmptyInput_ReturnsEmptyWithoutTouchingRepo(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	repo.findExistingIDsErr = errors.New("must not be reached")
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	got, err := svc.VisitasExistentesPorIDs(context.Background(), []uuid.UUID{})

	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, 0, repo.findExistingIDsCalls, "an empty ids slice must short-circuit before the repo call")
}

func TestVisitasExistentesPorIDs_DuplicateIDs_NoDuplicatesInResult(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	known := uuid.New()
	seedRepoVisita(t, repo, known)

	got, err := svc.VisitasExistentesPorIDs(context.Background(), []uuid.UUID{known, known, known})

	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{known}, got,
		"a duplicate id in the request must not produce a duplicate in the response — "+
			"a SQL WHERE ID IN (...) answers once per matching row regardless of repetition")
}

func TestVisitasExistentesPorIDs_RepoError_Propagates(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	repo.findExistingIDsErr = errors.New("boom")
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	_, err := svc.VisitasExistentesPorIDs(context.Background(), []uuid.UUID{uuid.New()})

	require.Error(t, err)
	assert.Equal(t, 1, repo.findExistingIDsCalls)
}
