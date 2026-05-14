# ADR 0004 — Firebase RealClient: production ID-token verification

- **Status:** Accepted
- **Date:** 2026-05-14
- **Supersedes:** [ADR-0002](0002-firebase-deferred.md) (in part — RealClient was deferred there).

## Context

ADR-0002 shipped the auth module with three implementations of `outbound.FirebaseClient`, leaving `RealClient` as a stub that fell back to `NotConfiguredClient` with a startup WARN. The frontend now needs to authenticate against a real Firebase project. The user has provisioned a service-account JSON for a test Firebase project and wants to enable end-to-end auth.

## Decision

Implement `RealClient` in `internal/auth/infra/firebase/real.go` wrapping `firebase.google.com/go/v4`.

Key design points:

1. **Internal `tokenVerifier` interface**. `RealClient` does not depend on `*auth.Client` directly; it depends on a one-method interface (`VerifyIDToken(ctx, idToken) (*auth.Token, error)`) that the SDK satisfies. Unit tests substitute a fake verifier.
2. **Plain `VerifyIDToken`, no revocation check**. Trade-off in favor of zero extra round-trips. Revoked tokens remain valid until natural expiration (~1h).
3. **Error classification**. SDK errors → `apperror.NewUnauthorized(code, message)` with codes that pinpoint the failure (`firebase_token_expired`, `firebase_token_invalid_signature`, `firebase_token_wrong_audience`, `firebase_token_revoked`, `firebase_token_invalid`). The middleware returns 401 regardless; the code is for logs and metrics.
4. **Fail-fast init**. `NewRealClient` validates that the service-account file exists and that the SDK accepts the credential before returning a client; any failure surfaces as `apperror.NewInternal` and aborts boot.
5. **Two test layers**:
   - Unit tests via `fakeVerifier` (no network, no Docker).
   - Integration test gated by `FIREBASE_AUTH_EMULATOR_HOST` env var, run against the Firebase Auth Emulator in Docker. Skips when the var is unset so the suite stays green on machines without the emulator.

## Out of scope

- Bootstrap of the first admin user (manual step via `./tmp/api auth-bootstrap`).
- Frontend migration to the Firebase Auth client SDK.
- Token-revocation-aware verification (`VerifyIDTokenAndCheckRevoked`). Can be added behind a config flag if a future incident requires it.

## Consequences

- Production refuses to boot if the service-account file is missing or malformed — no silent fallback.
- The Firebase Admin SDK pulls in a heavy set of transitive dependencies (`google.golang.org/api`, gRPC, etc.). Build size grows; runtime memory is unchanged.
- First request after boot pays ~100-300ms to fetch Google's public certificate set; subsequent requests are <1ms (signature math only). Cache TTL is ~6h.
- Dev mode (`FIREBASE_DEV_MODE=true`) continues to short-circuit verification with `DevModeClient`. The frontend team can flip `DEV_MODE=false` and point at Firebase real auth when ready.
