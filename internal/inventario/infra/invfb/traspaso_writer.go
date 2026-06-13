//nolint:misspell // Microsip column names are Spanish by convention.
package invfb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
	"github.com/abdimuy/msp-api/internal/inventario/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// sentinel errors for the TraspasoRepo write path.
var (
	errClaveArticuloNotFound = apperror.NewInternal(
		"clave_articulo_not_found",
		"no se encontró la clave del artículo en microsip",
	)
	errTraspasoVentaIDNil = apperror.NewInternal(
		"traspaso_venta_id_nil",
		"el traspaso no tiene venta_id asignado; no se puede guardar sin venta",
	)
)

// TraspasoRepo implements [outbound.TraspasoRepo] against Microsip's Firebird
// tables (DOCTOS_IN + DOCTOS_IN_DET + SUB_MOVTOS_IN) and the MSP_VENTAS_TRASPASOS
// lookup table. All writes execute through the caller's ambient transaction via
// [firebird.GetQuerier].
type TraspasoRepo struct {
	pool *firebird.Pool
	cfg  config.Inventario
}

// NewTraspasoRepo wires a TraspasoRepo to the given pool and inventario config.
func NewTraspasoRepo(cfg config.Inventario, pool *firebird.Pool) *TraspasoRepo {
	return &TraspasoRepo{pool: pool, cfg: cfg}
}

// Compile-time check.
var _ outbound.TraspasoRepo = (*TraspasoRepo)(nil)

// Save materializes a Traspaso into Microsip following the 6-step recipe:
//
//  1. Resolve clave_articulo per detalle (cached within call).
//  2. INSERT DOCTOS_IN with RETURNING DOCTO_IN_ID.
//  3. For each detalle: INSERT salida det, INSERT entrada det, INSERT 2 SUB_MOVTOS_IN rows.
//  4. EXECUTE PROCEDURE aplica_docto_in(doctoInID).
//  5. INSERT MSP_VENTAS_TRASPASOS lookup row (only when VentaID is set).
//
// The method does NOT commit or rollback; it executes within the caller's
// ambient transaction obtained via firebird.GetQuerier.
//
//nolint:funlen,cyclop // multi-step Microsip write recipe; each step is distinct.
func (r *TraspasoRepo) Save(ctx context.Context, t *domain.Traspaso) (int, error) {
	if t.VentaID() == nil {
		return 0, errTraspasoVentaIDNil
	}

	q := firebird.GetQuerier(ctx, r.pool.DB)

	// ── Resolve clave_articulo for each detalle (cached) ─────────────────────
	claves, err := r.resolveClaves(ctx, q, t)
	if err != nil {
		return 0, fmt.Errorf("invfb Save: resolve claves: %w", err)
	}

	// ── Derive timestamps from the aggregate's audit data ────────────────────
	fechaWC := firebird.ToWallClock(t.Fecha())
	audit := t.Audit()
	createdAtWC := firebird.ToWallClock(audit.CreatedAt())
	// MSP_VENTAS_TRASPASOS.CREATED_BY is CHAR(36) — holds the real UUID.
	createdBy := audit.CreatedBy().String()
	// DOCTOS_IN.USUARIO_CREADOR is Microsip's narrower user-name column
	// (~31 chars) and rejects a 36-char UUID with "string truncation". We
	// stamp the stable service identity used by the legacy Node API
	// (USUARIO_DEFAULT="SYSDBA") and capture the real actor in
	// MSP_VENTAS_TRASPASOS instead.
	const microsipUsuarioCreador = "SYSDBA"

	// ── Step 1: INSERT DOCTOS_IN ──────────────────────────────────────────────
	var doctoInID int
	err = q.QueryRowContext(ctx, insertDoctoIn,
		// ALMACEN_ID, CONCEPTO_IN_ID, SUCURSAL_ID
		t.AlmacenOrigen(), r.cfg.ConceptoInSalidaID, r.cfg.SucursalID,
		// FOLIO, NATURALEZA_CONCEPTO, FECHA, ALMACEN_DESTINO_ID
		t.Folio().Value(), naturalezaSalida, fechaWC, t.AlmacenDestino(),
		// CANCELADO, APLICADO, DESCRIPCION
		flagNo, flagSi, t.Descripcion(),
		// FORMA_EMITIDA, CONTABILIZADO, SISTEMA_ORIGEN
		flagNo, flagNo, sistemaOrigen,
		// USUARIO_CREADOR, FECHA_HORA_CREACION
		microsipUsuarioCreador, createdAtWC,
		// USUARIO_ULT_MODIF, FECHA_HORA_ULT_MODIF
		microsipUsuarioCreador, createdAtWC,
	).Scan(&doctoInID)
	if err != nil {
		return 0, fmt.Errorf("invfb Save: insert DOCTOS_IN: %w", firebird.MapError(err))
	}

	// ── Step 2: INSERT DOCTOS_IN_DET + SUB_MOVTOS_IN per detalle ─────────────
	for _, det := range t.DetallesForRepo() {
		clave := claves[det.ArticuloID()]
		cantidad := det.Cantidad().Value().StringFixed(existenciaScale)

		// Salida row
		var salidaID int
		err = q.QueryRowContext(ctx, insertDoctoInDet,
			// DOCTO_IN_ID, ALMACEN_ID, CONCEPTO_IN_ID
			doctoInID, t.AlmacenOrigen(), r.cfg.ConceptoInSalidaID,
			// CLAVE_ARTICULO, ARTICULO_ID, TIPO_MOVTO, UNIDADES
			clave, det.ArticuloID(), tipoMovtoSalida, cantidad,
			// METODO_COSTEO, CANCELADO, APLICADO, COSTEO_PEND, PEDIMENTO_PEND, ROL, FECHA
			metodoCosteo, flagNo, flagSi, flagSi, pedimentoPend, rolSalida, fechaWC,
		).Scan(&salidaID)
		if err != nil {
			return 0, fmt.Errorf("invfb Save: insert DOCTOS_IN_DET salida articulo_id=%d: %w",
				det.ArticuloID(), firebird.MapError(err))
		}

		// Entrada row
		var entradaID int
		err = q.QueryRowContext(ctx, insertDoctoInDet,
			// DOCTO_IN_ID, ALMACEN_ID, CONCEPTO_IN_ID
			doctoInID, t.AlmacenDestino(), r.cfg.ConceptoInEntradaID,
			// CLAVE_ARTICULO, ARTICULO_ID, TIPO_MOVTO, UNIDADES
			clave, det.ArticuloID(), tipoMovtoEntrada, cantidad,
			// METODO_COSTEO, CANCELADO, APLICADO, COSTEO_PEND, PEDIMENTO_PEND, ROL, FECHA
			metodoCosteo, flagNo, flagSi, flagSi, pedimentoPend, rolEntrada, fechaWC,
		).Scan(&entradaID)
		if err != nil {
			return 0, fmt.Errorf("invfb Save: insert DOCTOS_IN_DET entrada articulo_id=%d: %w",
				det.ArticuloID(), firebird.MapError(err))
		}

		// SUB_MOVTOS_IN: salida → entrada
		if _, err := q.ExecContext(ctx, insertSubMovtoIn, salidaID, entradaID); err != nil {
			return 0, fmt.Errorf("invfb Save: insert SUB_MOVTOS_IN salida→entrada: %w", firebird.MapError(err))
		}
		// SUB_MOVTOS_IN: entrada → salida
		if _, err := q.ExecContext(ctx, insertSubMovtoIn, entradaID, salidaID); err != nil {
			return 0, fmt.Errorf("invfb Save: insert SUB_MOVTOS_IN entrada→salida: %w", firebird.MapError(err))
		}
	}

	// ── Step 3: EXECUTE PROCEDURE aplica_docto_in ────────────────────────────
	if _, err := q.ExecContext(ctx, executeAplicaDoctoIn, doctoInID); err != nil {
		return 0, fmt.Errorf("invfb Save: aplica_docto_in(%d): %w", doctoInID, firebird.MapError(err))
	}

	// ── Step 4: INSERT MSP_VENTAS_TRASPASOS ───────────────────────────────────
	tipo := tipoTraspasoDirecto
	if t.TipoReverso() {
		tipo = tipoTraspasoReverso
	}
	// New traspasos (both directos and freshly-created reversos) always start
	// as not-reversed. REVERSADO is set to 'S' later via MarcarDirectoReversado
	// when the directo is superseded by an edit cycle.
	if _, err := q.ExecContext(ctx, insertVentaTraspaso,
		t.ID().String(), t.VentaID().String(), doctoInID, tipo, t.Folio().Value(),
		t.AlmacenOrigen(), t.AlmacenDestino(), createdAtWC, createdBy,
		reversadoNo,
	); err != nil {
		return 0, fmt.Errorf("invfb Save: insert MSP_VENTAS_TRASPASOS: %w", firebird.MapError(err))
	}

	return doctoInID, nil
}

