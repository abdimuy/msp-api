package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

// TestAppGraph_IsValid checks that every dependency in the fx graph can be
// resolved: each provider's parameters are satisfied and nothing is missing or
// ambiguous.
//
// This is cheap insurance against a whole class of mistake that is otherwise
// invisible until boot: adding a provider function without registering it (or
// registering one whose dependency nobody provides) compiles fine and fails
// only when the binary starts — and `fx.NopLogger` has hidden exactly those
// boot errors in production before.
//
// fx.ValidateApp builds the graph WITHOUT invoking constructors, so no
// Firebird connection, Firebase client, or filesystem access happens here.
func TestAppGraph_IsValid(t *testing.T) {
	t.Parallel()

	require.NoError(t, fx.ValidateApp(appOptions()...), "the fx dependency graph must resolve")
}
