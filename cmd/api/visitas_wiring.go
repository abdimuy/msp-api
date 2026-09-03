//nolint:misspell // visitas vocabulary is Spanish per project convention.
package main

import (
	"context"
	"io"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	ventasoutbound "github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
	visitasapp "github.com/abdimuy/msp-api/internal/visitas/app"
	visitasfb "github.com/abdimuy/msp-api/internal/visitas/infra/visitasfb"
	visitasoutbound "github.com/abdimuy/msp-api/internal/visitas/ports/outbound"
)

// provideVisitasClock returns the production UTC clock for the visitas module.
func provideVisitasClock() visitasoutbound.Clock {
	return visitasoutbound.ProductionClock{}
}

// provideVisitasRepoConcrete builds the Firebird-backed repo. It is exposed
// as a concrete type so both visitas ports below are satisfied by the SAME
// instance — the visita row and its images must be able to share one
// transaction, and two separately-constructed repos would only make that
// harder to see.
func provideVisitasRepoConcrete(pool *firebird.Pool) *visitasfb.Repo {
	return visitasfb.New(pool)
}

// provideVisitasRepo exposes the concrete repo as the VisitasRepo port.
func provideVisitasRepo(r *visitasfb.Repo) visitasoutbound.VisitasRepo { return r }

// provideVisitasImagenesRepo exposes the same concrete repo as the child
// collection port over MSP_VISITAS_IMAGENES.
func provideVisitasImagenesRepo(r *visitasfb.Repo) visitasoutbound.VisitasImagenesRepo { return r }

// provideVisitasStorage wraps the shared ventas FilesystemProvider in a
// visitas-shaped adapter. All three modules that store blobs — ventas,
// cobranza and now visitas — write under the same STORAGE_DIR tree, but each
// sees only its own port interface so the vertical-slice boundary holds
// (CLAUDE.md §2). Exactly the arrangement provideCobranzaStorage already uses.
func provideVisitasStorage(ventasStorage ventasoutbound.StorageProvider) visitasoutbound.StorageProvider {
	return &visitasStorageAdapter{inner: ventasStorage}
}

// provideVisitasService assembles the visitas command service, including the
// photo path. The transaction manager is the shared *firebird.TxManager,
// which satisfies visitasapp.TxRunner implicitly.
func provideVisitasService(
	repo visitasoutbound.VisitasRepo,
	imagenes visitasoutbound.VisitasImagenesRepo,
	storage visitasoutbound.StorageProvider,
	txMgr *firebird.TxManager,
	clock visitasoutbound.Clock,
) *visitasapp.Service {
	return visitasapp.NewService(repo, clock, visitasapp.WithImagenes(imagenes, storage, txMgr))
}

// visitasStorageAdapter re-packs a ventas StorageProvider as a visitas one.
// The method shapes are identical; only the StorageObject type differs, and
// it differs precisely because neither module may import the other's ports.
type visitasStorageAdapter struct {
	inner ventasoutbound.StorageProvider
}

func (a *visitasStorageAdapter) Store(ctx context.Context, key, contentType string, sizeBytes int64, body io.Reader) error {
	return a.inner.Store(ctx, key, contentType, sizeBytes, body)
}

func (a *visitasStorageAdapter) Get(ctx context.Context, key string) (visitasoutbound.StorageObject, error) {
	obj, err := a.inner.Get(ctx, key)
	if err != nil {
		return visitasoutbound.StorageObject{}, err
	}
	return visitasoutbound.StorageObject{
		Body:        obj.Body,
		ContentType: obj.ContentType,
		SizeBytes:   obj.SizeBytes,
	}, nil
}

func (a *visitasStorageAdapter) Delete(ctx context.Context, key string) error {
	return a.inner.Delete(ctx, key)
}
