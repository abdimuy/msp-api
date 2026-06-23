//nolint:misspell // rutas vocabulary is Spanish per project convention.
package main

import (
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	rutasapp "github.com/abdimuy/msp-api/internal/rutas/app"
	rutasfb "github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

// provideRutasRepo builds the Firebird-backed RutasRepo for the rutas module.
// Reads ZONAS_CLIENTES, COBRADORES, CLIENTES, MSP_CFG_ZONA_CAJA, and
// MSP_SALDOS_VENTAS; no tables are written.
func provideRutasRepo(pool *firebird.Pool) *rutasfb.RutasRepo {
	return rutasfb.NewRutasRepo(pool)
}

// provideRutasService assembles the rutas read-only query service.
// CobranzaRepo and CalendarioCobradorClient are stubbed with nil until
// their concrete implementations are wired in a later task.
func provideRutasService(repo *rutasfb.RutasRepo) *rutasapp.Service {
	return rutasapp.NewService(repo, outbound.CobranzaRepo(nil), outbound.CalendarioCobradorClient(nil))
}
