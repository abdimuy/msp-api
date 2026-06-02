// Package microsip implements outbound adapters that write to Microsip's
// legacy Firebird tables (DOCTOS_CC, IMPORTES_DOCTOS_CC, FORMAS_COBRO_DOCTOS).
// The package mirrors internal/ventas/infra/microsip in scope — write-side
// for one specific domain transition (here: aplicar un pago de cobranza).
//
//nolint:misspell // Microsip table/column identifiers are kept verbatim.
package microsip

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertion: PagoWriter satisfies the outbound port.
var _ outbound.MicrosipPagoWriter = (*PagoWriter)(nil)

// PagoWriter materializes a PagoRecibido into Microsip's DOCTOS_CC ledger in
// a 5-statement transaction. The caller (AplicarPago) owns the surrounding
// firebird tx via ctx; the writer routes all SQL through firebird.GetQuerier
// so it composes transparently.
type PagoWriter struct {
	pool *firebird.Pool
}

// NewPagoWriter constructs a PagoWriter bound to the given pool.
func NewPagoWriter(pool *firebird.Pool) *PagoWriter {
	return &PagoWriter{pool: pool}
}

// Hardcoded magic constants pulled from the legacy Node implementation
// (sys_msp_backend ventas/stores.ts insertDataToFirebird, ~líneas 432-495).
// Names match the Microsip column semantics; values are reproduced verbatim
// because they were discovered empirically across multiple iterations.
const (
	// sucursalID is the Microsip SUCURSAL_ID assigned to all cobranza pagos.
	// Hardcoded in the legacy to 225490.
	sucursalID = 225490
	// lugarExpedicionID is DOCTOS_CC.LUGAR_EXPEDICION_ID. Legacy uses 234.
	lugarExpedicionID = 234
	// tipoCambio is DOCTOS_CC.TIPO_CAMBIO. 1 = MXN (no FX).
	tipoCambio = 1
	// usuarioMarca is what the legacy writes into USUARIO_CREADOR /
	// USUARIO_ULT_MODIF / USUARIO_AUT_MODIF — a free-text marker identifying
	// the source system, not the actual user. The real user UUID lives in
	// MSP_PAGOS_RECIBIDOS.CREATED_BY.
	usuarioMarca = "COBRANZA EN RUTA 2.0"
	// folioPrefix is the prefix added to GEN_FOLIO_TEMP's numeric counter.
	folioPrefix = "Z"
)

// SQL identifier widths used by the legacy code; mirrored here so changes
// to the underlying Microsip schema get caught at compile time.
const (
	// naturalezaConcepto = "R" (recibo).
	naturalezaConcepto = "R"
	// sistemaOrigen = "CC" (cobranza, not "PV" which is ventas).
	sistemaOrigen = "CC"
	// estatusPreaplicado = "P".
	estatusPreaplicado = "P"
)

// ─── SQL ─────────────────────────────────────────────────────────────────────

const execGenFolioTempSQL = `EXECUTE PROCEDURE GEN_FOLIO_TEMP`

const selectClaveClienteSQL = `
SELECT CLAVE_CLIENTE
FROM CLAVES_CLIENTES
WHERE CLIENTE_ID = ?`

// insertDoctoCCSQL — 59 columns matching the legacy QUERTY_INSERT_PAGO
// statement. DOCTO_CC_ID is passed as -1; the BEFORE INSERT trigger replaces
// it with the next value from the Microsip generator and the RETURNING
// clause hands back the actual ID.
const insertDoctoCCSQL = `
INSERT INTO DOCTOS_CC (
	DOCTO_CC_ID, CONCEPTO_CC_ID, FOLIO, NATURALEZA_CONCEPTO, SUCURSAL_ID,
	FECHA, HORA, CLAVE_CLIENTE, IMPORTE_COBRO, FECHA_HORA_PAGO,
	CLIENTE_ID, TIPO_CAMBIO, CANCELADO, APLICADO, DESCRIPCION,
	LUGAR_EXPEDICION_ID, CUENTA_CONCEPTO, COBRADOR_ID, FORMA_EMITIDA,
	CONTABILIZADO, CONTABILIZADO_GYP, COND_PAGO_ID, FECHA_DSCTO_PPAG,
	PCTJE_DSCTO_PPAG, FACTURA_MOSTRADOR, SISTEMA_ORIGEN, ESTATUS, ESTATUS_ANT,
	FECHA_APLICACION, ES_CFD, TIENE_ANTICIPO, MODALIDAD_FACTURACION,
	CFDI_COBRO_DIFERIDO, FOLIO_RECIBO_PAGO, ENVIADO, FECHA_HORA_ENVIO,
	EMAIL_ENVIO, CFDI_CERTIFICADO, USO_CFDI, METODO_PAGO_SAT, INTEG_BA,
	CONTABILIZADO_BA, CUENTA_BAN_ID, REFER_MOVTO_BANCARIO, CFDI_ASOCIADO_ID,
	METODO_PAGO_CFDI_ASOCIADO, TERCERO_CO_ID, USUARIO_CREADOR,
	FECHA_HORA_CREACION, USUARIO_AUT_CREACION, USUARIO_ULT_MODIF,
	FECHA_HORA_ULT_MODIF, USUARIO_AUT_MODIF, USUARIO_CANCELACION,
	FECHA_HORA_CANCELACION, USUARIO_AUT_CANCELACION, LAT, LON, ENVIADO_WT
) VALUES (
	?, ?, ?, ?, ?,
	?, ?, ?, ?, ?,
	?, ?, ?, ?, ?,
	?, ?, ?, ?,
	?, ?, ?, ?,
	?, ?, ?, ?, ?,
	?, ?, ?, ?,
	?, ?, ?, ?,
	?, ?, ?, ?, ?,
	?, ?, ?, ?,
	?, ?, ?,
	?, ?, ?,
	?, ?, ?,
	?, ?, ?, ?, ?
) RETURNING DOCTO_CC_ID`

