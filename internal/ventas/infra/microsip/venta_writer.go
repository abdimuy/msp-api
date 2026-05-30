// Package microsip contains adapters that write directly to Microsip's
// proprietary Firebird tables (DOCTOS_PV, DOCTOS_CC, etc.). Nothing outside
// this package should import these adapters — they are wired at the
// composition root (cmd/api/ventas_wiring.go).
//
//nolint:misspell // Microsip column names are Spanish by convention.
package microsip

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// sentinel errors for the microsip adapter.
var (
	errFoliosConceptosMissing = apperror.NewInternal(
		"folios_conceptos_missing",
		"falta el registro de folios de conceptos para el enganche",
	)
	errClaveClienteNotFound = apperror.NewInternal(
		"clave_cliente_not_found",
		"no se encontró la clave de cliente en microsip",
	)
	errClaveArticuloNotFound = apperror.NewInternal(
		"clave_articulo_not_found",
		"no se encontró la clave del artículo en microsip",
	)
	errFoliosCajasMissing = apperror.NewInternal(
		"folios_cajas_missing",
		"falta el registro de folios de cajas para la caja",
	)
	errAlmacenIDMissing = apperror.NewInternal(
		"almacen_id_missing",
		"no se encontró el almacén de origen en los productos de la venta",
	)
	errCargoCCIDNotFound = apperror.NewInternal(
		"cargo_cc_id_not_found",
		"no se encontró el cargo en cuentas por cobrar generado por la cascada",
	)
)

// ─── Pricing assumptions (documented here and in the final report) ───────────
//
// IVA rate: 16% INCLUDED in the listed price (IMPUESTO_INCLUIDO='S').
//
//   CONTADO venta   → unit price  = producto.Precios().Contado()
//   CREDITO venta   → unit price  = producto.Precios().Anual()  (financed price)
//
//   PRECIO_UNITARIO_IMPTO = unit price  (tax-inclusive, as stored in Microsip)
//   PRECIO_UNITARIO (neto) = price / (1 + rate)  (rounded to 6 decimals), where
//   `rate` is the article's OWN configured IVA rate read from
//   IMPUESTOS_ARTICULOS → IMPUESTOS.PCTJE_IMPUESTO. Articles vary: some are 16%,
//   many are TASA 0%, a few 3/6/8%. An article with no tax row → rate 0 (the
//   net price equals the tax-inclusive price). We never assume a flat 16%.
//
// ALMACEN_ID selection:
//   Use the first producto (not in a combo) that has a non-nil AlmacenOrigen.
//   If all productos are inside combos, use the first combo's AlmacenOrigen.
//   Only single-warehouse sales are supported; if productos span warehouses
//   the first almacen is used and the discrepancy is noted in logs.
//
// ─────────────────────────────────────────────────────────────────────────────

// hundred is the percentage base used to turn PCTJE_IMPUESTO into a divisor.
var hundred = decimal.NewFromInt(100)

// pctjeImpuestoScale is the SQL scale of IMPUESTOS.PCTJE_IMPUESTO (NUMERIC(_,6)).
const pctjeImpuestoScale = 6

// ─── SQL constants ────────────────────────────────────────────────────────────

// selectNextID claims the next id from the shared Microsip generator used by
// DOCTOS_PV, DOCTOS_PV_DET, DOCTOS_PV_COBROS, DOCTOS_CC, and IMPORTES_DOCTOS_CC.
const selectNextID = `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`

// selectClavesCliente looks up the first CLAVE_CLIENTE string for a given CLIENTE_ID.
const selectClavesCliente = `SELECT FIRST 1 CLAVE_CLIENTE FROM CLAVES_CLIENTES WHERE CLIENTE_ID = ? ORDER BY CLAVE_CLIENTE_ID`

// selectClavesArticulo looks up the first CLAVE_ARTICULO string for a given ARTICULO_ID.
const selectClavesArticulo = `SELECT FIRST 1 CLAVE_ARTICULO FROM CLAVES_ARTICULOS WHERE ARTICULO_ID = ? ORDER BY CLAVE_ARTICULO_ID`

