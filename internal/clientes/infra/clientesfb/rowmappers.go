//nolint:misspell // Spanish domain vocabulary by project convention.
package clientesfb

import (
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// scannable abstracts *sql.Row and *sql.Rows so rawScan structs work for both.
type scannable interface {
	Scan(dest ...any) error
}

// ─── clienteRowRaw ───────────────────────────────────────────────────────────

// clienteRowRaw holds the raw Firebird scan targets for a CLIENTES row.
// Win1252 targets decode Windows-1252 bytes to UTF-8 at scan time.
// Ordering matches selectClienteCols exactly.
type clienteRowRaw struct {
	clienteID     int
	nombreRaw     firebird.Win1252
	limiteCrRaw   any
	notasRaw      firebird.Win1252 // BLOB Sub_Type 1 — Win1252 handles nil→"" at scan time
	estatus       string
	zonaClienteID sql.NullInt64
	zonaNombreRaw firebird.Win1252
	cobradorID    sql.NullInt64
	cobrNombreRaw firebird.Win1252
	// Direccion fields
	calleRaw    firebird.Win1252
	coloniaRaw  firebird.Win1252
	poblRaw     firebird.Win1252
	estadoRaw   firebird.Win1252
	telefonoRaw firebird.Win1252
	// GPS fields from LIBRES_CLIENTES (CHARACTER SET NONE, raw ASCII decimal text)
	latRaw sql.NullString
	lngRaw sql.NullString
}

func (r *clienteRowRaw) scanFrom(s scannable) error {
	return s.Scan(
		&r.clienteID,
		&r.nombreRaw,
		&r.limiteCrRaw,
		&r.notasRaw,
		&r.estatus,
		&r.zonaClienteID,
		&r.zonaNombreRaw,
		&r.cobradorID,
		&r.cobrNombreRaw,
		&r.calleRaw,
		&r.coloniaRaw,
		&r.poblRaw,
		&r.estadoRaw,
		&r.telefonoRaw,
		&r.latRaw,
		&r.lngRaw,
	)
}

func (r *clienteRowRaw) assemble() (*domain.Cliente, error) {
	lc, err := firebird.ScanDecimal(r.limiteCrRaw, 2)
	if err != nil {
		return nil, err
	}
	return domain.HydrateCliente(domain.HydrateClienteParams{
		ClienteID:      r.clienteID,
		Nombre:         string(r.nombreRaw),
		LimiteCredito:  lc,
		Notas:          string(r.notasRaw),
		Estatus:        r.estatus,
		ZonaClienteID:  nullableIntVal(r.zonaClienteID),
		ZonaNombre:     string(r.zonaNombreRaw),
		CobradorID:     nullableIntVal(r.cobradorID),
		CobradorNombre: string(r.cobrNombreRaw),
		Direccion: domain.HydrateDireccion(domain.HydrateDireccionParams{
			Calle:     string(r.calleRaw),
			Colonia:   string(r.coloniaRaw),
			Poblacion: string(r.poblRaw),
			Estado:    string(r.estadoRaw),
		}),
		Telefono:  string(r.telefonoRaw),
		Ubicacion: parseUbicacion(r.latRaw, r.lngRaw),
	}), nil
}

// parseUbicacion parses GPS coordinate strings from LIBRES_CLIENTES.U_LATITUD /
// U_LONGITUD (CHARACTER SET NONE, raw ASCII decimal text). Returns a zero-value
// Ubicacion (Disponible=false) when either value is absent, empty, non-numeric,
// or out of valid WGS-84 range.
func parseUbicacion(latStr, lngStr sql.NullString) domain.Ubicacion {
	if !latStr.Valid || !lngStr.Valid {
		return domain.Ubicacion{}
	}
	lat, errLat := strconv.ParseFloat(strings.TrimSpace(latStr.String), 64)
	lng, errLng := strconv.ParseFloat(strings.TrimSpace(lngStr.String), 64)
	if errLat != nil || errLng != nil {
		return domain.Ubicacion{}
	}
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return domain.Ubicacion{}
	}
	return domain.Ubicacion{Lat: lat, Lng: lng, Disponible: true}
}

