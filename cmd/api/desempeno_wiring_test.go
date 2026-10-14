//nolint:misspell // vocabulario español por convención del proyecto.
package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
)

// El servicio de desempeño está en el grafo de fx con todas sus dependencias
// (repos Firebird, contrato de rutas y reloj). ValidateApp no invoca
// constructores: no abre Firebird ni Firebase.
func TestDesempenoWiring_ServicioEnElGrafo(t *testing.T) {
	t.Parallel()
	opts := append(appOptions(), fx.Invoke(func(*desempenoapp.Service) {}))
	require.NoError(t, fx.ValidateApp(opts...))
}
