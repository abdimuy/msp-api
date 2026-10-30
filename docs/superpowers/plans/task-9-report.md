# Task 9 Report — Narrativa validation (direction check + trait filtering)

## Status

DONE — implementación completa, tests en verde.

## Packages created/modified

- `internal/analytics/app/narrativa_validate.go` — `ValidatedNarrativa`, `ValidarNarrativa`, helpers `pasaDirectionCheck`, `filtrarRasgos`, `caparContexto`.
- `internal/analytics/app/narrativa_validate_test.go` — suite completa (ver abajo).

## Resumen de implementación

- **(a) Direction check:** falla (→ fallback vacío) si el texto está vacío, es <40 o >1200 runes (`utf8.RuneCountInString`); o si `riesgoAlto` (TierRiesgo CRITICO/EN_RIESGO, EstadoPago MOROSO o BandaCredito CRITICO) y el texto NFC-normalizado+lowercased contiene alguna frase prohibida. Lista `forbiddenPhrases` package-level (tunable).
- **Fallback:** `ValidatedNarrativa{Texto: "", Rasgos: nil, UsedFallback: true}`. El texto queda vacío a propósito (no se concatenan los titulars Fase-1).
- **(b) Trait filtering** (solo si pasa direction check): conserva códigos válidos (`EsRasgoValido`), dedup por first-seen, cap a 3. Puede quedar vacío sin ser fallback.
- **Extensión vs brief:** campo adicional `ContextoOperativo` (trim + cap 240 runes), preservado incluso en fallback, ya que es contexto factual independiente del riesgo.

## Test summary

`go test ./internal/analytics/...` → OK (cached). Tests cubiertos:

| Test | Qué cubre |
|------|-----------|
| `TestValidarNarrativa_HappyPath` | narrativa válida + rasgos válidos pasan intactos |
| `TestValidarNarrativa_TraitFiltering` | inválidos descartados, dedup, cap a 3, orden preservado |
| `TestValidarNarrativa_ContradictionFallback` | contradicción bajo CRITICO → fallback vacío |
| `TestValidarNarrativa_EmptyOrTooShortFallback` | vacío/corto → fallback |
| `TestValidarNarrativa_TooLongFallback` | >1200 runes → fallback |
| `TestValidarNarrativa_LowRiskPositivePhraseOK` | frase positiva OK en AL_DIA |
| `TestValidarNarrativa_EnRiesgoContradiction` | EN_RIESGO dispara check |
| `TestValidarNarrativa_MorosoContradiction` | MOROSO solo dispara check |
| `TestValidarNarrativa_NoRasgosAfterFilter` | rasgos inválidos → Rasgos vacío, sin fallback |
| `TestValidarNarrativa_BandaCreditoCriticoContradiction` | BandaCredito CRITICO solo dispara check |
| `TestValidarNarrativa_ExactBoundaryLengths` | fronteras 40 y 1200 runes |
| `TestValidarNarrativa_ContextoTrimmed` | trim de ContextoOperativo |
| `TestValidarNarrativa_ContextoCappedAt240Runes` | cap a 240 runes |
| `TestValidarNarrativa_ContextoPreservedOnFallback` | ContextoOperativo preservado en fallback |

## Concerns

Ninguno funcional. La única diferencia contra el brief es la extensión `ContextoOperativo` (no pedida originalmente, pero coherente con el diseño del worker). Commit no ejecutado (pendiente a instrucción del usuario).