// ─── directorioRowRaw ────────────────────────────────────────────────────────

// directorioRowRaw extends clienteRowRaw with the aggregated saldo field.
// Ordering matches selectDirectorioColsGrouped: all clienteCols then saldo.
type directorioRowRaw struct {
	clienteRowRaw
	saldoRaw any
}

func (r *directorioRowRaw) scanFrom(s scannable) error {
	return s.Scan(
		&r.clienteID,
		&r.nombreRaw,
		&r.limiteCrRaw,
		&r.notasRaw,
		&r.estatus,
		&r.zonaClienteID,
		&r.zonaNombreRaw,
		&r.cobradorID,
		&r.cobrNombreRaw,
		&r.calleRaw,
		&r.coloniaRaw,
		&r.poblRaw,
		&r.estadoRaw,
		&r.telefonoRaw,
		&r.latRaw,
		&r.lngRaw,
		&r.saldoRaw,
	)
}

func (r *directorioRowRaw) assemble() (outbound.DirectorioItem, error) {
	c, err := r.clienteRowRaw.assemble()
	if err != nil {
		return outbound.DirectorioItem{}, err
	}
	saldo, err := firebird.ScanDecimal(r.saldoRaw, 2)
	if err != nil {
		// The SQL COALESCEs the saldo subquery to 0, so the value is never NULL.
		// A ScanDecimal failure here signals real column drift — propagate it.
		return outbound.DirectorioItem{}, err
	}
	return outbound.DirectorioItem{
		Cliente:    c,
		SaldoTotal: saldo,
	}, nil
}

// ─── ventaClienteRowRaw ───────────────────────────────────────────────────────

// ventaClienteRowRaw holds raw scan targets for a VentaCliente row.
// Ordering matches selectVentaClienteCols exactly.
// BE-2: horaStr, almacenRaw, primerArticuloRaw, numArticulosRaw are the four
// enrichment columns added in task BE-2.
type ventaClienteRowRaw struct {
	doctoPVID         int
	clienteID         int
	fechaRaw          any
	folio             string
	importeRaw        any
	tipoStr           string
	saldoRaw          any
	numPagosRaw       any
	horaStr           string           // "HH:MM:SS" from SUBSTRING(CAST(HORA AS VARCHAR(13)))
	almacenRaw        firebird.Win1252 // ALMACENES.NOMBRE — CHARACTER SET NONE
	primerArticuloRaw firebird.Win1252 // ARTICULOS.NOMBRE — CHARACTER SET NONE
	numArticulosRaw   any              // COUNT(*) — scanIntFromAny
}

func (r *ventaClienteRowRaw) scanFrom(s scannable) error {
	return s.Scan(
		&r.doctoPVID,
		&r.clienteID,
		&r.fechaRaw,
		&r.folio,
		&r.importeRaw,
		&r.tipoStr,
		&r.saldoRaw,
		&r.numPagosRaw,
		&r.horaStr,
		&r.almacenRaw,
		&r.primerArticuloRaw,
		&r.numArticulosRaw,
	)
}

func (r *ventaClienteRowRaw) assemble() (*domain.VentaCliente, error) {
	fecha, err := firebird.ScanUTCTime(r.fechaRaw)
	if err != nil {
		return nil, err
	}
	total, err := firebird.ScanDecimal(r.importeRaw, 2)
	if err != nil {
		return nil, err
	}
	saldo, err := firebird.ScanDecimal(r.saldoRaw, 2)
	if err != nil {
		// The SQL COALESCEs the per-sale saldo subquery to 0, so the value is
		// never NULL. A ScanDecimal failure signals real column drift — propagate.
		return nil, err
	}
	// firebirdsql v0.9.19 returns INTEGER columns as int32, not int64.
	// Use scanIntFromAny which handles int16/int32/int64/float64/*big.Int.
	numPagos, err := scanIntFromAny(r.numPagosRaw)
	if err != nil {
		return nil, err
	}
	numArticulos, err := scanIntFromAny(r.numArticulosRaw)
	if err != nil {
		return nil, err
	}
	tipo := tipoVentaFromStr(r.tipoStr)
	return domain.HydrateVentaCliente(domain.HydrateVentaClienteParams{
		DoctoPVID:      r.doctoPVID,
		ClienteID:      r.clienteID,
		Fecha:          fecha,
		Folio:          r.folio,
		Tipo:           tipo,
		Total:          total,
		SaldoVenta:     saldo,
		NumPagos:       numPagos,
		Hora:           r.horaStr,
		Almacen:        string(r.almacenRaw),
		PrimerArticulo: string(r.primerArticuloRaw),
		NumArticulos:   numArticulos,
	}), nil
}

