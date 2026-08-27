package auth_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// permConstNames parses a Go source file and returns the names of every
// package-level constant whose identifier starts with "Perm". Reading the AST
// (rather than reflecting over values) is the only way to compare two const
// blocks: untyped constants leave no runtime trace to enumerate.
func permConstNames(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	require.NoError(t, err, "parse %s", path)

	out := map[string]struct{}{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range vs.Names {
				if strings.HasPrefix(name.Name, "Perm") {
					out[name.Name] = struct{}{}
				}
			}
		}
	}
	return out
}

// TestPermissionConstantsAreReExported enforces the rule stated in
// auth_contracts.go: "Adding a new code requires adding a constant here as
// well as in internal/auth/domain/permission_codes.go". Without this test the
// two lists drift silently — a new permission compiles fine inside the auth
// module and only breaks when another module tries to reference it through
// the contracts package.
func TestPermissionConstantsAreReExported(t *testing.T) {
	t.Parallel()

	domainPerms := permConstNames(t, "domain/permission_codes.go")
	contractPerms := permConstNames(t, "auth_contracts.go")

	require.NotEmpty(t, domainPerms, "positive control: the domain file must yield constants")
	require.NotEmpty(t, contractPerms, "positive control: the contracts file must yield constants")

	for name := range domainPerms {
		_, ok := contractPerms[name]
		assert.Truef(t, ok,
			"domain.%s is not re-exported in internal/auth/auth_contracts.go", name)
	}
	for name := range contractPerms {
		_, ok := domainPerms[name]
		assert.Truef(t, ok,
			"auth.%s is re-exported but no longer exists in internal/auth/domain/permission_codes.go", name)
	}
}