// selectArticuloIVAPct reads the article's own IVA percentage from its tax
// configuration. An article has at most one tax row (verified in the dev DB);
// no row → no SQL row → treated as 0% (tax-exempt / not configured).
const selectArticuloIVAPct = `SELECT i.PCTJE_IMPUESTO
FROM IMPUESTOS_ARTICULOS ia
JOIN IMPUESTOS i ON i.IMPUESTO_ID = ia.IMPUESTO_ID
WHERE ia.ARTICULO_ID = ?`

// selectFoliosCajas reads the current folio counter row for a caja+tipo.
const selectFoliosCajas = `SELECT SERIE, CONSECUTIVO FROM FOLIOS_CAJAS WHERE CAJA_ID = ? AND TIPO_DOCTO = 'V'`

// updateFoliosCajas increments the folio counter.
const updateFoliosCajas = `UPDATE FOLIOS_CAJAS SET CONSECUTIVO = CONSECUTIVO + 1 WHERE CAJA_ID = ? AND TIPO_DOCTO = 'V'`

// insertDoctoPV inserts the DOCTOS_PV header with APLICADO='N'.
//
//nolint:gosec // SQL constant, not user input.
const insertDoctoPV = `INSERT INTO DOCTOS_PV
  (DOCTO_PV_ID, CAJA_ID, TIPO_DOCTO, SUCURSAL_ID, FOLIO,
   FECHA, HORA,
   CAJERO_ID,
   CLIENTE_ID, CLAVE_CLIENTE,
   ALMACEN_ID, MONEDA_ID,
   IMPUESTO_INCLUIDO, TIPO_CAMBIO,
   TIPO_DSCTO, DSCTO_PCTJE, DSCTO_IMPORTE,
   ESTATUS, APLICADO,
   IMPORTE_NETO, TOTAL_IMPUESTOS,
   SISTEMA_ORIGEN,
   CONTABILIZADO, TICKET_EMITIDO,
   CARGAR_SUN, UNID_COMPROM)
VALUES (?, ?, 'V', ?, ?,
        ?, ?,
        ?,
        ?, ?,
        ?, 1,
        'S', 1,
        'P', 0, 0,
        'N', 'N',
        0, 0,
        'PV',
        'N', 'N',
        'S', 'N')`

// insertDoctoPVDet inserts one DOCTOS_PV_DET line (POSICION = -1 → trigger assigns).
//
//nolint:gosec // SQL constant, not user input.
const insertDoctoPVDet = `INSERT INTO DOCTOS_PV_DET
  (DOCTO_PV_DET_ID, DOCTO_PV_ID,
   ARTICULO_ID, CLAVE_ARTICULO,
   UNIDADES, UNIDADES_DEV,
   PRECIO_UNITARIO, PRECIO_UNITARIO_IMPTO,
   PCTJE_DSCTO, PRECIO_TOTAL_NETO,
   PRECIO_MODIFICADO,
   PCTJE_COMIS,
   ROL, POSICION,
   TIPO_CONTAB_UNID, ES_TRAN_ELECT, IMPUESTO_POR_UNIDAD)
VALUES (?, ?,
        ?, ?,
        ?, 0,
        ?, ?,
        0, 0,
        'N',
        0,
        'N', -1,
        '0', 'N', 0)`

// insertDoctoPVCobros inserts the DOCTOS_PV_COBROS row.
//
//nolint:gosec // SQL constant, not user input.
const insertDoctoPVCobros = `INSERT INTO DOCTOS_PV_COBROS
  (DOCTO_PV_COBRO_ID, DOCTO_PV_ID,
   TIPO, FORMA_COBRO_ID,
   IMPORTE, IMPORTE_MON_DOC,
   TIPO_CAMBIO)
VALUES (?, ?,
        'C', ?,
        ?, ?,
        1)`

