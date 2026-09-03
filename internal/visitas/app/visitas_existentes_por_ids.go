package app

import (
	"context"

	"github.com/google/uuid"
)

// VisitasExistentesPorIDs answers "which of these visita ids do you already
// have?" — the query behind the visits reconciler channel (GET
// /v2/visitas/by-ids). Every id in ids was generated and uploaded by the
// phone itself; the phone never downloads visits, so this is purely a
// confirmation channel, not a read/list endpoint. An id the server has never
// seen is simply absent from the result — never an error, and the expected,
// common case. Pass-through to the repo; see
// outbound.VisitasRepo.FindExistingIDs.
func (s *Service) VisitasExistentesPorIDs(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error) {
	return s.repo.FindExistingIDs(ctx, ids)
}
