package main

import (
	"github.com/abdimuy/msp-api/internal/platform/config"
	platformllm "github.com/abdimuy/msp-api/internal/platform/llm"
)

// provideLLMClient constructs the platform LLM client from config.
// When LLM_ENABLED=false (the default), NewClient returns a disabled stub
// that satisfies platformllm.Client and returns ErrLLMDisabled on every call
// — no network connection is opened, no model is ever contacted.
func provideLLMClient(cfg *config.Config) platformllm.Client {
	return platformllm.NewClient(cfg.LLM)
}
