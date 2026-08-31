package whatsapp

import "github.com/abdimuy/msp-api/internal/platform/config"

// NewClient selects the Client implementation based on config.
//
// Selection matrix:
//
//	Enabled == true   → realClient (raw net/http against Meta's Graph API)
//	Enabled == false  → disabledClient (returns ErrWhatsAppDisabled on every call)
//
// The config layer (config.WhatsApp.validate) ensures Token, PhoneNumberID
// and BusinessAccountID are set when Enabled is true, so newRealClient
// always receives usable credentials here.
func NewClient(cfg config.WhatsApp) Client {
	if cfg.Enabled {
		return newRealClient(cfg)
	}
	return newDisabledClient()
}
