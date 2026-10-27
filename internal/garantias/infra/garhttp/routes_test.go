package garhttp

import (
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/app"
)

func TestMountRouter_RechazaConsultasNil(t *testing.T) {
	t.Parallel()

	r := chi.NewRouter()

	require.PanicsWithValue(
		t,
		"garhttp: Consultas es requerida",
		func() {
			MountRouter(r, Deps{})
		},
	)
}

func TestMountRouter_RechazaComandosNil(t *testing.T) {
	t.Parallel()

	r := chi.NewRouter()

	require.PanicsWithValue(
		t,
		"garhttp: Comandos es requerido",
		func() {
			MountRouter(r, Deps{Consultas: app.NewConsultas(app.ConsultasDeps{})})
		},
	)
}
