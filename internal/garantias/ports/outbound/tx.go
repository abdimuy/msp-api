package outbound

import "context"

// TxRunner is the transaction boundary. Every command that changes a folio
// wraps its work in one: the header, the articles and the pending timeline
// events either all land or none does, and the row lock taken by
// GarantiaRepo.ObtenerParaActualizar lives exactly as long as it needs to.
//
// *firebird.TxManager satisfies this interface as is — there is no adapter
// for it. The implementation is re-entrant (a nested RunInTx joins the
// ambient transaction), so a caller that already runs inside one is free to
// call it again.
type TxRunner interface {
	// RunInTx runs fn inside a transaction, committing on success and
	// rolling back on error.
	RunInTx(ctx context.Context, fn func(ctx context.Context) error) error
}
