package clients_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/config/infra/clients"
)

type fakeUsuariosLister struct {
	resumenes []auth.UsuarioResumen
	err       error
}

func (f fakeUsuariosLister) ListarUsuarios(_ context.Context) ([]auth.UsuarioResumen, error) {
	return f.resumenes, f.err
}

func TestAuthUsuariosClient_ListarUsuarios_MapeaResumenAAppUsuario(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	lister := fakeUsuariosLister{resumenes: []auth.UsuarioResumen{
		{ID: id, Nombre: "Daniel Hernández", Email: "daniel.hernandez@muebleriamsp.mx", Estatus: "activo"},
	}}
	client := clients.NewAuthUsuariosClient(lister)

	got, err := client.ListarUsuarios(context.Background())

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, id, got[0].ID)
	assert.Equal(t, "Daniel Hernández", got[0].Nombre)
	assert.Equal(t, "daniel.hernandez@muebleriamsp.mx", got[0].Email)
	assert.Equal(t, "activo", got[0].Estatus)
}

func TestAuthUsuariosClient_ListarUsuarios_PropagaError(t *testing.T) {
	t.Parallel()
	lister := fakeUsuariosLister{err: errors.New("boom")}
	client := clients.NewAuthUsuariosClient(lister)

	_, err := client.ListarUsuarios(context.Background())

	require.Error(t, err)
}
