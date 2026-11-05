// Package sender implements the [outbound.Sender] port used by the
// comprobantes module to deliver rendered receipt PDFs.
//
// The only implementation is [LocalSender], the test channel: it writes the
// document and a `.envio.json` sidecar to the local disk instead of calling
// the WhatsApp Business API. It exists because provisioning the WhatsApp
// Business account and approving the template takes weeks, and without it the
// module cannot be tested end to end. It is not disposable — it stays as the
// permanent testing mode and is what anyone uses to verify the flow without
// spending production message credits.
//
// If the real WhatsApp channel is ever implemented, add it as a new
// implementation alongside this one rather than reintroducing a selector
// abstraction.
package sender
