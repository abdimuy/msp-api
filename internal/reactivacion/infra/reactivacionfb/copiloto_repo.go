//nolint:misspell // Spanish domain vocabulary (copiloto, conversación, decisión) by project convention.
package reactivacionfb

import "github.com/abdimuy/msp-api/internal/platform/firebird"

// CopilotoRepo implements outbound.ConversacionRepo and outbound.DecisionRepo
// (MSP_RX_CONVERSACION, MSP_RX_TURNO, MSP_RX_DECISION) over the same
// *firebird.Pool as Repo.
//
// It is a separate struct from Repo — NOT an addition to it — for a
// mechanical reason forced by the Go type system: outbound.MensajeRepo
// (implemented by Repo, see mensaje_repo.go) declares
//
//	Insertar(ctx, []*domain.Mensaje) error
//	Listar(ctx, ListarMensajesParams) ([]*domain.Mensaje, error)
//
// while outbound.DecisionRepo declares Insertar(ctx, *domain.Decision) error
// and outbound.ConversacionRepo declares
// Listar(ctx, ListarConversacionesParams) ([]*domain.Conversacion, error).
// A single named type can only have one method named Insertar and one named
// Listar — Go does not support overloading by parameter type — so *Repo
// cannot satisfy both MensajeRepo and ConversacionRepo/DecisionRepo at once;
// the method names collide with incompatible signatures. CopilotoRepo shares
// Repo's *firebird.Pool (no new connection, no new lifecycle to manage), so
// wiring it at the composition root is a one-line
// reactivacionfb.NewCopilotoRepo(pool) alongside reactivacionfb.NewRepo(pool).
//
// NotaReader and ClienteFactsReader have no such collision (GetNotaCliente /
// GetFacts are unique names), so they remain on Repo per the original design
// (nota_repo.go, cliente_facts_repo.go).
type CopilotoRepo struct {
	pool *firebird.Pool
}

// NewCopilotoRepo builds a CopilotoRepo wired to the given pool.
func NewCopilotoRepo(pool *firebird.Pool) *CopilotoRepo {
	return &CopilotoRepo{pool: pool}
}