// insertLibresVtaPV inserts the LIBRES_VTA_PV row.
const insertLibresVtaPV = `INSERT INTO LIBRES_VTA_PV (DOCTO_PV_ID) VALUES (?)`

// updateDoctoPVAplicar flips APLICADO N→S and sets the final totals + folio.
const updateDoctoPVAplicar = `UPDATE DOCTOS_PV
SET APLICADO = 'S',
    FOLIO = ?,
    IMPORTE_NETO = ?,
    TOTAL_IMPUESTOS = ?
WHERE DOCTO_PV_ID = ?`

// selectCargoCCID retrieves the DOCTO_CC_ID of the cargo generated by the
// N→S cascade for a given DOCTO_PV_ID.
const selectCargoCCID = `SELECT D.DOCTO_CC_ID
FROM DOCTOS_ENTRE_SIS E
JOIN DOCTOS_CC D ON D.DOCTO_CC_ID = E.DOCTO_DEST_ID
WHERE E.CLAVE_SIS_FTE = 'PV' AND E.CLAVE_SIS_DEST = 'CC' AND E.DOCTO_FTE_ID = ?`

// insertLibresCargosCC inserts the LIBRES_CARGOS_CC row for CREDITO ventas.
//
//nolint:gosec // SQL constant, not user input.
const insertLibresCargosCC = `INSERT INTO LIBRES_CARGOS_CC
  (DOCTO_CC_ID,
   FORMA_DE_PAGO, PARCIALIDAD, CREDITO_EN_MESES,
   VENDEDOR_1, VENDEDOR_2, VENDEDOR_3,
   NUMERO_DE_VENDEDORES,
   ENGANCHE,
   PRECIO_DE_CONTADO,
   AVAL_O_RESPONSABLE,
   OBSERVACIONES)
VALUES (?,
        ?, ?, ?,
        ?, -1, -1,
        ?,
        ?,
        ?,
        ?,
        ?)`

// selectFoliosConceptos reads the enganche folio counter.
const selectFoliosConceptos = `SELECT SERIE, CONSECUTIVO FROM FOLIOS_CONCEPTOS WHERE FOLIO_CONCEPTO_ID = 475145`

// updateFoliosConceptos increments the enganche folio counter.
const updateFoliosConceptos = `UPDATE FOLIOS_CONCEPTOS SET CONSECUTIVO = CONSECUTIVO + 1 WHERE FOLIO_CONCEPTO_ID = 475145`

// insertDoctoCC inserts a DOCTOS_CC enganche document with APLICADO='N'.
//
//nolint:gosec // SQL constant, not user input.
const insertDoctoCC = `INSERT INTO DOCTOS_CC
  (DOCTO_CC_ID, CONCEPTO_CC_ID, FOLIO, NATURALEZA_CONCEPTO,
   SUCURSAL_ID, FECHA, CLIENTE_ID, CLAVE_CLIENTE,
   TIPO_CAMBIO, DESCRIPCION,
   SISTEMA_ORIGEN, APLICADO, ESTATUS, ESTATUS_ANT,
   CONTABILIZADO_GYP, ES_CFD, TIENE_ANTICIPO, CFDI_CERTIFICADO, ENVIADO,
   INTEG_BA, CONTABILIZADO_BA)
VALUES (?, 24533, ?, 'R',
        ?, ?, ?, ?,
        1, 'Enganche',
        'CC', 'N', 'N', 'N',
        'N', 'N', 'N', 'N', 'N',
        'N', 'N')`

// insertImportesDoctoCC inserts the IMPORTES_DOCTOS_CC link row.
//
//nolint:gosec // SQL constant, not user input.
const insertImportesDoctoCC = `INSERT INTO IMPORTES_DOCTOS_CC
  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
   TIPO_IMPTE, DOCTO_CC_ACR_ID,
   IMPORTE, IMPUESTO,
   APLICADO, ESTATUS)
VALUES (?, ?, ?,
        'R', ?,
        ?, 0,
        'N', 'N')`

