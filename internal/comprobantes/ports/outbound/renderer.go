//nolint:misspell // quotes the mandatory Spanish label of spec §6.3 verbatim.
package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
)

// Renderer turns a receipt model into the PDF bytes to store and send.
//
// Both documents carry the mandatory label of spec §6.3 — "comprobante
// informativo, no es un CFDI" — and that belongs to the renderer, not to the
// model: it is presentation, and the model has no presentation.
type Renderer interface {
	Venta(ctx context.Context, c domain.ComprobanteVenta) ([]byte, error)
	Pago(ctx context.Context, c domain.ComprobantePago) ([]byte, error)
}
