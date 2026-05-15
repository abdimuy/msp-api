package auth_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// approvedPlantCurrentUserCallers is the closed set of non-test
// production sites that may call auth.PlantCurrentUser. Each entry is a
// repo-relative path; adding a new caller is a security-sensitive change
// — see the godoc on PlantCurrentUser before extending this list.
//
// Test files (*_test.go) are exempt from this check and not listed here.
var approvedPlantCurrentUserCallers = []string{
	"internal/auth/infra/authhttp/authn.go",
	"internal/platform/failedintent/http/handlers.go",
}

// TestPlantCurrentUser_TrustedCallers enforces the trust-boundary
// documented on auth.PlantCurrentUser by walking every non-test .go file
// under internal/ and asserting that the only files invoking the
// function are in the approved list. A new caller — even an innocuous
// one — fails this test until it is reviewed and explicitly added.
//
// The check is a pure AST walk; it does not execute code and is cheap
// enough to run in the standard unit-test suite.
func TestPlantCurrentUser_TrustedCallers(t *testing.T) {
	t.Parallel()

	repoRoot := findRepoRoot(t)
	internalDir := filepath.Join(repoRoot, "internal")

	approved := make(map[string]struct{}, len(approvedPlantCurrentUserCallers))
	for _, p := range approvedPlantCurrentUserCallers {
		approved[filepath.FromSlash(p)] = struct{}{}
	}

	var unexpected []string
	fset := token.NewFileSet()

	err := filepath.WalkDir(internalDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		f, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}

		if !fileCallsPlantCurrentUser(f) {
			return nil
		}

		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			t.Fatalf("rel %s: %v", path, relErr)
		}
		if _, ok := approved[rel]; !ok {
			unexpected = append(unexpected, rel)
		}
		return nil
	})
	require.NoError(t, err)

	sort.Strings(unexpected)
	if len(unexpected) > 0 {
		t.Fatalf(
			"unauthorised callers of auth.PlantCurrentUser:\n  %s\n\n"+
				"PlantCurrentUser is a trust boundary — adding a caller is "+
				"security-sensitive. If the new caller is intentional, add "+
				"it to approvedPlantCurrentUserCallers in this file along "+
				"with a code-review justification.",
			strings.Join(unexpected, "\n  "),
		)
	}
}

// fileCallsPlantCurrentUser reports whether f contains any call to a
// function selector ending in `.PlantCurrentUser` or a bare identifier
// `PlantCurrentUser`. The check is intentionally permissive (matches
// shadowed aliases too) so a new caller cannot evade the whitelist with
// an import rename.
func fileCallsPlantCurrentUser(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if fn.Sel.Name == "PlantCurrentUser" {
				found = true
				return false
			}
		case *ast.Ident:
			if fn.Name == "PlantCurrentUser" {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// findRepoRoot walks upward from the test's working directory until it
// finds a go.mod, returning that directory.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	require.NoError(t, err)

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		} else if !os.IsNotExist(statErr) {
			t.Fatalf("stat go.mod at %s: %v", dir, statErr)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate go.mod walking up from test dir")
		}
		dir = parent
	}
}

// _ keeps the io/fs import in scope (used by filepath.WalkDir's signature).
var _ fs.DirEntry = (fs.DirEntry)(nil)