// updateDoctoCCAplicar flips the enganche document APLICADO N→S.
const updateDoctoCCAplicar = `UPDATE DOCTOS_CC SET APLICADO = 'S' WHERE DOCTO_CC_ID = ?`

// ─── VentaWriter ──────────────────────────────────────────────────────────────

// VentaWriter implements outbound.MicrosipVentaWriter against the Microsip
// Firebird database.
type VentaWriter struct {
	pool *firebird.Pool
}

// NewVentaWriter builds a VentaWriter wired to the given Firebird pool.
func NewVentaWriter(pool *firebird.Pool) *VentaWriter {
	return &VentaWriter{pool: pool}
}

// Compile-time check.
var _ outbound.MicrosipVentaWriter = (*VentaWriter)(nil)

// Aplicar materializes the venta into Microsip's DOCTOS_PV family within the
// caller's transaction. It runs phases 1-7 of the write recipe.
//
// Pricing assumptions:
//   - IVA 16% INCLUDED (IMPUESTO_INCLUIDO='S').
//   - CONTADO: unit price = Precios().Contado()
//   - CREDITO: unit price = Precios().Anual()
//
// ALMACEN_ID: first standalone producto's AlmacenOrigen, falling back to the
// first combo's AlmacenOrigen.
//
//nolint:funlen,cyclop // recipe has many phases; split into helpers below.
func (w *VentaWriter) Aplicar(ctx context.Context, in outbound.MicrosipVentaInput) (outbound.MicrosipVentaResult, error) {
	q := firebird.GetQuerier(ctx, w.pool.DB)
	v := in.Venta
	now := time.Now()

	// ── Resolve CLAVE_CLIENTE ─────────────────────────────────────────────
	claveCliente, err := w.lookupClaveCliente(ctx, q, *v.ClienteID())
	if err != nil {
		return outbound.MicrosipVentaResult{}, fmt.Errorf("microsip aplicar: clave_cliente: %w", err)
	}

	// ── Resolve ALMACEN_ID ────────────────────────────────────────────────
	almacenID, err := resolveAlmacenID(v)
	if err != nil {
		return outbound.MicrosipVentaResult{}, fmt.Errorf("microsip aplicar: almacen_id: %w", err)
	}

	// ── Read folio BEFORE inserting (claim it; increment after flip) ──────
	serie, consecutivo, err := readFoliosCajas(ctx, q, in.CajaID)
	if err != nil {
		return outbound.MicrosipVentaResult{}, fmt.Errorf("microsip aplicar: folio: %w", err)
	}
	folio := buildFolio(serie, consecutivo)

	// ── Claim DOCTO_PV_ID from generator ─────────────────────────────────
	doctoPVID, err := nextID(ctx, q)
	if err != nil {
		return outbound.MicrosipVentaResult{}, fmt.Errorf("microsip aplicar: claim docto_pv_id: %w", err)
	}

	// ── Phase 1: INSERT DOCTOS_PV with APLICADO='N' ───────────────────────
	// Temp folio must fit CHAR(9). Use the low 8 digits of doctoPVID (same
	// pattern Microsip's GEN_FOLIO_TEMP procedure uses: "_" + 8 digits).
	tempFolio := fmt.Sprintf("_%08d", doctoPVID%100000000)
	wc := firebird.ToWallClock(now)
	hora := firebird.ToWallClock(now)
	if err := execInsertDoctoPV(ctx, q, doctoPVID, in, *v.ClienteID(), claveCliente, almacenID, tempFolio, wc, hora); err != nil {
		return outbound.MicrosipVentaResult{}, fmt.Errorf("microsip aplicar: insert doctos_pv: %w", err)
	}

	// ── Phase 2: INSERT DOCTOS_PV_DET (one per producto) ─────────────────
	totals, err := w.insertDetalles(ctx, q, v, doctoPVID)
	if err != nil {
		return outbound.MicrosipVentaResult{}, fmt.Errorf("microsip aplicar: insert detalles: %w", err)
	}

	// ── Phase 4: INSERT DOCTOS_PV_COBROS ─────────────────────────────────
	cobroID, err := nextID(ctx, q)
	if err != nil {
		return outbound.MicrosipVentaResult{}, fmt.Errorf("microsip aplicar: claim cobro_id: %w", err)
	}
	totalConIVA := totals.neto.Add(totals.impuestos)
	if err := execInsertCobros(ctx, q, cobroID, doctoPVID, in.FormaCobroID, totalConIVA); err != nil {
		return outbound.MicrosipVentaResult{}, fmt.Errorf("microsip aplicar: insert cobros: %w", err)
	}

	// ── Phase 5: INSERT LIBRES_VTA_PV ────────────────────────────────────
	if _, err := q.ExecContext(ctx, insertLibresVtaPV, doctoPVID); err != nil {
		return outbound.MicrosipVentaResult{}, fmt.Errorf("microsip aplicar: insert libres_vta_pv: %w", err)
	}

	// ── Phase 6: UPDATE DOCTOS_PV APLICADO='S' (triggers cascade) ────────
	if _, err := q.ExecContext(ctx, updateDoctoPVAplicar,
		folio,
		totals.neto.StringFixed(6),
		totals.impuestos.StringFixed(6),
		doctoPVID,
	); err != nil {
		return outbound.MicrosipVentaResult{}, fmt.Errorf("microsip aplicar: flip aplicado: %w", err)
	}

	// ── Phase 6b: UPDATE FOLIOS_CAJAS + 1 ────────────────────────────────
	if _, err := q.ExecContext(ctx, updateFoliosCajas, in.CajaID); err != nil {
		return outbound.MicrosipVentaResult{}, fmt.Errorf("microsip aplicar: update folios_cajas: %w", err)
	}

	// ── Phase 7: datos particulares + enganche (CREDITO only) ────────────
	if v.TipoVenta() == domain.TipoVentaCredito {
		if err := w.insertDatosCredito(ctx, q, in, v, doctoPVID, *v.ClienteID(), claveCliente, in.SucursalID, totalConIVA, now); err != nil {
			return outbound.MicrosipVentaResult{}, fmt.Errorf("microsip aplicar: datos credito: %w", err)
		}
	}

	return outbound.MicrosipVentaResult{DoctoPVID: doctoPVID, Folio: folio}, nil
}

