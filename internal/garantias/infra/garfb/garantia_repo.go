package garfb

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

var _ outbound.GarantiaRepo = (*GarantiaRepo)(nil)

// GarantiaRepo persists and reads warranties and their articles from Firebird.
type GarantiaRepo struct {
	pool *firebird.Pool
}

// NewGarantiaRepo creates a GarantiaRepo backed by the provided Firebird pool.
func NewGarantiaRepo(pool *firebird.Pool) *GarantiaRepo {
	return &GarantiaRepo{
		pool: pool,
	}
}

// Crear inserts a warranty, its articles, and pending events using the active transaction.
func (r *GarantiaRepo) Crear(ctx context.Context, g *domain.Garantia) error {
	tx, err := firebird.RequireTx(ctx)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(
		ctx,
		insertarGarantiaSQL,
		g.ID().String(),
		g.Folio().String(),
		string(g.Origen()),
		g.ClienteID(),
		g.VentaID(),
		estadoCuentaArg(g.EstadoCuenta()),
		string(g.Estado()),
		g.Description(),
		dateArg(g.VigenciaHasta()),
		nullableString(g.Calle()),
		nullableString(g.NumeroExterior()),
		nullableString(g.Colonia()),
		nullableString(g.Localidad()),
		nullableString(g.Ciudad()),
		nullableString(g.CodigoPostal()),
		g.GPSLat(),
		g.GPSLon(),
		g.AbiertoPor(),
		timeArg(g.CerradoEn()),
		firebird.ToWallClock(g.CreatedAt()),
		firebird.ToWallClock(g.UpdatedAt()),
	)
	if err != nil {
		return firebird.MapError(err)
	}

	for _, a := range g.ArticulosForRepo() {
		if err := insertarArticulo(ctx, tx, a); err != nil {
			return err
		}
	}

	for _, e := range g.EventosPendientesForRepo() {
		if err := insertarEvento(ctx, tx, e); err != nil {
			return err
		}
	}

	return nil
}

// Guardar updates a warranty and its articles and inserts pending events using the active transaction.
func (r *GarantiaRepo) Guardar(ctx context.Context, g *domain.Garantia) error {
	tx, err := firebird.RequireTx(ctx)
	if err != nil {
		return err
	}

	result, err := tx.ExecContext(
		ctx,
		actualizarGarantiaSQL,
		string(g.Estado()),
		timeArg(g.CerradoEn()),
		firebird.ToWallClock(g.UpdatedAt()),
		g.ID().String(),
	)
	if err != nil {
		return firebird.MapError(err)
	}

	afectadas, err := result.RowsAffected()
	if err != nil {
		return firebird.MapError(err)
	}

	if afectadas == 0 {
		return domain.ErrGarantiaNoEncontrada
	}

	for _, a := range g.ArticulosForRepo() {
		result, err := tx.ExecContext(
			ctx,
			actualizarArticuloSQL,
			enumPtrArg(a.Ruta()),
			string(a.Etapa()),
			string(a.Ubicacion()),
			enumPtrArg(a.Dictamen()),
			enumPtrArg(a.Desenlace()),
			timeArg(a.CerradoEn()),
			firebird.ToWallClock(a.UpdatedAt()),
			a.ID().String(),
			a.GarantiaID().String(),
		)
		if err != nil {
			return firebird.MapError(err)
		}

		afectadas, err := result.RowsAffected()
		if err != nil {
			return firebird.MapError(err)
		}

		if afectadas == 0 {
			if err := insertarArticulo(ctx, tx, a); err != nil {
				return err
			}
		}
	}

	for _, e := range g.EventosPendientesForRepo() {
		if err := insertarEvento(ctx, tx, e); err != nil {
			return err
		}
	}

	return nil
}

// Obtener returns a warranty by ID.
func (r *GarantiaRepo) Obtener(
	ctx context.Context,
	id uuid.UUID,
) (*domain.Garantia, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	return obtenerGarantia(
		ctx,
		q,
		obtenerGarantiaSQL,
		id.String(),
	)
}

// ObtenerPorFolio returns a warranty by folio.
func (r *GarantiaRepo) ObtenerPorFolio(
	ctx context.Context,
	folio domain.Folio,
) (*domain.Garantia, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	return obtenerGarantia(
		ctx,
		q,
		obtenerGarantiaPorFolioSQL,
		folio.String(),
	)
}

