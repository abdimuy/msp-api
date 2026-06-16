//nolint:misspell // Spanish vocabulary (clientes, ficha, pulso, directorio, ventas, etc.) per project convention.
package app_test

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// ─── Helpers / shared test data ──────────────────────────────────────────────

var (
	fixedTime = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	zero      = decimal.Zero
)

// newCliente builds a minimal Cliente for tests.
func newCliente(id int, nombre string) *domain.Cliente {
	return domain.HydrateCliente(domain.HydrateClienteParams{
		ClienteID: id,
		Nombre:    nombre,
	})
}

// newVentaCliente builds a minimal VentaCliente for tests.
func newVentaCliente(doctoID, clienteID int) *domain.VentaCliente {
	return domain.HydrateVentaCliente(domain.HydrateVentaClienteParams{
		DoctoPVID: doctoID,
		ClienteID: clienteID,
		Fecha:     fixedTime,
		Tipo:      domain.TipoVentaContado,
	})
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }

// ─── fixedClock ──────────────────────────────────────────────────────────────

// fixedClock is an outbound.Clock that always returns T.
type fixedClock struct{ T time.Time }

func (c fixedClock) Now() time.Time { return c.T }

// ─── fakeClientesRepo ────────────────────────────────────────────────────────

type fakeClientesRepo struct {
	// ObtenerCliente
	clienteByID map[int]*domain.Cliente
	clienteErr  error

	// ObtenerResumenFicha
	resumen    outbound.ResumenFicha
	resumenErr error

	// ListarVentas
	ventasPage    outbound.Page[*domain.VentaCliente]
	ventasErr     error
	lastClienteID int
	lastParams    outbound.ListParams

	// ObtenerVentaDetalle
	detalleByID map[int]outbound.VentaDetalle
	detalleErr  error

	// ListarDirectorio
	dirPage    outbound.Page[outbound.DirectorioItem]
	dirErr     error
	lastFiltro outbound.FiltroDirectorio

	// ListarDirectorioCompleto
	dirCompleto       []outbound.DirectorioItem
	dirCompletoErr    error
	lastFiltroComplet outbound.FiltroDirectorio
	listarDirCalled   bool // true after ListarDirectorio is invoked
	listarComplCalled bool // true after ListarDirectorioCompleto is invoked

	// BuscarClienteIDsBasico
	basicIDs     []int
	basicErr     error
	lastBasicQ   string
	lastBasicLim int

	// LeerDocumentosBusqueda
	docs    []outbound.SearchDoc
	docsErr error
}

func (f *fakeClientesRepo) ObtenerCliente(_ context.Context, clienteID int) (*domain.Cliente, error) {
	if f.clienteErr != nil {
		return nil, f.clienteErr
	}
	c, ok := f.clienteByID[clienteID]
	if !ok {
		return nil, domain.ErrClienteNotFound
	}
	return c, nil
}

func (f *fakeClientesRepo) ObtenerResumenFicha(_ context.Context, _ int) (outbound.ResumenFicha, error) {
	if f.resumenErr != nil {
		return outbound.ResumenFicha{}, f.resumenErr
	}
	return f.resumen, nil
}

func (f *fakeClientesRepo) ListarVentas(_ context.Context, clienteID int, p outbound.ListParams) (outbound.Page[*domain.VentaCliente], error) {
	f.lastClienteID = clienteID
	f.lastParams = p
	if f.ventasErr != nil {
		return outbound.Page[*domain.VentaCliente]{}, f.ventasErr
	}
	return f.ventasPage, nil
}

func (f *fakeClientesRepo) ObtenerVentaDetalle(_ context.Context, doctoPVID int) (outbound.VentaDetalle, error) {
	if f.detalleErr != nil {
		return outbound.VentaDetalle{}, f.detalleErr
	}
	d, ok := f.detalleByID[doctoPVID]
	if !ok {
		return outbound.VentaDetalle{}, domain.ErrVentaNotFound
	}
	return d, nil
}

func (f *fakeClientesRepo) ListarDirectorio(_ context.Context, _ outbound.ListParams, fil outbound.FiltroDirectorio) (outbound.Page[outbound.DirectorioItem], error) {
	f.lastFiltro = fil
	f.listarDirCalled = true
	if f.dirErr != nil {
		return outbound.Page[outbound.DirectorioItem]{}, f.dirErr
	}
	return f.dirPage, nil
}

func (f *fakeClientesRepo) ListarDirectorioCompleto(_ context.Context, fil outbound.FiltroDirectorio) ([]outbound.DirectorioItem, error) {
	f.lastFiltroComplet = fil
	f.listarComplCalled = true
	if f.dirCompletoErr != nil {
		return nil, f.dirCompletoErr
	}
	return f.dirCompleto, nil
}

func (f *fakeClientesRepo) BuscarClienteIDsBasico(_ context.Context, query string, limit int) ([]int, error) {
	f.lastBasicQ = query
	f.lastBasicLim = limit
	if f.basicErr != nil {
		return nil, f.basicErr
	}
	return f.basicIDs, nil
}

func (f *fakeClientesRepo) LeerDocumentosBusqueda(_ context.Context) ([]outbound.SearchDoc, error) {
	if f.docsErr != nil {
		return nil, f.docsErr
	}
	return f.docs, nil
}

// ─── fakeAnalyticsClient ─────────────────────────────────────────────────────

type fakeAnalyticsClient struct {
	// ObtenerPulso
	pulsos     map[int]analytics.ClientePulsoContract
	pulsoErr   error
	pulsoFound bool // when set overrides map lookup (for single-ID tests)

	// ObtenerPulsos
	pulsosMap map[int]analytics.ClientePulsoContract
	pulsosErr error
}

func (f *fakeAnalyticsClient) ObtenerPulso(_ context.Context, clienteID int) (analytics.ClientePulsoContract, bool, error) {
	if f.pulsoErr != nil {
		return analytics.ClientePulsoContract{}, false, f.pulsoErr
	}
	p, ok := f.pulsos[clienteID]
	return p, ok, nil
}

func (f *fakeAnalyticsClient) ObtenerPulsos(_ context.Context, clienteIDs []int) (map[int]analytics.ClientePulsoContract, error) {
	if f.pulsosErr != nil {
		return nil, f.pulsosErr
	}
	result := make(map[int]analytics.ClientePulsoContract, len(clienteIDs))
	src := f.pulsosMap
	if src == nil {
		src = f.pulsos
	}
	for _, id := range clienteIDs {
		if p, ok := src[id]; ok {
			result[id] = p
		}
	}
	return result, nil
}

// ─── fakeSearchIndex ─────────────────────────────────────────────────────────

type fakeSearchIndex struct {
	ready  bool
	ids    []int
	busErr error

	lastQuery string
	lastLimit int
}

func (f *fakeSearchIndex) EstaListo() bool { return f.ready }

func (f *fakeSearchIndex) Buscar(_ context.Context, query string, limit int) ([]int, error) {
	f.lastQuery = query
	f.lastLimit = limit
	if f.busErr != nil {
		return nil, f.busErr
	}
	return f.ids, nil
}

func (f *fakeSearchIndex) Reconciliar(_ context.Context, _ []outbound.SearchDoc) error {
	return nil
}