// ─── Phase helpers ────────────────────────────────────────────────────────────

type detalleTotals struct {
	neto      decimal.Decimal
	impuestos decimal.Decimal
}

// insertDetalles iterates the venta's productos and inserts one
// DOCTOS_PV_DET row per producto. Returns the aggregated IMPORTE_NETO and
// TOTAL_IMPUESTOS for the header UPDATE in phase 6.
func (w *VentaWriter) insertDetalles(
	ctx context.Context,
	q firebird.Querier,
	v *domain.Venta,
	doctoPVID int,
) (detalleTotals, error) {
	var totalNeto, totalImpuestos decimal.Decimal
	for _, p := range v.ProductosForRepo() {
		claveArticulo, err := w.lookupClaveArticulo(ctx, q, p.ArticuloID())
		if err != nil {
			return detalleTotals{}, fmt.Errorf("clave_articulo articulo_id=%d: %w", p.ArticuloID(), err)
		}
		ivaPct, err := w.lookupIVAPct(ctx, q, p.ArticuloID())
		if err != nil {
			return detalleTotals{}, fmt.Errorf("iva_pct articulo_id=%d: %w", p.ArticuloID(), err)
		}
		precioConImpto := unitPrice(v.TipoVenta(), p)
		// neto = precio_con_impto / (1 + pctje/100). pctje=0 → neto = precio.
		divisor := decimal.NewFromInt(1).Add(ivaPct.Div(hundred))
		precioNeto := precioConImpto.Div(divisor).Round(6)
		impuestoPorUnidad := precioConImpto.Sub(precioNeto)

		cantidad := p.Cantidad()
		totalNeto = totalNeto.Add(precioNeto.Mul(cantidad))
		totalImpuestos = totalImpuestos.Add(impuestoPorUnidad.Mul(cantidad))

		detID, err := nextID(ctx, q)
		if err != nil {
			return detalleTotals{}, fmt.Errorf("claim det_id: %w", err)
		}
		if _, err := q.ExecContext(ctx, insertDoctoPVDet,
			detID, doctoPVID,
			p.ArticuloID(), claveArticulo,
			cantidad.StringFixed(6),
			precioNeto.StringFixed(6),
			precioConImpto.StringFixed(6),
		); err != nil {
			return detalleTotals{}, fmt.Errorf("insert det articulo_id=%d: %w", p.ArticuloID(), firebird.MapError(err))
		}
	}
	return detalleTotals{neto: totalNeto, impuestos: totalImpuestos}, nil
}

