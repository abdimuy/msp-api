// Package domain contains read-only projections of native Microsip tables.
// The clientes module owns no database tables and never writes to Microsip;
// Microsip is the authoritative source of truth. Entities here are hydrated
// by the repository layer and surfaced as a Customer 360 view.
//
//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import (
	"github.com/shopspring/decimal"
)

// Cliente is a read-only identity projection built from the native Microsip
// tables CLIENTES, DIRS_CLIENTES, ZONAS_CLIENTES, and COBRADORES. The module
// never creates or mutates clientes — Microsip is the owner.
//
// Deliberately deviates from the Type A/B/C entity standards:
//   - No audit embed (we own no timestamps — Microsip does).
//   - No Crear constructor and no domain events (read-only projection).
//   - HydrateCliente is the sole constructor; used exclusively by the repository.
type Cliente struct {
	clienteID      int
	nombre         string
	limiteCredito  decimal.Decimal
	notas          string
	estatus        string
	zonaClienteID  int
	zonaNombre     string
	cobradorID     int
	cobradorNombre string
	// address primitives (a Direccion value object is out of scope here — Task B2)
	calle     string
	colonia   string
	poblacion string
	estado    string
	telefono  string
}

// HydrateClienteParams holds all fields needed to reconstruct a Cliente from
// persisted Microsip rows. Used exclusively by the repository layer.
type HydrateClienteParams struct {
	ClienteID      int
	Nombre         string
	LimiteCredito  decimal.Decimal
	Notas          string
	Estatus        string
	ZonaClienteID  int
	ZonaNombre     string
	CobradorID     int
	CobradorNombre string
	Calle          string
	Colonia        string
	Poblacion      string
	Estado         string
	Telefono       string
}

// HydrateCliente reconstructs a Cliente from Microsip persistence with zero
// validation. Called only from the repository layer.
func HydrateCliente(p HydrateClienteParams) *Cliente {
	return &Cliente{
		clienteID:      p.ClienteID,
		nombre:         p.Nombre,
		limiteCredito:  p.LimiteCredito,
		notas:          p.Notas,
		estatus:        p.Estatus,
		zonaClienteID:  p.ZonaClienteID,
		zonaNombre:     p.ZonaNombre,
		cobradorID:     p.CobradorID,
		cobradorNombre: p.CobradorNombre,
		calle:          p.Calle,
		colonia:        p.Colonia,
		poblacion:      p.Poblacion,
		estado:         p.Estado,
		telefono:       p.Telefono,
	}
}

// ─── Getters ──────────────────────────────────────────────────────────────────

// ClienteID returns the Microsip primary key for the cliente.
func (c *Cliente) ClienteID() int { return c.clienteID }

// Nombre returns the client's full name.
func (c *Cliente) Nombre() string { return c.nombre }

// LimiteCredito returns the approved credit limit for this client.
func (c *Cliente) LimiteCredito() decimal.Decimal { return c.limiteCredito }

// Notas returns any notes associated with this client in Microsip.
func (c *Cliente) Notas() string { return c.notas }

// Estatus returns the Microsip status code (e.g. "A" = activo).
func (c *Cliente) Estatus() string { return c.estatus }

// ZonaClienteID returns the numeric ID of the sales zone this client belongs to.
func (c *Cliente) ZonaClienteID() int { return c.zonaClienteID }

// ZonaNombre returns the display name of the sales zone.
func (c *Cliente) ZonaNombre() string { return c.zonaNombre }

// CobradorID returns the numeric ID of the cobrador assigned to this client.
func (c *Cliente) CobradorID() int { return c.cobradorID }

// CobradorNombre returns the display name of the assigned cobrador.
func (c *Cliente) CobradorNombre() string { return c.cobradorNombre }

// Calle returns the street address component.
func (c *Cliente) Calle() string { return c.calle }

// Colonia returns the neighborhood (colonia) component of the address.
func (c *Cliente) Colonia() string { return c.colonia }

// Poblacion returns the city/town component of the address.
func (c *Cliente) Poblacion() string { return c.poblacion }

// Estado returns the state (e.g. "Jalisco") component of the address.
func (c *Cliente) Estado() string { return c.estado }

// Telefono returns the primary phone number for this client.
func (c *Cliente) Telefono() string { return c.telefono }
