package configfb

// This file adds the zonas/cajas administration slice to ConfigRepo:
// MSP_CFG_ZONA_CAJA (our own table, written per CLAUDE.md §1 — every value
// bound from Go, -1 sentinel bound explicitly, never a DB default) and the
// read-only Microsip catalogs it references (ZONAS_CLIENTES, CAJAS, CAJEROS,
// VENDEDORES, COBRADORES).

import (
	"context"
	"database/sql"
	"errors"

	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ─── ConfigRepo: MSP_CFG_ZONA_CAJA ─────────────────────────────────────────

const selectZonaCajaConfigs = `
SELECT ZONA_CLIENTE_ID, CAJA_ID, CAJERO_ID, VENDEDOR_ID, COBRADOR_ID
FROM MSP_CFG_ZONA_CAJA`

// ListarZonaCajaConfigs returns every row currently stored in
// MSP_CFG_ZONA_CAJA.
func (r *ConfigRepo) ListarZonaCajaConfigs(ctx context.Context) ([]configdomain.ZonaCajaConfig, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, selectZonaCajaConfigs)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var result []configdomain.ZonaCajaConfig
	for rows.Next() {
		var c configdomain.ZonaCajaConfig
		if serr := rows.Scan(&c.ZonaClienteID, &c.CajaID, &c.CajeroID, &c.VendedorID, &c.CobradorID); serr != nil {
			return nil, firebird.MapError(serr)
		}
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return result, nil
}

const updateZonaCajaConfig = `
UPDATE MSP_CFG_ZONA_CAJA
   SET CAJA_ID = ?, CAJERO_ID = ?, VENDEDOR_ID = ?, COBRADOR_ID = ?
 WHERE ZONA_CLIENTE_ID = ?`

const insertZonaCajaConfig = `
INSERT INTO MSP_CFG_ZONA_CAJA
	(ZONA_CLIENTE_ID, CAJA_ID, CAJERO_ID, VENDEDOR_ID, COBRADOR_ID)
VALUES (?, ?, ?, ?, ?)`

// UpsertZonaCajaConfig inserts or updates the mapping for c.ZonaClienteID via
// UPDATE-then-INSERT inside a single transaction — NOT MERGE, per the same
// firebirdsql driver limitation documented on UpsertVendedorMapping.
func (r *ConfigRepo) UpsertZonaCajaConfig(ctx context.Context, c configdomain.ZonaCajaConfig) error {
	return firebird.RunInTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)

		res, err := q.ExecContext(ctx, updateZonaCajaConfig,
			c.CajaID, c.CajeroID, c.VendedorID, c.CobradorID,
			c.ZonaClienteID,
		)
		if err != nil {
			return firebird.MapError(err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			return nil
		}

		_, err = q.ExecContext(ctx, insertZonaCajaConfig,
			c.ZonaClienteID, c.CajaID, c.CajeroID, c.VendedorID, c.CobradorID,
		)
		if err != nil {
			return firebird.MapError(err)
		}
		return nil
	})
}

// ─── CatalogoReader: Microsip catalogs (read-only) ─────────────────────────

