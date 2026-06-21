// Package app — rasgos_catalogo.go defines the curated behavioral trait catalog
// the analyst-AI uses to assign descriptive traits to clients. Helpers derive
// from the single source of truth CatalogoRasgos; no second list is maintained.
//
//nolint:misspell // Spanish vocabulary per project convention.
package app

import "github.com/abdimuy/msp-api/internal/analytics/domain"

// CatalogoRasgos is the authoritative list of behavioral traits the analyst-AI
// may assign to a client. The AI picks 1-3 codes exclusively from this list.
// These are descriptive/behavioral and do NOT duplicate deterministic badges
// (Moroso, CRÍTICO, segment, tier).
var CatalogoRasgos = []domain.Rasgo{
	{
		Codigo:     "loyal_but_stagnant",
		Etiqueta:   "Leal pero estancado",
		Definicion: "Compra desde hace tiempo y sigue presente, pero su frecuencia y su ticket dejaron de crecer; mantiene la relación sin profundizarla.",
	},
	{
		Codigo:     "recoverable_with_promo",
		Etiqueta:   "Recuperable con promoción",
		Definicion: "Bajó su ritmo o se alejó, pero su historial sugiere que responde a un incentivo puntual para reactivar la compra.",
	},
	{
		Codigo:     "enganche_sensitive",
		Etiqueta:   "Sensible al enganche",
		Definicion: "Su decisión de compra depende del enganche requerido; un enganche alto lo frena y uno accesible lo activa.",
	},
	{
		Codigo:     "seasonal_buyer",
		Etiqueta:   "Comprador de temporada",
		Definicion: "Concentra sus compras en ciertas épocas del año y permanece inactivo el resto, siguiendo un patrón estacional.",
	},
	{
		Codigo:     "pays_in_streaks",
		Etiqueta:   "Paga en rachas",
		Definicion: "Alterna periodos de pagos puntuales con pausas; cumple, pero de forma irregular y por tramos.",
	},
	{
		Codigo:     "steady_reliable",
		Etiqueta:   "Cumplido constante",
		Definicion: "Paga con regularidad y previsibilidad; su comportamiento es estable y de bajo mantenimiento.",
	},
	{
		Codigo:     "dormant_valuable",
		Etiqueta:   "Dormido valioso",
		Definicion: "Lleva tiempo sin comprar, pero su valor histórico lo vuelve prioritario para un intento de reactivación.",
	},
	{
		Codigo:     "high_value_at_risk",
		Etiqueta:   "Alto valor en riesgo",
		Definicion: "Representa ingresos importantes, pero señales recientes de atraso o silencio ponen esa relación en riesgo.",
	},
	{
		Codigo:     "cash_reliable",
		Etiqueta:   "Contado confiable",
		Definicion: "Prefiere comprar de contado y cumple sin generar exposición de crédito; relación sana y de bajo riesgo.",
	},
	{
		Codigo:     "churn_risk",
		Etiqueta:   "Riesgo de fuga",
		Definicion: "Su recencia y la caída de actividad indican que podría dejar de comprar si no se interviene pronto.",
	},
	{
		Codigo:     "growing_relationship",
		Etiqueta:   "Relación en crecimiento",
		Definicion: "Su frecuencia o su ticket vienen subiendo; es un cliente con impulso que conviene acompañar.",
	},
	{
		Codigo:     "price_sensitive",
		Etiqueta:   "Sensible al precio",
		Definicion: "Su compra reacciona al precio y a los descuentos; busca el mejor trato antes de decidir.",
	},
}

// rasgoPorCodigo is a package-level O(1) lookup table built once from
// CatalogoRasgos. All helpers derive from this single source of truth.
var rasgoPorCodigo = func() map[string]domain.Rasgo {
	m := make(map[string]domain.Rasgo, len(CatalogoRasgos))
	for _, r := range CatalogoRasgos {
		m[r.Codigo] = r
	}
	return m
}()

// EsRasgoValido reports whether codigo is a known catalog code.
func EsRasgoValido(codigo string) bool {
	_, ok := rasgoPorCodigo[codigo]
	return ok
}

// EtiquetaDe returns the Spanish display label for a code, or "" if unknown.
func EtiquetaDe(codigo string) string {
	return rasgoPorCodigo[codigo].Etiqueta
}
