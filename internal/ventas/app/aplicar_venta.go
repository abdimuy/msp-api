//nolint:misspell // ventas vocabulary is Spanish (CLIENTES, LIBRES_CLIENTES, etc.) per project convention.
package app

import (
	"context"
	"log/slog"
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
		if err := checkPreconditions(v, s.zonaObligatoria); err != nil {
			return err
		}
		// Idempotency: already aplicada → return as-is without re-materializing.
		if v.IsAplicada() {
			venta = v
			return nil
		}

		// Snapshot whether the venta already had a ClienteID before the
		// auto-create step. Auto-created clientes inherit the venta's zona
		// by construction, so the zona mismatch check only applies when a
		// pre-existing Microsip cliente was linked at create time.
		clienteIDPreExistente := v.ClienteID()

		// Auto-create cliente in Microsip when the venta has no ClienteID yet
		// but carries enough snapshot data (nombre + dirección postal). The
		// new CLIENTE_ID is linked back to the venta within the same tx.
		if err := s.autoCrearClienteSiNecesario(ctx, v, by); err != nil {
			return err
		}

		if err := s.validarZonaClienteMicrosipPreExistente(ctx, v, clienteIDPreExistente); err != nil {
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
		if err := s.reactivarClienteSiNecesario(ctx, v, res); err != nil {
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
func checkPreconditions(v *domain.Venta, zonaObligatoria bool) error {
	if v.Estado() != domain.EstadoActive {
		return domain.ErrVentaNoActiva
	}
	if v.IsAplicada() {
		return nil // idempotent fast-path; handled by the caller.
	}
	if v.Situacion() != domain.SituacionAprobada {
		return domain.ErrVentaNoAplicable
	}
	// Zona resolves the caja. With zonaObligatoria on it is required for every
	// venta type; with it off, only CREDITO needs it — CONTADO falls back to
	// the fixed mostrador caja so already-captured ventas can still drain.
	if zonaObligatoria || v.TipoVenta() == domain.TipoVentaCredito {
		if v.Direccion().ZonaClienteID() == nil {
			return domain.ErrVentaSinZona
		}
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
	defs, err := s.aplicarCfg.Defaults(ctx)
	if err != nil {
		return outbound.MicrosipVentaInput{}, err
	}

	cc, err := s.resolveCajaCajero(ctx, v)
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

	juegosPorCombo, err := s.resolveJuegosPorCombo(ctx, v)
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
		JuegosPorCombo:       juegosPorCombo,
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

	cc, err := s.resolveCajaCajero(ctx, v)
	if err != nil {
		return err
	}

	ciudad, err := s.resolveCiudad(ctx, v)
	if err != nil {
		return err
	}

	in := buildAutoCreateClienteInput(v, cc, ciudad)
	res, err := s.microsipCliente.Crear(ctx, in)
	if err != nil {
		return err
	}
	if err := v.AsignarClienteMicrosip(res.ClienteID, by); err != nil {
		return err
	}
	return s.ventas.UpdateCliente(ctx, v)
}

// resolveCajaCajero is the SINGLE place that decides which caja/cajero a venta
// posts to. Both call sites — the writer input and the cliente auto-create
// branch — go through it; they used to carry separate copies of the rule that
// could drift apart.
//
// With zonaObligatoria on, the zona mapping (MSP_CFG_ZONA_CAJA) is the only
// source for every venta type. With it off, CONTADO still falls back to the
// fixed mostrador caja from MSP_CFG_APLICAR so the backlog of ventas captured
// without a zona can drain.
//
// The nil-zona check is not redundant with checkPreconditions: both previous
// copies dereferenced ZonaClienteID() unguarded, so any future caller reaching
// here without a zona got a nil-pointer panic instead of an error.
func (s *Service) resolveCajaCajero(ctx context.Context, v *domain.Venta) (outbound.CajaCajero, error) {
	porZona := s.zonaObligatoria || v.TipoVenta() != domain.TipoVentaContado
	if porZona {
		zona := v.Direccion().ZonaClienteID()
		if zona == nil {
			return outbound.CajaCajero{}, domain.ErrVentaSinZona
		}
		return s.aplicarCfg.CajaCajero(ctx, *zona)
	}
	defs, err := s.aplicarCfg.Defaults(ctx)
	if err != nil {
		return outbound.CajaCajero{}, err
	}
	return contadoCajaCajeroFromDefaults(defs)
}

// contadoCajaCajeroFromDefaults builds the fixed mostrador CajaCajero from
// AplicarDefaults. Returns domain.ErrConfigCajaContadoFaltante when
// CAJA_CONTADO_ID / CAJERO_CONTADO_ID are NULL (sentinel -1 from the repo).
func contadoCajaCajeroFromDefaults(defs outbound.AplicarDefaults) (outbound.CajaCajero, error) {
	if defs.CajaContadoID < 0 || defs.CajeroContadoID < 0 {
		return outbound.CajaCajero{}, domain.ErrConfigCajaContadoFaltante
	}
	return outbound.CajaCajero{
		CajaID: defs.CajaContadoID, CajeroID: defs.CajeroContadoID,
		VendedorID: -1, CobradorID: -1,
	}, nil
}

// validarZonaClienteMicrosipPreExistente checks that the venta's zona matches
// the ZONA_CLIENTE_ID of a pre-existing Microsip cliente. clienteIDPreExistente
// is the ClienteID value captured BEFORE autoCrearClienteSiNecesario runs: when
// nil, the auto-create branch ran and the new cliente inherits the venta's zona
// — so the check is skipped. Returns nil when zonaReader is not wired.
// Contado ventas skip the check entirely — they have no zona constraint.
// When the repo returns nil (cliente exists but ZONA_CLIENTE_ID is NULL), the
// check is also skipped — a NULL zona means "no zona constraint" on the cliente.
func (s *Service) validarZonaClienteMicrosipPreExistente(ctx context.Context, v *domain.Venta, clienteIDPreExistente *int) error {
	if v.TipoVenta() == domain.TipoVentaContado {
		return nil
	}
	if clienteIDPreExistente == nil || s.zonaReader == nil {
		return nil
	}
	zonaPtr, err := s.zonaReader.ZonaDeCliente(ctx, *clienteIDPreExistente)
	if err != nil {
		return err
	}
	if zonaPtr == nil {
		// NULL zona in Microsip: no zona constraint on this cliente — skip check.
		return nil
	}
	return v.ValidarZonaCoincideMicrosip(*zonaPtr)
}

// buildAutoCreateClienteInput materializes a MicrosipClienteInput from the venta's
// snapshot + the zona's caja config + the hardcoded catálogo defaults from
// the outbound package. This is only called inside AplicarVenta's auto-create
// branch (when v.ClienteID() is nil).
// resolveCiudad turns the captured ciudad text into catalog IDs.
//
// With the flag off it returns the fixed Tehuacán/Puebla defaults — the
// pre-existing behavior, which discards whatever the vendor captured.
//
// With the flag on the captured text must match a CIUDADES row; a miss blocks
// the apply with ErrCiudadNoEnCatalogo instead of quietly filing the cliente
// under Tehuacán. The venta stays capturable and the office resolves it.
//
// The estado always comes from the SAME row as the ciudad.
func (s *Service) resolveCiudad(ctx context.Context, v *domain.Venta) (outbound.CiudadResuelta, error) {
	defaults := outbound.CiudadResuelta{
		CiudadID: outbound.DefaultCiudadID,
		EstadoID: outbound.DefaultEstadoID,
	}
	if !s.ciudadCatalogoEnabled {
		return defaults, nil
	}

	capturada := v.Direccion().Ciudad()
	if domain.NormalizeCiudad(capturada) == "" {
		return outbound.CiudadResuelta{}, domain.ErrCiudadNoEnCatalogo
	}

	match, err := s.ciudadCatalogo.Resolver(ctx, capturada)
	if err != nil {
		return outbound.CiudadResuelta{}, err
	}
	if match == nil {
		slog.WarnContext(ctx, "venta.ciudad_no_en_catalogo",
			slog.String("venta_id", v.ID().String()),
			slog.String("ciudad_capturada", capturada),
		)
		return outbound.CiudadResuelta{}, domain.ErrCiudadNoEnCatalogo
	}
	return *match, nil
}

func buildAutoCreateClienteInput(
	v *domain.Venta, cc outbound.CajaCajero, ciudad outbound.CiudadResuelta,
) outbound.MicrosipClienteInput {
	dir := v.Direccion()
	gps := v.GPS()

	// Use the sentinel -1 when zona is nil (contado ventas have no zona).
	zonaID := -1
	if dir.ZonaClienteID() != nil {
		zonaID = *dir.ZonaClienteID()
	}

	in := outbound.MicrosipClienteInput{
		Nombre:                  v.Cliente().Nombre().Value(),
		Calle:                   dir.Calle(),
		NumeroExterior:          dir.NumeroExterior(),
		Colonia:                 dir.Colonia(),
		Poblacion:               dir.Poblacion(),
		ZonaClienteID:           zonaID,
		CobradorID:              cc.CobradorID,
		VendedorID:              cc.VendedorID,
		CiudadID:                ciudad.CiudadID,
		EstadoID:                ciudad.EstadoID,
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

// resolveJuegosPorCombo iterates the venta's combos and calls the juego
// resolver once per combo to obtain the ARTICULOS.ARTICULO_ID for the
// matching (or newly created) juego. The call executes within the caller's
// ambient transaction so juego creation and the DOCTOS_PV write are atomic.
//
// Returns nil (no error) when the feature is off (resolver nil or
// juegosEnabled false), or when the venta has no combos.
// Any resolver error propagates immediately so AplicarVenta rolls back.
func (s *Service) resolveJuegosPorCombo(ctx context.Context, v *domain.Venta) (map[uuid.UUID]int, error) {
	if s.juegoResolver == nil || !s.juegosEnabled {
		return nil, nil //nolint:nilnil // both nil means "feature off"; the caller treats nil map as empty.
	}
	result := make(map[uuid.UUID]int)
	for combo := range v.Combos() {
		receta, err := v.RecetaDeCombo(combo.ID())
		if err != nil {
			return nil, err
		}
		res, err := s.juegoResolver.Resolve(ctx, outbound.MicrosipJuegoInput{
			Receta:          receta,
			NombrePropuesto: combo.Nombre(),
			LineaArticuloID: s.juegosLineaArticuloID,
		})
		if err != nil {
			return nil, err
		}
		result[combo.ID()] = res.ArticuloID
	}
	return result, nil
}

// reactivarClienteSiNecesario reactivates the venta's Microsip cliente
// (ESTATUS 'B' → 'A') when a CREDITO sale is applied for a cliente currently
// de baja. Runs inside AplicarVenta's ambient transaction, right after the
// Microsip writer commits the DOCTOS_PV/DOCTOS_CC rows — an error here rolls
// back the whole apply, same as any other step in the tx.
//
// Gated by MICROSIP_REACTIVAR_CLIENTE_ENABLED (WithReactivarCliente, default
// off) and scoped to CREDITO ventas only: a CONTADO sale carries no ongoing
// commercial relationship signal strong enough to justify reactivating a
// suspended cliente. Idempotent: the underlying UPDATE guards on
// ESTATUS='B', so a cliente that is already 'A' is a harmless no-op.
func (s *Service) reactivarClienteSiNecesario(ctx context.Context, v *domain.Venta, res outbound.MicrosipVentaResult) error {
	if !s.reactivarClienteEnabled || s.microsipCliente == nil {
		return nil
	}
	if v.TipoVenta() != domain.TipoVentaCredito {
		return nil
	}
	clienteID := v.ClienteID()
	if clienteID == nil {
		return nil
	}

	reactivado, err := s.microsipCliente.ReactivarSiEnBaja(ctx, *clienteID, s.clock.Now())
	if err != nil {
		return err
	}
	if !reactivado {
		slog.DebugContext(ctx, "ventas.cliente_reactivacion_noop",
			"cliente_id", *clienteID,
			"venta_id", v.ID(),
		)
		return nil
	}
	slog.InfoContext(ctx, "ventas.cliente_reactivado",
		"cliente_id", *clienteID,
		"venta_id", v.ID(),
		"docto_pv_id", res.DoctoPVID,
		"folio", res.Folio,
		"rows_affected", 1,
	)
	return nil
}
