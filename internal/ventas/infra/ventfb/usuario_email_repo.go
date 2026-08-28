//nolint:misspell // Spanish vocabulary (usuarios, vendedores) by convention.
package ventfb

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// UsuarioEmailRepo implements outbound.VendedorUsuarioEmailResolver by
// reading MSP_USUARIOS. It is the server-side replacement for the phone's
// call to POST /v2/usuarios/ensure-vendedores-by-email, minus the "ensure":
// it looks identities up, it never creates them.
type UsuarioEmailRepo struct {
	pool *firebird.Pool
}

// NewUsuarioEmailRepo builds a UsuarioEmailRepo wired to the given pool.
func NewUsuarioEmailRepo(pool *firebird.Pool) *UsuarioEmailRepo {
	return &UsuarioEmailRepo{pool: pool}
}

// Compile-time check: UsuarioEmailRepo satisfies the outbound port.
var _ outbound.VendedorUsuarioEmailResolver = (*UsuarioEmailRepo)(nil)

// UsuariosPorEmail returns id + nombre for every supplied email that has a row
// in MSP_USUARIOS, keyed by the canonical email. Emails without a row are
// absent from the map.
//
// The comparison is LOWER(EMAIL) rather than a bare equality even though the
// auth module lowercases every address it writes: the column is only UNIQUE,
// not case-insensitive, so a row written by anything other than that code
// path could hold capitals, and a bare equality would miss it — producing
// precisely the silent disappearance this whole change exists to end. The
// table holds tens of rows, so giving up the index costs nothing measurable.
//
// Deactivated usuarios are NOT filtered out. Deactivation renames the address
// to deleted-<uuid>-<email>, so those rows cannot match a real address in the
// first place; adding an ACTIVO predicate on top would only make this
// resolver disagree with VendedorUsuarioExistenceChecker, which has none —
// and a venta whose vendedor resolves here but is rejected there would fail
// for a reason nobody could read.
func (r *UsuarioEmailRepo) UsuariosPorEmail(
	ctx context.Context, emails []string,
) (map[string]outbound.UsuarioDeVendedor, error) {
	out := make(map[string]outbound.UsuarioDeVendedor, len(emails))

	unique := make([]string, 0, len(emails))
	seen := make(map[string]struct{}, len(emails))
	for _, e := range emails {
		canonical := domain.NormalizarEmail(e)
		if canonical == "" {
			continue
		}
		if _, dup := seen[canonical]; dup {
			continue
		}
		seen[canonical] = struct{}{}
		unique = append(unique, canonical)
	}
	if len(unique) == 0 {
		return out, nil
	}

	placeholders := strings.Repeat("?,", len(unique))
	placeholders = placeholders[:len(placeholders)-1] // drop trailing comma
	query := "SELECT ID, EMAIL, NOMBRE FROM MSP_USUARIOS WHERE LOWER(EMAIL) IN (" +
		placeholders + ") ORDER BY EMAIL"

	args := make([]any, len(unique))
	for i, e := range unique {
		args[i] = e
	}

	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var rawID, email, nombre string
		if scanErr := rows.Scan(&rawID, &email, &nombre); scanErr != nil {
			return nil, firebird.MapError(scanErr)
		}
		parsed, parseErr := uuid.Parse(strings.TrimSpace(rawID))
		if parseErr != nil {
			// MSP_USUARIOS.ID is CHAR(36) and only ever holds UUIDs we wrote.
			return nil, firebird.MapError(parseErr)
		}
		canonical := domain.NormalizarEmail(email)
		if _, dup := out[canonical]; dup {
			// Two rows whose addresses differ only by case. ORDER BY EMAIL
			// makes the winner deterministic instead of whatever the engine
			// happened to return first.
			continue
		}
		out[canonical] = outbound.UsuarioDeVendedor{
			ID:     parsed,
			Nombre: strings.TrimSpace(nombre),
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, firebird.MapError(rowsErr)
	}
	return out, nil
}
