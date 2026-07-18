package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
)

func intPtr(v int) *int { return &v }

func TestListarVendedores_OrdenaPorNombreYCalculaEstado(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, usuarios := newTestService()

	rosa := uuid.New()   // 3/3
	daniel := uuid.New() // 2/3
	brenda := uuid.New() // sin asignar (sin mapping row)

	usuarios.usuarios = []configdomain.AppUsuario{
		{ID: rosa, Nombre: "Rosa Martínez", Email: "rosa.martinez@muebleriamsp.mx", Estatus: "activo"},
		{ID: daniel, Nombre: "Daniel Hernández", Email: "daniel.hernandez@muebleriamsp.mx", Estatus: "activo"},
		{ID: brenda, Nombre: "Brenda López", Email: "brenda.lopez@muebleriamsp.mx", Estatus: "activo"},
	}

	repo.mappings[rosa] = configdomain.VendedorMapping{
		UsuarioID: rosa, ListaID1: intPtr(101), ListaID2: intPtr(201), ListaID3: intPtr(301),
	}
	repo.mappings[daniel] = configdomain.VendedorMapping{
		UsuarioID: daniel, ListaID1: intPtr(102), ListaID2: intPtr(202), ListaID3: nil,
	}

	catalogo.nombres = map[int]string{
		101: "Rosa Martínez", 201: "Rosa Martínez", 301: "Rosa Martínez",
		102: "Daniel Hernández", 202: "Daniel Hernández",
	}

	result, err := svc.ListarVendedores(context.Background())
	require.NoError(t, err)
	require.Len(t, result, 3)

	// Ordered by Nombre: Brenda, Daniel, Rosa.
	assert.Equal(t, "Brenda López", result[0].Nombre)
	assert.Equal(t, "sin asignar", result[0].Estado)
	assert.Nil(t, result[0].V1)
	assert.Nil(t, result[0].V2)
	assert.Nil(t, result[0].V3)

	assert.Equal(t, "Daniel Hernández", result[1].Nombre)
	assert.Equal(t, "2/3", result[1].Estado)
	require.NotNil(t, result[1].V1)
	require.NotNil(t, result[1].V2)
	assert.Nil(t, result[1].V3)
	assert.Equal(t, "Daniel Hernández", result[1].V1.Nombre)

	assert.Equal(t, "Rosa Martínez", result[2].Nombre)
	assert.Equal(t, "3/3", result[2].Estado)
	require.NotNil(t, result[2].V1)
	require.NotNil(t, result[2].V2)
	require.NotNil(t, result[2].V3)
}

func TestListarVendedores_ListaIDSinNombreResuelto_MantieneSlotConDesconocido(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, usuarios := newTestService()

	usuarioID := uuid.New()
	usuarios.usuarios = []configdomain.AppUsuario{
		{ID: usuarioID, Nombre: "Aldo Cortés", Email: "aldo.cortes@muebleriamsp.mx"},
	}
	repo.mappings[usuarioID] = configdomain.VendedorMapping{UsuarioID: usuarioID, ListaID1: intPtr(999)}
	catalogo.nombres = map[int]string{} // 999 unresolved

	result, err := svc.ListarVendedores(context.Background())
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].V1)
	assert.Equal(t, 999, result[0].V1.ListaID)
	assert.Equal(t, "(desconocido)", result[0].V1.Nombre)
}

func TestListarVendedores_UsuariosError_Propaga(t *testing.T) {
	t.Parallel()
	svc, _, _, usuarios := newTestService()
	usuarios.err = errors.New("firestore/firebird down")

	_, err := svc.ListarVendedores(context.Background())
	require.Error(t, err)
}

func TestListarVendedores_SinMappings_NoLlamaResolverNombres(t *testing.T) {
	t.Parallel()
	svc, _, catalogo, usuarios := newTestService()
	usuarios.usuarios = []configdomain.AppUsuario{
		{ID: uuid.New(), Nombre: "Sin Mapeo", Email: "sinmapeo@muebleriamsp.mx"},
	}
	catalogo.resolverErr = errors.New("no debería llamarse")

	result, err := svc.ListarVendedores(context.Background())
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "sin asignar", result[0].Estado)
}

func TestListarIdentidadesMicrosip_Passthrough(t *testing.T) {
	t.Parallel()
	svc, _, catalogo, _ := newTestService()
	catalogo.identidades = []configdomain.IdentidadMicrosip{
		{Nombre: "Rosa Martínez", V1ListaID: intPtr(101), V2ListaID: intPtr(201), V3ListaID: intPtr(301), MatchCount: 3},
	}

	result, err := svc.ListarIdentidadesMicrosip(context.Background())
	require.NoError(t, err)
	assert.Equal(t, catalogo.identidades, result)
}
