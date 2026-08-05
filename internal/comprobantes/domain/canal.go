package domain

// CanalLocal identifies the test sender that wrote the PDF to disk.
const CanalLocal = "local"

// CanalWhatsappBusiness identifies the official Meta API.
const CanalWhatsappBusiness = "whatsapp_business"

// Canal is a value object wrapping which sender implementation answered when
// the delivery was sent. Only "local" and "whatsapp_business" are valid.
type Canal struct{ value string }

// NewCanal validates and constructs a Canal. Rejects anything else with
// ErrCanalInvalido.
func NewCanal(s string) (Canal, error) {
	if s != CanalLocal && s != CanalWhatsappBusiness {
		return Canal{}, ErrCanalInvalido
	}
	return Canal{value: s}, nil
}

// HydrateCanal rebuilds one from persistence without validation. Intended for
// repository use only.
func HydrateCanal(s string) Canal { return Canal{value: s} }

// Value returns the raw channel string ("local" or "whatsapp_business").
func (c Canal) Value() string { return c.value }

// String returns the channel string representation.
func (c Canal) String() string { return c.value }

// Equals reports whether two Canal values are identical.
func (c Canal) Equals(other Canal) bool { return c.value == other.value }

// IsZero reports whether the Canal has its zero value (empty string).
func (c Canal) IsZero() bool { return c.value == "" }

// EsReal reports whether the delivery went through the production channel.
// True only for whatsapp_business so a test delivery is never counted as a
// real delivery.
func (c Canal) EsReal() bool { return c.value == CanalWhatsappBusiness }
