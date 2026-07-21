//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"fmt"
	"strings"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

// resumenMaxTurnos caps how many of the most recent turnos are included in
// the memory summary — only recent context matters for the next LLM call,
// and a bounded window keeps the prompt small regardless of how long a
// conversation runs.
const resumenMaxTurnos = 6

// resumenMaxRunes caps the summary's total length (defence in depth on top of
// resumenMaxTurnos, in case a handful of turnos are individually very long) —
// keeps the prompt payload bounded and predictable.
const resumenMaxRunes = 2000

// resumenSinHistorial is returned by construirResumen when turnos is empty —
// a brand-new conversation with no prior exchange to summarize.
const resumenSinHistorial = "sin historial de turnos previos."

// construirResumen builds a compact, deterministic, structured summary of a
// conversation's turnos for the LLM's ResumenMemoria input — NOT the raw chat
// transcript. It is a header line with the total turn count, followed by up
// to resumenMaxTurnos of the MOST RECENT turnos (turnos is assumed
// chronologically ascending, per ConversacionRepo.ListarTurnos' contract),
// each labeled by its Autor, capped overall to resumenMaxRunes runes.
//
// Pure function: same turnos → same summary, every time.
func construirResumen(turnos []*domain.Turno) string {
	if len(turnos) == 0 {
		return resumenSinHistorial
	}

	ventana := turnos
	if len(ventana) > resumenMaxTurnos {
		ventana = ventana[len(ventana)-resumenMaxTurnos:]
	}

	var b strings.Builder
	//nolint:revive // writes to strings.Builder never fail.
	_, _ = fmt.Fprintf(&b, "historial: %d turno(s) en total, mostrando los últimos %d.\n", len(turnos), len(ventana))
	for _, t := range ventana {
		//nolint:revive // writes to strings.Builder never fail.
		_, _ = fmt.Fprintf(&b, "- %s: %s\n", t.Autor().String(), t.Cuerpo())
	}

	resumen := strings.TrimSpace(b.String())
	runes := []rune(resumen)
	if len(runes) > resumenMaxRunes {
		resumen = strings.TrimSpace(string(runes[:resumenMaxRunes]))
	}
	return resumen
}
