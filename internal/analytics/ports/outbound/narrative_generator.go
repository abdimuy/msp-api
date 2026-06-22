package outbound

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// NarrativeInput carries the already-computed facts a generator may narrate.
// The generator MUST anchor on these facts and never invent numbers. Catalogo
// is the curated trait list (with definitions) the generator must choose from.
type NarrativeInput struct {
	ClienteID int
	Nombre    string
	Zona      string

	// Bands & scores (deterministic — the generator must not contradict them)
	Segmento      string
	TierRiesgo    string
	EstadoPago    string
	BandaCredito  string
	ScoreCredito  int
	BandaRecompra string
	ScoreRecompra int
	BandaCLV      string

	// Magnitudes
	Saldo           decimal.Decimal
	Monetary        decimal.Decimal
	MontoCLV        decimal.Decimal
	Frecuencia      int
	RecenciaDias    int
	CadenciaDias    int
	DiasAtrasoProm  int
	PctPagosATiempo decimal.Decimal

	// Fase-1 deterministic titulars (the generator synthesizes ACROSS these)
	CreditoResumen  string
	RecompraResumen string
	CLVResumen      string

	// Drivers (quantified bullet facts)
	CreditoDrivers  []string
	RecompraDrivers []string
	CLVDrivers      []string

	// Nota is the cobrador's free-text note (CLIENTES.NOTAS) for the client,
	// already decoded/NFC-normalized/trimmed/capped. Optional qualitative context
	// (payment agreements, responsibles, shared address, dates). Empty when none.
	Nota string

	// The finite trait catalog the generator must pick 1-3 codes from.
	Catalogo []domain.Rasgo
}

// NarrativeOutput is the generator's raw result: a Spanish analyst paragraph and
// the trait CODES it selected (unvalidated — the app layer validates against the
// catalog and caps/dedups).
type NarrativeOutput struct {
	Narrativa string
	Rasgos    []string
	// ContextoOperativo is the operational signals distilled from in.Nota (raw,
	// unvalidated — the app layer trims and caps it). Empty when the note adds nothing.
	ContextoOperativo string
}

// NarrativeGenerator produces an analyst reading + trait selection for one client
// from already-computed facts. Implementations: the local-LLM adapter (infra/llm)
// and a deterministic fake (tests). A disabled/unavailable generator returns an
// error; callers degrade gracefully.
type NarrativeGenerator interface {
	Generar(ctx context.Context, in NarrativeInput) (NarrativeOutput, error)
}
