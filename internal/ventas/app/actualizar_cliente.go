//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ActualizarClienteInput is the request DTO for editing a venta's cliente
// snapshot and optional cliente_id link.
type ActualizarClienteInput struct {
	VentaID       uuid.UUID
	ClienteID     *int
	ClienteNombre string
	ClienteTel    *string
	ClienteAval   *string
}

// ActualizarCliente edits the cliente snapshot + cliente_id link of a venta
// in StatusBorrador.
func (s *Service) ActualizarCliente(ctx context.Context, in ActualizarClienteInput, by uuid.UUID) (*domain.Venta, error) {
	if err := s.validateClienteID(ctx, in.ClienteID); err != nil {
		return nil, err
	}
	venta, err := s.ventas.FindByID(ctx, in.VentaID)
	if err != nil {
		return nil, err
	}
	cliente, err := buildClienteSnapshot(CrearVentaInput{
		ClienteNombre: in.ClienteNombre,
		ClienteTel:    in.ClienteTel,
		ClienteAval:   in.ClienteAval,
	})
	if err != nil {
		return nil, err
	}
	if err := venta.ActualizarCliente(domain.ActualizarClienteParams{
		ClienteID: in.ClienteID,
		Cliente:   cliente,
		By:        by,
		Now:       s.clock.Now(),
	}); err != nil {
		return nil, err
	}
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		return s.ventas.UpdateCliente(ctx, venta)
	}); err != nil {
		return nil, err
	}
	s.drainEvents(ctx, venta)
	return venta, nil
}
