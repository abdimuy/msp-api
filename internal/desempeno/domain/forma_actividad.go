//nolint:misspell // vocabulario español por convención del proyecto.
package domain

// EtiquetaFormaDeActividad es la forma de cobro que se muestra y se filtra
// (contrato §1 `pago.forma_cobro`, §4 `forma_cobro`): manda la clasificación
// del tipo (ClasificarTipoPago), no el FORMA_COBRO_ID crudo. Una condonación
// por concepto 27969 que Microsip guardó con forma 157 es "condonacion"; si se
// leyera el id saldría "efectivo" y el filtro de dinero la incluiría.
// Visitas (sin pago) dan "".
func EtiquetaFormaDeActividad(a Actividad) string {
	if a.Pago == nil {
		return ""
	}
	if a.Tipo == TipoCondonacion {
		return EtiquetaCondonacion
	}
	return EtiquetaFormaCobro(a.Pago.FormaCobroID)
}