// tipoVentaFromStr converts the SQL CASE expression result to domain.TipoVenta.
// The CASE expression emits 'CREDITO' or 'CONTADO' — exact match.
func tipoVentaFromStr(s string) domain.TipoVenta {
	if s == "CREDITO" {
		return domain.TipoVentaCredito
	}
	return domain.TipoVentaContado
}

// ─── productoRowRaw ───────────────────────────────────────────────────────────

// productoRowRaw holds raw scan targets for a ProductoVenta row.
// Ordering matches queryProductos column list.
type productoRowRaw struct {
	articuloID     int
	nombreRaw      firebird.Win1252
	unidadesRaw    any
	precioUnitRaw  any
	precioTotalRaw any
	pctjeDsctoRaw  any
}

func (r *productoRowRaw) scanFrom(s scannable) error {
	return s.Scan(
		&r.articuloID,
		&r.nombreRaw,
		&r.unidadesRaw,
		&r.precioUnitRaw,
		&r.precioTotalRaw,
		&r.pctjeDsctoRaw,
	)
}

func (r *productoRowRaw) assemble() (*domain.ProductoVenta, error) {
	// UNIDADES is NUMERIC(18,5) — scale 5
	unidades, err := firebird.ScanDecimal(r.unidadesRaw, 5)
	if err != nil {
		return nil, err
	}
	// PRECIO_UNITARIO is NUMERIC(18,6) — scale 6
	precioUnit, err := firebird.ScanDecimal(r.precioUnitRaw, 6)
	if err != nil {
		return nil, err
	}
	// PRECIO_TOTAL_NETO is NUMERIC(15,2) — scale 2
	precioTotal, err := firebird.ScanDecimal(r.precioTotalRaw, 2)
	if err != nil {
		return nil, err
	}
	// PCTJE_DSCTO is NUMERIC(9,6) — scale 6
	pctje, err := firebird.ScanDecimal(r.pctjeDsctoRaw, 6)
	if err != nil {
		pctje = decimal.Zero
	}
	return domain.HydrateProductoVenta(domain.HydrateProductoVentaParams{
		ArticuloID:      r.articuloID,
		Nombre:          string(r.nombreRaw),
		Unidades:        unidades,
		PrecioUnitario:  precioUnit,
		PrecioTotalNeto: precioTotal,
		PctjeDscto:      pctje,
	}), nil
}

// ─── contratoRowRaw ───────────────────────────────────────────────────────────

// contratoRowRaw holds raw scan targets for the credit contract query.
// Ordering matches queryContrato column list.
type contratoRowRaw struct {
	parcialidadRaw   sql.NullInt64
	engancheRaw      any
	precioContadoRaw any
	plazoMesesRaw    sql.NullInt64
	formaDePagoRaw   firebird.Win1252 // LISTAS_ATRIBUTOS.VALOR_DESPLEGADO — CHARACTER SET NONE
	vendedor1Raw     firebird.Win1252 // LISTAS_ATRIBUTOS.VALOR_DESPLEGADO — CHARACTER SET NONE
	vendedor2Raw     firebird.Win1252 // LISTAS_ATRIBUTOS.VALOR_DESPLEGADO — CHARACTER SET NONE
	vendedor3Raw     firebird.Win1252 // LISTAS_ATRIBUTOS.VALOR_DESPLEGADO — CHARACTER SET NONE
}

