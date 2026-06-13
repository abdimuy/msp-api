//nolint:misspell // Microsip column names are Spanish by convention.
package invfb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// FindByID loads a traspaso by its Microsip DOCTO_IN_ID. Returns
// [domain.ErrTraspasoNoEncontrado] when no matching row exists.
func (r *TraspasoRepo) FindByID(ctx context.Context, doctoInID int) (*domain.Traspaso, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	// Read the DOCTOS_IN header.
	raw, err := scanDoctoInRow(q.QueryRowContext(ctx, selectDoctoInByID, doctoInID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrTraspasoNoEncontrado.WithField("docto_in_id", doctoInID)
	}
	if err != nil {
		return nil, fmt.Errorf("invfb FindByID header: %w", firebird.MapError(err))
	}

	// Read the salida detail lines.
	detalles, err := r.loadDetalles(ctx, q, []int{doctoInID})
	if err != nil {
		return nil, fmt.Errorf("invfb FindByID detalles: %w", err)
	}

	// Read MSP_VENTAS_TRASPASOS to get ventaID + tipo + reversado.
	link, err := r.loadVentaLink(ctx, q, doctoInID)
	if err != nil {
		return nil, fmt.Errorf("invfb FindByID venta_link: %w", err)
	}

	return assembleTraspaso(raw, detalles[doctoInID], link.ventaID, link.tipoReverso, link.reversado, doctoInID)
}

// ListByVentaID returns all traspasos linked to the given venta, ordered by
// DOCTO_IN_ID ascending. Always returns a non-nil slice.
func (r *TraspasoRepo) ListByVentaID(ctx context.Context, ventaID uuid.UUID) ([]*domain.Traspaso, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	// Step 1: get all DOCTO_IN_IDs for this venta from the lookup table.
	doctoIDs, err := r.loadDoctoIDsForVenta(ctx, q, ventaID)
	if err != nil {
		return nil, fmt.Errorf("invfb ListByVentaID ids: %w", err)
	}
	if len(doctoIDs) == 0 {
		return []*domain.Traspaso{}, nil
	}

	// Step 2: batch-read headers.
	headers, err := r.loadHeaders(ctx, q, doctoIDs)
	if err != nil {
		return nil, fmt.Errorf("invfb ListByVentaID headers: %w", err)
	}

	// Step 3: batch-read detalles.
	detallesByDocto, err := r.loadDetalles(ctx, q, doctoIDs)
	if err != nil {
		return nil, fmt.Errorf("invfb ListByVentaID detalles: %w", err)
	}

	// Step 4: load venta link rows for tipo and reversado.
	linkByDocto, err := r.loadLinks(ctx, q, doctoIDs)
	if err != nil {
		return nil, fmt.Errorf("invfb ListByVentaID links: %w", err)
	}

	// Assemble in doctoIDs order (chronological).
	result := make([]*domain.Traspaso, 0, len(doctoIDs))
	for _, did := range doctoIDs {
		h, ok := headers[did]
		if !ok {
			continue
		}
		link := linkByDocto[did]
		vID := &ventaID
		t, err := assembleTraspaso(h, detallesByDocto[did], vID, link.tipoReverso, link.reversado, did)
		if err != nil {
			return nil, fmt.Errorf("invfb ListByVentaID assemble docto_in_id=%d: %w", did, err)
		}
		result = append(result, t)
	}
	return result, nil
}

// ─── Private helpers ──────────────────────────────────────────────────────────

// loadDoctoIDsForVenta reads all DOCTO_IN_IDs from MSP_VENTAS_TRASPASOS for
// the given venta, ordered ascending.
func (r *TraspasoRepo) loadDoctoIDsForVenta(
	ctx context.Context,
	q firebird.Querier,
	ventaID uuid.UUID,
) ([]int, error) {
	rows, err := q.QueryContext(ctx, selectVentaTraspasoIDsByVenta, ventaID.String())
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// loadHeaders reads DOCTOS_IN headers for a slice of IDs and returns a map
// keyed by DOCTO_IN_ID. Uses a dynamic IN(?,?,...) clause.
func (r *TraspasoRepo) loadHeaders(
	ctx context.Context,
	q firebird.Querier,
	ids []int,
) (map[int]doctoInRaw, error) {
	query, args := buildInQuery(selectDoctoInByIDsBase, ids)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[int]doctoInRaw, len(ids))
	for rows.Next() {
		raw, err := scanDoctoInRow(rows)
		if err != nil {
			return nil, err
		}
		result[raw.doctoInID] = raw
	}
	return result, rows.Err()
}

// loadDetalles reads DOCTOS_IN_DET salida rows for a slice of docto IDs,
// returning a map[doctoInID][]*domain.TraspasoDetalle.
func (r *TraspasoRepo) loadDetalles(
	ctx context.Context,
	q firebird.Querier,
	ids []int,
) (map[int][]*domain.TraspasoDetalle, error) {
	query, args := buildInQuery(selectDoctoInDetSalidaBase, ids)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[int][]*domain.TraspasoDetalle, len(ids))
	for rows.Next() {
		raw, err := scanDoctoInDetRow(rows)
		if err != nil {
			return nil, err
		}
		det, err := assembleDetalle(raw)
		if err != nil {
			return nil, err
		}
		result[raw.doctoInID] = append(result[raw.doctoInID], det)
	}
	return result, rows.Err()
}

