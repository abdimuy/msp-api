// Package outbound declares the outbound ports the microsip module depends
// on. Each port is a Firebird-backed read repo; there are no write ports
// because the module is strictly read-only.
package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/microsip/domain"
)

// AlmacenRepo exposes read access to Microsip's ALMACENES catalog plus the
// article-level breakdown for a single almacen.
type AlmacenRepo interface {
	// Listar returns every visible almacen with its total unit existencias,
	// ordered by existencias descending (matches the legacy contract).
	Listar(ctx context.Context) ([]domain.Almacen, error)

	// Obtener returns a single almacen by ID. Returns nil with no error
	// when the almacen does not exist (the caller maps to 404).
	Obtener(ctx context.Context, almacenID int) (*domain.Almacen, error)

	// ListarArticulos returns articulos with positive existencias for the
	// given almacen, optionally filtered by a substring on the articulo
	// name. An empty buscar string disables the filter.
	ListarArticulos(ctx context.Context, almacenID int, buscar string) ([]domain.ArticuloAlmacen, error)
}

// ZonaClienteRepo exposes read access to the zonas catalog. The returned
// Nombre field is already augmented with the top cobrador per zona to
// preserve 1:1 parity with the legacy API.
type ZonaClienteRepo interface {
	// Listar returns every zona with its name concatenated with the top
	// cobrador (by client count). Zonas with no cobrador keep the raw name.
	Listar(ctx context.Context) ([]domain.ZonaCliente, error)
}
