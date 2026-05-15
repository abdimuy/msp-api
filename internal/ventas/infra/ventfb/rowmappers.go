//nolint:misspell // Spanish vocabulary (productos, descripcion) by convention.
package ventfb

import (
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	platform "github.com/abdimuy/msp-api/internal/platform/domain"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// numericMonto is the SQL scale of the NUMERIC(14,2) monto columns.
const numericMonto = 2

// numericCantidad is the SQL scale of the NUMERIC(10,4) producto cantidad
// column.
const numericCantidad = 4

// rowScanner is the minimal surface satisfied by both *sql.Row and *sql.Rows.
// Repository helpers accept this so the same mapper works for single-row
// reads and paginated iteration.
type rowScanner interface {
	Scan(dest ...any) error
}

// parseUUIDColumn converts a CHAR(36) UUID column to a uuid.UUID, wrapping
// driver-side surprises in an apperror so callers do not return raw driver
// errors.
func parseUUIDColumn(column, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.NewInternal(
			"firebird_uuid_invalid",
			"uuid inválido en columna de base de datos",
		).
			WithSource("firebird").
			WithError(err).
			WithField("column", column).
			WithField("raw_value", raw)
	}
	return id, nil
}

// parseNullUUIDColumn is the nullable counterpart of parseUUIDColumn.
func parseNullUUIDColumn(column string, raw sql.NullString) (*uuid.UUID, error) {
	if !raw.Valid {
		return nil, nil //nolint:nilnil // optional pointer pattern.
	}
	id, err := parseUUIDColumn(column, raw.String)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// ventaRowRaw is the intermediate scan target for one MSP_VENTAS row. We use
// a struct to keep the wide Scan call readable and to share the rebuild logic
// across single-row and iteration callers.
//
// Fields declared as firebird.Win1252 correspond to NOT-NULL columns with
// CHARACTER SET ISO8859_1 — the Win1252 scanner handles the encoding boundary.
// Nullable ISO8859_1 columns remain sql.NullString; builder functions decode
// their .String field through Win1252 when Valid.
type ventaRowRaw struct {
	// Identity
	idRaw string

	// Cliente snapshot — ISO8859_1 text columns use Win1252 scanner.
	nombreCliente    firebird.Win1252 // NOMBRE_CLIENTE CHARACTER SET ISO8859_1
	telefono         sql.NullString   // ASCII-safe (digits/symbols); no encoding needed.
	avalOResponsable sql.NullString   // nullable; decoded through Win1252 when Valid

	// Dirección — ISO8859_1 text columns use Win1252 scanner.
	calle          firebird.Win1252 // CALLE CHARACTER SET ISO8859_1
	numeroExterior sql.NullString   // nullable; decoded through Win1252 when Valid
	colonia        firebird.Win1252 // COLONIA CHARACTER SET ISO8859_1
	poblacion      firebird.Win1252 // POBLACION CHARACTER SET ISO8859_1
	ciudad         firebird.Win1252 // CIUDAD CHARACTER SET ISO8859_1
	zonaClienteID  sql.NullInt32

	// GPS
	latitud, longitud float64

	// Venta metadata
	fechaVentaRaw any
	tipoVenta     string

	// Montos
	montoAnualRaw, montoCortoPlazoRaw, montoContadoRaw any

	// Plan crédito
	plazoMeses        sql.NullInt32
	engancheRaw       any
	parcialidadRaw    any
	frecPago          sql.NullString
	diaCobranzaSemana sql.NullString
	diaCobranzaMes    sql.NullInt32

	// Nota — nullable; decoded through Win1252 when Valid.
	nota sql.NullString

	// Audit
	createdAtRaw, updatedAtRaw any
	createdByRaw, updatedByRaw string

	// Cancelación
	canceledAtRaw any
	canceledByRaw sql.NullString
	cancelReason  sql.NullString // nullable; decoded through Win1252 when Valid

	// Status
	clienteID     sql.NullInt32
	status        string
	approvedAtRaw any
	approvedByRaw sql.NullString
}

// scanVentaRowRaw runs the wide Scan over an MSP_VENTAS row.
func scanVentaRowRaw(s rowScanner) (*ventaRowRaw, error) {
	var r ventaRowRaw
	if err := s.Scan(
		&r.idRaw, &r.nombreCliente, &r.telefono, &r.avalOResponsable,
		&r.calle, &r.numeroExterior, &r.colonia, &r.poblacion, &r.ciudad, &r.zonaClienteID,
		&r.latitud, &r.longitud,
		&r.fechaVentaRaw, &r.tipoVenta,
		&r.montoAnualRaw, &r.montoCortoPlazoRaw, &r.montoContadoRaw,
		&r.plazoMeses, &r.engancheRaw, &r.parcialidadRaw,
		&r.frecPago, &r.diaCobranzaSemana, &r.diaCobranzaMes,
		&r.nota,
		&r.createdAtRaw, &r.updatedAtRaw, &r.createdByRaw, &r.updatedByRaw,
		&r.canceledAtRaw, &r.canceledByRaw, &r.cancelReason,
		&r.clienteID, &r.status, &r.approvedAtRaw, &r.approvedByRaw,
	); err != nil {
		return nil, err
	}
	return &r, nil
}

// assembleVenta turns a scanned row plus already-loaded child slices into a
// hydrated domain.Venta. The children may be nil for header-only callers.
func assembleVenta(
	r *ventaRowRaw,
	combos []*domain.Combo,
	productos []*domain.Producto,
	vendedores []*domain.Vendedor,
	imagenes []*domain.Imagen,
) (*domain.Venta, error) {
	ids, err := parseVentaUUIDs(r)
	if err != nil {
		return nil, err
	}
	times, err := parseVentaTimes(r)
	if err != nil {
		return nil, err
	}
	montos, err := parseVentaMontos(r)
	if err != nil {
		return nil, err
	}
	cliente, err := buildClienteSnapshot(r)
	if err != nil {
		return nil, err
	}
	direccion, err := buildDireccion(r)
	if err != nil {
		return nil, err
	}
	gps := domain.HydrateGPSCoords(r.latitud, r.longitud)
	plan, err := buildPlanCredito(r)
	if err != nil {
		return nil, err
	}
	diaCobranza := buildDiaCobranza(r)
	cancelacion, err := buildCancelacion(r, times.canceledAt, ids.canceledBy)
	if err != nil {
		return nil, err
	}
	aprobacion, err := buildAprobacion(r)
	if err != nil {
		return nil, err
	}
	var nota *string
	if r.nota.Valid {
		// NOTA is CHARACTER SET ISO8859_1 — decode Win1252 → UTF-8.
		var w firebird.Win1252
		if err := w.Scan(r.nota.String); err != nil {
			return nil, err
		}
		v := string(w)
		nota = &v
	}
	var clienteID *int
	if r.clienteID.Valid {
		v := int(r.clienteID.Int32)
		clienteID = &v
	}
	return domain.HydrateVenta(domain.HydrateVentaParams{
		ID:          ids.id,
		ClienteID:   clienteID,
		Cliente:     cliente,
		Direccion:   direccion,
		GPS:         gps,
		FechaVenta:  times.fechaVenta,
		TipoVenta:   domain.TipoVenta(r.tipoVenta),
		Montos:      montos,
		PlanCredito: plan,
		DiaCobranza: diaCobranza,
		Nota:        nota,
		Status:      domain.VentaStatus(r.status),
		Combos:      combos,
		Productos:   productos,
		Vendedores:  vendedores,
		Imagenes:    imagenes,
		Cancelacion: cancelacion,
		Aprobacion:  aprobacion,
		CreatedAt:   times.createdAt,
		UpdatedAt:   times.updatedAt,
		CreatedBy:   ids.createdBy,
		UpdatedBy:   ids.updatedBy,
	}), nil
}

// buildAprobacion turns the optional APPROVED_AT/APPROVED_BY pair into a
// domain.Aprobacion or nil.
func buildAprobacion(r *ventaRowRaw) (*domain.Aprobacion, error) {
	approvedAt, err := firebird.ScanNullUTCTime(r.approvedAtRaw)
	if err != nil {
		return nil, err
	}
	if !approvedAt.Valid || !r.approvedByRaw.Valid {
		return nil, nil //nolint:nilnil // optional pointer pattern.
	}
	approvedBy, err := parseUUIDColumn("APPROVED_BY", r.approvedByRaw.String)
	if err != nil {
		return nil, err
	}
	a := domain.HydrateAprobacion(approvedAt.Time, approvedBy)
	return &a, nil
}

// ventaIDs bundles the parsed UUID columns of a venta header.
type ventaIDs struct {
	id, createdBy, updatedBy uuid.UUID
	canceledBy               *uuid.UUID
}

func parseVentaUUIDs(r *ventaRowRaw) (ventaIDs, error) {
	id, err := parseUUIDColumn("ID", r.idRaw)
	if err != nil {
		return ventaIDs{}, err
	}
	createdBy, err := parseUUIDColumn("CREATED_BY", r.createdByRaw)
	if err != nil {
		return ventaIDs{}, err
	}
	updatedBy, err := parseUUIDColumn("UPDATED_BY", r.updatedByRaw)
	if err != nil {
		return ventaIDs{}, err
	}
	canceledBy, err := parseNullUUIDColumn("CANCELED_BY", r.canceledByRaw)
	if err != nil {
		return ventaIDs{}, err
	}
	return ventaIDs{id: id, createdBy: createdBy, updatedBy: updatedBy, canceledBy: canceledBy}, nil
}

// ventaTimes bundles the parsed time columns of a venta header.
type ventaTimes struct {
	createdAt, updatedAt, fechaVenta time.Time
	canceledAt                       sql.NullTime
}

func parseVentaTimes(r *ventaRowRaw) (ventaTimes, error) {
	createdAt, err := firebird.ScanUTCTime(r.createdAtRaw)
	if err != nil {
		return ventaTimes{}, err
	}
	updatedAt, err := firebird.ScanUTCTime(r.updatedAtRaw)
	if err != nil {
		return ventaTimes{}, err
	}
	fechaVenta, err := firebird.ScanUTCTime(r.fechaVentaRaw)
	if err != nil {
		return ventaTimes{}, err
	}
	canceledAt, err := firebird.ScanNullUTCTime(r.canceledAtRaw)
	if err != nil {
		return ventaTimes{}, err
	}
	return ventaTimes{
		createdAt:  createdAt,
		updatedAt:  updatedAt,
		fechaVenta: fechaVenta,
		canceledAt: canceledAt,
	}, nil
}

func parseVentaMontos(r *ventaRowRaw) (domain.MontoSnapshot, error) {
	anual, err := firebird.ScanDecimal(r.montoAnualRaw, numericMonto)
	if err != nil {
		return domain.MontoSnapshot{}, err
	}
	cortoPlazo, err := firebird.ScanDecimal(r.montoCortoPlazoRaw, numericMonto)
	if err != nil {
		return domain.MontoSnapshot{}, err
	}
	contado, err := firebird.ScanDecimal(r.montoContadoRaw, numericMonto)
	if err != nil {
		return domain.MontoSnapshot{}, err
	}
	return domain.HydrateMontoSnapshot(anual, cortoPlazo, contado), nil
}

func buildClienteSnapshot(r *ventaRowRaw) (domain.ClienteSnapshot, error) {
	var telOpt *platform.Telefono
	if r.telefono.Valid {
		t := platform.HydrateTelefono(r.telefono.String)
		telOpt = &t
	}
	var avalOpt *domain.NombreCliente
	if r.avalOResponsable.Valid {
		// AVAL_O_RESPONSABLE is CHARACTER SET ISO8859_1 — decode Win1252 → UTF-8.
		var w firebird.Win1252
		if err := w.Scan(r.avalOResponsable.String); err != nil {
			return domain.ClienteSnapshot{}, err
		}
		a := domain.HydrateNombreCliente(string(w))
		avalOpt = &a
	}
	return domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{
		Nombre:   domain.HydrateNombreCliente(string(r.nombreCliente)),
		Telefono: telOpt,
		Aval:     avalOpt,
	}), nil
}

