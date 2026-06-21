package llm

import (
	"context"
	"errors"
	"log/slog"
)

// ErrLLMDisabled is returned by disabledClient.Chat when LLM_ENABLED is false.
// Callers may check with errors.Is(err, ErrLLMDisabled) to degrade gracefully
// (e.g. skip narrative generation, return an empty string, use a static
// fallback).
var ErrLLMDisabled = errors.New("llm: disabled")

// disabledClient is the safe fallback used when LLM_ENABLED=false (the
// default). Every Chat call returns ErrLLMDisabled immediately without
// attempting any network connection.
type disabledClient struct{}

// Compile-time assertion: disabledClient must satisfy Client.
var _ Client = (*disabledClient)(nil)

// newDisabledClient constructs the always-error client and logs a warning so
// operators know LLM features are running in degraded mode.
func newDisabledClient() *disabledClient {
	slog.Warn("llm.disabled: LLM features degraded; set LLM_ENABLED=true to activate")
	return &disabledClient{}
}

// Chat returns ErrLLMDisabled without making any network request.
func (c *disabledClient) Chat(_ context.Context, _ ChatReq) (string, error) {
	return "", ErrLLMDisabled
}
