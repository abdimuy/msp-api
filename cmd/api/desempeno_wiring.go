//nolint:misspell // vocabulario español por convención del proyecto.
package main

import (
	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	desempenoclients "github.com/abdimuy/msp-api/internal/desempeno/infra/clients"
	desempenofb "github.com/abdimuy/msp-api/internal/desempeno/infra/desempenofb"
	desempenooutbound "github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	rutasapp "github.com/abdimuy/msp-api/internal/rutas/app"
)

// provideDesempenoService arma el módulo de desempeño de cobradores (solo
// lectura). Los usuarios y la cobranza semanal llegan por el contrato de rutas
// (rutas.CobranzaUsuariosReader, que satisface *rutasapp.Service). Ese
// servicio lee Firestore con el cliente que arma provideCalendarioCobradorClient:
// el real cuando hay Firestore configurado (FIREBASE_DEV_MODE=false y
// FIREBASE_PROJECT_ID), y el noop en modo dev, con el que el tablero responde
// 503 desempeno_firestore_unavailable en vez de inventar zonas. La zona de
// negocio es la misma de la frontera Firebird (America/Mexico_City).
func provideDesempenoService(pool *firebird.Pool, rutasSvc *rutasapp.Service) *desempenoapp.Service {
	d := desempenoDeps(pool, rutasSvc)
	return desempenoapp.NewService(d.usuarios, d.actividades, d.cuentas,
		desempenooutbound.ProductionClock{}, firebird.BusinessTZ())
}

// dependenciasDesempeno son los adaptadores reales del módulo. Están aparte
// para que TestDesempenoWiring_TableroPorLote pueda exigir que cada uno lea por
// lote: un decorador que no reexponga la interfaz por lote degradaría el
// tablero, en silencio, a leer zona por zona (E-INF-014).
type dependenciasDesempeno struct {
	usuarios    desempenooutbound.UsuariosCobranza
	actividades desempenooutbound.ActividadesRepo
	cuentas     desempenooutbound.CuentasRepo
}

func desempenoDeps(pool *firebird.Pool, rutasSvc *rutasapp.Service) dependenciasDesempeno {
	return dependenciasDesempeno{
		usuarios:    desempenoclients.NewRutasUsuariosClient(rutasSvc),
		actividades: desempenofb.NewActividadesRepo(pool),
		cuentas:     desempenofb.NewCuentasRepo(pool),
	}
}
