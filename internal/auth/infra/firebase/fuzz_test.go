package firebase

import (
	"testing"
)

func FuzzDevModeParser(f *testing.F) {
	f.Add("dev:alice")
	f.Add("dev:alice:alice@example.com")
	f.Add("dev:")
	f.Add("")
	f.Add("not a token")
	f.Add("dev:alice:bob:charlie") // too many parts
	f.Fuzz(func(t *testing.T, in string) {
		// Must not panic; must always return (uid="", email="", err!=nil) OR
		// a non-empty uid and err=nil.
		uid, _, err := parseDevModeToken(in)
		if err == nil && uid == "" {
			t.Fatalf("parseDevModeToken returned no error but empty uid for %q", in)
		}
	})
}
