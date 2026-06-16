//nolint:misspell // Spanish vocabulary (clientes, busqueda, reindexar, etc.) per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// controlledSearchIndex is a full SearchIndex implementation for reindex tests,
// giving independent control over Buscar and Reconciliar errors.
type controlledSearchIndex struct {
	ready        bool
	ids          []int
	busErr       error
	reconcileErr error
	captured     []outbound.SearchDoc
}

func (c *controlledSearchIndex) EstaListo() bool { return c.ready }

func (c *controlledSearchIndex) Buscar(_ context.Context, _ string, _ int) ([]int, error) {
	return c.ids, c.busErr
}

func (c *controlledSearchIndex) Reconciliar(_ context.Context, docs []outbound.SearchDoc) error {
	c.captured = docs
	return c.reconcileErr
}

// buildReindexSvc constructs a *app.Service wired to the given fakes.
func buildReindexSvc(repo *fakeClientesRepo, idx *controlledSearchIndex) *app.Service {
	return app.NewService(repo, &fakeAnalyticsClient{}, idx, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})
}

func TestReindexarBusqueda_HappyPath(t *testing.T) {
	t.Parallel()

	docs := []outbound.SearchDoc{
		{ClienteID: 101, Texto: "García López Ramón"},
		{ClienteID: 202, Texto: "Martínez Reyes Sofía"},
		{ClienteID: 303, Texto: "Pérez Villanueva Carlos"},
	}
	repo := &fakeClientesRepo{docs: docs}
	idx := &controlledSearchIndex{}
	svc := buildReindexSvc(repo, idx)

	n, err := svc.ReindexarBusqueda(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Len(t, idx.captured, 3)
}

func TestReindexarBusqueda_EmptyDocs(t *testing.T) {
	t.Parallel()

	repo := &fakeClientesRepo{docs: []outbound.SearchDoc{}}
	idx := &controlledSearchIndex{}
	svc := buildReindexSvc(repo, idx)

	n, err := svc.ReindexarBusqueda(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestReindexarBusqueda_RepoError(t *testing.T) {
	t.Parallel()

	repo := &fakeClientesRepo{docsErr: errors.New("firebird timeout")}
	idx := &controlledSearchIndex{}
	svc := buildReindexSvc(repo, idx)

	_, err := svc.ReindexarBusqueda(context.Background())
	require.Error(t, err)
	var ae *apperror.Error
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, apperror.KindInternal, ae.Kind)
	assert.Equal(t, "reindexar_leer_docs_failed", ae.Code)
}

func TestReindexarBusqueda_ReconciliarError(t *testing.T) {
	t.Parallel()

	docs := []outbound.SearchDoc{{ClienteID: 1, Texto: "Test"}}
	repo := &fakeClientesRepo{docs: docs}
	idx := &controlledSearchIndex{reconcileErr: errors.New("index full")}
	svc := buildReindexSvc(repo, idx)

	_, err := svc.ReindexarBusqueda(context.Background())
	require.Error(t, err)
	var ae *apperror.Error
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, apperror.KindInternal, ae.Kind)
	assert.Equal(t, "reindexar_reconciliar_failed", ae.Code)
}
