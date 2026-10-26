//nolint:misspell // domain vocabulary is Spanish (correccion, etc.) per project convention.
package domain

// TipoEvento identifies what a folklore event records on the timeline. The
// ten stage-bearing types come from the folio method table plus the repair
// branch facts (DiagnosticoRegistrado, DictamenRegistrado, CambioAutorizado,
// DesenlaceRegistrado); the last three never carry a stage change.
type TipoEvento string

// Event types this folio logs. The ten stage-bearing types mirror the folio
// method table plus the repair-branch facts; the last three never carry a
// stage change.
const (
	TipoEventoFolioAbierto          TipoEvento = "folio_abierto"
	TipoEventoArticuloAgregado      TipoEvento = "articulo_agregado"
	TipoEventoEtapaAvanzada         TipoEvento = "etapa_avanzada"
	TipoEventoDiagnosticoRegistrado TipoEvento = "diagnostico_registrado"
	TipoEventoDictamenRegistrado    TipoEvento = "dictamen_registrado"
	TipoEventoCambioAutorizado      TipoEvento = "cambio_autorizado"
	TipoEventoDesenlaceRegistrado   TipoEvento = "desenlace_registrado"
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
		TipoEventoArticuloAgregado,
		TipoEventoEtapaAvanzada,
		TipoEventoDiagnosticoRegistrado,
		TipoEventoDictamenRegistrado,
		TipoEventoCambioAutorizado,
		TipoEventoDesenlaceRegistrado,
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
