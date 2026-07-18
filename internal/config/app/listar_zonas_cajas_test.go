package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
)

func TestListarZonasCajas_ZonaConMapeoCompleto_ResuelveTodosLosRefs(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()

	catalogo.zonas = []configdomain.CatalogoRef{{ID: 12271, Nombre: "R/01"}}
	catalogo.cajas = []configdomain.CatalogoRef{{ID: 12151, Nombre: "CAJA1"}}
	catalogo.cajeros = []configdomain.CatalogoRef{{ID: 22368, Nombre: "RUTA01"}}
	catalogo.vendedoresCat = []configdomain.CatalogoRef{{ID: 88240, Nombre: "RUTA01"}}
	catalogo.cobradores = []configdomain.CatalogoRef{{ID: 11294, Nombre: "RUTA 01 - JUAN CARLOS CASTRO"}}
	repo.zonaCajaConfigs[12271] = configdomain.ZonaCajaConfig{
		ZonaClienteID: 12271, CajaID: 12151, CajeroID: 22368, VendedorID: 88240, CobradorID: 11294,
	}

	result, err := svc.ListarZonasCajas(context.Background())
	require.NoError(t, err)
	require.Len(t, result, 1)

	asig := result[0]
	assert.Equal(t, 12271, asig.ZonaClienteID)
	assert.Equal(t, "R/01", asig.ZonaNombre)
	require.NotNil(t, asig.Caja)
	assert.Equal(t, "CAJA1", asig.Caja.Nombre)
	require.NotNil(t, asig.Cajero)
	assert.Equal(t, "RUTA01", asig.Cajero.Nombre)
	require.NotNil(t, asig.Vendedor)
	assert.Equal(t, "RUTA01", asig.Vendedor.Nombre)
	require.NotNil(t, asig.Cobrador)
	assert.Equal(t, "RUTA 01 - JUAN CARLOS CASTRO", asig.Cobrador.Nombre)
}

func TestListarZonasCajas_SlotSentinela_ResultaEnRefNil(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()

	catalogo.zonas = []configdomain.CatalogoRef{{ID: 99999, Nombre: "MAYOREO"}}
	repo.zonaCajaConfigs[99999] = configdomain.ZonaCajaConfig{
		ZonaClienteID: 99999, CajaID: -1, CajeroID: -1, VendedorID: -1, CobradorID: -1,
	}

	result, err := svc.ListarZonasCajas(context.Background())
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Nil(t, result[0].Caja)
	assert.Nil(t, result[0].Cajero)
	assert.Nil(t, result[0].Vendedor)
	assert.Nil(t, result[0].Cobrador)
}

func TestListarZonasCajas_ZonaSinFilaDeConfig_TodosLosSlotsNil(t *testing.T) {
	t.Parallel()
	svc, _, catalogo, _ := newTestService()
	catalogo.zonas = []configdomain.CatalogoRef{{ID: 12271, Nombre: "R/01"}}

	result, err := svc.ListarZonasCajas(context.Background())
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, 12271, result[0].ZonaClienteID)
	assert.Nil(t, result[0].Caja)
	assert.Nil(t, result[0].Cajero)
	assert.Nil(t, result[0].Vendedor)
	assert.Nil(t, result[0].Cobrador)
}

func TestListarZonasCajas_OrdenaPorZonaNombre(t *testing.T) {
	t.Parallel()
	svc, _, catalogo, _ := newTestService()
	catalogo.zonas = []configdomain.CatalogoRef{
		{ID: 3, Nombre: "R/03"},
		{ID: 1, Nombre: "R/01"},
		{ID: 2, Nombre: "R/02"},
	}

	result, err := svc.ListarZonasCajas(context.Background())
	require.NoError(t, err)
	require.Len(t, result, 3)
	assert.Equal(t, "R/01", result[0].ZonaNombre)
	assert.Equal(t, "R/02", result[1].ZonaNombre)
	assert.Equal(t, "R/03", result[2].ZonaNombre)
}

func TestListarZonasCajas_ResuelveCatalogosUnaSolaVez(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.zonas = []configdomain.CatalogoRef{
		{ID: 1, Nombre: "R/01"}, {ID: 2, Nombre: "R/02"}, {ID: 3, Nombre: "R/03"},
	}
	catalogo.cajas = []configdomain.CatalogoRef{{ID: 100, Nombre: "CAJA1"}}
	catalogo.cajeros = []configdomain.CatalogoRef{{ID: 200, Nombre: "CAJERO1"}}
	catalogo.vendedoresCat = []configdomain.CatalogoRef{{ID: 300, Nombre: "VEND1"}}
	catalogo.cobradores = []configdomain.CatalogoRef{{ID: 400, Nombre: "COB1"}}
	for _, id := range []int{1, 2, 3} {
		repo.zonaCajaConfigs[id] = configdomain.ZonaCajaConfig{
			ZonaClienteID: id, CajaID: 100, CajeroID: 200, VendedorID: 300, CobradorID: 400,
		}
	}

	_, err := svc.ListarZonasCajas(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, catalogo.listarZonasCall)
	assert.Equal(t, 1, catalogo.listarCajasCall)
	assert.Equal(t, 1, catalogo.listarCajerosCall)
	assert.Equal(t, 1, catalogo.listarVendedoresCall)
	assert.Equal(t, 1, catalogo.listarCobradoresCall)
}

