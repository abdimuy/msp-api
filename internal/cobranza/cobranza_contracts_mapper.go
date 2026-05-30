package cobranza

import "github.com/abdimuy/msp-api/internal/cobranza/domain"

// ToContract projects a domain Saldo into the cross-module Saldo view. The
// conversion is pure and allocates no shared state between domain and contract
// types.
func ToContract(s domain.Saldo) Saldo {
	return Saldo{
		DoctoCCID:      s.DoctoCCID(),
		DoctoPVID:      s.DoctoPVID(),
		ClienteID:      s.ClienteID(),
		ZonaClienteID:  s.ZonaClienteID(),
		Folio:          s.Folio(),
		FechaCargo:     s.FechaCargo(),
		PrecioTotal:    s.PrecioTotal(),
		TotalImporte:   s.TotalImporte(),
		ImpteRest:      s.ImpteRest(),
		Saldo:          s.Saldo(),
		NumPagos:       s.NumPagos(),
		FechaUltPago:   s.FechaUltPago(),
		CargoCancelado: s.CargoCancelado(),
		UpdatedAt:      s.UpdatedAt(),
	}
}

// ResumenToContract projects a domain ResumenZona into the cross-module
// ResumenZona view.
func ResumenToContract(r domain.ResumenZona) ResumenZona {
	return ResumenZona{
		ZonaID:      r.ZonaID(),
		TotalVentas: r.TotalVentas(),
		SaldoTotal:  r.SaldoTotal(),
	}
}

// PagoToContract projects a domain Pago into the cross-module Pago view.
func PagoToContract(p domain.Pago) Pago {
	return Pago{
		ImpteDoctoCCID: p.ImpteDoctoCCID(),
		DoctoCCID:      p.DoctoCCID(),
		DoctoCCAcrID:   p.DoctoCCAcrID(),
		ClienteID:      p.ClienteID(),
		ZonaClienteID:  p.ZonaClienteID(),
		Folio:          p.Folio(),
		ConceptoCCID:   p.ConceptoCCID(),
		Fecha:          p.Fecha(),
		Importe:        p.Importe(),
		Impuesto:       p.Impuesto(),
		Lat:            p.Lat(),
		Lon:            p.Lon(),
		Cancelado:      p.Cancelado(),
		Aplicado:       p.Aplicado(),
		UpdatedAt:      p.UpdatedAt(),
	}
}
