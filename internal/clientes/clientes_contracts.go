// Package clientes is the cross-module surface of the clientes bounded context.
// Other modules import only this package — never internal/clientes/domain,
// internal/clientes/app, or internal/clientes/infra. The depguard linter
// enforces the rule.
//
// In Phase 1 the clientes module exposes no Go cross-module contracts because
// no other Go module currently consumes it — its public surface is the HTTP API
// (Step 08). Contract types will be added here when a consumer (e.g. a future
// winback-as-lens integration) requires them.
//
//nolint:misspell // Spanish domain vocabulary (clientes, etc.) by project convention.
package clientes