// insertImporteDoctoCCSQL — 14 columns matching the legacy QUERY_INSERT_PAGO_IMPORTES.
// IMPTE_DOCTO_CC_ID is -1, trigger replaces, RETURNING captures the assigned id.
const insertImporteDoctoCCSQL = `
INSERT INTO IMPORTES_DOCTOS_CC (
	IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA, CANCELADO, APLICADO,
	ESTATUS, TIPO_IMPTE, DOCTO_CC_ACR_ID, IMPORTE, IMPUESTO,
	IVA_RETENIDO, ISR_RETENIDO, DSCTO_PPAG, PCTJE_COMIS_COB
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING IMPTE_DOCTO_CC_ID`

// insertFormaCobroDoctoSQL — 8 columns matching QUERY_INSERT_FORMA_COBRO.
const insertFormaCobroDoctoSQL = `
INSERT INTO FORMAS_COBRO_DOCTOS (
	FORMA_COBRO_DOC_ID, NOM_TABLA_DOCTOS, DOCTO_ID, FORMA_COBRO_ID,
	NUM_CTA_PAGO, CLAVE_SIS_FORMA_COB, REFERENCIA, IMPORTE
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

// ─── Aplicar ─────────────────────────────────────────────────────────────────

// Aplicar runs the 5-statement Microsip materialization in the caller's
// transaction. Returns DOCTO_CC_ID, IMPTE_DOCTO_CC_ID, and the folio string.
//
// The caller must roll back the surrounding transaction on any returned
// error; otherwise the DOCTOS_CC header is left orphaned without its
// IMPORTES_DOCTOS_CC line.
func (w *PagoWriter) Aplicar(ctx context.Context, in outbound.MicrosipPagoInput) (outbound.MicrosipPagoResult, error) {
	q := firebird.GetQuerier(ctx, w.pool.DB)

	folioTemp, err := w.execGenFolioTemp(ctx, q)
	if err != nil {
		return outbound.MicrosipPagoResult{}, err
	}
	folio := folioPrefix + folioTemp

	claveCliente, err := w.fetchClaveCliente(ctx, q, in.ClienteID)
	if err != nil {
		return outbound.MicrosipPagoResult{}, err
	}

	doctoCCID, err := w.insertDoctoCC(ctx, q, in, folio, claveCliente)
	if err != nil {
		return outbound.MicrosipPagoResult{}, err
	}

	impteDoctoCCID, err := w.insertImporteDoctoCC(ctx, q, doctoCCID, in)
	if err != nil {
		return outbound.MicrosipPagoResult{}, err
	}

	if err := w.insertFormaCobroDocto(ctx, q, doctoCCID, in.FormaCobroID); err != nil {
		return outbound.MicrosipPagoResult{}, err
	}

	return outbound.MicrosipPagoResult{
		DoctoCCID:      doctoCCID,
		ImpteDoctoCCID: impteDoctoCCID,
		Folio:          folio,
	}, nil
}

// execGenFolioTemp calls Microsip's GEN_FOLIO_TEMP stored procedure which
// returns the next FOLIO_TEMP value (a numeric string). The procedure has
// no arguments; the legacy passed clienteId by accident — we omit it.
func (w *PagoWriter) execGenFolioTemp(ctx context.Context, q firebird.Querier) (string, error) {
	var folioTemp sql.NullString
	if err := q.QueryRowContext(ctx, execGenFolioTempSQL).Scan(&folioTemp); err != nil {
		return "", firebird.MapError(err)
	}
	if !folioTemp.Valid || folioTemp.String == "" {
		return "", apperror.NewInternal(
			"microsip_folio_temp_vacio",
			"el procedimiento GEN_FOLIO_TEMP devolvió un folio vacío",
		)
	}
	return folioTemp.String, nil
}

// fetchClaveCliente resolves the textual CLAVE_CLIENTE from CLAVES_CLIENTES.
// Returns "" silently when the cliente has no clave (legacy behavior — it
// uses `|| ""` to fall through), so the DOCTOS_CC row can still be inserted.
func (w *PagoWriter) fetchClaveCliente(ctx context.Context, q firebird.Querier, clienteID int) (string, error) {
	var clave sql.NullString
	err := q.QueryRowContext(ctx, selectClaveClienteSQL, clienteID).Scan(&clave)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", firebird.MapError(err)
	}
	if !clave.Valid {
		return "", nil
	}
	return clave.String, nil
}

// insertDoctoCC writes the 59-column DOCTOS_CC row that represents the abono
// header. Hardcoded constants come from the legacy implementation.
func (w *PagoWriter) insertDoctoCC(ctx context.Context, q firebird.Querier, in outbound.MicrosipPagoInput, folio, claveCliente string) (int, error) {
	wall := firebird.ToWallClock(in.FechaHoraPago)
	fecha := wall.Format("2006-01-02")
	// Legacy adds 10 seconds to HORA — preserve that quirk so the new path
	// produces row-identical output to the old one.
	hora := wall.Add(10 * time.Second).Format("15:04:05")
	fechaHoraPago := wall.Format("2006-01-02 15:04:05")
	// USUARIO_AUT_MODIF in the legacy carries the user marker too, even
	// though it would normally be the authorizing user; preserved for
	// row-fidelity with existing Microsip data.
	now := time.Now().In(firebird.BusinessTZ()).Format("2006-01-02 15:04:05")

	lat, lon := optionalString(in.Lat), optionalString(in.Lon)

	args := []any{
		-1,                 // DOCTO_CC_ID — trigger replaces.
		in.ConceptoCCID,    // CONCEPTO_CC_ID.
		folio,              // FOLIO.
		naturalezaConcepto, // NATURALEZA_CONCEPTO = "R".
		sucursalID,         // SUCURSAL_ID (hardcoded).
		fecha,              // FECHA.
		hora,               // HORA (FechaHoraPago + 10s, legacy quirk).
		claveCliente,       // CLAVE_CLIENTE.
		0,                  // IMPORTE_COBRO (legacy hardcoded 0).
		fechaHoraPago,      // FECHA_HORA_PAGO.
		in.ClienteID,       // CLIENTE_ID.
		tipoCambio,         // TIPO_CAMBIO (MXN).
		"N",                // CANCELADO.
		"S",                // APLICADO.
		in.Cobrador,        // DESCRIPCION (cobrador name).
		lugarExpedicionID,  // LUGAR_EXPEDICION_ID (hardcoded).
		nil,                // CUENTA_CONCEPTO.
		in.CobradorID,      // COBRADOR_ID.
		"N",                // FORMA_EMITIDA.
		"N",                // CONTABILIZADO.
		"N",                // CONTABILIZADO_GYP.
		nil,                // COND_PAGO_ID.
		nil,                // FECHA_DSCTO_PPAG.
		0,                  // PCTJE_DSCTO_PPAG.
		nil,                // FACTURA_MOSTRADOR.
		sistemaOrigen,      // SISTEMA_ORIGEN = "CC".
		estatusPreaplicado, // ESTATUS = "P".
		estatusPreaplicado, // ESTATUS_ANT = "P".
		nil,                // FECHA_APLICACION.
		"N",                // ES_CFD.
		"N",                // TIENE_ANTICIPO.
		"PREIMP",           // MODALIDAD_FACTURACION.
		"false",            // CFDI_COBRO_DIFERIDO (legacy stores string).
		nil,                // FOLIO_RECIBO_PAGO.
		"N",                // ENVIADO.
		now,                // FECHA_HORA_ENVIO.
		nil,                // EMAIL_ENVIO.
		"N",                // CFDI_CERTIFICADO.
		nil,                // USO_CFDI.
		nil,                // METODO_PAGO_SAT.
		"N",                // INTEG_BA.
		"N",                // CONTABILIZADO_BA.
		nil,                // CUENTA_BAN_ID.
		nil,                // REFER_MOVTO_BANCARIO.
		nil,                // CFDI_ASOCIADO_ID.
		nil,                // METODO_PAGO_CFDI_ASOCIADO.
		nil,                // TERCERO_CO_ID.
		usuarioMarca,       // USUARIO_CREADOR.
		now,                // FECHA_HORA_CREACION.
		nil,                // USUARIO_AUT_CREACION.
		usuarioMarca,       // USUARIO_ULT_MODIF.
		now,                // FECHA_HORA_ULT_MODIF.
		usuarioMarca,       // USUARIO_AUT_MODIF (legacy quirk).
		nil,                // USUARIO_CANCELACION.
		nil,                // FECHA_HORA_CANCELACION.
		nil,                // USUARIO_AUT_CANCELACION.
		lat,                // LAT.
		lon,                // LON.
		nil,                // ENVIADO_WT.
	}

	var doctoCCID int
	if err := q.QueryRowContext(ctx, insertDoctoCCSQL, args...).Scan(&doctoCCID); err != nil {
		return 0, firebird.MapError(err)
	}
	if doctoCCID <= 0 {
		return 0, apperror.NewInternal(
			"microsip_docto_cc_id_invalido",
			"DOCTOS_CC.DOCTO_CC_ID inválido tras INSERT",
		).WithField("returned", strconv.Itoa(doctoCCID))
	}
	return doctoCCID, nil
}

// insertImporteDoctoCC writes the IMPORTES_DOCTOS_CC line. The legacy
// hardcodes IMPUESTO=0 plus the four reten/dscto/comis columns — preserved.
// FECHA on the importe row is the current date (not the FechaHoraPago) per
// the legacy; this is what triggers the MSP_PAGOS_VENTAS recompute via
// IMPORTES_DOCTOS_CC_AIUD.
func (w *PagoWriter) insertImporteDoctoCC(ctx context.Context, q firebird.Querier, doctoCCID int, in outbound.MicrosipPagoInput) (int, error) {
	fechaToday := time.Now().In(firebird.BusinessTZ()).Format("2006-01-02")
	importeStr := in.Importe.StringFixed(2)
	args := []any{
		-1,                 // IMPTE_DOCTO_CC_ID — trigger replaces.
		doctoCCID,          // DOCTO_CC_ID.
		fechaToday,         // FECHA (legacy: today's date, not fecha del pago).
		"N",                // CANCELADO.
		"S",                // APLICADO.
		estatusPreaplicado, // ESTATUS = "P".
		"R",                // TIPO_IMPTE = "R" (Recibo).
		in.CargoDoctoCCID,  // DOCTO_CC_ACR_ID — the cargo being acreditado.
		importeStr,         // IMPORTE.
		"0",                // IMPUESTO.
		"0",                // IVA_RETENIDO.
		"0",                // ISR_RETENIDO.
		"0",                // DSCTO_PPAG.
		"0",                // PCTJE_COMIS_COB.
	}
	var impteDoctoCCID int
	if err := q.QueryRowContext(ctx, insertImporteDoctoCCSQL, args...).Scan(&impteDoctoCCID); err != nil {
		return 0, firebird.MapError(err)
	}
	if impteDoctoCCID <= 0 {
		return 0, apperror.NewInternal(
			"microsip_impte_docto_cc_id_invalido",
			"IMPORTES_DOCTOS_CC.IMPTE_DOCTO_CC_ID inválido tras INSERT",
		)
	}
	return impteDoctoCCID, nil
}

// insertFormaCobroDocto writes the FORMAS_COBRO_DOCTOS row linking the abono
// to its forma_cobro_id (efectivo / cheque / transferencia). IMPORTE is
// legacy-zero here too — the actual amount lives in IMPORTES_DOCTOS_CC.
func (w *PagoWriter) insertFormaCobroDocto(ctx context.Context, q firebird.Querier, doctoCCID, formaCobroID int) error {
	args := []any{
		-1,           // FORMA_COBRO_DOC_ID.
		"DOCTOS_CC",  // NOM_TABLA_DOCTOS.
		doctoCCID,    // DOCTO_ID.
		formaCobroID, // FORMA_COBRO_ID.
		"",           // NUM_CTA_PAGO.
		"CC",         // CLAVE_SIS_FORMA_COB.
		"",           // REFERENCIA.
		0,            // IMPORTE.
	}
	if _, err := q.ExecContext(ctx, insertFormaCobroDoctoSQL, args...); err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// optionalString unwraps a *string into a value the driver can accept —
// nil → SQL NULL via driver convention; otherwise the dereferenced string.
func optionalString(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}
