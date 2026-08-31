package canalhttp_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	canalapp "github.com/abdimuy/msp-api/internal/canal/app"
	"github.com/abdimuy/msp-api/internal/canal/infra/canalhttp"
)

const (
	testAppSecret   = "meta-app-secret"
	testVerifyToken = "meta-verify-token"
	testSharedToken = "internal-shared-token"
)

// testDeps groups everything newTestRouter built, for tests that need to
// reach into a fake after making a request.
type testDeps struct {
	repo *buzonRepoFake
	wa   *waFake
}

// newTestRouter builds a chi.Router carrying canalhttp's full mounted
// surface — webhook, salientes, salud — wired against fresh in-memory
// fakes and testAppSecret/testVerifyToken/testSharedToken. Every test in
// this package exercises the real router via httptest, never a handler
// called in isolation.
func newTestRouter(t *testing.T) (chi.Router, testDeps) {
	t.Helper()

	repo := newBuzonRepoFake()
	clock := newFixedClock(time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC))
	wa := newWAFake()
	svc := canalapp.NewService(repo, forwarderFake{}, clock, nil, canalapp.ReenvioConfig{}, nil)

	r := chi.NewRouter()
	canalhttp.MountRouter(r, canalhttp.Deps{
		Svc:        svc,
		Clock:      clock,
		Pendientes: repo,
		WA:         wa,
		Cfg: canalhttp.Config{
			AppSecret:   testAppSecret,
			VerifyToken: testVerifyToken,
			SharedToken: testSharedToken,
		},
	})
	return r, testDeps{repo: repo, wa: wa}
}

// signBody computes Meta's own "sha256=<hex>" HMAC-SHA256 signature of body
// under secret, exactly as verifySignature expects it.
func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body) // hash.Hash.Write never returns a non-nil error.
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// metaTextPayload builds a minimal, realistic Meta WhatsApp webhook POST
// body carrying one inbound text message.
func metaTextPayload(wamid, from, phoneNumberID, texto string, ts time.Time) []byte {
	return []byte(fmt.Sprintf(`{
		"object": "whatsapp_business_account",
		"entry": [{
			"id": "waba-1",
			"changes": [{
				"field": "messages",
				"value": {
					"messaging_product": "whatsapp",
					"metadata": {"display_phone_number": "5215500000000", "phone_number_id": %q},
					"messages": [{
						"from": %q,
						"id": %q,
						"timestamp": %q,
						"type": "text",
						"text": {"body": %q}
					}]
				}
			}]
		}]
	}`, phoneNumberID, from, wamid, strconv.FormatInt(ts.Unix(), 10), texto))
}
