// Package app_test contains tests for application commands.
package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
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
	// imagenes and storage are the outbound dependencies Entregar needs. They
	// live here so setupService can keep its shape while every test reaches
	// them through the folio repo it already holds.
	imagenes *fakeFirmaImagenRepo
	storage  *fakeFirmaStorage
	// stagedImagenes models the image rows written in the same transaction:
	// Guardar/Crear stage them, and only a commit registers them.
	stagedImagenes []*domain.Imagen
}

func newFakeGarantiaRepo() *fakeGarantiaRepo {
	return &fakeGarantiaRepo{
		obtained:  make(map[uuid.UUID]*domain.Garantia),
		byFolio:   make(map[domain.Folio]*domain.Garantia),
		forUpdate: make(map[uuid.UUID]*domain.Garantia),
		imagenes:  newFakeFirmaImagenRepo(),
		storage:   newFakeFirmaStorage(),
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
	for _, img := range f.stagedImagenes {
		f.imagenes.registrar(img)
	}
	f.stagedCreated, f.stagedSaved, f.stagedImagenes = nil, nil, nil
}

func (f *fakeGarantiaRepo) rollback() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stagedCreated, f.stagedSaved, f.stagedImagenes = nil, nil, nil
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
	f.stagedCreated = append(f.stagedCreated, cloneGarantia(g))
	f.stagedImagenes = append(f.stagedImagenes, g.ImagenesPendientesForRepo()...)
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
	f.stagedImagenes = append(f.stagedImagenes, g.ImagenesPendientesForRepo()...)
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

// ObtenerDetalle exists only to satisfy the port: the command tests never
// read the timeline. The query tests use fakeConsultaGarantiaRepo.
func (f *fakeGarantiaRepo) ObtenerDetalle(
	ctx context.Context,
	id uuid.UUID,
) (*domain.Garantia, []*domain.Evento, error) {
	g, err := f.Obtener(ctx, id)
	return g, nil, err
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
	// porFolio keeps the timeline of each folio in insertion order, which is
	// what ListarPorGarantia returns. Cerrar reads it to audit the delivery.
	porFolio map[uuid.UUID][]*domain.Evento
	getErr   error
	listErr  error
	orden    *registrador
}

func newFakeEventoRepo() *fakeEventoRepo {
	return &fakeEventoRepo{
		events:   make(map[string]*domain.Evento),
		byID:     make(map[uuid.UUID]*domain.Evento),
		byClave:  make(map[string]*domain.Evento),
		porFolio: make(map[uuid.UUID][]*domain.Evento),
	}
}

func (f *fakeEventoRepo) addEvento(ev *domain.Evento) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[ev.ID()] = ev
	if ev.ClaveIdempotencia() != "" {
		f.byClave[ev.ClaveIdempotencia()] = ev
	}
	f.porFolio[ev.GarantiaID()] = append(f.porFolio[ev.GarantiaID()], ev)
}

func (f *fakeEventoRepo) ListarPorGarantia(ctx context.Context, garantiaID uuid.UUID) ([]*domain.Evento, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]*domain.Evento(nil), f.porFolio[garantiaID]...), nil
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

// fakeFirmaImagenRepo is the in-memory ImagenRepo. The folio fake stages the
// image rows of a write and registers them here on commit, exactly like the
// real repository inserts them in the folio's transaction.
type fakeFirmaImagenRepo struct {
	mu           sync.Mutex
	porID        map[uuid.UUID]*domain.Imagen
	registrarErr error
	obtenerErr   error
	registrados  int
}

func newFakeFirmaImagenRepo() *fakeFirmaImagenRepo {
	return &fakeFirmaImagenRepo{porID: make(map[uuid.UUID]*domain.Imagen)}
}

// registrar is the commit path: it keeps the row without counting it as a
// Registrar call (the folio repo is the one that "wrote" it).
func (f *fakeFirmaImagenRepo) registrar(img *domain.Imagen) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.porID[img.ID()] = img
}