func buildDireccion(r *ventaRowRaw) (domain.Direccion, error) {
	var numExt *string
	if r.numeroExterior.Valid {
		// NUMERO_EXTERIOR is CHARACTER SET ISO8859_1 — decode Win1252 → UTF-8.
		var w firebird.Win1252
		if err := w.Scan(r.numeroExterior.String); err != nil {
			return domain.Direccion{}, err
		}
		v := string(w)
		numExt = &v
	}
	var zonaID *int
	if r.zonaClienteID.Valid {
		v := int(r.zonaClienteID.Int32)
		zonaID = &v
	}
	return domain.HydrateDireccion(domain.NewDireccionParams{
		Calle:          string(r.calle),
		NumeroExterior: numExt,
		Colonia:        string(r.colonia),
		Poblacion:      string(r.poblacion),
		Ciudad:         string(r.ciudad),
		ZonaClienteID:  zonaID,
	}), nil
}

func buildPlanCredito(r *ventaRowRaw) (*domain.PlanCredito, error) {
	if !r.plazoMeses.Valid {
		return nil, nil //nolint:nilnil // optional pointer pattern.
	}
	enganche, err := firebird.ScanDecimal(r.engancheRaw, numericMonto)
	if err != nil {
		return nil, err
	}
	parcialidad, err := firebird.ScanDecimal(r.parcialidadRaw, numericMonto)
	if err != nil {
		return nil, err
	}
	frec := domain.FrecPago("")
	if r.frecPago.Valid {
		frec = domain.FrecPago(r.frecPago.String)
	}
	plan := domain.HydratePlanCredito(int(r.plazoMeses.Int32), enganche, parcialidad, frec)
	return &plan, nil
}