func TestListarZonasCajas_IDSinNombreResuelto_MantieneRefConDesconocido(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.zonas = []configdomain.CatalogoRef{{ID: 1, Nombre: "R/01"}}
	repo.zonaCajaConfigs[1] = configdomain.ZonaCajaConfig{
		ZonaClienteID: 1, CajaID: 999, CajeroID: -1, VendedorID: -1, CobradorID: -1,
	}
	// caja 999 not present in catalogo.cajas — unresolved.

	result, err := svc.ListarZonasCajas(context.Background())
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].Caja)
	assert.Equal(t, 999, result[0].Caja.ID)
	assert.Equal(t, "(desconocido)", result[0].Caja.Nombre)
}

func TestListarZonasCajas_ZonasError_Propaga(t *testing.T) {
	t.Parallel()
	svc, _, catalogo, _ := newTestService()
	catalogo.listarZonasErr = errors.New("firebird down")

	_, err := svc.ListarZonasCajas(context.Background())
	require.Error(t, err)
}

func TestListarZonasCajas_RepoError_Propaga(t *testing.T) {
	t.Parallel()
	svc, repo, catalogo, _ := newTestService()
	catalogo.zonas = []configdomain.CatalogoRef{{ID: 1, Nombre: "R/01"}}
	repo.listarZonaCajaErr = errors.New("firebird down")

	_, err := svc.ListarZonasCajas(context.Background())
	require.Error(t, err)
}

func TestListarZonasCajas_CatalogoError_Propaga(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(c *fakeCatalogoReader)
	}{
		{"cajas", func(c *fakeCatalogoReader) { c.listarCajasErr = errors.New("boom") }},
		{"cajeros", func(c *fakeCatalogoReader) { c.listarCajerosErr = errors.New("boom") }},
		{"vendedores", func(c *fakeCatalogoReader) { c.listarVendedoresErr = errors.New("boom") }},
		{"cobradores", func(c *fakeCatalogoReader) { c.listarCobradoresErr = errors.New("boom") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc, _, catalogo, _ := newTestService()
			catalogo.zonas = []configdomain.CatalogoRef{{ID: 1, Nombre: "R/01"}}
			tc.setup(catalogo)

			_, err := svc.ListarZonasCajas(context.Background())
			require.Error(t, err)
		})
	}
}

func TestListarOpcionesZonasCajas_CatalogoError_Propaga(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(c *fakeCatalogoReader)
	}{
		{"zonas", func(c *fakeCatalogoReader) { c.listarZonasErr = errors.New("boom") }},
		{"cajas", func(c *fakeCatalogoReader) { c.listarCajasErr = errors.New("boom") }},
		{"cajeros", func(c *fakeCatalogoReader) { c.listarCajerosErr = errors.New("boom") }},
		{"vendedores", func(c *fakeCatalogoReader) { c.listarVendedoresErr = errors.New("boom") }},
		{"cobradores", func(c *fakeCatalogoReader) { c.listarCobradoresErr = errors.New("boom") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc, _, catalogo, _ := newTestService()
			tc.setup(catalogo)

			_, err := svc.ListarOpcionesZonasCajas(context.Background())
			require.Error(t, err)
		})
	}
}

func TestListarOpcionesZonasCajas_DevuelveLosCincoCatalogos(t *testing.T) {
	t.Parallel()
	svc, _, catalogo, _ := newTestService()
	catalogo.zonas = []configdomain.CatalogoRef{{ID: 1, Nombre: "R/01"}}
	catalogo.cajas = []configdomain.CatalogoRef{{ID: 100, Nombre: "CAJA1"}}
	catalogo.cajeros = []configdomain.CatalogoRef{{ID: 200, Nombre: "CAJERO1"}}
	catalogo.vendedoresCat = []configdomain.CatalogoRef{{ID: 300, Nombre: "VEND1"}}
	catalogo.cobradores = []configdomain.CatalogoRef{{ID: 400, Nombre: "COB1"}}

	opciones, err := svc.ListarOpcionesZonasCajas(context.Background())
	require.NoError(t, err)
	assert.Equal(t, catalogo.zonas, opciones.Zonas)
	assert.Equal(t, catalogo.cajas, opciones.Cajas)
	assert.Equal(t, catalogo.cajeros, opciones.Cajeros)
	assert.Equal(t, catalogo.vendedoresCat, opciones.Vendedores)
	assert.Equal(t, catalogo.cobradores, opciones.Cobradores)
}
