// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

var errFakeNotFound = errors.New("not found")

type fakeGarantiaRepo struct {
	mu        sync.Mutex
	created   []*domain.Garantia
	saved     []*domain.Garantia
	obtained  map[uuid.UUID]*domain.Garantia
	byFolio   map[domain.Folio]*domain.Garantia
	forUpdate map[uuid.UUID]*domain.Garantia
	// staged writes model the real repository: rows are written while the
	// transaction is open, and only a commit keeps them. Guardar/Crear stage
	// their rows even when they are about to return an error, so a half-write
	// that got rolled back is indistinguishable from never having happened.
	stagedCreated []*domain.Garantia
	stagedSaved   []*domain.Garantia
	// lastSaved/lastCreated are the raw aggregates handed to Guardar/Crear,
	// kept for assertions on their pending events (the clones lose them).
	lastSaved   *domain.Garantia
	lastCreated *domain.Garantia
	createErr   error
	saveErr     error
	getErr      error
	seeded      int
	onSave      func()
	onCreate    func()
	orden       *registrador
}

func newFakeGarantiaRepo() *fakeGarantiaRepo {
	return &fakeGarantiaRepo{
		obtained:  make(map[uuid.UUID]*domain.Garantia),
		byFolio:   make(map[domain.Folio]*domain.Garantia),
		forUpdate: make(map[uuid.UUID]*domain.Garantia),
	}
}

// commit keeps the writes staged since the last commit/rollback; rollback
// drops them. The tx runner fires them through onCommit/onRollback, wired in
// setupService.
func (f *fakeGarantiaRepo) commit() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.stagedCreated {
		f.created = append(f.created, c)
		f.obtained[c.ID()] = c
		f.byFolio[c.Folio()] = c
	}
	for _, s := range f.stagedSaved {
		f.saved = append(f.saved, s)
		f.obtained[s.ID()] = s
		f.byFolio[s.Folio()] = s
	}
	f.stagedCreated, f.stagedSaved = nil, nil
}

func (f *fakeGarantiaRepo) rollback() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stagedCreated, f.stagedSaved = nil, nil
}

func (f *fakeGarantiaRepo) Crear(ctx context.Context, g *domain.Garantia) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Mirror Guardar: onCreate fires before the error so a test can register
	// the twin's event while the pre-check has already come back empty.
	if f.onCreate != nil {
		f.onCreate()
	}
	f.lastCreated = g
	clone := cloneGarantia(g)
	f.stagedCreated = append(f.stagedCreated, clone)
	if f.createErr != nil {
		return f.createErr
	}
	return nil
}

func (f *fakeGarantiaRepo) Guardar(ctx context.Context, g *domain.Garantia) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastSaved = g
	// onSave fires before saveErr so a test can register the twin's event while
	// the pre-check has already come back empty — the only way to reach the
	// race branch (spec §3.4) instead of the cheap replay path.
	if f.onSave != nil {
		f.onSave()
	}
	// The write lands even when the error is about to be returned, mirroring
	// the real row insert that fails on the event UNIQUE.
	f.stagedSaved = append(f.stagedSaved, cloneGarantia(g))
	if f.saveErr != nil {
		return f.saveErr
	}
	return nil
}

func (f *fakeGarantiaRepo) ObtenerParaActualizar(ctx context.Context, id uuid.UUID) (*domain.Garantia, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orden.registra("obtener_para_actualizar")
	if g, ok := f.forUpdate[id]; ok {
		return cloneGarantia(g), nil
	}
	if g, ok := f.obtained[id]; ok {
		return cloneGarantia(g), nil
	}
	return nil, domain.ErrGarantiaNoEncontrada
}

func (f *fakeGarantiaRepo) Obtener(ctx context.Context, id uuid.UUID) (*domain.Garantia, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if g, ok := f.obtained[id]; ok {
		return cloneGarantia(g), nil
	}
	return nil, domain.ErrGarantiaNoEncontrada
}

func (f *fakeGarantiaRepo) ObtenerPorFolio(ctx context.Context, folio domain.Folio) (*domain.Garantia, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if g, ok := f.byFolio[folio]; ok {
		return cloneGarantia(g), nil
	}
	return nil, domain.ErrGarantiaNoEncontrada
}

type fakeEventoRepo struct {
	mu      sync.Mutex
	events  map[string]*domain.Evento
	byID    map[uuid.UUID]*domain.Evento
	byClave map[string]*domain.Evento
	getErr  error
	orden   *registrador
}

