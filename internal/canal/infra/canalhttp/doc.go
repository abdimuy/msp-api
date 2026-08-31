// Package canalhttp is the canal module's HTTP surface: the security
// boundary of the whole canal-whatsapp plan.
//
// Three kinds of traffic terminate here, each with its own authentication:
//
//   - Meta's webhook (GET/POST /canal/v1/webhook/whatsapp), mounted on raw
//     chi rather than Huma — see the "Endpoints binarios" escape hatch in
//     docs/module-standards/08-handlers-routes.md. The GET answers the
//     subscription challenge; the POST delivers inbound WhatsApp messages,
//     authenticated by an HMAC-SHA256 signature over the exact request
//     bytes (X-Hub-Signature-256). Nothing may decode and re-encode the
//     body before the signature check — see verifySignature's doc comment.
//   - The internal API (Huma, over the same chi router): POST
//     /canal/v1/salientes sends one outbound WhatsApp message on the
//     store's behalf, authenticated by a shared token; GET /canal/v1/salud
//     reports liveness and the mailbox backlog, deliberately unauthenticated
//     — see the doc comment on registerSalud for why.
//   - ForwarderClient, the outbound.Forwarder implementation that pushes
//     mailboxed entrantes to the store's on-premise server, authenticated by
//     the same shared token from the other side.
//
// canal is a sealed module (ADR-0009): this package imports only the
// standard library, uuid, decimal, internal/canal/… and internal/platform/…
// — never internal/auth, never Firebase. Meta authenticates by signature;
// the store authenticates by shared token; no MSP user's permissions are
// ever checked here. mapAppError is copied (not imported) from
// internal/reactivacion/infra/reactivacionhttp/auth.go for exactly that
// reason.
package canalhttp
