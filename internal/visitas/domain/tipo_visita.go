package domain

// TipoVisita is the CLOSED catalogue of visit outcomes a cobrador may record.
//
// ─── Why this is closed on the server, and why it contains what it contains ──
//
// TIPO_VISITA is a VARCHAR(100) free-text column in MSP_VISITAS. Until this
// type existed, the write path validated only length and safe characters, so
// any string at all could be persisted. Closing it server-side gives the
// module a single authoritative list that the mobile app, the read path and
// the reporting queries can all agree on.
//
// The catalogue was NOT guessed. It is the union of two MEASURED sets:
//
//  1. The 11 literals the current Android app sends
//     (core/utils/Constants.kt:31-41).
//
//  2. Every distinct value that actually exists in MSP_VISITAS today,
//     measured over all 226,746 rows:
//
//     SELECT TIPO_VISITA, COUNT(*) FROM MSP_VISITAS GROUP BY TIPO_VISITA;
//
// That measurement returned 15 groups whose counts sum to exactly 226,746 —
// 14 non-empty values plus the empty string. All 11 app literals were among
// them. The three extra non-empty values are retired literals from older app
// builds, and they are in this catalogue ON PURPOSE:
//
//	"No va a dar pago"        38,855 rows  2024-09-12 → 2025-08-12
//	"Dijo que no va a pagar"  16,925 rows  2025-04-11 → 2026-02-18
//	"Se esconde y no sale"     3,682 rows  2024-09-14 → 2025-08-10
//
// "Dijo que no va a pagar" is the one that matters most. The app renamed that
// constant in commit f7de5f02 (2025-09-02) — same Kotlin identifier, new
// wire value:
//
//	before: const val NO_VA_A_DAR_PAGO = "Dijo que no va a pagar"
//	after:  const val NO_VA_A_DAR_PAGO = "No pagará esta ocasión"
//
// and the new literal duly starts appearing on 2025-09-13. But the OLD one
// keeps arriving for another five months — 852 rows in the last 90 days of
// the dataset, the newest on 2026-02-18. Those are phones in the street that
// never took the update. Dropping the retired literals from this catalogue
// would start rejecting their writes, which is precisely the data loss this
// module exists to prevent.
//
// ─── The empty string is deliberately NOT here ──────────────────────────────
//
// 200 rows carry an empty TIPO_VISITA — the zero-length string, not NULL: the
// measurement found 0 NULLs and 0 whitespace-only values. They all fall in a
// ten-day window at the very start of the dataset (2024-09-15 → 2024-09-25) and
// nothing has produced one since. NewVisita already rejects a blank tipo with
// ErrVisitaTipoRequerido and that stays: an empty outcome is not a valid new
// capture. Those 200 rows still READ back fine — see RehydrateVisita.
//
// ─── Read path vs write path ────────────────────────────────────────────────
//
// This type is enforced on the WRITE path ONLY (NewVisita → ParseTipoVisita).
// RehydrateVisita does NOT validate: it casts whatever bytes the row holds
// straight into a TipoVisita, exactly as HydrateImagenStorage does for
// StorageKind. If it validated, GET /v2/visitas/{id} would fail on any legacy
// row whose value ever falls outside this list.
//
// Modelled on internal/cobranza/domain/imagen_storage.go's StorageKind
// (ParseX / IsValid / String / hydrate-without-validation). It differs in one
// respect: StorageKind has a single member and compares directly, while this
// catalogue has 14 and therefore keeps one slice as the single source of
// truth rather than duplicating the list between IsValid and the accessor.
type TipoVisita string

// The 11 literals the current Android app sends. Names mirror the Kotlin
// constants in core/utils/Constants.kt:31-41 so the two lists can be diffed
// by eye; the VALUES are what travel on the wire and must not be edited.
const (
	// TipoVisitaNoSeEncontraba — Constants.NO_SE_ENCONTRABA. The single most
	// common outcome (118,514 rows).
	TipoVisitaNoSeEncontraba TipoVisita = "No se encontraba"
	// TipoVisitaCasaCerrada — Constants.CASA_CERRADA.
	TipoVisitaCasaCerrada TipoVisita = "Casa cerrada con candado"
	// TipoVisitaSoloMenores — Constants.SOLO_MENORES.
	TipoVisitaSoloMenores TipoVisita = "Solo había menores"
	// TipoVisitaNoVaADarPago — Constants.NO_VA_A_DAR_PAGO, current value.
	TipoVisitaNoVaADarPago TipoVisita = "No pagará esta ocasión"
	// TipoVisitaPideTiempo — Constants.PIDE_TIEMPO.
	TipoVisitaPideTiempo TipoVisita = "Pidió que regrese otro día"
	// TipoVisitaTienePeroNoPaga — Constants.TIENE_PERO_NO_PAGA.
	TipoVisitaTienePeroNoPaga TipoVisita = "Tiene dinero pero no quiso pagar"
	// TipoVisitaFueGrosero — Constants.FUE_GROSERO.
	TipoVisitaFueGrosero TipoVisita = "Fue grosero o agresivo"
	// TipoVisitaSeEsconde — Constants.SE_ESCONDE, current value.
	TipoVisitaSeEsconde TipoVisita = "Se asomó pero no salió"
	// TipoVisitaNoResponde — Constants.NO_RESPONDE.
	TipoVisitaNoResponde TipoVisita = "No responde aunque está"
	// TipoVisitaSeEscuchanRuidos — Constants.SE_ESCUCHAN_RUIDOS.
	TipoVisitaSeEscuchanRuidos TipoVisita = "Se escuchan ruidos pero no abre"
	// TipoVisitaPideReagendar — Constants.PIDE_REAGENDAR.
	TipoVisitaPideReagendar TipoVisita = "Pidió reagendar visita"
)

