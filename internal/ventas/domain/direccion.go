package domain

import "strings"

// Maximum widths mirror the Firebird column widths in MSP_VENTAS.
const (
	maxCalleLength          = 300
	maxNumeroExteriorLength = 20
	maxColoniaLength        = 120
	maxPoblacionLength      = 120
	maxCiudadLength         = 120
)

// Direccion is the structured postal address snapshot stored on every venta.
type Direccion struct {
	calle          string
	numeroExterior *string
	colonia        string
	poblacion      string
	ciudad         string
	zonaClienteID  *int
}

// NewDireccionParams carries the inputs to NewDireccion.
type NewDireccionParams struct {
	Calle          string
	NumeroExterior *string
	Colonia        string
	Poblacion      string
	Ciudad         string
	ZonaClienteID  *int
}

// NewDireccion validates and constructs a Direccion. String fields are
// trimmed before validation.
func NewDireccion(p NewDireccionParams) (Direccion, error) {
	calle, err := requireBounded(p.Calle, maxCalleLength, ErrCalleRequerida, ErrCalleDemasiadoLarga)
	if err != nil {
		return Direccion{}, err
	}
	colonia, err := requireBounded(p.Colonia, maxColoniaLength, ErrColoniaRequerida, ErrColoniaDemasiadoLarga)
	if err != nil {
		return Direccion{}, err
	}
	poblacion, err := requireBounded(p.Poblacion, maxPoblacionLength, ErrPoblacionRequerida, ErrPoblacionDemasiadoLarga)
	if err != nil {
		return Direccion{}, err
	}
	ciudad, err := requireBounded(p.Ciudad, maxCiudadLength, ErrCiudadRequerida, ErrCiudadDemasiadoLarga)
	if err != nil {
		return Direccion{}, err
	}
	numExt, err := trimOptionalBounded(p.NumeroExterior, maxNumeroExteriorLength, ErrNumeroExteriorDemasiadoLargo)
	if err != nil {
		return Direccion{}, err
	}
	return Direccion{
		calle:          calle,
		numeroExterior: numExt,
		colonia:        colonia,
		poblacion:      poblacion,
		ciudad:         ciudad,
		zonaClienteID:  p.ZonaClienteID,
	}, nil
}

// HydrateDireccion rebuilds a Direccion from persistence without validation.
func HydrateDireccion(p NewDireccionParams) Direccion {
	return Direccion{
		calle:          p.Calle,
		numeroExterior: p.NumeroExterior,
		colonia:        p.Colonia,
		poblacion:      p.Poblacion,
		ciudad:         p.Ciudad,
		zonaClienteID:  p.ZonaClienteID,
	}
}

// Calle returns the street name.
func (d Direccion) Calle() string { return d.calle }

// NumeroExterior returns the optional exterior number.
func (d Direccion) NumeroExterior() *string { return d.numeroExterior }

// Colonia returns the colonia/neighborhood.
func (d Direccion) Colonia() string { return d.colonia }

// Poblacion returns the población name.
func (d Direccion) Poblacion() string { return d.poblacion }

// Ciudad returns the ciudad/city name.
func (d Direccion) Ciudad() string { return d.ciudad }

// ZonaClienteID returns the cliente zone identifier when set.
func (d Direccion) ZonaClienteID() *int { return d.zonaClienteID }

// requireBounded trims s, rejects empty, rejects strings longer than max,
// and rejects strings carrying unsafe characters (NUL or non-WIN1252 runes).
func requireBounded(s string, maxLen int, errRequired, errTooLong error) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errRequired
	}
	if len(s) > maxLen {
		return "", errTooLong
	}
	if err := validateSafeChars(s); err != nil {
		return "", err
	}
	return s, nil
}

// trimOptionalBounded trims an optional pointer string. A nil input or a
// pointer to an all-whitespace string both yield nil output. Non-blank
// inputs are also screened for unsafe characters.
func trimOptionalBounded(p *string, maxLen int, errTooLong error) (*string, error) {
	if p == nil {
		return nil, nil //nolint:nilnil // optional pointer pattern: nil ptr + nil err means "not provided".
	}
	trimmed := strings.TrimSpace(*p)
	if trimmed == "" {
		return nil, nil //nolint:nilnil // optional pointer pattern: blank input normalizes to "not provided".
	}
	if len(trimmed) > maxLen {
		return nil, errTooLong
	}
	if err := validateSafeChars(trimmed); err != nil {
		return nil, err
	}
	return &trimmed, nil
}