// insertDatosCredito handles Phase 7 (LIBRES_CARGOS_CC + optional enganche).
//
//nolint:funlen // multi-step credit-only block; kept together for traceability.
func (w *VentaWriter) insertDatosCredito(
	ctx context.Context,
	q firebird.Querier,
	in outbound.MicrosipVentaInput,
	v *domain.Venta,
	doctoPVID int,
	clienteID int,
	claveCliente string,
	sucursalID int,
	totalConIVA decimal.Decimal,
	now time.Time,
) error {
	// 7a — retrieve the cargo generated by the cascade.
	cargoCCID, err := readCargoCCID(ctx, q, doctoPVID)
	if err != nil {
		return fmt.Errorf("read cargo_cc_id: %w", err)
	}

	plan := v.PlanCredito()
	// 7b — INSERT LIBRES_CARGOS_CC.
	var aval, obs any
	if v.Cliente().Aval() != nil {
		aval = v.Cliente().Aval().Value()
	}
	if v.Nota() != nil {
		obs = *v.Nota()
	}

	if _, err := q.ExecContext(ctx, insertLibresCargosCC,
		cargoCCID,
		*in.FormaDePagoID, plan.Parcialidad().StringFixed(2), *in.CreditoEnMesesID,
		in.VendedorID,
		in.NumeroDeVendedoresID,
		plan.Enganche().StringFixed(2),
		v.Montos().Contado().StringFixed(2),
		aval,
		obs,
	); err != nil {
		return fmt.Errorf("insert libres_cargos_cc: %w", firebird.MapError(err))
	}

	// 7c — enganche document (only when enganche > 0).
	if plan.Enganche().Sign() > 0 {
		if err := w.insertEnganche(ctx, q, cargoCCID, clienteID, claveCliente, sucursalID, plan.Enganche(), now); err != nil {
			return fmt.Errorf("insert enganche: %w", err)
		}
	}

	_ = totalConIVA // consumed above for totals; kept for traceability.
	return nil
}

