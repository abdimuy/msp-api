// Package render turns comprobante models into informational PDF documents.
//
// The sale receipt and the payment receipt share one type, PDFRenderer,
// implementing outbound.Renderer. Both carry the mandatory §6.3 label —
// "comprobante informativo, no es un CFDI" — because a paper with folio, amount
// and a stamp that arrives over WhatsApp reads enough like a fiscal document
// for someone to treat it as one. That label lives in the renderer, not in the
// model: it is presentation, and the model has no presentation.
//
// The PDF is deterministic: SetCreationDate is fixed to the comprobante's own
// timestamp, never time.Now(), so regenerating the same model yields the same
// bytes — this is what makes the content tests stable and the document
// reproducible a year later.
//
// Dates are shown in the business zone (America/Mexico_City), not in the raw
// UTC the model carries, so the printed day is the day a cobrador sees in CDMX
// — the same rule DATETIME_HANDLING.md applies to every user-facing date.
//
//nolint:misspell // Spanish labels are user-facing (comprobante, enganche, etc.).
package render
