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