// MarcarDirectoReversado sets REVERSADO='S' on the MSP_VENTAS_TRASPASOS row
// for the given DOCTO_IN_ID (TIPO='directo' only). It executes within the
// caller's ambient transaction via firebird.GetQuerier, matching the pattern
// used by Save.
func (r *TraspasoRepo) MarcarDirectoReversado(ctx context.Context, doctoInID int) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	if _, err := q.ExecContext(ctx, updateVentaTraspasoReversado, reversadoSi, doctoInID, tipoTraspasoDirecto); err != nil {
		return fmt.Errorf("invfb MarcarDirectoReversado docto_in_id=%d: %w", doctoInID, firebird.MapError(err))
	}
	return nil
}

// resolveClaves returns a map[articuloID]claveArticulo for all detalles.
// It makes one DB round-trip per unique articuloID (typically 1-5 in a real
// traspaso), caching within the call so repeated articulos are looked up once.
func (r *TraspasoRepo) resolveClaves(
	ctx context.Context,
	q firebird.Querier,
	t *domain.Traspaso,
) (map[int]string, error) {
	cache := make(map[int]string)
	for _, det := range t.DetallesForRepo() {
		aid := det.ArticuloID()
		if _, ok := cache[aid]; ok {
			continue
		}
		var clave string
		err := q.QueryRowContext(ctx, selectClaveArticulo, claveArticuloRolID, aid).Scan(&clave)
		if errors.Is(err, sql.ErrNoRows) {
			slog.Warn("invfb: clave_articulo not found",
				slog.Int("articulo_id", aid),
				slog.Int("rol_clave_art_id", claveArticuloRolID),
			)
			return nil, errClaveArticuloNotFound.WithField("articulo_id", aid)
		}
		if err != nil {
			return nil, fmt.Errorf("invfb: lookup clave_articulo articulo_id=%d: %w", aid, firebird.MapError(err))
		}
		cache[aid] = clave
	}
	return cache, nil
}
