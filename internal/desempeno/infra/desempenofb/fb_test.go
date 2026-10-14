package desempenofb_test

import (
	"os"
	"testing"
)

// requireFBEnv salta la prueba si no hay base Firebird (CI corre sin
// FB_DATABASE; CLAUDE.md §7).
func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
	}
}
