// Package configfb is the Firebird-backed implementation of the config
// module's outbound ports: MSP_CFG_VENDEDOR_MICROSIP (our own table, written
// per CLAUDE.md §1 — no DB defaults, every value bound from Go) and the
// read-only Microsip LISTAS_ATRIBUTOS catalog.
package configfb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
	"github.com/abdimuy/msp-api/internal/config/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// LISTAS_ATRIBUTOS.ATRIBUTO_ID values for the three credit-vendor free-list
// fields on LIBRES_CARGOS_CC (VENDEDOR_1/2/3). See migration 000032.
const (
	atributoVendedor1 = 19985
	atributoVendedor2 = 19986
	atributoVendedor3 = 19987
)

// ConfigRepo implements outbound.ConfigRepo and outbound.CatalogoReader.
type ConfigRepo struct {
	pool *firebird.Pool
}

// NewConfigRepo builds a ConfigRepo wired to the given pool.
func NewConfigRepo(pool *firebird.Pool) *ConfigRepo {
	return &ConfigRepo{pool: pool}
}

// Compile-time assertions: ConfigRepo satisfies both outbound ports.
var (
	_ outbound.ConfigRepo     = (*ConfigRepo)(nil)
	_ outbound.CatalogoReader = (*ConfigRepo)(nil)
)

// ─── ConfigRepo: MSP_CFG_VENDEDOR_MICROSIP ─────────────────────────────────

const selectVendedorMappings = `
SELECT USUARIO_ID, VENDEDOR_LISTA_ID_1, VENDEDOR_LISTA_ID_2, VENDEDOR_LISTA_ID_3
FROM MSP_CFG_VENDEDOR_MICROSIP`

// ListarVendedorMappings returns every row currently stored.
func (r *ConfigRepo) ListarVendedorMappings(ctx context.Context) ([]configdomain.VendedorMapping, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, selectVendedorMappings)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var result []configdomain.VendedorMapping
	for rows.Next() {
		var usuarioIDStr string
		var l1, l2, l3 sql.NullInt64
		if serr := rows.Scan(&usuarioIDStr, &l1, &l2, &l3); serr != nil {
			return nil, firebird.MapError(serr)
		}
		usuarioID, perr := uuid.Parse(usuarioIDStr)
		if perr != nil {
			return nil, fmt.Errorf("configfb: parse usuario_id %q: %w", usuarioIDStr, perr)
		}
		result = append(result, configdomain.VendedorMapping{
			UsuarioID: usuarioID,
			ListaID1:  nullIntPtr(l1),
			ListaID2:  nullIntPtr(l2),
			ListaID3:  nullIntPtr(l3),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return result, nil
}

const updateVendedorMapping = `
UPDATE MSP_CFG_VENDEDOR_MICROSIP
   SET VENDEDOR_LISTA_ID_1 = ?, VENDEDOR_LISTA_ID_2 = ?, VENDEDOR_LISTA_ID_3 = ?
 WHERE USUARIO_ID = ?`

const insertVendedorMapping = `
INSERT INTO MSP_CFG_VENDEDOR_MICROSIP
	(USUARIO_ID, VENDEDOR_LISTA_ID_1, VENDEDOR_LISTA_ID_2, VENDEDOR_LISTA_ID_3)
VALUES (?, ?, ?, ?)`

// UpsertVendedorMapping inserts or updates the mapping for m.UsuarioID via
// UPDATE-then-INSERT inside a single transaction — NOT MERGE, which the
// firebirdsql driver (v0.9.19) fails to bind a `?` parameter inside a
// `MERGE USING (SELECT ?)` subselect (-804).
func (r *ConfigRepo) UpsertVendedorMapping(ctx context.Context, m configdomain.VendedorMapping) error {
	return firebird.RunInTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)

		res, err := q.ExecContext(ctx, updateVendedorMapping,
			intPtrToNull(m.ListaID1), intPtrToNull(m.ListaID2), intPtrToNull(m.ListaID3),
			m.UsuarioID.String(),
		)
		if err != nil {
			return firebird.MapError(err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			return nil
		}

		_, err = q.ExecContext(ctx, insertVendedorMapping,
			m.UsuarioID.String(),
			intPtrToNull(m.ListaID1), intPtrToNull(m.ListaID2), intPtrToNull(m.ListaID3),
		)
		if err != nil {
			return firebird.MapError(err)
		}
		return nil
	})
}

const deleteVendedorMapping = `DELETE FROM MSP_CFG_VENDEDOR_MICROSIP WHERE USUARIO_ID = ?`

