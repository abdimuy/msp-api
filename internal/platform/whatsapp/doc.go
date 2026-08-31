// Package whatsapp provides a generic transport client for Meta's WhatsApp
// Cloud API (Graph API). See docs/adr/0010-whatsapp-cloud-api-and-the-always-on-edge.md
// for why this is Meta's official Cloud API and not whatsmeow.
//
// Two implementations are shipped:
//
//   - disabledClient: safe fallback used when WhatsApp is not configured
//     (WHATSAPP_ENABLED=false, the default). Every call returns
//     ErrWhatsAppDisabled so callers can degrade gracefully.
//   - realClient: production client using raw net/http against Meta's Graph
//     API. Initialized at boot when WHATSAPP_ENABLED=true.
//
// Selection happens in the factory (NewClient) at boot. The config layer
// (see internal/platform/config) gates which selection is legal.
//
// This package is GENERIC: it must not import anything from internal/canal,
// internal/reactivacion, internal/comprobantes, or any other domain module.
// It knows Meta's wire format and nothing else — it exposes a transport, not
// a business capability. The domain lives in the modules that consume this
// client: internal/canal (the sealed VPS-side module), the eventual
// internal/reactivacion/infra/reactivacionsender, and later
// internal/comprobantes.
package whatsapp
