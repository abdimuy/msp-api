//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import "time"

// Narrativa is the materialized LLM "analyst reading" + AI-selected traits for
// one client. Texto is the Spanish analyst paragraph; Rasgos are validated
// catalog codes (see Rasgo). InputHash ties the row to the facts it was
// generated from — when the facts change, the hash changes and the row is stale.
type Narrativa struct {
	ClienteID  int
	Texto      string
	Rasgos     []string
	InputHash  string
	Modelo     string
	GeneradaEn time.Time
}
