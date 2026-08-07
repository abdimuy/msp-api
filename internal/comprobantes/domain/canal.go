package domain

// Canal enumerates which sender implementation answered when the delivery
// was sent.
type Canal string

// Canal enum values. The string forms match the persisted column values in
// MSP_CM_ENVIO.CANAL.
const (
	// CanalLocal identifies the test sender that wrote the PDF to disk.
	CanalLocal Canal = "local"
	// CanalWhatsappBusiness identifies the official Meta API.
	CanalWhatsappBusiness Canal = "whatsapp_business"
)

// ParseCanal parses a string into a Canal or returns ErrCanalInvalido.
func ParseCanal(s string) (Canal, error) {
	c := Canal(s)
	if !c.IsValid() {
		return "", ErrCanalInvalido
	}
	return c, nil
}

// IsValid reports whether c is a recognized Canal.
func (c Canal) IsValid() bool {
	switch c {
	case CanalLocal, CanalWhatsappBusiness:
		return true
	}
	return false
}

// String returns the canonical string representation.
func (c Canal) String() string { return string(c) }

// EsReal reports whether the delivery went through the production channel.
// True only for whatsapp_business so a test delivery is never counted as a
// real delivery.
func (c Canal) EsReal() bool { return c == CanalWhatsappBusiness }
