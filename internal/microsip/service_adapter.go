//nolint:misspell // microsip adapter — Spanish vocabulary (Articulo, Almacen) per project convention.
package microsip

// Adapter that wraps the app-layer Service so it satisfies the cross-module
// Catalogo interface. Lives in the contract package so depguard can enforce
// the rule that consumers (reactivación, cmd/api) import only "microsip",
// never "microsip/app" or "microsip/domain".

import (
	"context"

	"github.com/abdimuy/msp-api/internal/microsip/app"
)

// ServiceAdapter projects the app.Service onto the Catalogo contract. It is
// the single bridge between the microsip internals (domain, app) and the
// cross-module surface. Nothing here implements business logic; it only
// re-shapes outputs across the contract boundary (parsing the legacy Precios
// string into a map via ArticuloCatalogoFromDomain).
type ServiceAdapter struct {
	inner *app.Service
}

// NewServiceAdapter wraps a built app.Service so it can be handed to consumers
// as a Catalogo.
func NewServiceAdapter(inner *app.Service) *ServiceAdapter {
	return &ServiceAdapter{inner: inner}
}

// Compile-time check.
var _ Catalogo = (*ServiceAdapter)(nil)

// ListarEnStock delegates to the inner service (which already filters
// EXISTENCIAS > 0) and projects each domain row onto the contract DTO.
func (a *ServiceAdapter) ListarEnStock(ctx context.Context, almacenID int, buscar string) ([]ArticuloCatalogo, error) {
	arts, err := a.inner.ListarArticulosDelAlmacen(ctx, almacenID, buscar)
	if err != nil {
		return nil, err
	}
	out := make([]ArticuloCatalogo, len(arts))
	for i, art := range arts {
		out[i] = ArticuloCatalogoFromDomain(art)
	}
	return out, nil
}
