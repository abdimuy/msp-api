//nolint:misspell // vocabulario español por convención del proyecto.
package domain

// Formas de cobro de los pagos de cobranza. Los IDs NO salen del catálogo
// FORMAS_COBRO (que miente: E-COB-017) sino de FORMAS_COBRO_DOCTOS cruzado con
// los pagos reales; coinciden con msp-app-kt VentanaCobro.FORMAS_COBRO_COBRANZA
// y PaymentDao.kt:17-23.
const (
	FormaCobroEfectivo      = 157
	FormaCobroCheque        = 158
	FormaCobroTransferencia = 52569
	// FormaCobroCondonacion es "Condonacion" en FORMAS_COBRO_CC; la app la
	// manda con el concepto 27969 (Condonaciones). No es dinero.
	FormaCobroCondonacion = 137026
)

// Conceptos de Microsip que viajan al teléfono (pagoConceptoFilter del sync).
// Los nombres son los de CONCEPTOS_CC, la autoridad (COBRANZA-SYNC.md §7.2):
// 27969 es "Condonaciones", no "abono mostrador".
const (
	ConceptoCobranzaEnRuta = 87327
	ConceptoCondonaciones  = 27969
)

// Etiquetas de forma de cobro del contrato §1 (`pago.forma_cobro`).
const (
	EtiquetaEfectivo      = "efectivo"
	EtiquetaTransferencia = "transferencia"
	EtiquetaCheque        = "cheque"
	EtiquetaCondonacion   = "condonacion"
	EtiquetaOtra          = "otra"
)

// EtiquetaFormaCobro traduce el ID al literal del contrato. Todo lo que no sea
// una de las cuatro formas conocidas es "otra" (incluido 0 = sin dato).
func EtiquetaFormaCobro(formaCobroID int) string {
	switch formaCobroID {
	case FormaCobroEfectivo:
		return EtiquetaEfectivo
	case FormaCobroTransferencia:
		return EtiquetaTransferencia
	case FormaCobroCheque:
		return EtiquetaCheque
	case FormaCobroCondonacion:
		return EtiquetaCondonacion
	default:
		return EtiquetaOtra
	}
}

// EsFormaDeDinero dice si la forma cuenta como dinero cobrado en el estado por
// venta (spec §2: 157, 158, 52569). La condonación y "otra" no.
func EsFormaDeDinero(formaCobroID int) bool {
	switch formaCobroID {
	case FormaCobroEfectivo, FormaCobroCheque, FormaCobroTransferencia:
		return true
	default:
		return false
	}
}

// FormaCobroCatalogo es una entrada de GET /desempeno/catalogos (contrato §4b).
type FormaCobroCatalogo struct {
	Codigo   string
	Etiqueta string
}

// CatalogoFormasCobro lista los códigos que acepta el filtro `forma_cobro`, en el
// orden en que se ofrecen.
func CatalogoFormasCobro() []FormaCobroCatalogo {
	return []FormaCobroCatalogo{
		{EtiquetaEfectivo, "Efectivo"},
		{EtiquetaTransferencia, "Transferencia"},
		{EtiquetaCheque, "Cheque"},
		{EtiquetaCondonacion, "Condonación"},
		{EtiquetaOtra, "Otra"},
	}
}
