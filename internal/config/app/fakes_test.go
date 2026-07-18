package app_test

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/config/app"
	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
)

// fakeConfigRepo is an in-memory outbound.ConfigRepo for app tests.
type fakeConfigRepo struct {
	mappings map[uuid.UUID]configdomain.VendedorMapping

	upsertErr  error
	deleteErr  error
	listarErr  error
	upsertCall int

	zonaCajaConfigs    map[int]configdomain.ZonaCajaConfig
	listarZonaCajaErr  error
	upsertZonaCajaErr  error
	upsertZonaCajaCall int
}

func newFakeConfigRepo() *fakeConfigRepo {
	return &fakeConfigRepo{
		mappings:        map[uuid.UUID]configdomain.VendedorMapping{},
		zonaCajaConfigs: map[int]configdomain.ZonaCajaConfig{},
	}
}

func (f *fakeConfigRepo) ListarVendedorMappings(_ context.Context) ([]configdomain.VendedorMapping, error) {
	if f.listarErr != nil {
		return nil, f.listarErr
	}
	out := make([]configdomain.VendedorMapping, 0, len(f.mappings))
	for _, m := range f.mappings {
		out = append(out, m)
	}
	return out, nil
}

func (f *fakeConfigRepo) UpsertVendedorMapping(_ context.Context, m configdomain.VendedorMapping) error {
	f.upsertCall++
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.mappings[m.UsuarioID] = m
	return nil
}

func (f *fakeConfigRepo) DeleteVendedorMapping(_ context.Context, usuarioID uuid.UUID) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.mappings, usuarioID)
	return nil
}