// insertEnganche executes Phase 7c: folio claim → INSERT DOCTOS_CC(enganche)
// → INSERT IMPORTES_DOCTOS_CC → UPDATE APLICADO='S' → UPDATE FOLIOS_CONCEPTOS.
func (w *VentaWriter) insertEnganche(
	ctx context.Context,
	q firebird.Querier,
	cargoCCID int,
	clienteID int,
	claveCliente string,
	sucursalID int,
	enganche decimal.Decimal,
	now time.Time,
) error {
	// Claim folio from FOLIOS_CONCEPTOS.
	var serie string
	var consecutivo int
	err := q.QueryRowContext(ctx, selectFoliosConceptos).Scan(&serie, &consecutivo)
	if errors.Is(err, sql.ErrNoRows) {
		return errFoliosConceptosMissing
	}
	if err != nil {
		return firebird.MapError(err)
	}
	engancheFolio := buildFolioConceptos(consecutivo)

	// Claim DOCTO_CC_ID for enganche.
	engancheDoctoID, err := nextID(ctx, q)
	if err != nil {
		return fmt.Errorf("claim enganche docto_cc_id: %w", err)
	}

	// Claim IMPTE_DOCTO_CC_ID.
	impteID, err := nextID(ctx, q)
	if err != nil {
		return fmt.Errorf("claim impte_docto_cc_id: %w", err)
	}

	wc := firebird.ToWallClock(now)

	// INSERT DOCTOS_CC (enganche, APLICADO='N').
	if _, err := q.ExecContext(ctx, insertDoctoCC,
		engancheDoctoID, engancheFolio,
		sucursalID, wc, clienteID, claveCliente,
	); err != nil {
		return fmt.Errorf("insert doctos_cc enganche: %w", firebird.MapError(err))
	}

	// INSERT IMPORTES_DOCTOS_CC (liga enganche → cargo).
	if _, err := q.ExecContext(ctx, insertImportesDoctoCC,
		impteID, engancheDoctoID, wc,
		cargoCCID,
		enganche.StringFixed(2),
	); err != nil {
		return fmt.Errorf("insert importes_doctos_cc: %w", firebird.MapError(err))
	}

	// UPDATE DOCTOS_CC APLICADO='S' (triggers AFECTA_SALDOS_CC).
	if _, err := q.ExecContext(ctx, updateDoctoCCAplicar, engancheDoctoID); err != nil {
		return fmt.Errorf("flip enganche aplicado: %w", firebird.MapError(err))
	}

	// UPDATE FOLIOS_CONCEPTOS + 1.
	if _, err := q.ExecContext(ctx, updateFoliosConceptos); err != nil {
		return fmt.Errorf("update folios_conceptos: %w", firebird.MapError(err))
	}

	return nil
}

// ─── Small lookup helpers ─────────────────────────────────────────────────────

// lookupClaveCliente queries CLAVES_CLIENTES for the first CLAVE_CLIENTE of
// the given CLIENTE_ID.
func (w *VentaWriter) lookupClaveCliente(ctx context.Context, q firebird.Querier, clienteID int) (string, error) {
	var clave string
	err := q.QueryRowContext(ctx, selectClavesCliente, clienteID).Scan(&clave)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errClaveClienteNotFound.WithField("cliente_id", clienteID)
	}
	if err != nil {
		return "", firebird.MapError(err)
	}
	return clave, nil
}

// lookupClaveArticulo queries CLAVES_ARTICULOS for the first CLAVE_ARTICULO.
func (w *VentaWriter) lookupClaveArticulo(ctx context.Context, q firebird.Querier, articuloID int) (string, error) {
	var clave string
	err := q.QueryRowContext(ctx, selectClavesArticulo, articuloID).Scan(&clave)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errClaveArticuloNotFound.WithField("articulo_id", articuloID)
	}
	if err != nil {
		return "", firebird.MapError(err)
	}
	return clave, nil
}

// lookupIVAPct returns the article's own IVA percentage (e.g. 16 or 0) from its
// Microsip tax configuration. An article with no configured tax row is treated
// as 0% (tax-exempt) rather than an error — many articles legitimately carry
// TASA 0%. The value is a percentage, not a fraction (16 means 16%).
func (w *VentaWriter) lookupIVAPct(ctx context.Context, q firebird.Querier, articuloID int) (decimal.Decimal, error) {
	var raw any
	err := q.QueryRowContext(ctx, selectArticuloIVAPct, articuloID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return decimal.Zero, nil
	}
	if err != nil {
		return decimal.Zero, firebird.MapError(err)
	}
	// PCTJE_IMPUESTO is NUMERIC(_,6); the driver hands back an int64/[]byte that
	// must go through ScanDecimal rather than a direct decimal.Decimal scan.
	pct, err := firebird.ScanDecimal(raw, pctjeImpuestoScale)
	if err != nil {
		return decimal.Zero, err
	}
	return pct, nil
}

// ─── Pure helpers ─────────────────────────────────────────────────────────────

// nextID claims the next value from the shared Microsip generator.
func nextID(ctx context.Context, q firebird.Querier) (int, error) {
	var id int
	if err := q.QueryRowContext(ctx, selectNextID).Scan(&id); err != nil {
		return 0, fmt.Errorf("GEN_ID(ID_DOCTOS): %w", firebird.MapError(err))
	}
	return id, nil
}

