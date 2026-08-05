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

// ─── Fixed test time ────────────────────────────────────────────────────────

var fixedNow = time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

// ─── fakeClock ──────────────────────────────────────────────────────────────

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }

// ─── fakeVisitasRepo ────────────────────────────────────────────────────────

// fakeVisitasRepo is an in-memory stand-in for outbound.VisitasRepo. Insert
// and FindByID errors are injectable so tests can drive every branch of
// RegistrarVisita without a real Firebird connection.
type fakeVisitasRepo struct {
	byID map[uuid.UUID]*domain.Visita

	insertErr   error // when non-nil, Insert returns this error instead of storing
	findByIDErr error // when non-nil, FindByID returns this error instead of looking up

	insertCalls   int
	findByIDCalls int
}

func newFakeVisitasRepo() *fakeVisitasRepo {
	return &fakeVisitasRepo{byID: make(map[uuid.UUID]*domain.Visita)}
}

func (r *fakeVisitasRepo) Insert(_ context.Context, v *domain.Visita) error {
	r.insertCalls++
	if r.insertErr != nil {
		return r.insertErr
	}
	r.byID[v.ID()] = v
	return nil
}

func (r *fakeVisitasRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Visita, error) {
	r.findByIDCalls++
	if r.findByIDErr != nil {
		return nil, r.findByIDErr
	}
	v, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrVisitaNoEncontrada
	}
	return v, nil
}

// ─── Test helpers ───────────────────────────────────────────────────────────

// validInput returns a RegistrarVisitaInput that passes domain validation,
// using realistic Mexican Spanish data. Each call mints a fresh UUID so
// multiple instances never collide by default.
func validInput() app.RegistrarVisitaInput {
	return app.RegistrarVisitaInput{
		ID:            uuid.New(),
		Cobrador:      "Ramírez García, Jorge",
		CobradorID:    42,
		Fecha:         fixedNow.Add(-time.Hour),
		FormaCobroID:  87327,
		Lat:           19.432608,
		Lng:           -99.133209,
		Nota:          "María salió, vuelve mañana",
		TipoVisita:    "cobro",
		ZonaClienteID: 7,
		ClienteID:     11486,
	}
}

// ─── Tests ──────────────────────────────────────────────────────────────────

// TestRegistrarVisita_HappyPath verifies that a fresh, valid input builds and
// stores a Visita, and the returned aggregate reflects every input field.
func TestRegistrarVisita_HappyPath(t *testing.T) {
	t.Parallel()
	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	by := uuid.New()
	in := validInput()

	got, err := svc.RegistrarVisita(context.Background(), in, by)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, in.ID, got.ID())
	assert.Equal(t, in.Cobrador, got.Cobrador())
	assert.Equal(t, in.CobradorID, got.CobradorID())
	assert.True(t, in.Fecha.Equal(got.Fecha()))
	assert.Equal(t, in.FormaCobroID, got.FormaCobroID())
	assert.InDelta(t, in.Lat, got.Lat(), 0)
	assert.InDelta(t, in.Lng, got.Lng(), 0)
	assert.Equal(t, in.Nota, got.Nota())
	assert.Equal(t, in.TipoVisita, got.TipoVisita())
	assert.Equal(t, in.ZonaClienteID, got.ZonaClienteID())
	assert.Equal(t, in.ClienteID, got.ClienteID())
	assert.Nil(t, got.ImpteDoctoCCID())

	assert.Equal(t, 1, repo.insertCalls, "Insert must be called exactly once")
	assert.Equal(t, 0, repo.findByIDCalls, "FindByID must not be called on the happy path")

	stored, ok := repo.byID[in.ID]
	require.True(t, ok, "the visita must have been persisted in the fake repo")
	assert.Equal(t, got, stored)
}

// TestRegistrarVisita_HappyPath_WithImpteDoctoCCID verifies the optional
// pointer field round-trips through the service when set.
func TestRegistrarVisita_HappyPath_WithImpteDoctoCCID(t *testing.T) {
	t.Parallel()
	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	in := validInput()
	impte := 789012
	in.ImpteDoctoCCID = &impte

	got, err := svc.RegistrarVisita(context.Background(), in, uuid.New())

	require.NoError(t, err)
	require.NotNil(t, got.ImpteDoctoCCID())
	assert.Equal(t, impte, *got.ImpteDoctoCCID())
}

