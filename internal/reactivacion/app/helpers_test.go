//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app_test

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

// mustSeg parses a segmento string, panicking on an invalid value (test-only).
func mustSeg(raw string) domain.Segmento {
	s, err := domain.ParseSegmento(raw)
	if err != nil {
		panic(err)
	}
	return s
}

// cohorteRow builds a persisted-style CohorteCliente for query/attribution tests.
// ultimaCompra is the (post-cohorte) last purchase used to decide conversion.
func cohorteRow(clienteID int, enControl, contactado bool, cohorteFecha, ultimaCompra time.Time) *domain.CohorteCliente {
	return domain.HydrateCohorteCliente(domain.HydrateCohorteClienteParams{
		ID:                    uuid.New(),
		ClienteID:             clienteID,
		Nombre:                "Cliente Cohorte",
		Telefono:              "238 333 4444",
		Segmento:              domain.SegmentoRecienLiquidado,
		EnControl:             enControl,
		FueContactado:         contactado,
		CohorteFecha:          cohorteFecha,
		FechaUltimaCompraBase: ultimaCompra,
		Saldo:                 decimal.Zero,
		PorLiquidarPct:        decimal.Zero,
		CreatedAt:             cohorteFecha,
		UpdatedAt:             cohorteFecha,
	})
}
