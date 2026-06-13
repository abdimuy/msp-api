//nolint:misspell // test vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ─── Fixed test time ────────────────────────────────────────────────────────

var fixedNow = time.Date(2026, 6, 8, 18, 0, 0, 0, time.UTC)

// ─── fakeClock ──────────────────────────────────────────────────────────────

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }

// ─── fakeFolioMinter ────────────────────────────────────────────────────────

type fakeFolioMinter struct{ counter atomic.Int64 }

func (f *fakeFolioMinter) MintFolio(_ context.Context) (domain.Folio, error) {
	n := f.counter.Add(1)
	s := fmt.Sprintf("MST%06d", n)
	return domain.NewFolio(s)
}

// ─── fakeOutbox ─────────────────────────────────────────────────────────────

type outboxEntry struct {
	aggregate   string
	aggregateID uuid.UUID
	eventType   string
	payload     any
}

type fakeOutbox struct {
	entries []outboxEntry
	err     error // when non-nil, Enqueue returns this error
}

func (o *fakeOutbox) Enqueue(_ context.Context, aggregate string, aggregateID uuid.UUID, eventType string, payload any) error {
	if o.err != nil {
		return o.err
	}
	o.entries = append(o.entries, outboxEntry{aggregate, aggregateID, eventType, payload})
	return nil
}

// ─── fakeTraspasoRepo ───────────────────────────────────────────────────────

type fakeTraspasoRepo struct {
	byID      map[int]*domain.Traspaso
	byVentaID map[uuid.UUID][]*domain.Traspaso
	counter   int
	SaveCalls int
	LastSaved *domain.Traspaso
	saveErr   error
	findErr   error
}

func newFakeTraspasoRepo() *fakeTraspasoRepo {
	return &fakeTraspasoRepo{
		byID:      make(map[int]*domain.Traspaso),
		byVentaID: make(map[uuid.UUID][]*domain.Traspaso),
	}
}

func (r *fakeTraspasoRepo) Save(_ context.Context, t *domain.Traspaso) (int, error) {
	if r.saveErr != nil {
		return 0, r.saveErr
	}
	r.counter++
	id := r.counter
	r.byID[id] = t
	if t.VentaID() != nil {
		r.byVentaID[*t.VentaID()] = append(r.byVentaID[*t.VentaID()], t)
	}
	r.SaveCalls++
	r.LastSaved = t
	return id, nil
}

func (r *fakeTraspasoRepo) FindByID(_ context.Context, doctoInID int) (*domain.Traspaso, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	t, ok := r.byID[doctoInID]
	if !ok {
		return nil, domain.ErrTraspasoNoEncontrado
	}
	return t, nil
}

func (r *fakeTraspasoRepo) ListByVentaID(_ context.Context, ventaID uuid.UUID) ([]*domain.Traspaso, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	list := r.byVentaID[ventaID]
	if list == nil {
		return []*domain.Traspaso{}, nil
	}
	return list, nil
}

func (r *fakeTraspasoRepo) MarcarDirectoReversado(_ context.Context, doctoInID int) error {
	t, ok := r.byID[doctoInID]
	if !ok {
		return nil // nothing to do — consistent with the real repo
	}
	// Rebuild the traspaso with reversado=true so subsequent ListByVentaID
	// / activeDirect calls see the updated state.
	aud := t.Audit()
	rebuilt := domain.HydrateTraspaso(domain.HydrateTraspasoParams{
		ID:             t.ID(),
		Folio:          t.Folio(),
		AlmacenOrigen:  t.AlmacenOrigen(),
		AlmacenDestino: t.AlmacenDestino(),
		Fecha:          t.Fecha(),
		Descripcion:    t.Descripcion(),
		VentaID:        t.VentaID(),
		TipoReverso:    t.TipoReverso(),
		Reversado:      true,
		DoctoInID:      t.DoctoInID(),
		Detalles:       t.DetallesForRepo(),
		CreatedAt:      aud.CreatedAt(),
		UpdatedAt:      aud.UpdatedAt(),
		CreatedBy:      aud.CreatedBy(),
		UpdatedBy:      aud.UpdatedBy(),
	})
	r.byID[doctoInID] = rebuilt
	// Also update the byVentaID slice so ListByVentaID returns the new state.
	if rebuilt.VentaID() != nil {
		vID := *rebuilt.VentaID()
		for i, tr := range r.byVentaID[vID] {
			if tr.DoctoInID() != nil && *tr.DoctoInID() == doctoInID {
				r.byVentaID[vID][i] = rebuilt
				break
			}
		}
	}
	return nil
}

// ─── fakeExistenciaQuery ────────────────────────────────────────────────────

type fakeExistenciaQuery struct {
	// stock[articuloID][almacenID] = cantidad
	stock map[int]map[int]decimal.Decimal
	err   error
}

func newFakeExistenciaQuery() *fakeExistenciaQuery {
	return &fakeExistenciaQuery{stock: make(map[int]map[int]decimal.Decimal)}
}

func (q *fakeExistenciaQuery) set(articuloID, almacenID int, cantidad decimal.Decimal) {
	if q.stock[articuloID] == nil {
		q.stock[articuloID] = make(map[int]decimal.Decimal)
	}
	q.stock[articuloID][almacenID] = cantidad
}

func (q *fakeExistenciaQuery) Existencia(_ context.Context, articuloID, almacenID int) (decimal.Decimal, error) {
	if q.err != nil {
		return decimal.Zero, q.err
	}
	if byAlmacen, ok := q.stock[articuloID]; ok {
		if v, ok2 := byAlmacen[almacenID]; ok2 {
			return v, nil
		}
	}
	return decimal.Zero, nil
}

func (q *fakeExistenciaQuery) ExistenciasPorAlmacen(_ context.Context, almacenID int) ([]domain.Existencia, error) {
	if q.err != nil {
		return nil, q.err
	}
	var out []domain.Existencia
	for artID, byAlmacen := range q.stock {
		if v, ok := byAlmacen[almacenID]; ok {
			out = append(out, domain.Existencia{ArticuloID: artID, AlmacenID: almacenID, Cantidad: v})
		}
	}
	return out, nil
}

// ─── fakeAlmacenRepo ────────────────────────────────────────────────────────

type fakeAlmacenRepo struct {
	almacenes map[int]domain.Almacen
}

func newFakeAlmacenRepo(almacenes ...domain.Almacen) *fakeAlmacenRepo {
	m := make(map[int]domain.Almacen, len(almacenes))
	for _, a := range almacenes {
		m[a.ID] = a
	}
	return &fakeAlmacenRepo{almacenes: m}
}

func (r *fakeAlmacenRepo) FindByID(_ context.Context, id int) (*domain.Almacen, error) {
	a, ok := r.almacenes[id]
	if !ok {
		return nil, domain.ErrAlmacenNoEncontrado
	}
	return &a, nil
}

func (r *fakeAlmacenRepo) ListAll(_ context.Context) ([]domain.Almacen, error) {
	out := make([]domain.Almacen, 0, len(r.almacenes))
	for _, a := range r.almacenes {
		out = append(out, a)
	}
	return out, nil
}

// ─── helpers ────────────────────────────────────────────────────────────────

var errSentinel = errors.New("sentinel error")

func mustDecimal(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

func isAppError(err error, code string) bool {
	appErr, ok := apperror.As(err)
	return ok && appErr.Code == code
}