func (r *contratoRowRaw) scanFrom(s scannable) error {
	return s.Scan(
		&r.parcialidadRaw,
		&r.engancheRaw,
		&r.precioContadoRaw,
		&r.plazoMesesRaw,
		&r.formaDePagoRaw,
		&r.vendedor1Raw,
		&r.vendedor2Raw,
		&r.vendedor3Raw,
	)
}

func (r *contratoRowRaw) assemble() (*outbound.ContratoCredito, error) {
	// PARCIALIDAD is SMALLINT(4) per B2 research; stored as integer pesos.
	parcialidad := decimal.Zero
	if r.parcialidadRaw.Valid {
		parcialidad = decimal.NewFromInt(r.parcialidadRaw.Int64)
	}
	// ENGANCHE is NUMERIC(6,2) — scale 2
	enganche, err := scanNullDecimalOrZero(r.engancheRaw)
	if err != nil {
		return nil, err
	}
	// PRECIO_DE_CONTADO is NUMERIC(17,2) — scale 2
	precioContado, err := scanNullDecimalOrZero(r.precioContadoRaw)
	if err != nil {
		return nil, err
	}
	plazoMeses := 0
	if r.plazoMesesRaw.Valid {
		plazoMeses = int(r.plazoMesesRaw.Int64)
	}
	// UPPER() previously applied in SQL is now done in Go to avoid Firebird
	// transliteration errors on Win1252 NONE columns with a UTF-8 connection.
	vendedores := collectVendedores(
		strings.ToUpper(string(r.vendedor1Raw)),
		strings.ToUpper(string(r.vendedor2Raw)),
		strings.ToUpper(string(r.vendedor3Raw)),
	)
	return &outbound.ContratoCredito{
		Parcialidad:     parcialidad,
		Enganche:        enganche,
		PrecioDeContado: precioContado,
		PlazoMeses:      plazoMeses,
		FormaDePago:     string(r.formaDePagoRaw),
		Vendedores:      vendedores,
	}, nil
}

// collectVendedores gathers non-empty, deduplicated vendedor names.
func collectVendedores(v1, v2, v3 string) []string {
	seen := make(map[string]struct{}, 3)
	result := make([]string, 0, 3)
	for _, v := range []string{v1, v2, v3} {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}
	return result
}

// ─── pagoRowRaw ──────────────────────────────────────────────────────────────

// pagoRowRaw holds raw scan targets for the pagos query.
// Ordering matches queryPagos column list.
type pagoRowRaw struct {
	doctoCCID     int
	fechaRaw      any
	importeRaw    any
	formaCobroRaw firebird.Win1252 // FORMAS_COBRO.NOMBRE — CHARACTER SET NONE
	cargoIDRaw    int
	conceptoCCID  int
	conceptoRaw   firebird.Win1252 // CONCEPTOS_CC.NOMBRE — CHARACTER SET NONE
	cobradorRaw   firebird.Win1252 // COBRADORES.NOMBRE or DOCTOS_CC.DESCRIPCION — CHARACTER SET NONE
}

func (r *pagoRowRaw) scanFrom(s scannable) error {
	return s.Scan(
		&r.doctoCCID,
		&r.fechaRaw,
		&r.importeRaw,
		&r.formaCobroRaw,
		&r.cargoIDRaw,
		&r.conceptoCCID,
		&r.conceptoRaw,
		&r.cobradorRaw,
	)
}

func (r *pagoRowRaw) assemble() (*domain.Pago, error) {
	fecha, err := firebird.ScanUTCTime(r.fechaRaw)
	if err != nil {
		return nil, err
	}
	importe, err := firebird.ScanDecimal(r.importeRaw, 2)
	if err != nil {
		return nil, err
	}
	return domain.HydratePago(domain.HydratePagoParams{
		DoctoCCID:      r.doctoCCID,
		Fecha:          fecha,
		Importe:        importe,
		FormaCobro:     string(r.formaCobroRaw),
		AplicaACargoID: r.cargoIDRaw,
		ConceptoCCID:   r.conceptoCCID,
		Concepto:       string(r.conceptoRaw),
		Categoria:      domain.ClasificarConcepto(r.conceptoCCID),
		Cobrador:       string(r.cobradorRaw),
	}), nil
}