// listCatalogoRef runs a `SELECT <id>, NOMBRE FROM <table> ORDER BY NOMBRE`
// query and scans it into []configdomain.CatalogoRef. NOMBRE is read via
// firebird.Win1252 like every other legacy Microsip text column.
func (r *ConfigRepo) listCatalogoRef(ctx context.Context, query string) ([]configdomain.CatalogoRef, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var result []configdomain.CatalogoRef
	for rows.Next() {
		var id int
		var nombreRaw firebird.Win1252
		if serr := rows.Scan(&id, &nombreRaw); serr != nil {
			return nil, firebird.MapError(serr)
		}
		result = append(result, configdomain.CatalogoRef{ID: id, Nombre: string(nombreRaw)})
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return result, nil
}

const selectZonas = `SELECT ZONA_CLIENTE_ID, NOMBRE FROM ZONAS_CLIENTES ORDER BY NOMBRE`

// ListarZonas returns every ZONAS_CLIENTES row (id + NOMBRE).
func (r *ConfigRepo) ListarZonas(ctx context.Context) ([]configdomain.CatalogoRef, error) {
	return r.listCatalogoRef(ctx, selectZonas)
}

const selectCajas = `SELECT CAJA_ID, NOMBRE FROM CAJAS ORDER BY NOMBRE`

// ListarCajas returns every CAJAS row (id + NOMBRE).
func (r *ConfigRepo) ListarCajas(ctx context.Context) ([]configdomain.CatalogoRef, error) {
	return r.listCatalogoRef(ctx, selectCajas)
}

// selectCajeros reads Microsip's CAJEROS catalog (confirmed empirically
// against the dev DB: CAJEROS.CAJERO_ID / CAJEROS.NOMBRE, verified 2026-07-18
// against MSP_CFG_ZONA_CAJA.CAJERO_ID values — every id resolves to a
// RUTA-named cajero).
const selectCajeros = `SELECT CAJERO_ID, NOMBRE FROM CAJEROS ORDER BY NOMBRE`

// ListarCajeros returns every CAJEROS row (id + NOMBRE).
func (r *ConfigRepo) ListarCajeros(ctx context.Context) ([]configdomain.CatalogoRef, error) {
	return r.listCatalogoRef(ctx, selectCajeros)
}

const selectVendedoresCatalogo = `SELECT VENDEDOR_ID, NOMBRE FROM VENDEDORES ORDER BY NOMBRE`

// ListarVendedoresCatalogo returns every VENDEDORES row (id + NOMBRE).
func (r *ConfigRepo) ListarVendedoresCatalogo(ctx context.Context) ([]configdomain.CatalogoRef, error) {
	return r.listCatalogoRef(ctx, selectVendedoresCatalogo)
}

const selectCobradores = `SELECT COBRADOR_ID, NOMBRE FROM COBRADORES ORDER BY NOMBRE`

// ListarCobradores returns every COBRADORES row (id + NOMBRE).
func (r *ConfigRepo) ListarCobradores(ctx context.Context) ([]configdomain.CatalogoRef, error) {
	return r.listCatalogoRef(ctx, selectCobradores)
}

// existeEnCatalogo runs a `SELECT 1 FROM <table> WHERE <id column> = ?`
// existence check.
func (r *ConfigRepo) existeEnCatalogo(ctx context.Context, query string, id int) (bool, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	var one int
	err := q.QueryRowContext(ctx, query, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, firebird.MapError(err)
	}
	return true, nil
}

const selectZonaExiste = `SELECT 1 FROM ZONAS_CLIENTES WHERE ZONA_CLIENTE_ID = ?`

// ZonaExiste reports whether id exists in ZONAS_CLIENTES.
func (r *ConfigRepo) ZonaExiste(ctx context.Context, id int) (bool, error) {
	return r.existeEnCatalogo(ctx, selectZonaExiste, id)
}

const selectCajaExiste = `SELECT 1 FROM CAJAS WHERE CAJA_ID = ?`

// CajaExiste reports whether id exists in CAJAS.
func (r *ConfigRepo) CajaExiste(ctx context.Context, id int) (bool, error) {
	return r.existeEnCatalogo(ctx, selectCajaExiste, id)
}

const selectCajeroExiste = `SELECT 1 FROM CAJEROS WHERE CAJERO_ID = ?`

// CajeroExiste reports whether id exists in CAJEROS.
func (r *ConfigRepo) CajeroExiste(ctx context.Context, id int) (bool, error) {
	return r.existeEnCatalogo(ctx, selectCajeroExiste, id)
}

const selectVendedorExiste = `SELECT 1 FROM VENDEDORES WHERE VENDEDOR_ID = ?`

// VendedorExiste reports whether id exists in VENDEDORES.
func (r *ConfigRepo) VendedorExiste(ctx context.Context, id int) (bool, error) {
	return r.existeEnCatalogo(ctx, selectVendedorExiste, id)
}

const selectCobradorExiste = `SELECT 1 FROM COBRADORES WHERE COBRADOR_ID = ?`

// CobradorExiste reports whether id exists in COBRADORES.
func (r *ConfigRepo) CobradorExiste(ctx context.Context, id int) (bool, error) {
	return r.existeEnCatalogo(ctx, selectCobradorExiste, id)
}
