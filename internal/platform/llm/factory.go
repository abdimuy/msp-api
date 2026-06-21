package llm

import "github.com/abdimuy/msp-api/internal/platform/config"

// NewClient selects the Client implementation based on config.
//
// Selection matrix:
//
//	Enabled == true   → realClient (raw net/http, OpenAI-compatible)
//	Enabled == false  → disabledClient (returns ErrLLMDisabled on every call)
//
// The config layer (config.LLM.validate) ensures BaseURL is set when Enabled
// is true, so newRealClient always receives a non-empty BaseURL here.
func NewClient(cfg config.LLM) Client {
	if cfg.Enabled {
		return newRealClient(cfg)
	}
	return newDisabledClient()
}
