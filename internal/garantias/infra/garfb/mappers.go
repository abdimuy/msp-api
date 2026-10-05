package garfb

import (
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

type rowScanner interface {
	Scan(dest ...any) error
}

type garantiaRow struct {
	id             string
	folio          string
	origen         string
	clienteID      sql.NullInt64
	ventaID        sql.NullInt64
	estadoCuenta   sql.NullString
	estado         string
	description    []byte
	vigenciaHasta  sql.NullTime
	calle          sql.NullString
	numeroExterior sql.NullString
	colonia        sql.NullString
	localidad      sql.NullString
	ciudad         sql.NullString
	codigoPostal   sql.NullString
	gpsLat         sql.NullFloat64
	gpsLon         sql.NullFloat64
	abiertoPor     string
	cerradoRaw     any
	createdRaw     any
	updatedRaw     any
}

type articuloRow struct {
	id          string
	garantiaID  string
	rol         string
	articuloID  sql.NullInt64
	clave       sql.NullString
	description string
	ruta        sql.NullString
	etapa       string
	ubicacion   string
	dictamen    sql.NullString
	desenlace   sql.NullString
	cerradoRaw  any
	createdRaw  any
	updatedRaw  any
	reemplazaA  sql.NullString
}

func scanGarantia(row rowScanner) (*garantiaRow, error) {
	var r garantiaRow

	err := row.Scan(
		&r.id,
		&r.folio,
		&r.origen,
		&r.clienteID,
		&r.ventaID,
		&r.estadoCuenta,
		&r.estado,
		&r.description,
		&r.vigenciaHasta,
		&r.calle,
		&r.numeroExterior,
		&r.colonia,
		&r.localidad,
		&r.ciudad,
		&r.codigoPostal,
		&r.gpsLat,
		&r.gpsLon,
		&r.abiertoPor,
		&r.cerradoRaw,
		&r.createdRaw,
		&r.updatedRaw,
	)
	if err != nil {
		return nil, firebird.MapError(err)
	}

	return &r, nil
}

func scanArticulo(row rowScanner) (*articuloRow, error) {
	var r articuloRow

	err := row.Scan(
		&r.id,
		&r.garantiaID,
		&r.rol,
		&r.articuloID,
		&r.clave,
		&r.description,
		&r.ruta,
		&r.etapa,
		&r.ubicacion,
		&r.dictamen,
		&r.desenlace,
		&r.cerradoRaw,
		&r.createdRaw,
		&r.updatedRaw,
		&r.reemplazaA,
	)
	if err != nil {
		return nil, firebird.MapError(err)
	}

	return &r, nil
}

func intPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}

	n := int(v.Int64)
	return &n
}

func floatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}

	n := v.Float64
	return &n
}

func uuidPtr(v sql.NullString) (*uuid.UUID, error) {
	if !v.Valid {
		return nil, nil
	}

	id, err := uuid.Parse(v.String)
	if err != nil {
		return nil, err
	}

	return &id, nil
}

func nullTimePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}

	t := v.Time
	return &t
}

func parseEstadoCuentaPtr(v sql.NullString) (*domain.EstadoCuenta, error) {
	if !v.Valid {
		return nil, nil
	}

	estado, err := domain.ParseEstadoCuenta(v.String)
	if err != nil {
		return nil, err
	}

	return &estado, nil
}

func parseRutaPtr(v sql.NullString) (*domain.RutaReparacion, error) {
	if !v.Valid {
		return nil, nil
	}

	ruta, err := domain.ParseRutaReparacion(v.String)
	if err != nil {
		return nil, err
	}

	return &ruta, nil
}

func parseDictamenPtr(v sql.NullString) (*domain.Dictamen, error) {
	if !v.Valid {
		return nil, nil
	}

	dictamen, err := domain.ParseDictamen(v.String)
	if err != nil {
		return nil, err
	}

	return &dictamen, nil
}

