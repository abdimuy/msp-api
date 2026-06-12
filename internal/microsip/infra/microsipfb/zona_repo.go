package microsipfb

import (
	"context"
	"fmt"

	"github.com/abdimuy/msp-api/internal/microsip/domain"
	"github.com/abdimuy/msp-api/internal/microsip/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ZonaRepo is the Firebird-backed implementation of
// outbound.ZonaClienteRepo. Listar runs two queries (zonas + top cobrador
// per zona) and concatenates the cobrador onto the zona name to preserve
// the legacy contract: clients receive "<zona> - <cobrador>" so the user
// can disambiguate zonas with shared base names.
type ZonaRepo struct {
	pool *firebird.Pool
}

// NewZonaRepo wires a ZonaRepo to the given pool.
func NewZonaRepo(pool *firebird.Pool) *ZonaRepo {
	return &ZonaRepo{pool: pool}
}

// Compile-time check.
var _ outbound.ZonaClienteRepo = (*ZonaRepo)(nil)

// Listar returns every zona with its top cobrador appended to the name.
// Zonas without a cobrador keep the raw name.
func (r *ZonaRepo) Listar(ctx context.Context) ([]domain.ZonaCliente, error) {
	zonas, err := r.listarZonas(ctx)
	if err != nil {
		return nil, err
	}
	cobradores, err := r.listarTopCobradoresPorZona(ctx)
	if err != nil {
		return nil, err
	}

	for i, z := range zonas {
		if c, ok := cobradores[z.ID]; ok && c != "" {
			zonas[i].Nombre = fmt.Sprintf("%s - %s", z.Nombre, c)
		}
	}
	return zonas, nil
}

func (r *ZonaRepo) listarZonas(ctx context.Context) ([]domain.ZonaCliente, error) {
	rows, err := r.pool.QueryContext(ctx, selectZonasCliente)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.ZonaCliente
	for rows.Next() {
		var (
			id     int
			nombre firebird.Win1252
		)
		if err := rows.Scan(&id, &nombre); err != nil {
			return nil, firebird.MapError(err)
		}
		out = append(out, domain.ZonaCliente{ID: id, Nombre: string(nombre)})
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return out, nil
}

// listarTopCobradoresPorZona returns a map from zona ID to the top
// cobrador's name. The query already filters RN = 1 so each zona appears
// at most once.
func (r *ZonaRepo) listarTopCobradoresPorZona(ctx context.Context) (map[int]string, error) {
	rows, err := r.pool.QueryContext(ctx, selectCobradoresPorZona)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[int]string)
	for rows.Next() {
		var (
			zonaID, cobradorID int
			cobrador           firebird.Win1252
		)
		if err := rows.Scan(&zonaID, &cobradorID, &cobrador); err != nil {
			return nil, firebird.MapError(err)
		}
		out[zonaID] = string(cobrador)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return out, nil
}