// TestRegistrarVisita_IdempotentReplay verifies that when Insert reports
// ErrVisitaYaExiste (duplicate ID), RegistrarVisita returns the already-stored
// visita via FindByID instead of propagating the conflict.
func TestRegistrarVisita_IdempotentReplay(t *testing.T) {
	t.Parallel()
	repo := newFakeVisitasRepo()
	repo.insertErr = domain.ErrVisitaYaExiste
	in := validInput()
	existing, err := domain.NewVisita(domain.CrearVisitaParams{
		ID:             in.ID,
		Cobrador:       in.Cobrador,
		CobradorID:     in.CobradorID,
		Fecha:          in.Fecha,
		FormaCobroID:   in.FormaCobroID,
		Lat:            in.Lat,
		Lng:            in.Lng,
		Nota:           in.Nota,
		TipoVisita:     in.TipoVisita,
		ZonaClienteID:  in.ZonaClienteID,
		ClienteID:      in.ClienteID,
		ImpteDoctoCCID: in.ImpteDoctoCCID,
		CreatedBy:      uuid.New(),
		Now:            fixedNow,
	})
	require.NoError(t, err)
	repo.byID[in.ID] = existing

	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	got, err := svc.RegistrarVisita(context.Background(), in, uuid.New())

	require.NoError(t, err, "idempotent replay must not surface ErrVisitaYaExiste")
	require.NotNil(t, got)
	assert.Equal(t, existing, got, "replay must return the FindByID result field-for-field")
	assert.Equal(t, 1, repo.insertCalls)
	assert.Equal(t, 1, repo.findByIDCalls, "FindByID must be called exactly once on replay")
}

// TestRegistrarVisita_IdempotentReplay_FindByIDFails verifies that if the
// FindByID fallback (on ErrVisitaYaExiste) itself fails, RegistrarVisita
// propagates that error rather than masking it.
func TestRegistrarVisita_IdempotentReplay_FindByIDFails(t *testing.T) {
	t.Parallel()
	repo := newFakeVisitasRepo()
	repo.insertErr = domain.ErrVisitaYaExiste
	wantErr := errors.New("boom: findByID broke")
	repo.findByIDErr = wantErr

	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	got, err := svc.RegistrarVisita(context.Background(), validInput(), uuid.New())

	require.Error(t, err)
	assert.Nil(t, got)
	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, 1, repo.findByIDCalls)
}

// TestRegistrarVisita_DomainValidationError_PropagatesUnchanged verifies that
// an input rejected by domain.NewVisita never reaches the repo, and the
// original sentinel error is returned unchanged.
func TestRegistrarVisita_DomainValidationError_PropagatesUnchanged(t *testing.T) {
	t.Parallel()
	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	in := validInput()
	in.ClienteID = 0 // triggers ErrVisitaClienteRequerido

	got, err := svc.RegistrarVisita(context.Background(), in, uuid.New())

	require.Error(t, err)
	assert.Nil(t, got)
	require.ErrorIs(t, err, domain.ErrVisitaClienteRequerido)
	assert.Equal(t, 0, repo.insertCalls, "repo must never be called when domain validation fails")
	assert.Equal(t, 0, repo.findByIDCalls)
}

// TestRegistrarVisita_DomainValidationError_FechaFutura verifies a second
// validation branch propagates unchanged too — guards against a fix that
// only special-cases one sentinel.
func TestRegistrarVisita_DomainValidationError_FechaFutura(t *testing.T) {
	t.Parallel()
	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	in := validInput()
	in.Fecha = fixedNow.Add(72 * time.Hour) // beyond the 48h tolerance

	got, err := svc.RegistrarVisita(context.Background(), in, uuid.New())

	require.Error(t, err)
	assert.Nil(t, got)
	require.ErrorIs(t, err, domain.ErrVisitaFechaFutura)
	assert.Equal(t, 0, repo.insertCalls)
}

// TestRegistrarVisita_RepoNonConflictError_Propagates verifies that a repo
// error unrelated to idempotency (e.g. a connection failure) is returned
// as-is, without a FindByID fallback.
func TestRegistrarVisita_RepoNonConflictError_Propagates(t *testing.T) {
	t.Parallel()
	repo := newFakeVisitasRepo()
	wantErr := errors.New("boom: firebird unreachable")
	repo.insertErr = wantErr

	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	got, err := svc.RegistrarVisita(context.Background(), validInput(), uuid.New())

	require.Error(t, err)
	assert.Nil(t, got)
	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, 0, repo.findByIDCalls, "FindByID must not be called for a non-conflict error")
}
