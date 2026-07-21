//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

// umbralConfianzaBaja is the LLM confidence floor (0-100) below which triar
// escalates regardless of what Senales the LLM reported (spec §5's "<~65%"
// calibration for the confianza_baja signal). The routing narrative elsewhere
// mentions a <70% line — that is the SAME knob at a different informal
// rounding; this constant is the single source of truth.
const umbralConfianzaBaja = 65

// Escalation-reason strings (Spanish, lowercase, no trailing period) — the
// human-readable RazonEscalamiento a DecisionFinal carries when triar
// escalates. One per policy rule in triar, plus the two escalation paths that
// do not come from a raw LLM Senal (the debt-figure guard and the
// copiloto-unavailable safe fallback).
const (
	razonDeuda                = "deuda"
	razonSenalCompra          = "señal de compra"
	razonPideHumano           = "solicitud de humano"
	razonEnojoLoop            = "enojo o repetición"
	razonFueraAllowlist       = "fuera del allowlist"
	razonConfianzaBaja        = "confianza baja"
	razonCifraDeuda           = "el borrador menciona una cifra de deuda"
	razonCopilotoNoDisponible = "copiloto no disponible"
)

// debtKeywords lists the lowercased, accent-normalized Spanish words that
// signal the cliente's outstanding balance is being discussed — used by
// borradorMencionaCifraDeuda (triaje.go) to guard against the LLM ever
// stating a debt figure. Curated deliberately narrow (false negatives are
// safer here than false positives that would over-escalate innocuous drafts):
//
//   - deuda, debe, debes, adeudo: direct references to owing money.
//   - saldo, pendiente, restante, resta: references to an outstanding balance.
//   - abonar: a payment made against a balance (implies one exists).
var debtKeywords = []string{
	"deuda",
	"debe",
	"debes",
	"adeudo",
	"saldo",
	"pendiente",
	"restante",
	"resta",
	"abonar",
}

// allowlistText is the Spanish description of what the copiloto bot may say,
// fed to the LLM as AnalizarInput.Allowlist/RedactarInput.Allowlist (spec §8).
// It is a steering rail for the LLM's OWN proposal — the deterministic triar
// function is what actually enforces these rules on the app side; the LLM
// text below never substitutes for that enforcement.
func allowlistText() string {
	return "Puede OFRECER productos del catálogo con stock confirmado, el siguiente mejor producto, " +
		"y planes de pago (enganche y parcialidad) leídos de la base de datos. Puede AFIRMAR el estatus " +
		"de una compra completada, en tono positivo. Debe ESCALAR (no responder) ante: una señal de compra, " +
		"cualquier cifra de deuda, una solicitud de hablar con un humano, contenido fuera de este allowlist, " +
		"o confianza baja en su propia lectura. Nunca debe: mencionar cifras de deuda o saldo pendiente, " +
		"inventar precios o fechas, hablar de cobranza, ni citar la nota del cobrador directamente. " +
		"Los únicos montos permitidos son los de la compra nueva que se está ofreciendo."
}
