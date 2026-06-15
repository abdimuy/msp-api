//nolint:misspell // Spanish vocabulary (clientes, busqueda, reindexar, etc.) per project convention.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ReindexarBusqueda rebuilds the full-text search index from the current client
// directory. Returns the number of documents indexed. Used by the admin refresh
// endpoint and the periodic reconciliation worker.
func (s *Service) ReindexarBusqueda(ctx context.Context) (int, error) {
	const source = "clientes.ReindexarBusqueda"

	docs, err := s.repo.LeerDocumentosBusqueda(ctx)
	if err != nil {
		return 0, apperror.NewInternal(
			"reindexar_leer_docs_failed",
			"error al leer los documentos para reindexar",
		).WithSource(source).WithError(err)
	}

	if err := s.search.Reconciliar(ctx, docs); err != nil {
		return 0, apperror.NewInternal(
			"reindexar_reconciliar_failed",
			"error al reconciliar el índice de búsqueda",
		).WithSource(source).WithError(err)
	}

	return len(docs), nil
}
