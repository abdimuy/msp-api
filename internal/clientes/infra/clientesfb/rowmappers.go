//nolint:misspell // Spanish domain vocabulary by project convention.
package clientesfb

import (
	"database/sql"
	"strings"

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
		Telefono: string(r.telefonoRaw),
	}), nil
}

// ─── directorioRowRaw ────────────────────────────────────────────────────────

// directorioRowRaw extends clienteRowRaw with the aggregated saldo field.
// Ordering matches selectDirectorioCols: all clienteCols then saldo.
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
		// When saldo subquery returns NULL (no CC), COALESCE gives 0.
		// If the driver still sends a nil (unexpected), treat as zero.
		saldo = decimal.Zero
	}
	return outbound.DirectorioItem{
		Cliente:    c,
		SaldoTotal: saldo,
	}, nil
}

// ─── ventaClienteRowRaw ───────────────────────────────────────────────────────

// ventaClienteRowRaw holds raw scan targets for a VentaCliente row.
// Ordering matches selectVentaClienteCols exactly.
type ventaClienteRowRaw struct {
	doctoPVID   int
	clienteID   int
	fechaRaw    any
	folio       string
	importeRaw  any
	tipoStr     string
	saldoRaw    any
	numPagosRaw any
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
		saldo = decimal.Zero
	}
	numPagos := 0
	if r.numPagosRaw != nil {
		if n, ok := r.numPagosRaw.(int64); ok {
			numPagos = int(n)
		}
	}
	tipo := tipoVentaFromStr(r.tipoStr)
	return domain.HydrateVentaCliente(domain.HydrateVentaClienteParams{
		DoctoPVID:  r.doctoPVID,
		ClienteID:  r.clienteID,
		Fecha:      fecha,
		Folio:      r.folio,
		Tipo:       tipo,
		Total:      total,
		SaldoVenta: saldo,
		NumPagos:   numPagos,
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
	vendedores := collectVendedores(
		string(r.vendedor1Raw),
		string(r.vendedor2Raw),
		string(r.vendedor3Raw),
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
}

func (r *pagoRowRaw) scanFrom(s scannable) error {
	return s.Scan(
		&r.doctoCCID,
		&r.fechaRaw,
		&r.importeRaw,
		&r.formaCobroRaw,
		&r.cargoIDRaw,
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
	}), nil
}

// ─── resumenFichaTotalesRaw ───────────────────────────────────────────────────

// resumenFichaTotalesRaw holds raw scan targets for the ficha totals query.
type resumenFichaTotalesRaw struct {
	totalCompradoRaw any
	totalAbonadoRaw  any
	numVentasRaw     int
	numPagosRaw      int
}

func (r *resumenFichaTotalesRaw) scanFrom(s scannable) error {
	return s.Scan(
		&r.totalCompradoRaw,
		&r.totalAbonadoRaw,
		&r.numVentasRaw,
		&r.numPagosRaw,
	)
}

func (r *resumenFichaTotalesRaw) assemble() (decimal.Decimal, decimal.Decimal, int, int, error) {
	totalComprado, err := firebird.ScanDecimal(r.totalCompradoRaw, 2)
	if err != nil {
		return decimal.Zero, decimal.Zero, 0, 0, err
	}
	totalAbonado, err := firebird.ScanDecimal(r.totalAbonadoRaw, 2)
	if err != nil {
		return decimal.Zero, decimal.Zero, 0, 0, err
	}
	return totalComprado, totalAbonado, r.numVentasRaw, r.numPagosRaw, nil
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

// ─── searchDocRaw ─────────────────────────────────────────────────────────────

// searchDocRaw holds raw scan targets for the search document query.
type searchDocRaw struct {
	clienteID  int
	nombreRaw  firebird.Win1252
	calleRaw   firebird.Win1252
	coloniaRaw firebird.Win1252
	poblRaw    firebird.Win1252
}

func (r *searchDocRaw) scanFrom(s scannable) error {
	return s.Scan(
		&r.clienteID,
		&r.nombreRaw,
		&r.calleRaw,
		&r.coloniaRaw,
		&r.poblRaw,
	)
}

// assembleSearchDoc builds the SearchDoc from Win1252-decoded text fields.
// Texto concatenates all available text so the index can match on any component.
func (r *searchDocRaw) assembleSearchDoc() outbound.SearchDoc {
	parts := make([]string, 0, 4)
	if v := strings.TrimSpace(string(r.nombreRaw)); v != "" {
		parts = append(parts, v)
	}
	if v := strings.TrimSpace(string(r.calleRaw)); v != "" {
		parts = append(parts, v)
	}
	if v := strings.TrimSpace(string(r.coloniaRaw)); v != "" {
		parts = append(parts, v)
	}
	if v := strings.TrimSpace(string(r.poblRaw)); v != "" {
		parts = append(parts, v)
	}
	return outbound.SearchDoc{
		ClienteID: r.clienteID,
		Texto:     strings.Join(parts, " "),
	}
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