// ─── resumenFichaCompradoRaw / resumenFichaAbonadoRaw ────────────────────────

// resumenFichaCompradoRaw holds raw scan targets for queryResumenFichaComprado
// (TotalComprado + NumVentas, filtered by cargo.FECHA / sale date).
type resumenFichaCompradoRaw struct {
	totalCompradoRaw any
	numVentasRaw     int
}

func (r *resumenFichaCompradoRaw) scanFrom(s scannable) error {
	return s.Scan(&r.totalCompradoRaw, &r.numVentasRaw)
}

func (r *resumenFichaCompradoRaw) assemble() (decimal.Decimal, int, error) {
	totalComprado, err := firebird.ScanDecimal(r.totalCompradoRaw, 2)
	if err != nil {
		return decimal.Zero, 0, err
	}
	return totalComprado, r.numVentasRaw, nil
}

// resumenFichaAbonadoRaw holds raw scan targets for queryResumenFichaAbonado
// (TotalAbonado + NumPagos, filtered by abono.FECHA / payment date).
type resumenFichaAbonadoRaw struct {
	totalAbonadoRaw any
	numPagosRaw     int
}

func (r *resumenFichaAbonadoRaw) scanFrom(s scannable) error {
	return s.Scan(&r.totalAbonadoRaw, &r.numPagosRaw)
}

func (r *resumenFichaAbonadoRaw) assemble() (decimal.Decimal, int, error) {
	totalAbonado, err := firebird.ScanDecimal(r.totalAbonadoRaw, 2)
	if err != nil {
		return decimal.Zero, 0, err
	}
	return totalAbonado, r.numPagosRaw, nil
}

// ─── abonoMesRowRaw ───────────────────────────────────────────────────────────

// abonoMesRowRaw holds raw scan targets for the monthly abono totals query.
type abonoMesRowRaw struct {
	anioRaw  any
	mesRaw   any
	montoRaw any
}

func (r *abonoMesRowRaw) scanFrom(s scannable) error {
	return s.Scan(&r.anioRaw, &r.mesRaw, &r.montoRaw)
}

func (r *abonoMesRowRaw) assemble() (outbound.PuntoMensual, error) {
	anio, err := scanIntFromAny(r.anioRaw)
	if err != nil {
		return outbound.PuntoMensual{}, err
	}
	mes, err := scanIntFromAny(r.mesRaw)
	if err != nil {
		return outbound.PuntoMensual{}, err
	}
	monto, err := firebird.ScanDecimal(r.montoRaw, 2)
	if err != nil {
		return outbound.PuntoMensual{}, err
	}
	return outbound.PuntoMensual{Anio: anio, Mes: mes, Monto: monto}, nil
}

// ─── compradoVsAbonadoRowRaw ─────────────────────────────────────────────────

// compradoVsAbonadoRowRaw holds raw scan targets for the dual-series chart query.
type compradoVsAbonadoRowRaw struct {
	anioRaw     any
	mesRaw      any
	compradoRaw any
	abonadoRaw  any
}

func (r *compradoVsAbonadoRowRaw) scanFrom(s scannable) error {
	return s.Scan(&r.anioRaw, &r.mesRaw, &r.compradoRaw, &r.abonadoRaw)
}

func (r *compradoVsAbonadoRowRaw) assemble() (outbound.PuntoCompradoAbonado, error) {
	anio, err := scanIntFromAny(r.anioRaw)
	if err != nil {
		return outbound.PuntoCompradoAbonado{}, err
	}
	mes, err := scanIntFromAny(r.mesRaw)
	if err != nil {
		return outbound.PuntoCompradoAbonado{}, err
	}
	comprado, err := firebird.ScanDecimal(r.compradoRaw, 2)
	if err != nil {
		return outbound.PuntoCompradoAbonado{}, err
	}
	abonado, err := firebird.ScanDecimal(r.abonadoRaw, 2)
	if err != nil {
		return outbound.PuntoCompradoAbonado{}, err
	}
	return outbound.PuntoCompradoAbonado{
		Anio:     anio,
		Mes:      mes,
		Comprado: comprado,
		Abonado:  abonado,
	}, nil
}

