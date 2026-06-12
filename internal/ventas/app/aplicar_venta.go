//nolint:misspell // ventas vocabulary is Spanish (CLIENTES, LIBRES_CLIENTES, etc.) per project convention.
package app

import (
	"context"
	"strconv"

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

		// Auto-create cliente in Microsip when the venta has no ClienteID yet
		// but carries enough snapshot data (nombre + dirección postal). The
		// new CLIENTE_ID is linked back to the venta within the same tx.
		if err := s.autoCrearClienteSiNecesario(ctx, v, by); err != nil {
			return err
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
	// Zona must be set before checking ClienteID: if both are missing, the
	// caller sees ErrVentaSinZona first (the more actionable error).
	if v.Direccion().ZonaClienteID() == nil {
		return domain.ErrVentaSinZona
	}
	// ClienteID is nil only when the auto-create branch will run inside the
	// tx. If the venta lacks enough snapshot data to auto-create, reject now
	// rather than failing mid-transaction.
	if v.ClienteID() == nil && !puedeAutoCrearCliente(v) {
		return domain.ErrVentaSinClienteMicrosip
	}
	// Defense in depth: every venta in production must carry evidencia
	// (firma / ID del cliente) before it can hit Microsip. The atomic
	// multipart CrearVentaConImagenes already enforces ≥1 imagen at
	// creation, but a venta created through any other path (admin tool,
	// legacy CrearVenta, manual SQL fixup) would otherwise slip through.
	// We reject here so the auditoría invariant holds end-to-end.
	if v.ImagenesCount() == 0 {
		return domain.ErrVentaEvidenciaRequerida
	}
	return nil
}

// puedeAutoCrearCliente reports whether the venta carries enough snapshot
// data to auto-create the cliente in Microsip during AplicarVenta. Required:
// nombre cliente (always set at CrearVenta time), and a non-empty postal
// dirección (calle + colonia + poblacion). Zona is checked separately above.
func puedeAutoCrearCliente(v *domain.Venta) bool {
	if v.Cliente().Nombre().IsZero() {
		return false
	}
	d := v.Direccion()
	if d.Calle() == "" || d.Colonia() == "" || d.Poblacion() == "" {
		return false
	}
	return true
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

	vendedorListaIDs, err := s.resolveVendedorListaIDs(ctx, v)
	if err != nil {
		return outbound.MicrosipVentaInput{}, err
	}

	return outbound.MicrosipVentaInput{
		Venta:                v,
		CajaID:               cc.CajaID,
		CajeroID:             cc.CajeroID,
		VendedorID:           cc.VendedorID,
		VendedorListaIDs:     vendedorListaIDs,
		SucursalID:           defs.SucursalID,
		FormaCobroID:         formaCobroID,
		FormaDePagoID:        formaDePagoID,
		CreditoEnMesesID:     creditoEnMesesID,
		NumeroDeVendedoresID: numVendedoresID,
	}, nil
}

// resolveVendedorListaIDs maps the venta's vendedores (in order) to the
// LIBRES_CARGOS_CC.VENDEDOR_1/2/3 columns. The seller in slot k (0-based)
// contributes its own LISTA_ATRIB_ID for atributo 19985+k. Slots beyond the
// venta's seller count, or sellers without a mapping, stay at the sentinel -1.
func (s *Service) resolveVendedorListaIDs(ctx context.Context, v *domain.Venta) ([3]int, error) {
	listaIDs := [3]int{-1, -1, -1}
	k := 0
	for vd := range v.Vendedores() {
		if k >= len(listaIDs) {
			break
		}
		ids, err := s.aplicarCfg.VendedorListaIDs(ctx, vd.UsuarioID())
		if err != nil {
			return [3]int{-1, -1, -1}, err
		}
		listaIDs[k] = ids[k]
		k++
	}
	return listaIDs, nil
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

// autoCrearClienteSiNecesario runs the auto-create-cliente branch when the
// venta has no ClienteID. It is a no-op when ClienteID is already set.
func (s *Service) autoCrearClienteSiNecesario(ctx context.Context, v *domain.Venta, by uuid.UUID) error {
	if v.ClienteID() != nil {
		return nil
	}
	if s.microsipCliente == nil {
		return domain.ErrVentaSinClienteMicrosip
	}
	zona := *v.Direccion().ZonaClienteID()
	cc, err := s.aplicarCfg.CajaCajero(ctx, zona)
	if err != nil {
		return err
	}
	in := buildAutoCreateClienteInput(v, cc)
	res, err := s.microsipCliente.Crear(ctx, in)
	if err != nil {
		return err
	}
	if err := v.AsignarClienteMicrosip(res.ClienteID, by); err != nil {
		return err
	}
	return s.ventas.UpdateCliente(ctx, v)
}

// buildAutoCreateClienteInput materializes a MicrosipClienteInput from the venta's
// snapshot + the zona's caja config + the hardcoded catálogo defaults from
// the outbound package. This is only called inside AplicarVenta's auto-create
// branch (when v.ClienteID() is nil).
func buildAutoCreateClienteInput(v *domain.Venta, cc outbound.CajaCajero) outbound.MicrosipClienteInput {
	dir := v.Direccion()
	gps := v.GPS()

	in := outbound.MicrosipClienteInput{
		Nombre:                  v.Cliente().Nombre().Value(),
		Calle:                   dir.Calle(),
		NumeroExterior:          dir.NumeroExterior(),
		Colonia:                 dir.Colonia(),
		Poblacion:               dir.Poblacion(),
		ZonaClienteID:           *dir.ZonaClienteID(),
		CobradorID:              cc.CobradorID,
		VendedorID:              cc.VendedorID,
		CiudadID:                outbound.DefaultCiudadID,
		EstadoID:                outbound.DefaultEstadoID,
		PaisID:                  outbound.DefaultPaisID,
		CondPagoID:              outbound.DefaultCondPagoID,
		TipoClienteID:           outbound.DefaultTipoClienteID,
		MonedaID:                outbound.DefaultMonedaID,
		ViaEmbarqueID:           outbound.DefaultViaEmbarqueID,
		ComprobanteDomicilioID:  outbound.DefaultComprobanteDomicilioID,
		IdentificacionOficialID: outbound.DefaultIdentificacionOficialID,
	}

	if tel := v.Cliente().Telefono(); tel != nil {
		s := tel.Value()
		in.Telefono = &s
	}

	// GPS as string lat/lng for LIBRES_CLIENTES.U_LATITUD / U_LONGITUD.
	// GPSCoords zero-value is (0,0); treat that as "not set" — both lat and
	// lng must be exactly zero. (Sales near the equator/Greenwich are not
	// a realistic risk in Tehuacán.)
	if gps.Latitud() != 0 || gps.Longitud() != 0 {
		lat := strconv.FormatFloat(gps.Latitud(), 'f', -1, 64)
		lng := strconv.FormatFloat(gps.Longitud(), 'f', -1, 64)
		in.Latitud = &lat
		in.Longitud = &lng
	}

	if ref := v.Cliente().Referencia(); ref != nil {
		in.Referencia = ref
	}

	return in
}