func (f *fakeConfigRepo) ListarZonaCajaConfigs(_ context.Context) ([]configdomain.ZonaCajaConfig, error) {
	if f.listarZonaCajaErr != nil {
		return nil, f.listarZonaCajaErr
	}
	out := make([]configdomain.ZonaCajaConfig, 0, len(f.zonaCajaConfigs))
	for _, c := range f.zonaCajaConfigs {
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeConfigRepo) UpsertZonaCajaConfig(_ context.Context, c configdomain.ZonaCajaConfig) error {
	f.upsertZonaCajaCall++
	if f.upsertZonaCajaErr != nil {
		return f.upsertZonaCajaErr
	}
	f.zonaCajaConfigs[c.ZonaClienteID] = c
	return nil
}

// fakeCatalogoReader is an in-memory outbound.CatalogoReader for app tests.
type fakeCatalogoReader struct {
	nombres         map[int]string
	identidades     []configdomain.IdentidadMicrosip
	perteneceByPair map[[2]int]bool

	resolverErr    error
	identidadesErr error
	perteneceErr   error

	zonas, cajas, cajeros, vendedoresCat, cobradores []configdomain.CatalogoRef
	listarZonasErr, listarCajasErr, listarCajerosErr error
	listarVendedoresErr, listarCobradoresErr         error

	zonasExistentes, cajasExistentes              map[int]bool
	cajerosExistentes, vendedoresExistentes       map[int]bool
	cobradoresExistentes                          map[int]bool
	zonaExisteErr, cajaExisteErr, cajeroExisteErr error
	vendedorExisteErr, cobradorExisteErr          error

	listarZonasCall, listarCajasCall, listarCajerosCall int
	listarVendedoresCall, listarCobradoresCall          int
}

func newFakeCatalogoReader() *fakeCatalogoReader {
	return &fakeCatalogoReader{
		nombres:         map[int]string{},
		perteneceByPair: map[[2]int]bool{},

		zonasExistentes:      map[int]bool{},
		cajasExistentes:      map[int]bool{},
		cajerosExistentes:    map[int]bool{},
		vendedoresExistentes: map[int]bool{},
		cobradoresExistentes: map[int]bool{},
	}
}

func (f *fakeCatalogoReader) ResolverNombresLista(_ context.Context, listaIDs []int) (map[int]string, error) {
	if f.resolverErr != nil {
		return nil, f.resolverErr
	}
	out := make(map[int]string, len(listaIDs))
	for _, id := range listaIDs {
		if n, ok := f.nombres[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

func (f *fakeCatalogoReader) ListarIdentidadesMicrosip(_ context.Context) ([]configdomain.IdentidadMicrosip, error) {
	if f.identidadesErr != nil {
		return nil, f.identidadesErr
	}
	return f.identidades, nil
}

func (f *fakeCatalogoReader) ListaIDPerteneceAtributo(_ context.Context, listaID, atributoID int) (bool, error) {
	if f.perteneceErr != nil {
		return false, f.perteneceErr
	}
	return f.perteneceByPair[[2]int{listaID, atributoID}], nil
}

func (f *fakeCatalogoReader) ListarZonas(_ context.Context) ([]configdomain.CatalogoRef, error) {
	f.listarZonasCall++
	if f.listarZonasErr != nil {
		return nil, f.listarZonasErr
	}
	return f.zonas, nil
}

func (f *fakeCatalogoReader) ListarCajas(_ context.Context) ([]configdomain.CatalogoRef, error) {
	f.listarCajasCall++
	if f.listarCajasErr != nil {
		return nil, f.listarCajasErr
	}
	return f.cajas, nil
}

func (f *fakeCatalogoReader) ListarCajeros(_ context.Context) ([]configdomain.CatalogoRef, error) {
	f.listarCajerosCall++
	if f.listarCajerosErr != nil {
		return nil, f.listarCajerosErr
	}
	return f.cajeros, nil
}

func (f *fakeCatalogoReader) ListarVendedoresCatalogo(_ context.Context) ([]configdomain.CatalogoRef, error) {
	f.listarVendedoresCall++
	if f.listarVendedoresErr != nil {
		return nil, f.listarVendedoresErr
	}
	return f.vendedoresCat, nil
}

func (f *fakeCatalogoReader) ListarCobradores(_ context.Context) ([]configdomain.CatalogoRef, error) {
	f.listarCobradoresCall++
	if f.listarCobradoresErr != nil {
		return nil, f.listarCobradoresErr
	}
	return f.cobradores, nil
}

func (f *fakeCatalogoReader) ZonaExiste(_ context.Context, id int) (bool, error) {
	if f.zonaExisteErr != nil {
		return false, f.zonaExisteErr
	}
	return f.zonasExistentes[id], nil
}

func (f *fakeCatalogoReader) CajaExiste(_ context.Context, id int) (bool, error) {
	if f.cajaExisteErr != nil {
		return false, f.cajaExisteErr
	}
	return f.cajasExistentes[id], nil
}

func (f *fakeCatalogoReader) CajeroExiste(_ context.Context, id int) (bool, error) {
	if f.cajeroExisteErr != nil {
		return false, f.cajeroExisteErr
	}
	return f.cajerosExistentes[id], nil
}

func (f *fakeCatalogoReader) VendedorExiste(_ context.Context, id int) (bool, error) {
	if f.vendedorExisteErr != nil {
		return false, f.vendedorExisteErr
	}
	return f.vendedoresExistentes[id], nil
}

func (f *fakeCatalogoReader) CobradorExiste(_ context.Context, id int) (bool, error) {
	if f.cobradorExisteErr != nil {
		return false, f.cobradorExisteErr
	}
	return f.cobradoresExistentes[id], nil
}

// fakeUsuariosReader is an in-memory outbound.UsuariosReader for app tests.
type fakeUsuariosReader struct {
	usuarios []configdomain.AppUsuario
	err      error
}

func (f *fakeUsuariosReader) ListarUsuarios(_ context.Context) ([]configdomain.AppUsuario, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.usuarios, nil
}

// newTestService wires a Service against the three fakes, returning them for
// per-test mutation.
func newTestService() (*app.Service, *fakeConfigRepo, *fakeCatalogoReader, *fakeUsuariosReader) {
	repo := newFakeConfigRepo()
	catalogo := newFakeCatalogoReader()
	usuarios := &fakeUsuariosReader{}
	return app.NewService(repo, catalogo, usuarios, catalogo, catalogo), repo, catalogo, usuarios
}
