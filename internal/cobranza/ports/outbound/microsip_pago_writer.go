//nolint:misspell // Microsip table/column identifiers (DESCRIPCION, IMPORTES, conceptos) are kept verbatim.
package outbound

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// MicrosipPagoInput is the request value object passed to MicrosipPagoWriter.
// Built by the app layer from a PagoRecibido aggregate; the writer is dumb
// about domain — it only cares about the column values to INSERT.
type MicrosipPagoInput struct {
	// CargoDoctoCCID is the DOCTOS_CC row id of the cargo being abonado.
	CargoDoctoCCID int

	// ClienteID is the Microsip cliente_id.
	ClienteID int

	// CobradorID is the Microsip cobradores.cobrador_id.
	CobradorID int

	// Cobrador is the free-text cobrador name written to
	// DOCTOS_CC.DESCRIPCION.
	Cobrador string

	// FormaCobroID is the Microsip formas_cobro.forma_cobro_id (efectivo,
	// cheque, transferencia, etc.).
	FormaCobroID int

	// ConceptoCCID is the Microsip conceptos_cc.concepto_cc_id derived from
	// FormaCobroID by the domain layer (27969 abono mostrador, 87327 cobranza
	// ruta).
	ConceptoCCID int

	// Importe is the payment amount.
	Importe decimal.Decimal

	// FechaHoraPago is the client-captured payment timestamp. The writer
	// breaks it into DOCTOS_CC.FECHA / .HORA / .FECHA_HORA_PAGO.
	FechaHoraPago time.Time

	// Lat / Lon are optional GPS coordinates; the writer stores them as
	// strings in DOCTOS_CC.LAT / .LON (legacy format).
	Lat *string
	Lon *string
}

// MicrosipPagoResult is what the writer returns after the 5-statement
// transaction completes successfully. The app layer feeds these back into
// PagoRecibido.MarcarAplicada.
type MicrosipPagoResult struct {
	// DoctoCCID is the auto-generated DOCTOS_CC.DOCTO_CC_ID of the abono
	// header.
	DoctoCCID int

	// ImpteDoctoCCID is the auto-generated IMPORTES_DOCTOS_CC.IMPTE_DOCTO_CC_ID
	// of the importe row.
	ImpteDoctoCCID int

	// Folio is the Microsip folio assigned to the abono (format "Z<N>"
	// after EXECUTE PROCEDURE GEN_FOLIO_TEMP).
	Folio string
}

// MicrosipPagoWriter materializes a PagoRecibido into Microsip's DOCTOS_CC /
// IMPORTES_DOCTOS_CC / FORMAS_COBRO_DOCTOS tables in a single atomic
// transaction (5 statements). The caller (AplicarPago in app layer) is
// responsible for the surrounding transaction begin/commit.
//
// On any error the caller treats it as transient: RegistrarFallo on the
// aggregate and leave ESTADO='P' so the retry worker picks it up later.
// The writer itself is stateless — it owns no retry/backoff logic.
type MicrosipPagoWriter interface {
	// Aplicar runs the 5-statement INSERT sequence and returns the generated
	// IDs and folio. The implementation MUST be safely re-runnable from the
	// caller's side: the caller's transaction must be rolled back on error,
	// and a new MicrosipPagoResult will be generated on the next attempt
	// (new DoctoCCID, new ImpteDoctoCCID, possibly new Folio).
	Aplicar(ctx context.Context, in MicrosipPagoInput) (MicrosipPagoResult, error)
}
