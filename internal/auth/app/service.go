// Package app contains the auth module's command and query services. It
// depends only on the auth domain, the module's outbound ports, and a small
// set of platform helpers. Wiring (database pool, http handlers) lives in
// infra; cross-module surfaces live in the auth root package.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Service is the auth module's command/query surface. Handlers depend on
// *Service; everything Service depends on goes through the outbound ports.
type Service struct {
	usuarios outbound.UsuarioRepo
	roles    outbound.RolRepo
	permisos outbound.PermisoRepo
	clock    outbound.Clock
	outbox   outbound.OutboxEnqueuer
	firebase outbound.FirebaseClient
	txMgr    *firebird.TxManager
	// nombreResolver is optional. Tests omit it; production wires it via
	// WithNombreResolver. When nil, first-login user creation derives the
	// name from the token claim / email exactly as before — actor names
	// stay best-effort.
	nombreResolver outbound.NombreResolver
}

// WithNombreResolver attaches a NombreResolver so first-login user creation
// prefers the canonical name from Firestore (users/{uid}.NOMBRE) over the
// frequently-empty token name claim. Returns s for fluent wiring at the
// composition root.
func (s *Service) WithNombreResolver(r outbound.NombreResolver) *Service {
	s.nombreResolver = r
	return s
}

// NewService builds a Service wired against the given ports. The
// *firebird.TxManager is required so multi-step writes (e.g. SyncFromFirebase)
// run inside a single transaction; pass nil only in tests that exercise
// in-memory fakes which do not need transactional semantics.
func NewService(
	usuarios outbound.UsuarioRepo,
	roles outbound.RolRepo,
	permisos outbound.PermisoRepo,
	clock outbound.Clock,
	outbox outbound.OutboxEnqueuer,
	firebase outbound.FirebaseClient,
	txMgr *firebird.TxManager,
) *Service {
	return &Service{
		usuarios: usuarios,
		roles:    roles,
		permisos: permisos,
		clock:    clock,
		outbox:   outbox,
		firebase: firebase,
		txMgr:    txMgr,
	}
}

// runInTx delegates to the configured TxManager when one is wired, otherwise
// invokes fn directly so in-memory tests can omit a TxManager.
func (s *Service) runInTx(ctx context.Context, fn func(context.Context) error) error {
	if s.txMgr == nil {
		return fn(ctx)
	}
	return s.txMgr.RunInTx(ctx, fn)
}