// Retired literals. No current app build sends these, but rows carrying them
// exist in MSP_VISITAS and phones running older APKs were still sending
// "Dijo que no va a pagar" at the end of the measured window. They stay
// ACCEPTED on write until telemetry proves no field device emits them.
// Removing one is a breaking change for whoever still runs that build.
const (
	// TipoVisitaLegacyNoVaADarPago is the pre-2025-09 wording of what the app
	// now calls NO_VA_A_DAR_PAGO. 38,855 rows, last seen 2025-08-12.
	TipoVisitaLegacyNoVaADarPago TipoVisita = "No va a dar pago"
	// TipoVisitaLegacyDijoQueNoVaAPagar is the literal commit f7de5f02
	// renamed away on 2025-09-02. 16,925 rows, still arriving on 2026-02-18
	// from phones that never updated — the reason this block exists.
	TipoVisitaLegacyDijoQueNoVaAPagar TipoVisita = "Dijo que no va a pagar"
	// TipoVisitaLegacySeEscondeYNoSale is the pre-rename wording of
	// SE_ESCONDE. 3,682 rows, last seen 2025-08-10.
	TipoVisitaLegacySeEscondeYNoSale TipoVisita = "Se esconde y no sale"
)

// tiposVisitaValidos is the SINGLE source of truth for the catalogue. IsValid
// scans it and TiposVisitaValidos copies it, so the two can never disagree.
// Order is stable (current app literals first, then retired ones) because
// TiposVisitaValidos feeds documentation and the mobile catalogue.
var tiposVisitaValidos = []TipoVisita{
	TipoVisitaNoSeEncontraba,
	TipoVisitaCasaCerrada,
	TipoVisitaSoloMenores,
	TipoVisitaNoVaADarPago,
	TipoVisitaPideTiempo,
	TipoVisitaTienePeroNoPaga,
	TipoVisitaFueGrosero,
	TipoVisitaSeEsconde,
	TipoVisitaNoResponde,
	TipoVisitaSeEscuchanRuidos,
	TipoVisitaPideReagendar,
	TipoVisitaLegacyNoVaADarPago,
	TipoVisitaLegacyDijoQueNoVaAPagar,
	TipoVisitaLegacySeEscondeYNoSale,
}

// ParseTipoVisita converts s into a TipoVisita, or returns
// ErrVisitaTipoInvalido — a 422 validation error carrying a distinguishable
// code, never a 500. The caller is expected to have trimmed and
// NFC-normalized s already (NewVisita does, via requireBounded); an accented
// literal in a different normalization form will NOT match otherwise.
func ParseTipoVisita(s string) (TipoVisita, error) {
	t := TipoVisita(s)
	if !t.IsValid() {
		return "", ErrVisitaTipoInvalido
	}
	return t, nil
}

// IsValid reports whether t is in the closed catalogue. Comparison is exact:
// byte-for-byte, case-sensitive, accents included.
func (t TipoVisita) IsValid() bool {
	for _, valido := range tiposVisitaValidos {
		if t == valido {
			return true
		}
	}
	return false
}

// String returns the canonical wire representation — the literal that travels
// in JSON and lands in MSP_VISITAS.TIPO_VISITA.
func (t TipoVisita) String() string { return string(t) }

// TiposVisitaValidos returns a copy of the catalogue in stable order. Callers
// get a fresh slice, so mutating the result cannot corrupt the catalogue.
func TiposVisitaValidos() []TipoVisita {
	out := make([]TipoVisita, len(tiposVisitaValidos))
	copy(out, tiposVisitaValidos)
	return out
}
