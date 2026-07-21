//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAllowlistText_NotEmpty(t *testing.T) {
	t.Parallel()
	assert.NotEmpty(t, allowlistText())
}

func TestAllowlistText_MentionsEscalationTriggers(t *testing.T) {
	t.Parallel()
	text := strings.ToLower(allowlistText())
	// The allowlist rails must tell the LLM what forces an escalation, since
	// the deterministic triar policy is the final authority — this text only
	// steers the LLM's own proposal.
	for _, must := range []string{"escalar", "deuda", "humano", "confianza"} {
		assert.Contains(t, text, must)
	}
}

func TestAllowlistText_ForbidsDebtFiguresAndInventedData(t *testing.T) {
	t.Parallel()
	text := strings.ToLower(allowlistText())
	assert.Contains(t, text, "nunca")
	assert.Contains(t, text, "cifra")
}