// ─── pagoCrudoRowRaw ──────────────────────────────────────────────────────────

// pagoCrudoRowRaw holds raw scan targets for a single payment row returned by
// queryRitmoPagosBase. Ordering matches the SELECT column list exactly:
// FECHA, IMPORTE, DOCTO_CC_ID.
type pagoCrudoRowRaw struct {
	fechaRaw   any
	importeRaw any
	doctoCCID  int
}

func (r *pagoCrudoRowRaw) scanFrom(s scannable) error {
	return s.Scan(&r.fechaRaw, &r.importeRaw, &r.doctoCCID)
}

func (r *pagoCrudoRowRaw) assemble() (domain.PagoCrudo, error) {
	fecha, err := firebird.ScanUTCTime(r.fechaRaw)
	if err != nil {
		return domain.PagoCrudo{}, err
	}
	importe, err := firebird.ScanDecimal(r.importeRaw, 2)
	if err != nil {
		return domain.PagoCrudo{}, err
	}
	return domain.PagoCrudo{Fecha: fecha, Importe: importe, DoctoCCID: r.doctoCCID}, nil
}

// ─── ventaCrudaRowRaw ─────────────────────────────────────────────────────────

// ventaCrudaRowRaw holds raw scan targets for a single sale header row returned
// by queryRitmoVentasBase. Ordering matches the SELECT column list exactly.
type ventaCrudaRowRaw struct {
	fechaRaw      any
	totalRaw      any
	doctoPVID     int
	folio         string
	tipoStr       string
	plazoMesesRaw any
}

func (r *ventaCrudaRowRaw) scanFrom(s scannable) error {
	return s.Scan(&r.fechaRaw, &r.totalRaw, &r.doctoPVID, &r.folio, &r.tipoStr, &r.plazoMesesRaw)
}

func (r *ventaCrudaRowRaw) assemble() (domain.VentaCruda, error) {
	fecha, err := firebird.ScanUTCTime(r.fechaRaw)
	if err != nil {
		return domain.VentaCruda{}, err
	}
	total, err := firebird.ScanDecimal(r.totalRaw, 2)
	if err != nil {
		return domain.VentaCruda{}, err
	}
	plazo, perr := scanIntFromAny(r.plazoMesesRaw)
	if perr != nil {
		plazo = 0
	}
	return domain.VentaCruda{
		Fecha:      fecha,
		Total:      total,
		DoctoPvID:  r.doctoPVID,
		Folio:      r.folio,
		EsCredito:  r.tipoStr == "CREDITO",
		PlazoMeses: plazo,
	}, nil
}

// ─── shared helpers ───────────────────────────────────────────────────────────

// nullableIntVal converts sql.NullInt64 to int, returning 0 when not valid.
func nullableIntVal(v sql.NullInt64) int {
	if !v.Valid {
		return 0
	}
	return int(v.Int64)
}

// scanNullDecimalOrZero returns decimal.Zero when raw is nil or SQL NULL,
// otherwise delegates to firebird.ScanDecimal with scale 2.
func scanNullDecimalOrZero(raw any) (decimal.Decimal, error) {
	if raw == nil {
		return decimal.Zero, nil
	}
	return firebird.ScanDecimal(raw, 2)
}

// scanIntFromAny converts a Firebird EXTRACT(…) result to int.
// EXTRACT returns NUMERIC(9,0) which the driver hands as int32 or int64.
func scanIntFromAny(raw any) (int, error) {
	switch v := raw.(type) {
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case int16:
		return int(v), nil
	case float64:
		return int(v), nil
	case nil:
		return 0, nil
	default:
		// Use ScanDecimal as fallback — covers *big.Int from driver
		d, err := firebird.ScanDecimal(raw, 0)
		if err != nil {
			return 0, err
		}
		return int(d.IntPart()), nil
	}
}

