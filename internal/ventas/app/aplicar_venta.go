package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// AplicarVenta materializes an approved MSP venta into Microsip's DOCTOS_PV
// ledger. The full write (Microsip INSERTs + cascade flip + MSP header Update)
// runs inside a single transaction so they are atomic.
//
// Idempotency: if the venta is already aplicada the existing artifact triple
// (DoctoPVID, Folio, AplicadaAt) is returned unchanged without calling the
// Microsip writer again.
//
// Concurrency: the transaction first takes a pessimistic row lock on the venta
// (repo.LockByID → SELECT ... WITH LOCK). Two concurrent applies on the same
// venta serialize there — the second blocks until the first commits, then
// re-reads and hits the idempotent fast-path (already aplicada → returns the
// existing artifacts). This prevents a double-submit from materializing two
// DOCTOS_PV. The Idempotency-Key middleware is complementary, not the guard.
func (s *Service) AplicarVenta(ctx context.Context, ventaID, by uuid.UUID) (*domain.Venta, error) {
	var venta *domain.Venta

	if err := s.runInTx(ctx, func(ctx context.Context) error {
		if err := s.ventas.LockByID(ctx, ventaID); err != nil {
			return err
		}
		v, err := s.ventas.FindByID(ctx, ventaID)
		if err != nil {
			return err
		}
		if err := checkPreconditions(v); err != nil {
			return err
		}
		// Idempotency: already aplicada → return as-is without re-materializing.
		if v.IsAplicada() {
			venta = v
			return nil
		}

		writerIn, err := s.buildWriterInput(ctx, v)
		if err != nil {
			return err
		}
		res, err := s.microsipWriter.Aplicar(ctx, writerIn)
		if err != nil {
			return err
		}
		if err := v.MarcarAplicada(res.DoctoPVID, res.Folio, s.clock.Now(), by); err != nil {
			return err
		}
		if err := s.ventas.Update(ctx, v); err != nil {
			return err
		}
		venta = v
		return nil
	}); err != nil {
		return nil, err
	}

	s.drainEvents(ctx, venta)
	return venta, nil
}

// checkPreconditions validates the state machine invariants before attempting
// materialization.
func checkPreconditions(v *domain.Venta) error {
	if v.Estado() != domain.EstadoActive {
		return domain.ErrVentaNoActiva
	}
	if v.IsAplicada() {
		return nil // idempotent fast-path; handled by the caller.
	}
	if v.Situacion() != domain.SituacionAprobada {
		return domain.ErrVentaNoAplicable
	}
	if v.ClienteID() == nil {
		return domain.ErrVentaSinClienteMicrosip
	}
	if v.Direccion().ZonaClienteID() == nil {
		return domain.ErrVentaSinZona
	}
	return nil
}

// buildWriterInput resolves all Microsip config IDs needed by the writer.
func (s *Service) buildWriterInput(ctx context.Context, v *domain.Venta) (outbound.MicrosipVentaInput, error) {
	zona := *v.Direccion().ZonaClienteID()
	cc, err := s.aplicarCfg.CajaCajero(ctx, zona)
	if err != nil {
		return outbound.MicrosipVentaInput{}, err
	}
	defs, err := s.aplicarCfg.Defaults(ctx)
	if err != nil {
		return outbound.MicrosipVentaInput{}, err
	}

	formaCobroID := defs.FormaCobroContadoID
	if v.TipoVenta() == domain.TipoVentaCredito {
		formaCobroID = defs.FormaCobroCreditoID
	}

	formaDePagoID, creditoEnMesesID, err := s.resolveCreditoIDs(ctx, v)
	if err != nil {
		return outbound.MicrosipVentaInput{}, err
	}

	numVendedoresID, err := s.aplicarCfg.NumeroDeVendedoresID(ctx, v.VendedoresCount())
	if err != nil {
		return outbound.MicrosipVentaInput{}, err
	}

	return outbound.MicrosipVentaInput{
		Venta:                v,
		CajaID:               cc.CajaID,
		CajeroID:             cc.CajeroID,
		VendedorID:           cc.VendedorID,
		SucursalID:           defs.SucursalID,
		FormaCobroID:         formaCobroID,
		FormaDePagoID:        formaDePagoID,
		CreditoEnMesesID:     creditoEnMesesID,
		NumeroDeVendedoresID: numVendedoresID,
	}, nil
}

// resolveCreditoIDs looks up the forma_de_pago and credito_en_meses list IDs
// for CREDITO ventas; returns nil pointers for CONTADO ventas.
//
//nolint:nonamedreturns // multi-arity tuple is clearer when named.
func (s *Service) resolveCreditoIDs(ctx context.Context, v *domain.Venta) (formaDePagoID, creditoEnMesesID *int, err error) {
	if v.TipoVenta() != domain.TipoVentaCredito || v.PlanCredito() == nil {
		return nil, nil, nil //nolint:nilnil // both are optional pointer returns.
	}
	plan := v.PlanCredito()
	fpID, fpErr := s.aplicarCfg.FormaDePagoID(ctx, plan.FrecPago().String())
	if fpErr != nil {
		return nil, nil, fpErr
	}
	cmID, cmErr := s.aplicarCfg.CreditoEnMesesID(ctx, plan.PlazoMeses())
	if cmErr != nil {
		return nil, nil, cmErr
	}
	return &fpID, &cmID, nil
}