func parseDesenlacePtr(v sql.NullString) (*domain.Desenlace, error) {
	if !v.Valid {
		return nil, nil
	}

	desenlace, err := domain.ParseDesenlace(v.String)
	if err != nil {
		return nil, err
	}

	return &desenlace, nil
}

func hydrateArticulo(r *articuloRow) (*domain.Articulo, error) {
	id, err := uuid.Parse(r.id)
	if err != nil {
		return nil, err
	}

	garantiaID, err := uuid.Parse(r.garantiaID)
	if err != nil {
		return nil, err
	}

	rol, err := domain.ParseRolArticulo(r.rol)
	if err != nil {
		return nil, err
	}

	ruta, err := parseRutaPtr(r.ruta)
	if err != nil {
		return nil, err
	}

	etapa, err := domain.ParseEtapa(r.etapa)
	if err != nil {
		return nil, err
	}

	ubicacion, err := domain.ParseUbicacion(r.ubicacion)
	if err != nil {
		return nil, err
	}

	dictamen, err := parseDictamenPtr(r.dictamen)
	if err != nil {
		return nil, err
	}

	desenlace, err := parseDesenlacePtr(r.desenlace)
	if err != nil {
		return nil, err
	}

	reemplazaA, err := uuidPtr(r.reemplazaA)
	if err != nil {
		return nil, err
	}

	cerrado, err := firebird.ScanNullUTCTime(r.cerradoRaw)
	if err != nil {
		return nil, err
	}

	createdAt, err := firebird.ScanUTCTime(r.createdRaw)
	if err != nil {
		return nil, err
	}

	updatedAt, err := firebird.ScanUTCTime(r.updatedRaw)
	if err != nil {
		return nil, err
	}

	return domain.HydrateArticulo(domain.HydrateArticuloParams{
		ID:          id,
		GarantiaID:  garantiaID,
		Rol:         rol,
		ReemplazaA:  reemplazaA,
		ArticuloID:  intPtr(r.articuloID),
		Clave:       r.clave.String,
		Description: r.description,
		Ruta:        ruta,
		Etapa:       etapa,
		Ubicacion:   ubicacion,
		Dictamen:    dictamen,
		Desenlace:   desenlace,
		CerradoEn:   nullTimePtr(cerrado),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}), nil
}

func hydrateGarantia(
	r *garantiaRow,
	articulos []*domain.Articulo,
) (*domain.Garantia, error) {
	id, err := uuid.Parse(r.id)
	if err != nil {
		return nil, err
	}

	folio, err := domain.ParseFolio(r.folio)
	if err != nil {
		return nil, err
	}

	origen, err := domain.ParseOrigenFolio(r.origen)
	if err != nil {
		return nil, err
	}

	estadoCuenta, err := parseEstadoCuentaPtr(r.estadoCuenta)
	if err != nil {
		return nil, err
	}

	estado, err := domain.ParseEstadoFolio(r.estado)
	if err != nil {
		return nil, err
	}

	cerrado, err := firebird.ScanNullUTCTime(r.cerradoRaw)
	if err != nil {
		return nil, err
	}

	createdAt, err := firebird.ScanUTCTime(r.createdRaw)
	if err != nil {
		return nil, err
	}

	updatedAt, err := firebird.ScanUTCTime(r.updatedRaw)
	if err != nil {
		return nil, err
	}

	return domain.HydrateGarantia(domain.HydrateGarantiaParams{
		ID:             id,
		Folio:          folio,
		Origen:         origen,
		ClienteID:      intPtr(r.clienteID),
		VentaID:        intPtr(r.ventaID),
		EstadoCuenta:   estadoCuenta,
		Estado:         estado,
		Description:    string(r.description),
		VigenciaHasta:  datePtr(r.vigenciaHasta),
		Calle:          r.calle.String,
		NumeroExterior: r.numeroExterior.String,
		Colonia:        r.colonia.String,
		Localidad:      r.localidad.String,
		Ciudad:         r.ciudad.String,
		CodigoPostal:   r.codigoPostal.String,
		GPSLat:         floatPtr(r.gpsLat),
		GPSLon:         floatPtr(r.gpsLon),
		AbiertoPor:     r.abiertoPor,
		CerradoEn:      nullTimePtr(cerrado),
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		Articulos:      articulos,
	}), nil
}

func datePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}

	t := v.Time
	d := time.Date(
		t.Year(),
		t.Month(),
		t.Day(),
		0, 0, 0, 0,
		time.UTC,
	)

	return &d
}

type eventoRow struct {
	id                string
	garantiaID        string
	articuloRef       sql.NullString
	tipo              string
	description       []byte
	etapaDesde        sql.NullString
	etapaHasta        sql.NullString
	usuario           string
	rolDecisor        sql.NullString
	gpsLat            sql.NullFloat64
	gpsLon            sql.NullFloat64
	createdRaw        any
	deviceCreatedRaw  any
	claveIdempotencia string
}

func scanEvento(row rowScanner) (*eventoRow, error) {
	var r eventoRow

	err := row.Scan(
		&r.id,
		&r.garantiaID,
		&r.articuloRef,
		&r.tipo,
		&r.description,
		&r.etapaDesde,
		&r.etapaHasta,
		&r.usuario,
		&r.rolDecisor,
		&r.gpsLat,
		&r.gpsLon,
		&r.createdRaw,
		&r.deviceCreatedRaw,
		&r.claveIdempotencia,
	)
	if err != nil {
		return nil, firebird.MapError(err)
	}

	return &r, nil
}

func parseEtapaPtr(v sql.NullString) (*domain.Etapa, error) {
	if !v.Valid {
		return nil, nil
	}

	etapa, err := domain.ParseEtapa(v.String)
	if err != nil {
		return nil, err
	}

	return &etapa, nil
}

func parseRolDecisorPtr(v sql.NullString) (*domain.RolDecisor, error) {
	if !v.Valid {
		return nil, nil
	}

	rol, err := domain.ParseRolDecisor(v.String)
	if err != nil {
		return nil, err
	}

	return &rol, nil
}

func hydrateEvento(r *eventoRow) (*domain.Evento, error) {
	id, err := uuid.Parse(r.id)
	if err != nil {
		return nil, err
	}

	garantiaID, err := uuid.Parse(r.garantiaID)
	if err != nil {
		return nil, err
	}

	articuloRef, err := uuidPtr(r.articuloRef)
	if err != nil {
		return nil, err
	}

	tipo, err := domain.ParseTipoEvento(r.tipo)
	if err != nil {
		return nil, err
	}

	etapaDesde, err := parseEtapaPtr(r.etapaDesde)
	if err != nil {
		return nil, err
	}

	etapaHasta, err := parseEtapaPtr(r.etapaHasta)
	if err != nil {
		return nil, err
	}

	rolDecisor, err := parseRolDecisorPtr(r.rolDecisor)
	if err != nil {
		return nil, err
	}

	createdAt, err := firebird.ScanUTCTime(r.createdRaw)
	if err != nil {
		return nil, err
	}

	deviceCreatedAt, err := firebird.ScanUTCTime(r.deviceCreatedRaw)
	if err != nil {
		return nil, err
	}

	return domain.HydrateEvento(domain.HydrateEventoParams{
		ID:                id,
		GarantiaID:        garantiaID,
		ArticuloRef:       articuloRef,
		Tipo:              tipo,
		Description:       string(r.description),
		EtapaDesde:        etapaDesde,
		EtapaHasta:        etapaHasta,
		Usuario:           r.usuario,
		RolDecisor:        rolDecisor,
		GPSLat:            floatPtr(r.gpsLat),
		GPSLon:            floatPtr(r.gpsLon),
		CreatedAt:         createdAt,
		DeviceCreatedAt:   deviceCreatedAt,
		ClaveIdempotencia: r.claveIdempotencia,
	}), nil
}
