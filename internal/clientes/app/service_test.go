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

	// ListarDirectorioCompleto
	dirCompleto       []outbound.DirectorioItem
	dirCompletoErr    error
	lastFiltroComplet outbound.FiltroDirectorio
	listarComplCalled bool // true after ListarDirectorioCompleto is invoked
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

func (f *fakeClientesRepo) ListarDirectorioCompleto(_ context.Context, fil outbound.FiltroDirectorio) ([]outbound.DirectorioItem, error) {
	f.lastFiltroComplet = fil
	f.listarComplCalled = true
	if f.dirCompletoErr != nil {
		return nil, f.dirCompletoErr
	}
	return f.dirCompleto, nil
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

// ─── fakeDirectoryIndex ──────────────────────────────────────────────────────

// fakeDirectoryIndex is a test double for outbound.DirectoryIndex that records
// calls to Reconciliar for assertion in unit tests. Buscar always returns an
// empty result so tests that exercise reconcile/reindex paths compile cleanly.
type fakeDirectoryIndex struct {
	lastDocs []outbound.DirectorioDoc
	err      error
}

func (f *fakeDirectoryIndex) Buscar(_ context.Context, _ outbound.DirectorioQuery) (outbound.DirectorioResultado, error) {
	if f.err != nil {
		return outbound.DirectorioResultado{}, f.err
	}
	return outbound.DirectorioResultado{Items: []outbound.DirectorioDoc{}, Facets: nil, Total: 0}, nil
}

func (f *fakeDirectoryIndex) Reconciliar(_ context.Context, docs []outbound.DirectorioDoc) error {
	if f.err != nil {
		return f.err
	}
	f.lastDocs = docs
	return nil
}