// readFoliosCajas reads the current SERIE + CONSECUTIVO for a caja's venta
// folio sequence without yet incrementing it.
//
//nolint:nonamedreturns // multi-arity tuple is clearer when named for doc purposes.
func readFoliosCajas(ctx context.Context, q firebird.Querier, cajaID int) (serie string, consecutivo int, err error) {
	row := q.QueryRowContext(ctx, selectFoliosCajas, cajaID)
	if scanErr := row.Scan(&serie, &consecutivo); scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return "", 0, errFoliosCajasMissing.WithField("caja_id", cajaID)
		}
		return "", 0, firebird.MapError(scanErr)
	}
	return serie, consecutivo, nil
}

// buildFolio constructs the FOLIO string: SERIE + LPAD(CONSECUTIVO, 8, '0').
// e.g. "Y" + 2262 → "Y00002262".
func buildFolio(serie string, consecutivo int) string {
	return fmt.Sprintf("%s%08d", serie, consecutivo)
}

// buildFolioConceptos constructs the enganche folio: LPAD(CONSECUTIVO, 9, '0').
func buildFolioConceptos(consecutivo int) string {
	return fmt.Sprintf("%09d", consecutivo)
}

// resolveAlmacenID picks the ALMACEN_ID for the DOCTOS_PV header.
// Priority: first standalone producto's AlmacenOrigen, then first combo's.
func resolveAlmacenID(v *domain.Venta) (int, error) {
	// First check standalone productos.
	for _, p := range v.ProductosForRepo() {
		if p.ComboID() == nil && p.AlmacenOrigen() != nil {
			return *p.AlmacenOrigen(), nil
		}
	}
	// Fall back to first combo.
	for _, c := range v.CombosForRepo() {
		return c.AlmacenOrigen(), nil
	}
	return 0, errAlmacenIDMissing
}

// unitPrice returns the per-product unit price (con IVA) based on tipo_venta.
func unitPrice(tipo domain.TipoVenta, p *domain.Producto) decimal.Decimal {
	if tipo == domain.TipoVentaContado {
		return p.Precios().Contado()
	}
	// CREDITO → use the annual (financed) price tier.
	return p.Precios().Anual()
}

// readCargoCCID reads the DOCTO_CC_ID of the cargo document generated by the
// cascade after flipping APLICADO N→S.
func readCargoCCID(ctx context.Context, q firebird.Querier, doctoPVID int) (int, error) {
	var id int
	err := q.QueryRowContext(ctx, selectCargoCCID, doctoPVID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errCargoCCIDNotFound.WithField("docto_pv_id", doctoPVID)
	}
	if err != nil {
		return 0, firebird.MapError(err)
	}
	return id, nil
}

// execInsertDoctoPV executes the DOCTOS_PV header INSERT.
func execInsertDoctoPV(
	ctx context.Context,
	q firebird.Querier,
	doctoPVID int,
	in outbound.MicrosipVentaInput,
	clienteID int,
	claveCliente string,
	almacenID int,
	tempFolio string,
	fecha, hora interface{},
) error {
	_, err := q.ExecContext(ctx, insertDoctoPV,
		doctoPVID, in.CajaID, in.SucursalID, tempFolio,
		fecha, hora,
		in.CajeroID,
		clienteID, claveCliente,
		almacenID,
	)
	if err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// execInsertCobros executes the DOCTOS_PV_COBROS INSERT.
func execInsertCobros(
	ctx context.Context,
	q firebird.Querier,
	cobroID, doctoPVID, formaCobroID int,
	total decimal.Decimal,
) error {
	_, err := q.ExecContext(ctx, insertDoctoPVCobros,
		cobroID, doctoPVID,
		formaCobroID,
		total.StringFixed(2),
		total.StringFixed(2),
	)
	if err != nil {
		return firebird.MapError(err)
	}
	return nil
}
