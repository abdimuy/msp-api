// Package llm provides a thin OpenAI-compatible chat client for local inference
// servers (e.g. Ollama, llama-server).
//
// Two implementations are shipped:
//
//   - disabledClient: safe fallback used when LLM_ENABLED is false (the
//     default). Every Chat call returns ErrLLMDisabled so callers can degrade
//     gracefully (e.g. skip narrative generation).
//   - realClient: production client using raw net/http with no third-party SDK.
//     Initialized at boot when LLM_ENABLED=true and LLM_BASE_URL is set.
//
// Selection happens in the factory (NewClient) at boot. The config layer
// (see internal/platform/config) gates which selection is legal.
//
// This package is GENERIC: it must not import anything from internal/analytics
// or any other domain module. Domain-specific prompts and response schemas live
// in the module that uses this client.
package llm
