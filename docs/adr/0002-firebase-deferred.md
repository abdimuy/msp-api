# ADR 0002 — Defer Firebase Admin SDK, ship with stubs

- **Status:** Superseded by [ADR-0004](0004-firebase-real-client.md)
- **Date:** 2026-05-10
- **Decision drivers:** the auth module needs to ship; the Firebase service-account JSON is not yet available; we want to validate the rest of the auth surface without blocking on credentials.

> **Update (2026-05-14):** `RealClient` is now implemented; see [ADR-0004](0004-firebase-real-client.md) for details.

## Context

Authentication in `msp-api` delegates to Firebase: clients hand the API a Firebase ID token, and the API verifies it with the Firebase Admin SDK. The production verifier is `firebase.google.com/go/v4`, which needs a service-account credential file plus network egress to Google's token-introspection endpoint.

At the moment we have:

- A documented auth contract (`internal/auth/ports/outbound/firebase_client.go`).
- A complete handler / middleware / service implementation.
- **No** service-account JSON. The user is requesting one from the GCP project owner.

We want to merge the auth module now so we can build other modules on top, but the real Firebase SDK can't be wired up yet.

## Decision

Implement `FirebaseClient` as a port with three concrete implementations selected by config:

1. **`NotConfiguredClient`** (default). Every `VerifyIDToken` call returns `apperror.NewUnauthorized("firebase_not_configured", "firebase no configurado")`. Equivalent to "auth is permanently off"; any caller hitting an authenticated endpoint gets 401.
2. **`DevModeClient`** (development only). Parses tokens shaped `dev:<uid>[:<email>]` and returns a synthetic `FirebaseToken`. Logs `auth.dev_mode_token_accepted` on every accepted token. Refuses to be constructed when `APP_ENV != "development"`.
3. **`RealClient`** (deferred). Wraps `firebase.google.com/go/v4`. Not implemented in v1.

The factory at `internal/auth/infra/firebase/factory.go` selects the implementation based on env vars, gated at `config.Load` time. The matrix:

| APP_ENV       | FIREBASE_PROJECT_ID | FIREBASE_DEV_MODE | FIREBASE_ALLOW_UNCONFIGURED | Result                             |
|---------------|---------------------|-------------------|-----------------------------|------------------------------------|
| production    | set                 | —                 | —                           | RealClient (v2 — falls back to NotConfigured in v1) |
| production    | unset               | —                 | —                           | **Boot refuses** (see config.validate) |
| development   | —                   | true              | —                           | DevModeClient                      |
| development   | set                 | false             | —                           | RealClient (v2 — falls back to NotConfigured in v1) |
| development   | unset               | false             | true                        | NotConfiguredClient                |
| anything else | unset               | false             | false                       | **Boot refuses**                   |

The Postgres / config rules around `FIREBASE_DEV_MODE=true` (only legal in development) live in [`internal/platform/config/config.go`](../../internal/platform/config/config.go).

## Security guards on DevModeClient

The DevMode client is the riskiest of the three because it's an explicit authentication bypass. The risk surface is:

1. **Leakage to production.** `DevModeClient`'s constructor refuses to instantiate when `APP_ENV != "development"`. `config.validate` refuses the env var combination earlier. Two layers of defense.
2. **Silent acceptance.** Every accepted token logs a structured `auth.dev_mode_token_accepted` warn line with `uid`, `request_id`, `remote_addr`. This is intentional noise — if the DevMode client is enabled in a CI run, the build logs scream about it.
3. **Boot banner.** On startup, when the factory wires DevMode, it logs `auth.dev_mode_enabled` at error level so anyone tailing the log notices.
4. **Token shape.** Only the prefix `dev:` followed by an ASCII identifier passes. Real Firebase tokens (signed JWTs starting with `eyJ`) are rejected — the surface for confusion is small.

## Migration path to real Firebase

When the service-account JSON arrives:

1. Add `firebase.google.com/go/v4` to `go.mod`.
2. Implement `internal/auth/infra/firebase/real.go` against the SDK.
3. Update `factory.go` to instantiate `RealClient` in the cells of the matrix marked "v2 — falls back to NotConfigured in v1".
4. No other code in the codebase changes. The `FirebaseClient` interface stays the same.

Estimated effort: 1 day including a real-token integration test against the Firebase Auth emulator.

## Consequences

- **DX**: developers can log in locally with `curl -H "Authorization: Bearer dev:alice" …` and exercise every authenticated endpoint without Firebase credentials.
- **CI**: integration tests run against DevModeClient — fast, deterministic, no network egress to Google.
- **Production**: production refuses to boot without `FIREBASE_PROJECT_ID`. We cannot accidentally ship the stub.
- **Audit**: the structured `auth.dev_mode_*` log lines give a paper trail when reviewing whether DevMode was ever on outside development.
