//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

// Rasgo is a curated behavioral trait the analyst-AI may assign to a client.
// Codigo is the stable enum key (English snake_case); Etiqueta is the Spanish
// display label; Definicion is a short Spanish description (~50 words) used to
// anchor the LLM's choice.
type Rasgo struct {
	Codigo     string
	Etiqueta   string
	Definicion string
}