func (f *fakeFirmaImagenRepo) Registrar(ctx context.Context, img *domain.Imagen) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.registrarErr != nil {
		return f.registrarErr
	}
	f.registrados++
	f.porID[img.ID()] = img
	return nil
}

func (f *fakeFirmaImagenRepo) Obtener(ctx context.Context, id uuid.UUID) (*domain.Imagen, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.obtenerErr != nil {
		return nil, f.obtenerErr
	}
	if img, ok := f.porID[id]; ok {
		return img, nil
	}
	return nil, domain.ErrImagenNoEncontrada
}

func (f *fakeFirmaImagenRepo) ListarPorEvento(ctx context.Context, eventoID uuid.UUID) ([]*domain.Imagen, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.Imagen
	for _, img := range f.porID {
		if img.EventoID() == eventoID {
			out = append(out, img)
		}
	}
	return out, nil
}

func (f *fakeFirmaImagenRepo) ListarPorEventos(ctx context.Context, eventoIDs []uuid.UUID) ([]*domain.Imagen, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.Imagen
	for _, img := range f.porID {
		for _, id := range eventoIDs {
			if img.EventoID() == id {
				out = append(out, img)
				break
			}
		}
	}
	return out, nil
}

// total returns how many image rows the store holds.
func (f *fakeFirmaImagenRepo) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.porID)
}

// porTipo returns the rows of one image type.
func (f *fakeFirmaImagenRepo) porTipo(tipo domain.TipoImagen) []*domain.Imagen {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.Imagen
	for _, img := range f.porID {
		if img.Tipo() == tipo {
			out = append(out, img)
		}
	}
	return out
}

// fakeFirmaStorage is the in-memory StorageProvider. Delete is idempotent, as
// the port requires.
type fakeFirmaStorage struct {
	mu          sync.Mutex
	blobs       map[string][]byte
	storeErr    error
	deleteErr   error
	storeCalls  int
	deleteCalls int
}

func newFakeFirmaStorage() *fakeFirmaStorage {
	return &fakeFirmaStorage{blobs: make(map[string][]byte)}
}

func (f *fakeFirmaStorage) Store(ctx context.Context, key, contentType string, sizeBytes int64, body io.Reader) error {
	datos, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.storeCalls++
	if f.storeErr != nil {
		return f.storeErr
	}
	f.blobs[key] = datos
	return nil
}

func (f *fakeFirmaStorage) Get(ctx context.Context, key string) (outbound.StorageObject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	datos, ok := f.blobs[key]
	if !ok {
		return outbound.StorageObject{}, errFakeNotFound
	}
	return outbound.StorageObject{
		Body:      io.NopCloser(bytes.NewReader(datos)),
		SizeBytes: int64(len(datos)),
	}, nil
}

func (f *fakeFirmaStorage) Delete(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.blobs, key)
	return nil
}

// total returns how many blobs are stored.
func (f *fakeFirmaStorage) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.blobs)
}

// claves returns the stored keys.
func (f *fakeFirmaStorage) claves() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.blobs))
	for k := range f.blobs {
		out = append(out, k)
	}
	return out
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
		Ruta:        a.Ruta(),
		Etapa:       a.Etapa(),
		Ubicacion:   a.Ubicacion(),
		Dictamen:    a.Dictamen(),
		Desenlace:   a.Desenlace(),
		CerradoEn:   a.CerradoEn(),
		ReemplazaA:  a.ReemplazaA(),
		CreatedAt:   a.CreatedAt(),
		UpdatedAt:   a.UpdatedAt(),
	})
}

var (
	_ outbound.GarantiaRepo    = (*fakeGarantiaRepo)(nil)
	_ outbound.EventoRepo      = (*fakeEventoRepo)(nil)
	_ outbound.ImagenRepo      = (*fakeFirmaImagenRepo)(nil)
	_ outbound.StorageProvider = (*fakeFirmaStorage)(nil)
	_ outbound.FolioGenerator  = (*fakeFolioGen)(nil)
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
