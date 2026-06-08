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

	// existenciaScale is the NUMERIC scale for SALDOS_IN stock figures.
	existenciaScale = 4
)
