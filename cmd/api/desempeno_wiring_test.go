//nolint:misspell // vocabulario español por convención del proyecto.
package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	desempenooutbound "github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	rutasapp "github.com/abdimuy/msp-api/internal/rutas/app"
)

// El servicio de desempeño está en el grafo de fx con todas sus dependencias
// (repos Firebird, contrato de rutas y reloj). ValidateApp no invoca
// constructores: no abre Firebird ni Firebase.
func TestDesempenoWiring_ServicioEnElGrafo(t *testing.T) {
	t.Parallel()
	opts := append(appOptions(), fx.Invoke(func(*desempenoapp.Service) {}))
	require.NoError(t, fx.ValidateApp(opts...))
}

// Los adaptadores reales que arma el API leen por lote. Si alguno deja de
// implementar la interfaz por lote (p. ej. un decorador nuevo que no la
// reexpone), el tablero volvería a leer zona por zona —5 sentencias por zona
// en vez de 6 en total— sin que ninguna otra prueba lo note.
func TestDesempenoWiring_TableroPorLote(t *testing.T) {
	t.Parallel()
	d := desempenoDeps(&firebird.Pool{}, rutasapp.NewService(nil, nil, nil))
	_, ok := d.actividades.(desempenooutbound.ActividadesLoteRepo)
	require.True(t, ok, "las actividades del API deben leer por lote")
	_, ok = d.cuentas.(desempenooutbound.CuentasLoteRepo)
	require.True(t, ok, "el universo del API debe leerse por lote")
	_, ok = d.usuarios.(desempenooutbound.CobrosLote)
	require.True(t, ok, "el cobro de rutas del API debe leerse por lote")
}
