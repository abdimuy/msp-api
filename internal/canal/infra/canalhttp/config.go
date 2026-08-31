package canalhttp

// Config carries the secrets MountRouter checks. All three come from
// internal/platform/config (config.WhatsApp.AppSecret and config.Canal's
// WebhookVerifyToken/SharedToken) — whoever builds this (Task 7's
// composition root) reads them from there. Never hardcode a value here,
// and never log a Config: every field is a credential.
type Config struct {
	// AppSecret is Meta's app secret — the HMAC-SHA256 key that verifies
	// X-Hub-Signature-256 on every POST to the webhook. Empty means the
	// webhook is misconfigured and every POST is rejected (see
	// verifySignature).
	AppSecret string
	// VerifyToken is Meta's hub.verify_token, checked on the GET
	// subscription challenge. Empty means the challenge is always rejected
	// (see handleChallenge).
	VerifyToken string
	// SharedToken authenticates POST /canal/v1/salientes. Empty means that
	// route is always rejected (see sharedTokenMiddleware).
	SharedToken string
}
