package llm

import "testing"

func TestExtractJSON(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{"bare object", `{"a":1}`, `{"a":1}`, true},
		{"fenced", "```json\n{\"a\":1}\n```", `{"a":1}`, true},
		{"think prefix", "<think>thoughts</think>\n{\"a\":1}", `{"a":1}`, true},
		{"nested", `{"a":{"b":2},"c":3}`, `{"a":{"b":2},"c":3}`, true},
		{"with string braces", `{"a":"{not a brace}"}`, `{"a":"{not a brace}"}`, true},
		{"no json", "just text", "", false},
		{"only think", "<think>stuff</think>", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := extractJSON(tc.input)
			if ok != tc.ok {
				t.Fatalf("ok: got %v, want %v (input=%q)", ok, tc.ok, tc.input)
			}
			if ok && got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