func buildDiaCobranza(r *ventaRowRaw) *domain.DiaCobranza {
	var semanaPtr *domain.DiaSemana
	if r.diaCobranzaSemana.Valid {
		d := domain.DiaSemana(r.diaCobranzaSemana.String)
		semanaPtr = &d
	}
	var mesPtr *int
	if r.diaCobranzaMes.Valid {
		v := int(r.diaCobranzaMes.Int32)
		mesPtr = &v
	}
	if semanaPtr == nil && mesPtr == nil {
		return nil
	}
	dc := domain.HydrateDiaCobranza(semanaPtr, mesPtr)
	return &dc
}

func buildCancelacion(
	r *ventaRowRaw,
	canceledAt sql.NullTime,
	canceledBy *uuid.UUID,
) (*domain.Cancelacion, error) {
	if !canceledAt.Valid || canceledBy == nil {
		return nil, nil //nolint:nilnil // optional pointer pattern.
	}
	reason := ""
	if r.cancelReason.Valid {
		// CANCEL_REASON is CHARACTER SET ISO8859_1 — decode Win1252 → UTF-8.
		var w firebird.Win1252
		if err := w.Scan(r.cancelReason.String); err != nil {
			return nil, err
		}
		reason = string(w)
	}
	c := domain.HydrateCancelacion(canceledAt.Time, *canceledBy, reason)
	return &c, nil
}

