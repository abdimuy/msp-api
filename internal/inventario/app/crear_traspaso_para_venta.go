//nolint:misspell // domain vocabulary is Spanish (traspaso, venta, etc.) per project convention.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// CrearTraspasoDetalleInput is one article line in the create-traspaso request.
type CrearTraspasoDetalleInput struct {
	ArticuloID int
	Cantidad   decimal.Decimal
}

// CrearTraspasoParaVentaParams aggregates the fields needed to create a
// traspaso linked to a venta.
type CrearTraspasoParaVentaParams struct {
	VentaID        uuid.UUID
	AlmacenOrigen  int
	AlmacenDestino int
	Fecha          time.Time
	Descripcion    string
	Detalles       []CrearTraspasoDetalleInput
	CreatedBy      uuid.UUID
}

// CrearTraspasoParaVenta builds a Traspaso aggregate, persists it in Microsip
// inside a transaction, and best-effort emits the buffered events to the
// outbox. Returns the persisted aggregate and its Microsip DOCTO_IN_ID.
func (s *Service) CrearTraspasoParaVenta(ctx context.Context, p CrearTraspasoParaVentaParams) (*domain.Traspaso, int, error) {
	var (
		t         *domain.Traspaso
		doctoInID int
	)
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		tr, id, err := s.crearDirecto(ctx, p)
		if err != nil {
			return err
		}
		t = tr
		doctoInID = id
		return nil
	}); err != nil {
		return nil, 0, err
	}
	s.drainEvents(ctx, t)
	return t, doctoInID, nil
}

// crearDirecto is the shared core for building and persisting a single directo
// traspaso. It must be called from within the caller's ambient transaction —
// it does NOT open its own runInTx.
//
// It converts decimal cantidades to domain Cantidad VOs, mints a folio,
// calls domain.CrearTraspaso with tipoReverso=false, persists via Save,
// marks the aggregate applied, and returns the aggregate + its DOCTO_IN_ID.
func (s *Service) crearDirecto(ctx context.Context, p CrearTraspasoParaVentaParams) (*domain.Traspaso, int, error) {
	now := s.clock.Now()

	// Build domain Cantidad VOs.
	detalleInputs := make([]domain.CrearTraspasoDetalleInput, 0, len(p.Detalles))
	for _, d := range p.Detalles {
		cant, err := domain.NewCantidad(d.Cantidad)
		if err != nil {
			return nil, 0, err
		}
		detalleInputs = append(detalleInputs, domain.CrearTraspasoDetalleInput{
			ID:         uuid.New(),
			ArticuloID: d.ArticuloID,
			Cantidad:   cant,
		})
	}

	// Mint folio.
	folio, err := s.folioMinter.MintFolio(ctx)
	if err != nil {
		return nil, 0, err
	}

	ventaID := p.VentaID
	t, err := domain.CrearTraspaso(domain.CrearTraspasoParams{
		ID:             uuid.New(),
		Folio:          folio,
		AlmacenOrigen:  p.AlmacenOrigen,
		AlmacenDestino: p.AlmacenDestino,
		Fecha:          p.Fecha,
		Descripcion:    p.Descripcion,
		VentaID:        &ventaID,
		Detalles:       detalleInputs,
		CreatedBy:      p.CreatedBy,
		Now:            now,
	})
	if err != nil {
		return nil, 0, err
	}

	id, saveErr := s.traspasos.Save(ctx, t)
	if saveErr != nil {
		return nil, 0, saveErr
	}
	if err := t.MarcarAplicado(id); err != nil {
		return nil, 0, err
	}
	return t, id, nil
}
