package outbound

import (
	"context"
)

// DatosCliente is the client half of a receipt.
//
// Telefono may be empty: a client with no usable phone is a normal case, and
// the delivery is then created directly in sin_telefono. Do not model this as
// an error.
type DatosCliente struct {
	Nombre    string
	Telefono  string
	Domicilio string
}

// ClienteReader reads the client behind a receipt, through the clientes
// module's contracts package.
type ClienteReader interface {
	Leer(ctx context.Context, clienteID int) (DatosCliente, error)
}