// scanCombo rebuilds a domain.Combo from one MSP_VENTAS_COMBOS row.
func scanCombo(s rowScanner) (*domain.Combo, error) {
	var (
		idRaw                         string
		nombre                        firebird.Win1252 // NOMBRE_COMBO CHARACTER SET ISO8859_1
		anualRaw, cortoRaw, conRaw    any
		cantidadRaw                   any
		almacenOrigen, almacenDestino int
		createdAtRaw, updatedAtRaw    any
		createdByRaw, updatedByRaw    string
	)
	if err := s.Scan(
		&idRaw, &nombre,
		&anualRaw, &cortoRaw, &conRaw,
		&cantidadRaw, &almacenOrigen, &almacenDestino,
		&createdAtRaw, &updatedAtRaw, &createdByRaw, &updatedByRaw,
	); err != nil {
		return nil, err
	}
	id, err := parseUUIDColumn("ID", idRaw)
	if err != nil {
		return nil, err
	}
	createdBy, err := parseUUIDColumn("CREATED_BY", createdByRaw)
	if err != nil {
		return nil, err
	}
	updatedBy, err := parseUUIDColumn("UPDATED_BY", updatedByRaw)
	if err != nil {
		return nil, err
	}
	createdAt, err := firebird.ScanUTCTime(createdAtRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := firebird.ScanUTCTime(updatedAtRaw)
	if err != nil {
		return nil, err
	}
	anual, err := firebird.ScanDecimal(anualRaw, numericMonto)
	if err != nil {
		return nil, err
	}
	corto, err := firebird.ScanDecimal(cortoRaw, numericMonto)
	if err != nil {
		return nil, err
	}
	contado, err := firebird.ScanDecimal(conRaw, numericMonto)
	if err != nil {
		return nil, err
	}
	cantidad, err := firebird.ScanDecimal(cantidadRaw, numericCantidad)
	if err != nil {
		return nil, err
	}
	return domain.HydrateCombo(domain.HydrateComboParams{
		ID:             id,
		Nombre:         string(nombre),
		Precios:        domain.HydrateMontoSnapshot(anual, corto, contado),
		Cantidad:       cantidad,
		AlmacenOrigen:  almacenOrigen,
		AlmacenDestino: almacenDestino,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		CreatedBy:      createdBy,
		UpdatedBy:      updatedBy,
	}), nil
}

// scanProducto rebuilds a domain.Producto from one MSP_VENTAS_PRODUCTOS row.
//
//nolint:funlen // wide column set; splitting the scanner buys nothing.
func scanProducto(s rowScanner) (*domain.Producto, error) {
	var (
		idRaw                         string
		articuloID                    int
		articulo                      firebird.Win1252 // ARTICULO CHARACTER SET ISO8859_1
		cantidadRaw                   any
		anualRaw, cortoRaw, conRaw    any
		comboIDRaw                    sql.NullString
		almacenOrigen, almacenDestino sql.NullInt32
		createdAtRaw, updatedAtRaw    any
		createdByRaw, updatedByRaw    string
	)
	if err := s.Scan(
		&idRaw, &articuloID, &articulo, &cantidadRaw,
		&anualRaw, &cortoRaw, &conRaw,
		&comboIDRaw, &almacenOrigen, &almacenDestino,
		&createdAtRaw, &updatedAtRaw, &createdByRaw, &updatedByRaw,
	); err != nil {
		return nil, err
	}
	id, err := parseUUIDColumn("ID", idRaw)
	if err != nil {
		return nil, err
	}
	comboID, err := parseNullUUIDColumn("COMBO_ID", comboIDRaw)
	if err != nil {
		return nil, err
	}
	createdBy, err := parseUUIDColumn("CREATED_BY", createdByRaw)
	if err != nil {
		return nil, err
	}
	updatedBy, err := parseUUIDColumn("UPDATED_BY", updatedByRaw)
	if err != nil {
		return nil, err
	}
	createdAt, err := firebird.ScanUTCTime(createdAtRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := firebird.ScanUTCTime(updatedAtRaw)
	if err != nil {
		return nil, err
	}
	cantidad, err := firebird.ScanDecimal(cantidadRaw, numericCantidad)
	if err != nil {
		return nil, err
	}
	anual, err := firebird.ScanDecimal(anualRaw, numericMonto)
	if err != nil {
		return nil, err
	}
	corto, err := firebird.ScanDecimal(cortoRaw, numericMonto)
	if err != nil {
		return nil, err
	}
	contado, err := firebird.ScanDecimal(conRaw, numericMonto)
	if err != nil {
		return nil, err
	}
	var almOrgPtr, almDstPtr *int
	if almacenOrigen.Valid {
		v := int(almacenOrigen.Int32)
		almOrgPtr = &v
	}
	if almacenDestino.Valid {
		v := int(almacenDestino.Int32)
		almDstPtr = &v
	}
	return domain.HydrateProducto(domain.HydrateProductoParams{
		ID:             id,
		ArticuloID:     articuloID,
		Articulo:       string(articulo),
		Cantidad:       cantidad,
		Precios:        domain.HydrateMontoSnapshot(anual, corto, contado),
		ComboID:        comboID,
		AlmacenOrigen:  almOrgPtr,
		AlmacenDestino: almDstPtr,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		CreatedBy:      createdBy,
		UpdatedBy:      updatedBy,
	}), nil
}

// scanVendedor rebuilds a domain.Vendedor from one MSP_VENTAS_VENDEDORES row.
func scanVendedor(s rowScanner) (*domain.Vendedor, error) {
	var (
		idRaw, usuarioIDRaw        string
		email                      string
		nombre                     firebird.Win1252 // VENDEDOR_NOMBRE CHARACTER SET ISO8859_1
		createdAtRaw, updatedAtRaw any
		createdByRaw, updatedByRaw string
	)
	if err := s.Scan(
		&idRaw, &usuarioIDRaw, &email, &nombre,
		&createdAtRaw, &updatedAtRaw, &createdByRaw, &updatedByRaw,
	); err != nil {
		return nil, err
	}
	id, err := parseUUIDColumn("ID", idRaw)
	if err != nil {
		return nil, err
	}
	usuarioID, err := parseUUIDColumn("VENDEDOR_USUARIO_ID", usuarioIDRaw)
	if err != nil {
		return nil, err
	}
	createdBy, err := parseUUIDColumn("CREATED_BY", createdByRaw)
	if err != nil {
		return nil, err
	}
	updatedBy, err := parseUUIDColumn("UPDATED_BY", updatedByRaw)
	if err != nil {
		return nil, err
	}
	createdAt, err := firebird.ScanUTCTime(createdAtRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := firebird.ScanUTCTime(updatedAtRaw)
	if err != nil {
		return nil, err
	}
	snap := domain.HydrateVendedorSnapshot(domain.NewVendedorSnapshotParams{
		UsuarioID: usuarioID, Email: email, Nombre: string(nombre),
	})
	return domain.HydrateVendedor(domain.HydrateVendedorParams{
		ID:        id,
		Snapshot:  snap,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		CreatedBy: createdBy,
		UpdatedBy: updatedBy,
	}), nil
}

// scanImagen rebuilds a domain.Imagen from one MSP_VENTAS_IMAGENES row.
func scanImagen(s rowScanner) (*domain.Imagen, error) {
	var (
		idRaw                      string
		storageKind, storageKey    string
		mime                       string
		sizeBytes                  int64
		descripcion                sql.NullString
		createdAtRaw, updatedAtRaw any
		createdByRaw, updatedByRaw string
	)
	if err := s.Scan(
		&idRaw, &storageKind, &storageKey, &mime, &sizeBytes, &descripcion,
		&createdAtRaw, &updatedAtRaw, &createdByRaw, &updatedByRaw,
	); err != nil {
		return nil, err
	}
	id, err := parseUUIDColumn("ID", idRaw)
	if err != nil {
		return nil, err
	}
	createdBy, err := parseUUIDColumn("CREATED_BY", createdByRaw)
	if err != nil {
		return nil, err
	}
	updatedBy, err := parseUUIDColumn("UPDATED_BY", updatedByRaw)
	if err != nil {
		return nil, err
	}
	createdAt, err := firebird.ScanUTCTime(createdAtRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := firebird.ScanUTCTime(updatedAtRaw)
	if err != nil {
		return nil, err
	}
	var descPtr *string
	if descripcion.Valid {
		// DESCRIPCION is CHARACTER SET ISO8859_1 — decode Win1252 → UTF-8.
		var w firebird.Win1252
		if err := w.Scan(descripcion.String); err != nil {
			return nil, err
		}
		v := string(w)
		descPtr = &v
	}
	return domain.HydrateImagen(domain.HydrateImagenParams{
		ID:          id,
		Storage:     domain.HydrateImagenStorage(domain.StorageKind(storageKind), storageKey),
		Mime:        mime,
		SizeBytes:   sizeBytes,
		Descripcion: descPtr,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
		CreatedBy:   createdBy,
		UpdatedBy:   updatedBy,
	}), nil
}
