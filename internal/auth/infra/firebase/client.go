// Package firebase provides the auth module's Firebase ID-token verifier.
//
// Three implementations of outbound.FirebaseClient are shipped here:
//
//   - NotConfiguredClient: default fallback; every VerifyIDToken call
//     returns apperror.Unauthorized("firebase_not_configured").
//   - DevModeClient: development-only; accepts tokens shaped
//     "dev:<uid>[:<email>]". Refuses to be constructed outside
//     APP_ENV=development.
//   - RealClient: deferred until the Firebase Admin SDK credential is
//     available. See docs/adr/0002-firebase-deferred.md.
//
// Selection happens in the factory at boot. The config layer (see
// internal/platform/config) gates which selection is legal.
package firebase
