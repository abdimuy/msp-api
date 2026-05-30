package domain

import "github.com/shopspring/decimal"

// ResumenZona is a read-only aggregate view of all open saldos grouped by
// zona. Values are computed by the repo query — this type only projects the
// result into Go.
type ResumenZona struct {
	zonaClienteID int
	totalVentas   int
	saldoTotal    decimal.Decimal
}

// HydrateResumenZona builds a ResumenZona from repo-computed values.
func HydrateResumenZona(zonaID, totalVentas int, saldoTotal decimal.Decimal) ResumenZona {
	return ResumenZona{
		zonaClienteID: zonaID,
		totalVentas:   totalVentas,
		saldoTotal:    saldoTotal,
	}
}

// ZonaID returns the zona's primary key.
func (r ResumenZona) ZonaID() int { return r.zonaClienteID }

// TotalVentas returns the count of open cargo rows in this zona.
func (r ResumenZona) TotalVentas() int { return r.totalVentas }

// SaldoTotal returns the sum of outstanding balances across all cargos in
// this zona.
func (r ResumenZona) SaldoTotal() decimal.Decimal { return r.saldoTotal }
