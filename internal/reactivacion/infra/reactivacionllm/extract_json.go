//nolint:misspell // Spanish domain vocabulary per project convention.
package reactivacionllm

import (
	"errors"
	"strings"
)

// ErrNoJSONInResponse is returned when the LLM response contains no balanced JSON object.
var ErrNoJSONInResponse = errors.New("reactivacionllm: no JSON object found in response")

// extractJSON strips <think>...</think> blocks and markdown fences, then finds
// and returns the first balanced JSON object in s.
func extractJSON(s string) (string, bool) {
	// Remove <think>...</think> blocks (some reasoning models emit these).
	// An unclosed <think> is stripped to end-of-string so stray '{' inside
	// a malformed block can't be mis-parsed as a JSON object.
	for {
		start := strings.Index(s, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(s, "</think>")
		if end == -1 {
			s = s[:start]
			break
		}
		s = s[:start] + s[end+len("</think>"):]
	}

	// Find first '{'.
	i := strings.Index(s, "{")
	if i == -1 {
		return "", false
	}

	depth := 0
	inStr := false
	for j := i; j < len(s); j++ {
		ch := s[j]
		if inStr {
			if ch == '\\' {
				j++ // skip escaped char
				continue
			}
			if ch == '"' {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[i : j+1], true
			}
		}
	}
	return "", false
}