// ObtenerParaActualizar returns a warranty by ID while locking its row in the active transaction.
func (r *GarantiaRepo) ObtenerParaActualizar(
	ctx context.Context,
	id uuid.UUID,
) (*domain.Garantia, error) {
	tx, err := firebird.RequireTx(ctx)
	if err != nil {
		return nil, err
	}

	return obtenerGarantia(
		ctx,
		tx,
		obtenerGarantiaParaActualizarSQL,
		id.String(),
	)
}

func obtenerGarantia(
	ctx context.Context,
	q firebird.Querier,
	query string,
	arg any,
) (*domain.Garantia, error) {
	row := q.QueryRowContext(ctx, query, arg)

	gr, err := scanGarantia(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrGarantiaNoEncontrada
		}

		return nil, err
	}

	articulos, err := listarArticulos(ctx, q, gr.id)
	if err != nil {
		return nil, err
	}

	return hydrateGarantia(gr, articulos)
}

func listarArticulos(
	ctx context.Context,
	q firebird.Querier,
	garantiaID string,
) ([]*domain.Articulo, error) {
	rows, err := q.QueryContext(
		ctx,
		listarArticulosPorGarantiaSQL,
		garantiaID,
	)
	if err != nil {
		return nil, firebird.MapError(err)
	}

	defer func() {
		_ = rows.Close()
	}()

	var articulos []*domain.Articulo

	for rows.Next() {
		ar, err := scanArticulo(rows)
		if err != nil {
			return nil, err
		}

		a, err := hydrateArticulo(ar)
		if err != nil {
			return nil, err
		}

		articulos = append(articulos, a)
	}

	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}

	return articulos, nil
}

func insertarArticulo(
	ctx context.Context,
	q firebird.Querier,
	a *domain.Articulo,
) error {
	_, err := q.ExecContext(
		ctx,
		insertarArticuloSQL,
		a.ID().String(),
		a.GarantiaID().String(),
		string(a.Rol()),
		a.ArticuloID(),
		nullableString(a.Clave()),
		a.Description(),
		enumPtrArg(a.Ruta()),
		string(a.Etapa()),
		string(a.Ubicacion()),
		enumPtrArg(a.Dictamen()),
		enumPtrArg(a.Desenlace()),
		timeArg(a.CerradoEn()),
		firebird.ToWallClock(a.CreatedAt()),
		firebird.ToWallClock(a.UpdatedAt()),
		uuidPtrArg(a.ReemplazaA()),
	)
	if err != nil {
		return firebird.MapError(err)
	}

	return nil
}

func insertarEvento(
	ctx context.Context,
	q firebird.Querier,
	e *domain.Evento,
) error {
	_, err := q.ExecContext(
		ctx,
		insertarEventoSQL,
		e.ID().String(),
		e.GarantiaID().String(),
		uuidPtrArg(e.ArticuloRef()),
		string(e.Tipo()),
		nullableString(e.Description()),
		enumPtrArg(e.EtapaDesde()),
		enumPtrArg(e.EtapaHasta()),
		e.Usuario(),
		enumPtrArg(e.RolDecisor()),
		e.GPSLat(),
		e.GPSLon(),
		firebird.ToWallClock(e.CreatedAt()),
		firebird.ToWallClock(e.DeviceCreatedAt()),
		e.ClaveIdempotencia(),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UQ_MSP_GA_EVENTO_CLAVE") {
			return domain.ErrClaveIdempotenciaDuplicada
		}

		return firebird.MapError(err)
	}

	return nil
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}

	return v
}

func uuidPtrArg(v *uuid.UUID) any {
	if v == nil {
		return nil
	}

	return v.String()
}

func timeArg(v *time.Time) any {
	if v == nil {
		return nil
	}

	return firebird.ToWallClock(*v)
}

func dateArg(v *time.Time) any {
	if v == nil {
		return nil
	}

	return *v
}

func estadoCuentaArg(v *domain.EstadoCuenta) any {
	if v == nil {
		return nil
	}

	return string(*v)
}

func enumPtrArg[T ~string](v *T) any {
	if v == nil {
		return nil
	}

	return string(*v)
}
