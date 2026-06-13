//nolint:misspell // ventas vocabulary is Spanish (productos, etc.) per project convention.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ActualizarHeaderInput is the request DTO for editing a venta's header
// fields. Mirrors the editable subset — TipoVenta, ClienteID, almacenes and
// child collections are intentionally absent. Montos (header totals) are
// excluded because they are derived automatically from line items by the
// domain; callers must use the reemplazar endpoints to change prices.
type ActualizarHeaderInput struct {
	VentaID        uuid.UUID
	Calle          string
	NumeroExterior *string
	Colonia        string
	Poblacion      string
	Ciudad         string
	ZonaClienteID  *int
	Latitud        float64
	Longitud       float64
	FechaVenta     time.Time
	PlanCredito    *CrearVentaPlanCreditoInput
	DiaCobranza    *CrearVentaDiaCobranzaInput
	Nota           *string
}

// ActualizarHeader edits the header fields of a venta in StatusBorrador.
func (s *Service) ActualizarHeader(ctx context.Context, in ActualizarHeaderInput, by uuid.UUID) (*domain.Venta, error) {
	venta, err := s.ventas.FindByID(ctx, in.VentaID)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	params, err := in.intoDomain(by, now)
	if err != nil {
		return nil, err
	}
	if err := venta.ActualizarHeader(params); err != nil {
		return nil, err
	}
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		return s.ventas.UpdateHeader(ctx, venta)
	}); err != nil {
		return nil, err
	}
	s.drainEvents(ctx, venta)
	return venta, nil
}

// intoDomain constructs the domain.ActualizarHeaderParams from primitives.
func (in ActualizarHeaderInput) intoDomain(by uuid.UUID, now time.Time) (domain.ActualizarHeaderParams, error) {
	gps, err := domain.NewGPSCoords(in.Latitud, in.Longitud)
	if err != nil {
		return domain.ActualizarHeaderParams{}, err
	}
	dir, err := domain.NewDireccion(domain.NewDireccionParams{
		Calle:          in.Calle,
		NumeroExterior: in.NumeroExterior,
		Colonia:        in.Colonia,
		Poblacion:      in.Poblacion,
		Ciudad:         in.Ciudad,
		ZonaClienteID:  in.ZonaClienteID,
	})
	if err != nil {
		return domain.ActualizarHeaderParams{}, err
	}
	plan, err := buildOptionalPlanCredito(in.PlanCredito)
	if err != nil {
		return domain.ActualizarHeaderParams{}, err
	}
	dia, err := buildOptionalDiaCobranza(in.DiaCobranza)
	if err != nil {
		return domain.ActualizarHeaderParams{}, err
	}
	return domain.ActualizarHeaderParams{
		Direccion:   dir,
		GPS:         gps,
		FechaVenta:  in.FechaVenta,
		PlanCredito: plan,
		DiaCobranza: dia,
		Nota:        in.Nota,
		By:          by,
		Now:         now,
	}, nil
}
