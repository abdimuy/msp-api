//nolint:misspell // Spanish domain vocabulary (venta, producto, articulo) by project convention.
package ventfb

import (
	"context"
	"database/sql"
	"strings"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// selectProductosCols is the canonical SELECT list for the sale line-item
// query. Order matches productoRowScan.scanFrom one-to-one.
//
// Projection notes (escalas idénticas a clientes.queryProductos, ampliada con
// DET_ID / PV_ID / FOLIO / POSICION para calzar 1:1 con el modelo Product de
// la app):
//   - UNIDADES es NUMERIC(18,5) (scale 5) — se trunca a entero para CANTIDAD.
//   - PRECIO_UNITARIO_IMPTO es NUMERIC(18,6) (scale 6), precio unitario CON
//     impuesto, mismo criterio que el total de la venta.
//   - IMPORTE = precio_unitario_impto * unidades * (1 - dscto/100), casteado a
//     NUMERIC(18,2) por el bug de escala del driver en agregados/expresiones.
//   - ARTICULOS.NOMBRE es CHARACTER SET NONE (Win1252) — se escanea con
//     firebird.Win1252 (igual que clientes/infra/clientesfb/rowmappers.go);
//     una conexión UTF8 tira "Malformed string" al forzar acentos legacy.
const selectProductosCols = `
	det.DOCTO_PV_DET_ID,
	det.DOCTO_PV_ID,
	dc.FOLIO,
	det.ARTICULO_ID,
	a.NOMBRE,
	det.UNIDADES,
	det.PRECIO_UNITARIO_IMPTO,
	CAST(det.PRECIO_UNITARIO_IMPTO * det.UNIDADES * (1 - det.PCTJE_DSCTO / 100) AS NUMERIC(18,2)) AS IMPORTE,
	det.POSICION`

// productosFromClause joins DOCTOS_PV_DET with the article name and the parent
// document (for FOLIO).
const productosFromClause = `
FROM DOCTOS_PV_DET det
JOIN ARTICULOS a  ON a.ARTICULO_ID = det.ARTICULO_ID
JOIN DOCTOS_PV dc ON dc.DOCTO_PV_ID = det.DOCTO_PV_ID`

// productosRolFilter keeps ROL IN ('N','J') and drops ROL='C'. See
// clientes.queryProductos / project memory reference_microsip_juegos_kits:
//   - ROL='N': línea normal de un artículo — siempre se muestra.
//   - ROL='J': cabecera de kit/juego con precio — se muestra.
//   - ROL='C': componentes de kit a precio cero — EXCLUIDOS (evita duplicar
//     el kit ya representado por la línea ROL='J').
const productosRolFilter = `det.ROL IN ('N', 'J')`

// ProductosByPVIDs returns the line items for every DOCTOS_PV document in
// pvIDs, keyed by DOCTO_PV_ID. Batch query (single round-trip, anti N+1) —
// structural mirror of VentasRepo.ByIDs. Documents with no matching lines are
// simply absent from the map; the caller defaults to an empty slice.
//
// Duplicate IDs in the input are deduplicated before querying.
func (r *VentasRepo) ProductosByPVIDs(ctx context.Context, pvIDs []int) (map[int][]domain.ProductoVenta, error) {
	if len(pvIDs) == 0 {
		return map[int][]domain.ProductoVenta{}, nil
	}
	// Dedup input IDs.
	seen := make(map[int]struct{}, len(pvIDs))
	unique := make([]int, 0, len(pvIDs))
	for _, id := range pvIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	placeholders := make([]string, len(unique))
	args := make([]any, len(unique))
	for i, id := range unique {
		placeholders[i] = "?"
		args[i] = id
	}

	query := `
SELECT ` + selectProductosCols + productosFromClause + `
WHERE det.DOCTO_PV_ID IN (` + strings.Join(placeholders, ",") + `)
  AND ` + productosRolFilter + `
ORDER BY det.DOCTO_PV_ID, det.POSICION`

	result := make(map[int][]domain.ProductoVenta, len(unique))
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, query, args...)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var rs productoRowScan
			if serr := rs.scanFrom(rows); serr != nil {
				return firebird.MapError(serr)
			}
			p, herr := rs.hydrate()
			if herr != nil {
				return herr
			}
			result[p.DoctoPVID()] = append(result[p.DoctoPVID()], p)
		}
		return firebird.MapError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// productoRowScan mirrors selectProductosCols 1:1.
type productoRowScan struct {
	doctoPVDetID   int
	doctoPVID      int
	folio          sql.NullString
	articuloID     int
	articuloRaw    firebird.Win1252 // ARTICULOS.NOMBRE — CHARACTER SET NONE (Win1252)
	unidadesRaw    any
	precioUnitRaw  any
	precioTotalRaw any
	posicion       int
}

func (s *productoRowScan) scanFrom(r scannable) error {
	return r.Scan(
		&s.doctoPVDetID,
		&s.doctoPVID,
		&s.folio,
		&s.articuloID,
		&s.articuloRaw,
		&s.unidadesRaw,
		&s.precioUnitRaw,
		&s.precioTotalRaw,
		&s.posicion,
	)
}

// hydrate converts the raw scan values into a domain.ProductoVenta.
func (s *productoRowScan) hydrate() (domain.ProductoVenta, error) {
	// UNIDADES is NUMERIC(18,5) — scale 5; truncate to int for CANTIDAD.
	unidades, err := firebird.ScanDecimal(s.unidadesRaw, 5)
	if err != nil {
		return domain.ProductoVenta{}, err
	}
	// PRECIO_UNITARIO_IMPTO is NUMERIC(18,6) — scale 6.
	precioUnit, err := firebird.ScanDecimal(s.precioUnitRaw, 6)
	if err != nil {
		return domain.ProductoVenta{}, err
	}
	// IMPORTE is CAST to NUMERIC(18,2) — scale 2.
	precioTotal, err := firebird.ScanDecimal(s.precioTotalRaw, 2)
	if err != nil {
		return domain.ProductoVenta{}, err
	}
	return domain.HydrateProductoVenta(domain.HydrateProductoVentaParams{
		DoctoPVDetID:    s.doctoPVDetID,
		DoctoPVID:       s.doctoPVID,
		Folio:           nullableString(s.folio),
		ArticuloID:      s.articuloID,
		Articulo:        string(s.articuloRaw),
		Cantidad:        int(unidades.IntPart()),
		PrecioUnitario:  precioUnit,
		PrecioTotalNeto: precioTotal,
		Posicion:        s.posicion,
	}), nil
}