// ventaLinkRow holds the scanned columns from a MSP_VENTAS_TRASPASOS lookup.
type ventaLinkRow struct {
	ventaID     *uuid.UUID
	tipoReverso bool
	reversado   bool
}

// loadVentaLink reads back the MSP_VENTAS_TRASPASOS row for a single doctoInID.
func (r *TraspasoRepo) loadVentaLink(
	ctx context.Context,
	q firebird.Querier,
	doctoInID int,
) (ventaLinkRow, error) {
	var ventaIDRaw sql.NullString
	var tipo string
	var reversadoRaw string
	err := q.QueryRowContext(ctx, selectVentaTraspasoRowByDoctoIn, doctoInID).
		Scan(&ventaIDRaw, &tipo, &reversadoRaw)
	if errors.Is(err, sql.ErrNoRows) {
		// No lookup row — traspaso exists in Microsip but not linked to a venta.
		return ventaLinkRow{}, nil
	}
	if err != nil {
		return ventaLinkRow{}, firebird.MapError(err)
	}
	ventaID, err := parseNullUUIDCol("VENTA_ID", ventaIDRaw)
	if err != nil {
		return ventaLinkRow{}, err
	}
	return ventaLinkRow{
		ventaID:     ventaID,
		tipoReverso: tipo == tipoTraspasoReverso,
		reversado:   reversadoRaw == reversadoSi,
	}, nil
}

// loadLinks reads tipo_reverso and reversado for a batch of docto IDs from
// MSP_VENTAS_TRASPASOS. N+1 is acceptable here: ListByVentaID typically
// returns 1-5 rows.
func (r *TraspasoRepo) loadLinks(
	ctx context.Context,
	q firebird.Querier,
	ids []int,
) (map[int]ventaLinkRow, error) {
	result := make(map[int]ventaLinkRow, len(ids))
	for _, did := range ids {
		link, err := r.loadVentaLink(ctx, q, did)
		if err != nil {
			return nil, fmt.Errorf("loadLinks docto_in_id=%d: %w", did, err)
		}
		result[did] = link
	}
	return result, nil
}

// buildInQuery appends a dynamic IN(?,?,…) placeholder list to a base query
// and returns the completed query string and args slice.
func buildInQuery(base string, ids []int) (string, []any) {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	return base + "(" + strings.Join(placeholders, ",") + ")", args
}
