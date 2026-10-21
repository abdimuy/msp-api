//nolint:misspell // domain vocabulary is Spanish (correccion, etc.) per project convention.
package domain

// TipoEvento identifies what a folio event records on the timeline: a stage
// change (the stage-bearing types) or a folio-level, manual or correction fact
// that never carries a stage change.
type TipoEvento string

// Event types this folio logs. The stage-bearing types mirror the folio method
// table plus the repair-branch facts; the rest record folio-level or manual
// facts and never carry a stage change.
const (
	TipoEventoFolioAbierto          TipoEvento = "folio_abierto"
	TipoEventoFolioEnProceso        TipoEvento = "folio_en_proceso"
	TipoEventoFolioListoEntrega     TipoEvento = "folio_listo_entrega"
	TipoEventoArticuloAgregado      TipoEvento = "articulo_agregado"
	TipoEventoArticuloMovido        TipoEvento = "articulo_movido"
	TipoEventoEtapaAvanzada         TipoEvento = "etapa_avanzada"
	TipoEventoDiagnosticoRegistrado TipoEvento = "diagnostico_registrado"
	TipoEventoDictamenRegistrado    TipoEvento = "dictamen_registrado"
	TipoEventoCambioAutorizado      TipoEvento = "cambio_autorizado"
	TipoEventoDesenlaceRegistrado   TipoEvento = "desenlace_registrado"
	TipoEventoDevolucionFallida     TipoEvento = "devolucion_fallida"
	TipoEventoFolioEntregado        TipoEvento = "folio_entregado"
	TipoEventoFolioCerrado          TipoEvento = "folio_cerrado"
	TipoEventoFolioCancelado        TipoEvento = "folio_cancelado"
	TipoEventoEvidenciaAdjuntada    TipoEvento = "evidencia_adjuntada"
	TipoEventoCorrection            TipoEvento = "correccion"
	TipoEventoNota                  TipoEvento = "nota"
)

// ParseTipoEvento validates a TipoEvento in its wire form.
func ParseTipoEvento(s string) (TipoEvento, error) {
	t := TipoEvento(s)
	if !t.IsValid() {
		return "", ErrTipoEventoInvalido
	}
	return t, nil
}

// IsValid reports whether t is one of the recognized event types.
func (t TipoEvento) IsValid() bool {
	switch t {
	case TipoEventoFolioAbierto,
		TipoEventoFolioEnProceso,
		TipoEventoFolioListoEntrega,
		TipoEventoArticuloAgregado,
		TipoEventoArticuloMovido,
		TipoEventoEtapaAvanzada,
		TipoEventoDiagnosticoRegistrado,
		TipoEventoDictamenRegistrado,
		TipoEventoCambioAutorizado,
		TipoEventoDesenlaceRegistrado,
		TipoEventoDevolucionFallida,
		TipoEventoFolioEntregado,
		TipoEventoFolioCerrado,
		TipoEventoFolioCancelado,
		TipoEventoEvidenciaAdjuntada,
		TipoEventoCorrection,
		TipoEventoNota:
		return true
	default:
		return false
	}
}

func (t TipoEvento) String() string { return string(t) }
