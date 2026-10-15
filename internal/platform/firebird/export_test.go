package firebird

import "time"

// RegisteredDefaultStatementTimeout exposes, to the external test package, the
// process default the single driver registration captured. Tests in one
// binary share it, and whichever firebird.New runs first decides it (some
// tests pass a config with no timeout at all), so integration tests must
// compare against this, never against their own cfg.
func RegisteredDefaultStatementTimeout() time.Duration {
	return registeredOptions.statementTimeout
}