func newFakeEventoRepo() *fakeEventoRepo {
	return &fakeEventoRepo{
		events:  make(map[string]*domain.Evento),
		byID:    make(map[uuid.UUID]*domain.Evento),
		byClave: make(map[string]*domain.Evento),
	}
}

func (f *fakeEventoRepo) addEvento(ev *domain.Evento) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[ev.ID()] = ev
	if ev.ClaveIdempotencia() != "" {
		f.byClave[ev.ClaveIdempotencia()] = ev
	}
}

func (f *fakeEventoRepo) ListarPorGarantia(ctx context.Context, garantiaID uuid.UUID) ([]*domain.Evento, error) {
	return nil, nil
}

func (f *fakeEventoRepo) ObtenerPorClaveIdempotencia(ctx context.Context, clave string) (*domain.Evento, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orden.registra("buscar_clave")
	if f.getErr != nil {
		return nil, f.getErr
	}
	if ev, ok := f.byClave[clave]; ok {
		return ev, nil
	}
	return nil, nil
}

type fakeFolioGen struct {
	mu    sync.Mutex
	nums  []int
	next  int
	calls int
	err   error
}

func (f *fakeFolioGen) Siguiente(ctx context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	if f.next < len(f.nums) {
		n := f.nums[f.next]
		f.next++
		return n, nil
	}
	if len(f.nums) > 0 {
		return f.nums[len(f.nums)-1] + 1, nil
	}
	return 1, nil
}

func cloneGarantia(g *domain.Garantia) *domain.Garantia {
	var arts []*domain.Articulo
	for a := range g.Articulos() {
		arts = append(arts, cloneArticulo(a))
	}
	h := domain.HydrateGarantia(domain.HydrateGarantiaParams{
		ID:             g.ID(),
		Folio:          g.Folio(),
		Origen:         g.Origen(),
		ClienteID:      g.ClienteID(),
		VentaID:        g.VentaID(),
		EstadoCuenta:   g.EstadoCuenta(),
		Estado:         g.Estado(),
		Description:    g.Description(),
		VigenciaHasta:  g.VigenciaHasta(),
		Calle:          g.Calle(),
		NumeroExterior: g.NumeroExterior(),
		Colonia:        g.Colonia(),
		Localidad:      g.Localidad(),
		Ciudad:         g.Ciudad(),
		CodigoPostal:   g.CodigoPostal(),
		GPSLat:         g.GPSLat(),
		GPSLon:         g.GPSLon(),
		AbiertoPor:     g.AbiertoPor(),
		CerradoEn:      g.CerradoEn(),
		CreatedAt:      g.CreatedAt(),
		UpdatedAt:      g.UpdatedAt(),
		Articulos:      arts,
	})
	return h
}

func cloneArticulo(a *domain.Articulo) *domain.Articulo {
	return domain.HydrateArticulo(domain.HydrateArticuloParams{
		ID:          a.ID(),
		GarantiaID:  a.GarantiaID(),
		Rol:         a.Rol(),
		ArticuloID:  a.ArticuloID(),
		Clave:       a.Clave(),
		Description: a.Description(),
		Etapa:       a.Etapa(),
		Ubicacion:   a.Ubicacion(),
		ReemplazaA:  a.ReemplazaA(),
		CreatedAt:   a.CreatedAt(),
		UpdatedAt:   a.UpdatedAt(),
	})
}

var (
	_ outbound.GarantiaRepo   = (*fakeGarantiaRepo)(nil)
	_ outbound.EventoRepo     = (*fakeEventoRepo)(nil)
	_ outbound.FolioGenerator = (*fakeFolioGen)(nil)
)

// recordingRepo captures whether every write happened while a transaction was
// open. A write outside RunInTx is the most expensive defect in this layer and
// the easiest to miss by reading, so the brief asks for it to be asserted.
type recordingRepo struct {
	*fakeGarantiaRepo
	dentroDeTx []bool
	enTx       func() bool
}

func (r *recordingRepo) Crear(ctx context.Context, g *domain.Garantia) error {
	r.dentroDeTx = append(r.dentroDeTx, r.enTx())
	return r.fakeGarantiaRepo.Crear(ctx, g)
}

func (r *recordingRepo) Guardar(ctx context.Context, g *domain.Garantia) error {
	r.dentroDeTx = append(r.dentroDeTx, r.enTx())
	return r.fakeGarantiaRepo.Guardar(ctx, g)
}
