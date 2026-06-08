//nolint:misspell // Microsip column names are Spanish by convention.
package invfb

// Firebird flag constants used in DOCTOS_IN and DOCTOS_IN_DET inserts.
// These mirror the single-char strings that Microsip treats as booleans /
// enum values. They are all ASCII strings stored in CHARACTER SET ASCII cols.
const (
	naturalezaSalida = "S" // DOCTOS_IN.NATURALEZA_CONCEPTO: salida de almacén
	flagNo           = "N" // CANCELADO, FORMA_EMITIDA, CONTABILIZADO, PEDIMENTO_PEND
	flagSi           = "S" // APLICADO, COSTEO_PEND
	sistemaOrigen    = "IN"
	metodoCosteo     = "C" // costeo promedio
	tipoMovtoSalida  = "S" // DOCTOS_IN_DET.TIPO_MOVTO for the outbound leg
	tipoMovtoEntrada = "E" // DOCTOS_IN_DET.TIPO_MOVTO for the inbound leg
	rolSalida        = "S" // DOCTOS_IN_DET.ROL for the outbound leg
	rolEntrada       = "E" // DOCTOS_IN_DET.ROL for the inbound leg
	pedimentoPend    = "N" // DOCTOS_IN_DET.PEDIMENTO_PEND

	// claveArticuloRolID is the ROL_CLAVE_ART_ID used by this branch. The
	// legacy Node API uses 17 for all traspaso articulo lookups.
	claveArticuloRolID = 17

	// tipoTraspasoDirecto and tipoTraspasoReverso are the values stored in
	// MSP_VENTAS_TRASPASOS.TIPO.
	tipoTraspasoDirecto = "directo"
	tipoTraspasoReverso = "reverso"

	// existenciaScale is the NUMERIC scale for SALDOS_IN.ENTRADAS_UNIDADES /
	// SALIDAS_UNIDADES, DOCTOS_IN_DET.UNIDADES and DOCTOS_PV_DET.UNIDADES —
	// all of them declared as NUMERIC(18,5) in Microsip's schema (verified
	// against RDB$FIELDS on the dev DB). Using a different scale here causes
	// stock readings to drift by a factor of 10 per unit of mismatch, since
	// the driver returns the unscaled integer (e.g. 300000 for 3.00000) and
	// ScanDecimal recovers the decimal point via 10^-scale.
	existenciaScale = 5
)
