//nolint:misspell // Spanish vocabulary (zona, cajero, frecuencia, etc.) by convention.
package ventfb

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ─── SQL queries ─────────────────────────────────────────────────────────────

const selectCajaCajero = `
SELECT CAJA_ID, CAJERO_ID, VENDEDOR_ID, COBRADOR_ID
FROM MSP_CFG_ZONA_CAJA
WHERE ZONA_CLIENTE_ID = ?`

const selectFormaDePagoID = `
SELECT FORMA_DE_PAGO_ID
FROM MSP_CFG_FRECUENCIA_FORMA_PAGO
WHERE FRECUENCIA = ?`

const selectCreditoEnMesesID = `
SELECT CREDITO_EN_MESES_ID
FROM MSP_CFG_PLAZO_CREDITO
WHERE PLAZO_MESES = ?`

const selectNumeroDeVendedoresID = `
SELECT NUMERO_DE_VENDEDORES_ID
FROM MSP_CFG_NUM_VENDEDORES
WHERE NUM_VENDEDORES = ?`

const selectAplicarDefaults = `
SELECT SUCURSAL_ID, FORMA_COBRO_CONTADO_ID, FORMA_COBRO_CREDITO_ID
FROM MSP_CFG_APLICAR
WHERE ID = 1`

const selectVendedorListaIDs = `
SELECT VENDEDOR_LISTA_ID_1, VENDEDOR_LISTA_ID_2, VENDEDOR_LISTA_ID_3
FROM MSP_CFG_VENDEDOR_MICROSIP
WHERE USUARIO_ID = ?`

// ─── Repo ─────────────────────────────────────────────────────────────────────

// AplicarConfigRepo implements outbound.AplicarConfig by consulting the
// MSP_CFG_* configuration tables in the Microsip Firebird database.
type AplicarConfigRepo struct {
	pool *firebird.Pool
}

// NewAplicarConfigRepo builds an AplicarConfigRepo wired to the given pool.
func NewAplicarConfigRepo(pool *firebird.Pool) *AplicarConfigRepo {
	return &AplicarConfigRepo{pool: pool}
}

// Compile-time check: AplicarConfigRepo satisfies the outbound port.
var _ outbound.AplicarConfig = (*AplicarConfigRepo)(nil)

// CajaCajero resolves the caja and cajero assigned to the given zona.
// Returns domain.ErrZonaSinCaja when the zona has no mapping.
func (r *AplicarConfigRepo) CajaCajero(ctx context.Context, zonaClienteID int) (outbound.CajaCajero, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	var cc outbound.CajaCajero
	err := q.QueryRowContext(ctx, selectCajaCajero, zonaClienteID).Scan(
		&cc.CajaID, &cc.CajeroID, &cc.VendedorID, &cc.CobradorID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return outbound.CajaCajero{}, domain.ErrZonaSinCaja
	}
	if err != nil {
		return outbound.CajaCajero{}, firebird.MapError(err)
	}
	return cc, nil
}

// FormaDePagoID maps a credit frequency to its Microsip list id.
// Returns domain.ErrFrecuenciaSinFormaPago when unmapped.
func (r *AplicarConfigRepo) FormaDePagoID(ctx context.Context, frecuencia string) (int, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	var id int
	err := q.QueryRowContext(ctx, selectFormaDePagoID, frecuencia).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, domain.ErrFrecuenciaSinFormaPago
	}
	if err != nil {
		return 0, firebird.MapError(err)
	}
	return id, nil
}

// CreditoEnMesesID maps a credit term in months to its Microsip list id.
// Returns domain.ErrPlazoSinCreditoMeses when unmapped.
func (r *AplicarConfigRepo) CreditoEnMesesID(ctx context.Context, plazoMeses int) (int, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	var id int
	err := q.QueryRowContext(ctx, selectCreditoEnMesesID, plazoMeses).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, domain.ErrPlazoSinCreditoMeses
	}
	if err != nil {
		return 0, firebird.MapError(err)
	}
	return id, nil
}

// NumeroDeVendedoresID maps a seller count to its Microsip list id.
// Returns domain.ErrNumVendedoresSinMapeo when unmapped.
func (r *AplicarConfigRepo) NumeroDeVendedoresID(ctx context.Context, n int) (int, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	var id int
	err := q.QueryRowContext(ctx, selectNumeroDeVendedoresID, n).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, domain.ErrNumVendedoresSinMapeo
	}
	if err != nil {
		return 0, firebird.MapError(err)
	}
	return id, nil
}

// VendedorListaIDs resolves the three Microsip LISTA_ATRIB_ID values mapped to
// a vendedor usuario in MSP_CFG_VENDEDOR_MICROSIP. A missing row or a NULL
// column maps to the sentinel -1 — an unmapped seller is not an error.
func (r *AplicarConfigRepo) VendedorListaIDs(ctx context.Context, usuarioID uuid.UUID) ([3]int, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	var id1, id2, id3 sql.NullInt64
	err := q.QueryRowContext(ctx, selectVendedorListaIDs, usuarioID.String()).Scan(&id1, &id2, &id3)
	if errors.Is(err, sql.ErrNoRows) {
		return [3]int{-1, -1, -1}, nil
	}
	if err != nil {
		return [3]int{-1, -1, -1}, firebird.MapError(err)
	}
	return [3]int{nullIntOr(id1, -1), nullIntOr(id2, -1), nullIntOr(id3, -1)}, nil
}

// nullIntOr returns the int value of n, or def when n is NULL.
func nullIntOr(n sql.NullInt64, def int) int {
	if !n.Valid {
		return def
	}
	return int(n.Int64)
}

// Defaults returns the singleton MSP_CFG_APLICAR row.
// Returns domain.ErrConfigAplicarFaltante when the row is absent.
func (r *AplicarConfigRepo) Defaults(ctx context.Context) (outbound.AplicarDefaults, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	var d outbound.AplicarDefaults
	err := q.QueryRowContext(ctx, selectAplicarDefaults).Scan(
		&d.SucursalID,
		&d.FormaCobroContadoID,
		&d.FormaCobroCreditoID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return outbound.AplicarDefaults{}, domain.ErrConfigAplicarFaltante
	}
	if err != nil {
		return outbound.AplicarDefaults{}, firebird.MapError(err)
	}
	return d, nil
}
