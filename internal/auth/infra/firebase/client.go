// Package firebase provides the auth module's Firebase ID-token verifier.
//
// Three implementations of outbound.FirebaseClient are shipped here:
//
//   - NotConfiguredClient: default fallback; every VerifyIDToken call
//     returns apperror.Unauthorized("firebase_not_configured").
//   - DevModeClient: development-only; accepts tokens shaped
//     "dev:<uid>[:<email>]". Refuses to be constructed outside
//     APP_ENV=development.
//   - RealClient: production verifier wrapping the Firebase Admin SDK
//     (firebase.google.com/go/v4). Initialized at boot when
//     FIREBASE_PROJECT_ID is set. See docs/adr/0004-firebase-real-client.md.
//
// Selection happens in the factory at boot. The config layer (see
// internal/platform/config) gates which selection is legal.
package firebase
