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
}

func newFakeConfigRepo() *fakeConfigRepo {
	return &fakeConfigRepo{mappings: map[uuid.UUID]configdomain.VendedorMapping{}}
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

// fakeCatalogoReader is an in-memory outbound.CatalogoReader for app tests.
type fakeCatalogoReader struct {
	nombres         map[int]string
	identidades     []configdomain.IdentidadMicrosip
	perteneceByPair map[[2]int]bool

	resolverErr    error
	identidadesErr error
	perteneceErr   error
}

func newFakeCatalogoReader() *fakeCatalogoReader {
	return &fakeCatalogoReader{
		nombres:         map[int]string{},
		perteneceByPair: map[[2]int]bool{},
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
	return app.NewService(repo, catalogo, usuarios), repo, catalogo, usuarios
}