// ─── pagoDetalleRowRaw ────────────────────────────────────────────────────────

// pagoDetalleRowRaw holds raw scan targets for queryPagoDetalle.
// Column order matches the SELECT list exactly.
type pagoDetalleRowRaw struct {
	doctoCCID      int
	fechaRaw       any
	folio          string
	canceladoStr   string
	aplicadoStr    string
	conceptoCCID   int
	importeRaw     any
	ivaRaw         any
	cobradorID     int
	conceptoRaw    firebird.Win1252 // CONCEPTOS_CC.NOMBRE — CHARACTER SET NONE
	cobradorNomRaw firebird.Win1252 // COBRADORES.NOMBRE — CHARACTER SET NONE
	descripcionRaw firebird.Win1252 // DOCTOS_CC.DESCRIPCION — CHARACTER SET NONE
	formaCobroID   int
	formaCobroRaw  firebird.Win1252 // FORMAS_COBRO.NOMBRE — CHARACTER SET NONE
	referenciaRaw  firebird.Win1252 // FORMAS_COBRO_DOCTOS.REFERENCIA — CHARACTER SET NONE
	aplicaACargoID int
	saldoCargoRaw  any // nullable NUMERIC from MSP_SALDOS_VENTAS
	doctoPVID      int
	// MSP_PAGOS_RECIBIDOS enrichment (all nullable)
	mspCobradorID   sql.NullInt64
	mspCobrador     sql.NullString // UTF8 — NOT Win1252
	mspFormaCobroID sql.NullInt64
	mspLat          sql.NullString
	mspLon          sql.NullString
	mspRecibidoAt   any
	mspAplicadoAt   any
}

func (r *pagoDetalleRowRaw) scanFrom(s scannable) error {
	return s.Scan(
		&r.doctoCCID,
		&r.fechaRaw,
		&r.folio,
		&r.canceladoStr,
		&r.aplicadoStr,
		&r.conceptoCCID,
		&r.importeRaw,
		&r.ivaRaw,
		&r.cobradorID,
		&r.conceptoRaw,
		&r.cobradorNomRaw,
		&r.descripcionRaw,
		&r.formaCobroID,
		&r.formaCobroRaw,
		&r.referenciaRaw,
		&r.aplicaACargoID,
		&r.saldoCargoRaw,
		&r.doctoPVID,
		&r.mspCobradorID,
		&r.mspCobrador,
		&r.mspFormaCobroID,
		&r.mspLat,
		&r.mspLon,
		&r.mspRecibidoAt,
		&r.mspAplicadoAt,
	)
}

// mspEnrichment holds the merged MSP_PAGOS_RECIBIDOS values for a pago row.
// It is populated by applyMSPEnrichment and kept separate to reduce the cognitive
// complexity of pagoDetalleRowRaw.assemble.
type mspEnrichment struct {
	cobradorID   int
	cobrador     string
	formaCobroID int
	lat          *decimal.Decimal
	lon          *decimal.Decimal
	recibidoAt   time.Time
	aplicadoAt   time.Time
	origen       string
}

