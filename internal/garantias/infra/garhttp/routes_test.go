package garhttp

import (
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
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