// DeleteVendedorMapping removes the row for usuarioID entirely, if present.
func (r *ConfigRepo) DeleteVendedorMapping(ctx context.Context, usuarioID uuid.UUID) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	if _, err := q.ExecContext(ctx, deleteVendedorMapping, usuarioID.String()); err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// ─── CatalogoReader: LISTAS_ATRIBUTOS (Microsip, read-only) ────────────────

// ResolverNombresLista returns lista_id → VALOR_DESPLEGADO for the given ids.
// Returns an empty (non-nil) map for an empty input.
func (r *ConfigRepo) ResolverNombresLista(ctx context.Context, listaIDs []int) (map[int]string, error) {
	if len(listaIDs) == 0 {
		return map[int]string{}, nil
	}

	placeholders := make([]string, len(listaIDs))
	args := make([]any, len(listaIDs))
	for i, id := range listaIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := `
SELECT LISTA_ATRIB_ID, VALOR_DESPLEGADO
FROM LISTAS_ATRIBUTOS
WHERE LISTA_ATRIB_ID IN (` + strings.Join(placeholders, ",") + `)`

	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[int]string, len(listaIDs))
	for rows.Next() {
		var id int
		var nombreRaw firebird.Win1252
		if serr := rows.Scan(&id, &nombreRaw); serr != nil {
			return nil, firebird.MapError(serr)
		}
		result[id] = string(nombreRaw)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return result, nil
}

const selectIdentidadesMicrosip = `
SELECT LISTA_ATRIB_ID, ATRIBUTO_ID, VALOR_DESPLEGADO
FROM LISTAS_ATRIBUTOS
WHERE ATRIBUTO_ID IN (?, ?, ?)`

// identidadAcc accumulates the up-to-three lista ids for one VALOR_DESPLEGADO
// while scanning ListarIdentidadesMicrosip rows.
type identidadAcc struct {
	listaID1, listaID2, listaID3 *int
}

// ListarIdentidadesMicrosip groups LISTAS_ATRIBUTOS rows for the three
// credit-vendor attributes by VALOR_DESPLEGADO, preserving first-seen order.
func (r *ConfigRepo) ListarIdentidadesMicrosip(ctx context.Context) ([]configdomain.IdentidadMicrosip, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, selectIdentidadesMicrosip,
		atributoVendedor1, atributoVendedor2, atributoVendedor3)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	byNombre := make(map[string]*identidadAcc)
	var order []string
	for rows.Next() {
		var listaID, atributoID int
		var nombreRaw firebird.Win1252
		if serr := rows.Scan(&listaID, &atributoID, &nombreRaw); serr != nil {
			return nil, firebird.MapError(serr)
		}
		nombre := string(nombreRaw)
		acc, ok := byNombre[nombre]
		if !ok {
			acc = &identidadAcc{}
			byNombre[nombre] = acc
			order = append(order, nombre)
		}
		id := listaID
		switch atributoID {
		case atributoVendedor1:
			acc.listaID1 = &id
		case atributoVendedor2:
			acc.listaID2 = &id
		case atributoVendedor3:
			acc.listaID3 = &id
		}
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}

	result := make([]configdomain.IdentidadMicrosip, 0, len(order))
	for _, nombre := range order {
		acc := byNombre[nombre]
		matchCount := 0
		for _, v := range []*int{acc.listaID1, acc.listaID2, acc.listaID3} {
			if v != nil {
				matchCount++
			}
		}
		result = append(result, configdomain.IdentidadMicrosip{
			Nombre:     nombre,
			V1ListaID:  acc.listaID1,
			V2ListaID:  acc.listaID2,
			V3ListaID:  acc.listaID3,
			MatchCount: matchCount,
		})
	}
	return result, nil
}

const selectListaIDPerteneceAtributo = `
SELECT 1 FROM LISTAS_ATRIBUTOS WHERE LISTA_ATRIB_ID = ? AND ATRIBUTO_ID = ?`

// ListaIDPerteneceAtributo reports whether listaID exists under atributoID.
func (r *ConfigRepo) ListaIDPerteneceAtributo(ctx context.Context, listaID, atributoID int) (bool, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	var one int
	err := q.QueryRowContext(ctx, selectListaIDPerteneceAtributo, listaID, atributoID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, firebird.MapError(err)
	}
	return true, nil
}

// ─── scan helpers ───────────────────────────────────────────────────────────

// nullIntPtr converts a nullable INTEGER scan target into *int (nil when NULL).
func nullIntPtr(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

// intPtrToNull converts *int into a bindable sql.NullInt64 (NULL when nil).
func intPtrToNull(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}