// applyMSPEnrichment merges native Microsip values with MSP_PAGOS_RECIBIDOS overrides.
// When a MSP row exists it sets origen="app" and replaces each native field
// where the MSP value is non-empty / valid.
//
//nolint:cyclop // each branch checks one optional MSP field — no natural further split.
func (r *pagoDetalleRowRaw) applyMSPEnrichment(
	nativeCobradorID int,
	nativeCobrador string,
	nativeFormaCobroID int,
) mspEnrichment {
	e := mspEnrichment{
		cobradorID:   nativeCobradorID,
		cobrador:     nativeCobrador,
		formaCobroID: nativeFormaCobroID,
		origen:       "microsip",
	}

	// Any non-null MSP column means a row joined — use all nullable fields so
	// a pago with only GPS or timestamps populated still yields origen="app".
	hasMSP := r.mspCobradorID.Valid || r.mspCobrador.Valid || r.mspFormaCobroID.Valid ||
		r.mspLat.Valid || r.mspLon.Valid || r.mspRecibidoAt != nil || r.mspAplicadoAt != nil
	if !hasMSP {
		return e
	}

	e.origen = "app"
	if r.mspCobradorID.Valid {
		e.cobradorID = int(r.mspCobradorID.Int64)
	}
	if r.mspCobrador.Valid && r.mspCobrador.String != "" {
		e.cobrador = r.mspCobrador.String
	}
	if r.mspFormaCobroID.Valid {
		e.formaCobroID = int(r.mspFormaCobroID.Int64)
	}
	if r.mspLat.Valid && r.mspLat.String != "" {
		if d, err := decimal.NewFromString(strings.TrimSpace(r.mspLat.String)); err == nil {
			e.lat = &d
		}
	}
	if r.mspLon.Valid && r.mspLon.String != "" {
		if d, err := decimal.NewFromString(strings.TrimSpace(r.mspLon.String)); err == nil {
			e.lon = &d
		}
	}
	if r.mspRecibidoAt != nil {
		if t, err := firebird.ScanUTCTime(r.mspRecibidoAt); err == nil {
			e.recibidoAt = t
		}
	}
	if r.mspAplicadoAt != nil {
		if t, err := firebird.ScanUTCTime(r.mspAplicadoAt); err == nil {
			e.aplicadoAt = t
		}
	}
	return e
}

func (r *pagoDetalleRowRaw) assemble() (outbound.PagoDetalle, error) {
	fecha, err := firebird.ScanUTCTime(r.fechaRaw)
	if err != nil {
		return outbound.PagoDetalle{}, err
	}
	importe, err := firebird.ScanDecimal(r.importeRaw, 2)
	if err != nil {
		return outbound.PagoDetalle{}, err
	}
	iva, err := firebird.ScanDecimal(r.ivaRaw, 2)
	if err != nil {
		return outbound.PagoDetalle{}, err
	}

	// Saldo cargo is nullable (cargo may not be in MSP_SALDOS_VENTAS cache).
	// Silent nil-on-parse-error is intentional: saldo_cargo is optional enrichment,
	// consistent with the codebase convention for best-effort cached fields.
	var saldoCargo *decimal.Decimal
	if r.saldoCargoRaw != nil {
		if s, serr := firebird.ScanDecimal(r.saldoCargoRaw, 2); serr == nil {
			saldoCargo = &s
		}
	}

	enr := r.applyMSPEnrichment(
		r.cobradorID,
		coalesce1252(r.cobradorNomRaw, r.descripcionRaw),
		r.formaCobroID,
	)

	return outbound.PagoDetalle{
		DoctoCCID:      r.doctoCCID,
		Fecha:          fecha,
		Folio:          r.folio,
		Cancelado:      r.canceladoStr == "S",
		Aplicado:       r.aplicadoStr == "S",
		Importe:        importe,
		IVA:            iva,
		ConceptoCCID:   r.conceptoCCID,
		Concepto:       string(r.conceptoRaw),
		Categoria:      string(domain.ClasificarConcepto(r.conceptoCCID)),
		CobradorID:     enr.cobradorID,
		Cobrador:       enr.cobrador,
		FormaCobroID:   enr.formaCobroID,
		FormaCobro:     string(r.formaCobroRaw),
		Referencia:     string(r.referenciaRaw),
		AplicaACargoID: r.aplicaACargoID,
		SaldoCargo:     saldoCargo,
		DoctoPVID:      r.doctoPVID,
		Lat:            enr.lat,
		Lon:            enr.lon,
		RecibidoAt:     enr.recibidoAt,
		AplicadoAt:     enr.aplicadoAt,
		Origen:         enr.origen,
	}, nil
}

// coalesce1252 returns the string of a when non-empty, else b.
// Both are Win1252-decoded strings from CHARACTER SET NONE columns.
func coalesce1252(a, b firebird.Win1252) string {
	if s := string(a); s != "" {
		return s
	}
	return string(b)
}
