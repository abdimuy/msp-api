//nolint:misspell // Spanish vocabulary (productos, descripcion) by convention.
package ventfb

// Pure unit tests for the rowmappers in this package. These exercise the
// scanning helpers without touching a real database by feeding a
// mockRowScanner that satisfies the unexported rowScanner interface.
//
// Bugs in encoding, decimal precision, or timestamp parsing silently corrupt
// persisted state — these tests pin every error and edge case directly.

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"testing/quick"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// mockRowScanner satisfies the unexported rowScanner interface. It returns
// the configured values into the destination pointers in order, mimicking
// what *sql.Row / *sql.Rows would do — including invoking sql.Scanner.Scan
// for destinations that implement it (Win1252, sql.NullString, …).
type mockRowScanner struct {
	values []any
	err    error
}

func (m *mockRowScanner) Scan(dest ...any) error {
	if m.err != nil {
		return m.err
	}
	if len(dest) != len(m.values) {
		return fmt.Errorf("mockRowScanner: %d dests, %d values", len(dest), len(m.values))
	}
	for i, d := range dest {
		if err := assignTo(d, m.values[i]); err != nil {
			return fmt.Errorf("col %d (%T): %w", i, d, err)
		}
	}
	return nil
}

// assignTo emulates database/sql's Scan dispatch enough for these tests:
//   - direct same-type assignment short-circuits to a copy (avoids invoking
//     sql.NullString.Scan with a sql.NullString src, which it rejects).
//   - sql.Scanner implementations (Win1252) get their Scan called.
//   - *any destinations receive src verbatim — same as the real driver does
//     for raw timestamp / decimal columns.
//   - other typed pointers use reflect to convert when assignable.
func assignTo(dest, src any) error {
	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Pointer || dv.IsNil() {
		return errors.New("dest must be non-nil pointer")
	}
	elem := dv.Elem()
	if src != nil {
		sv := reflect.ValueOf(src)
		if sv.Type().AssignableTo(elem.Type()) {
			elem.Set(sv)
			return nil
		}
	}
	if scanner, ok := dest.(sql.Scanner); ok {
		return scanner.Scan(src)
	}
	if dst, ok := dest.(*any); ok {
		*dst = src
		return nil
	}
	if src == nil {
		elem.Set(reflect.Zero(elem.Type()))
		return nil
	}
	sv := reflect.ValueOf(src)
	if sv.Type().ConvertibleTo(elem.Type()) {
		elem.Set(sv.Convert(elem.Type()))
		return nil
	}
	return fmt.Errorf("cannot assign %T to %T", src, dest)
}

// ─── parseUUIDColumn ───────────────────────────────────────────────────────

func TestParseUUIDColumn_Empty(t *testing.T) {
	t.Parallel()
	_, err := parseUUIDColumn("ID", "")
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "firebird_uuid_invalid", ae.Code)
	assert.Equal(t, "ID", ae.Fields["column"])
}

func TestParseUUIDColumn_Malformed(t *testing.T) {
	t.Parallel()
	_, err := parseUUIDColumn("CREATED_BY", "not-a-uuid-but-thirtysix-chars-XX")
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "firebird_uuid_invalid", ae.Code)
	assert.Equal(t, "CREATED_BY", ae.Fields["column"])
}

func TestParseUUIDColumn_ValidString(t *testing.T) {
	t.Parallel()
	want := uuid.New()
	got, err := parseUUIDColumn("ID", want.String())
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestParseUUIDColumn_NilUUIDString(t *testing.T) {
	t.Parallel()
	got, err := parseUUIDColumn("ID", "00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
	assert.Equal(t, uuid.Nil, got)
}

func TestParseNullUUIDColumn_Null(t *testing.T) {
	t.Parallel()
	got, err := parseNullUUIDColumn("CANCELED_BY", sql.NullString{Valid: false})
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestParseNullUUIDColumn_ValidWraps(t *testing.T) {
	t.Parallel()
	want := uuid.New()
	got, err := parseNullUUIDColumn("CANCELED_BY", sql.NullString{String: want.String(), Valid: true})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want, *got)
}

func TestParseNullUUIDColumn_ValidButMalformed(t *testing.T) {
	t.Parallel()
	_, err := parseNullUUIDColumn("CANCELED_BY", sql.NullString{String: "garbage", Valid: true})
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "firebird_uuid_invalid", ae.Code)
}

// ─── parseVentaMontos ──────────────────────────────────────────────────────

// fillRequiredFields populates the parts of ventaRowRaw that parseVentaMontos
// reads. Other fields are left at their zero values.
func ventaRowMontos(anual, cortoPlazo, contado any) *ventaRowRaw {
	return &ventaRowRaw{
		montoAnualRaw:      anual,
		montoCortoPlazoRaw: cortoPlazo,
		montoContadoRaw:    contado,
	}
}

func TestParseVentaMontos_AtMaxValue(t *testing.T) {
	t.Parallel()
	maxMonto := domain.MaxMontoVenta // 999999999999.99
	r := ventaRowMontos(maxMonto, maxMonto, maxMonto)
	got, err := parseVentaMontos(r)
	require.NoError(t, err)
	assert.True(t, got.Anual().Equal(maxMonto))
	assert.True(t, got.CortoPlazo().Equal(maxMonto))
	assert.True(t, got.Contado().Equal(maxMonto))
}

func TestParseVentaMontos_Zero(t *testing.T) {
	t.Parallel()
	r := ventaRowMontos(decimal.Zero, decimal.Zero, decimal.Zero)
	got, err := parseVentaMontos(r)
	require.NoError(t, err)
	assert.True(t, got.Anual().IsZero())
	assert.True(t, got.CortoPlazo().IsZero())
	assert.True(t, got.Contado().IsZero())
}

func TestParseVentaMontos_NegativeRoundTrip(t *testing.T) {
	t.Parallel()
	// parseVentaMontos itself is sign-agnostic — domain validation is what
	// rejects negatives at construction. This pins the mapper boundary: it
	// returns whatever the driver gave it without sign-mangling.
	neg := decimal.RequireFromString("-1500.00")
	r := ventaRowMontos(neg, neg, neg)
	got, err := parseVentaMontos(r)
	require.NoError(t, err)
	assert.True(t, got.Anual().Equal(neg))
}

func TestParseVentaMontos_Int64ScaleRecovery(t *testing.T) {
	t.Parallel()
	// When the driver returns int64 (scale-non-negative path of ScanDecimal),
	// the mapper must recover the decimal point at scale=2.
	// 150_050 with scale 2 → 1500.50.
	r := ventaRowMontos(int64(150050), int64(0), int64(0))
	got, err := parseVentaMontos(r)
	require.NoError(t, err)
	assert.True(t, got.Anual().Equal(decimal.RequireFromString("1500.50")),
		"expected 1500.50, got %s", got.Anual())
}

func TestParseVentaMontos_NilColumnIsScanError(t *testing.T) {
	t.Parallel()
	r := ventaRowMontos(nil, decimal.Zero, decimal.Zero)
	_, err := parseVentaMontos(r)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "firebird_scan_error", ae.Code)
}

func TestParseVentaMontos_PropertyRoundTrip(t *testing.T) {
	t.Parallel()
	// For any decimal in the contract domain (≤ MaxMontoVenta, scale ≤ 2),
	// parseVentaMontos returns byte-equal output for a decimal.Decimal input.
	prop := func(cents uint32) bool {
		// Constrain cents to ≤ 99_999_999_999_999 (max NUMERIC(14,2) cents).
		// uint32 maxes at ~4.3e9, comfortably under the cap.
		d := decimal.New(int64(cents), -2)
		r := ventaRowMontos(d, d, d)
		got, err := parseVentaMontos(r)
		if err != nil {
			t.Logf("parseVentaMontos errored on %s: %v", d, err)
			return false
		}
		return got.Anual().Equal(d) && got.CortoPlazo().Equal(d) && got.Contado().Equal(d)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatal(err)
	}
}

// ─── scanCombo ─────────────────────────────────────────────────────────────

// comboRow returns a value slice in the exact order scanCombo expects. Caller
// can override individual fields by mutating the returned slice.
func comboRow(t *testing.T) []any {
	t.Helper()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	return []any{
		uuid.New().String(),                 // ID
		utf8Bytes(t, "Combo Especial ñá"),   // NOMBRE_COMBO (Win1252 bytes)
		decimal.RequireFromString("100.50"), // PRECIO_ANUAL
		decimal.RequireFromString("90.25"),  // PRECIO_CORTO_PLAZO
		decimal.RequireFromString("80.00"),  // PRECIO_CONTADO
		decimal.RequireFromString("2.5000"), // CANTIDAD
		1,                                   // ALMACEN_ORIGEN_ID
		2,                                   // ALMACEN_DESTINO_ID
		now,                                 // CREATED_AT
		now,                                 // UPDATED_AT
		uuid.New().String(),                 // CREATED_BY
		uuid.New().String(),                 // UPDATED_BY
	}
}

// utf8Bytes returns the raw UTF-8 bytes for s. Kept as a helper for tests that
// want to assert byte-level identity through the rowmapper — post UTF8 column
// migration the bytes are just plain UTF-8.
func utf8Bytes(_ *testing.T, s string) string { return s }

func TestScanCombo_Happy(t *testing.T) {
	t.Parallel()
	row := comboRow(t)
	combo, err := scanCombo(&mockRowScanner{values: row})
	require.NoError(t, err)
	require.NotNil(t, combo)
	assert.Equal(t, "Combo Especial ñá", combo.Nombre())
	assert.True(t, combo.Cantidad().Equal(decimal.RequireFromString("2.5000")))
	assert.True(t, combo.Precios().Anual().Equal(decimal.RequireFromString("100.50")))
}

func TestScanCombo_PrecisionEdgeCase(t *testing.T) {
	t.Parallel()
	row := comboRow(t)
	row[5] = decimal.RequireFromString("0.0001") // CANTIDAD min positive
	row[2] = domain.MaxMontoVenta                // PRECIO_ANUAL max
	combo, err := scanCombo(&mockRowScanner{values: row})
	require.NoError(t, err)
	assert.True(t, combo.Cantidad().Equal(decimal.RequireFromString("0.0001")))
	assert.True(t, combo.Precios().Anual().Equal(domain.MaxMontoVenta))
}

func TestScanCombo_BadUUIDSurfacesAppError(t *testing.T) {
	t.Parallel()
	row := comboRow(t)
	row[0] = "not-a-uuid-thirtysix-chars-XXXXXXXXX"
	_, err := scanCombo(&mockRowScanner{values: row})
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "firebird_uuid_invalid", ae.Code)
}

func TestScanCombo_ScannerErrorPropagates(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("driver exploded")
	_, err := scanCombo(&mockRowScanner{err: sentinel})
	require.ErrorIs(t, err, sentinel)
}

// ─── scanProducto ─────────────────────────────────────────────────────────

func productoRow(t *testing.T) []any {
	t.Helper()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	return []any{
		uuid.New().String(),                 // ID
		100,                                 // ARTICULO_ID
		utf8Bytes(t, "Mesa redonda café"),   // ARTICULO
		decimal.RequireFromString("3.0000"), // CANTIDAD
		decimal.RequireFromString("200.00"), // PRECIO_ANUAL
		decimal.RequireFromString("180.00"), // PRECIO_CORTO_PLAZO
		decimal.RequireFromString("150.00"), // PRECIO_CONTADO
		sql.NullString{Valid: false},        // COMBO_ID null
		sql.NullInt32{Valid: false},         // ALMACEN_ORIGEN_ID null
		sql.NullInt32{Valid: false},         // ALMACEN_DESTINO_ID null
		now,                                 // CREATED_AT
		now,                                 // UPDATED_AT
		uuid.New().String(),                 // CREATED_BY
		uuid.New().String(),                 // UPDATED_BY
	}
}

func TestScanProducto_NullableComboAndAlmacenes(t *testing.T) {
	t.Parallel()
	row := productoRow(t)
	p, err := scanProducto(&mockRowScanner{values: row})
	require.NoError(t, err)
	assert.Nil(t, p.ComboID())
	assert.Nil(t, p.AlmacenOrigen())
	assert.Nil(t, p.AlmacenDestino())
	assert.Equal(t, "Mesa redonda café", p.Articulo())
}

func TestScanProducto_CantidadScale4Boundary(t *testing.T) {
	t.Parallel()
	row := productoRow(t)
	// Max NUMERIC(10,4) value: 999999.9999
	row[3] = decimal.RequireFromString("999999.9999")
	p, err := scanProducto(&mockRowScanner{values: row})
	require.NoError(t, err)
	assert.True(t, p.Cantidad().Equal(decimal.RequireFromString("999999.9999")))

	// Min positive value.
	row[3] = decimal.RequireFromString("0.0001")
	p, err = scanProducto(&mockRowScanner{values: row})
	require.NoError(t, err)
	assert.True(t, p.Cantidad().Equal(decimal.RequireFromString("0.0001")))
}

func TestScanProducto_ComboIDAndAlmacenesPopulated(t *testing.T) {
	t.Parallel()
	row := productoRow(t)
	comboID := uuid.New()
	row[7] = sql.NullString{String: comboID.String(), Valid: true}
	row[8] = sql.NullInt32{Int32: 5, Valid: true}
	row[9] = sql.NullInt32{Int32: 7, Valid: true}
	p, err := scanProducto(&mockRowScanner{values: row})
	require.NoError(t, err)
	require.NotNil(t, p.ComboID())
	assert.Equal(t, comboID, *p.ComboID())
	require.NotNil(t, p.AlmacenOrigen())
	assert.Equal(t, 5, *p.AlmacenOrigen())
	require.NotNil(t, p.AlmacenDestino())
	assert.Equal(t, 7, *p.AlmacenDestino())
}

// ─── scanVendedor ─────────────────────────────────────────────────────────

func TestScanVendedor_Win1252Decoded(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	row := []any{
		uuid.New().String(),
		uuid.New().String(),
		"vendedor@muebleriamsp.mx",
		utf8Bytes(t, "Vendedor Núñez"),
		now, now,
		uuid.New().String(), uuid.New().String(),
	}
	v, err := scanVendedor(&mockRowScanner{values: row})
	require.NoError(t, err)
	assert.Equal(t, "Vendedor Núñez", v.Snapshot().Nombre())
	assert.Equal(t, "vendedor@muebleriamsp.mx", v.Snapshot().Email())
}

// ─── scanImagen ───────────────────────────────────────────────────────────

func imagenRow(t *testing.T) []any {
	t.Helper()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	return []any{
		uuid.New().String(),                  // ID
		string(domain.StorageKindFilesystem), // STORAGE_KIND
		"ventas/2026/05/abc.jpg",             // STORAGE_KEY
		domain.MimeJPEG,                      // MIME
		int64(2048),                          // SIZE_BYTES
		sql.NullString{Valid: false},         // DESCRIPCION null
		now,                                  // CREATED_AT
		now,                                  // UPDATED_AT
		uuid.New().String(),                  // CREATED_BY
		uuid.New().String(),                  // UPDATED_BY
	}
}

func TestScanImagen_NullDescripcion(t *testing.T) {
	t.Parallel()
	row := imagenRow(t)
	img, err := scanImagen(&mockRowScanner{values: row})
	require.NoError(t, err)
	assert.Nil(t, img.Descripcion(), "null DESCRIPCION must surface as nil pointer")
	assert.Equal(t, "ventas/2026/05/abc.jpg", img.Storage().Key())
	assert.Equal(t, domain.StorageKindFilesystem, img.Storage().Kind())
	assert.Equal(t, domain.MimeJPEG, img.Mime())
	assert.Equal(t, int64(2048), img.SizeBytes())
}

func TestScanImagen_DescripcionWin1252Decoded(t *testing.T) {
	t.Parallel()
	row := imagenRow(t)
	row[5] = sql.NullString{String: utf8Bytes(t, "Recibo de cobranza ñ"), Valid: true}
	img, err := scanImagen(&mockRowScanner{values: row})
	require.NoError(t, err)
	require.NotNil(t, img.Descripcion())
	assert.Equal(t, "Recibo de cobranza ñ", *img.Descripcion())
}

func TestScanImagen_StorageKeyPreserved(t *testing.T) {
	t.Parallel()
	row := imagenRow(t)
	row[2] = "ventas/special-chars/key with spaces/abc.jpg"
	img, err := scanImagen(&mockRowScanner{values: row})
	require.NoError(t, err)
	assert.Equal(t, "ventas/special-chars/key with spaces/abc.jpg", img.Storage().Key())
}

// ─── assembleVenta ────────────────────────────────────────────────────────

// completeVentaRowRaw builds a fully populated *ventaRowRaw mimicking what
// scanVentaRowRaw would produce for a happy-path CONTADO header row.
func completeVentaRowRaw(t *testing.T) *ventaRowRaw {
	t.Helper()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	return &ventaRowRaw{
		idRaw:              uuid.New().String(),
		nombreCliente:      "Cliente Núñez",
		telefono:           sql.NullString{Valid: false},
		avalOResponsable:   sql.NullString{Valid: false},
		calle:              "Av. Reforma",
		numeroExterior:     sql.NullString{Valid: false},
		colonia:            "Centro",
		poblacion:          "CDMX",
		ciudad:             "CDMX",
		zonaClienteID:      sql.NullInt32{Valid: false},
		latitud:            19.4326,
		longitud:           -99.1332,
		fechaVentaRaw:      now,
		tipoVenta:          string(domain.TipoVentaContado),
		montoAnualRaw:      decimal.RequireFromString("1500.00"),
		montoCortoPlazoRaw: decimal.RequireFromString("1300.00"),
		montoContadoRaw:    decimal.RequireFromString("1000.00"),
		plazoMeses:         sql.NullInt32{Valid: false},
		engancheRaw:        decimal.Zero,
		parcialidadRaw:     decimal.Zero,
		frecPago:           sql.NullString{Valid: false},
		diaCobranzaSemana:  sql.NullString{Valid: false},
		diaCobranzaMes:     sql.NullInt32{Valid: false},
		nota:               sql.NullString{Valid: false},
		createdAtRaw:       now,
		updatedAtRaw:       now,
		createdByRaw:       uuid.New().String(),
		updatedByRaw:       uuid.New().String(),
		canceledAtRaw:      nil,
		canceledByRaw:      sql.NullString{Valid: false},
		cancelReason:       sql.NullString{Valid: false},
		clienteID:          sql.NullInt32{Valid: false},
		status:             string(domain.EstadoActive),
		approvedAtRaw:      nil,
		approvedByRaw:      sql.NullString{Valid: false},
		situacion:          string(domain.SituacionBorrador),
		sincronizacion:     string(domain.SincronizacionPendiente),
	}
}

func TestAssembleVenta_HappyPath(t *testing.T) {
	t.Parallel()
	r := completeVentaRowRaw(t)
	v, err := assembleVenta(r, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, "Cliente Núñez", v.Cliente().Nombre().Value())
	assert.Equal(t, domain.TipoVentaContado, v.TipoVenta())
	assert.Nil(t, v.PlanCredito())
	assert.Nil(t, v.DiaCobranza())
	assert.Nil(t, v.Cancelacion())
	assert.Nil(t, v.Aprobacion())
}

func TestAssembleVenta_BadHeaderUUIDSurfacesAppError(t *testing.T) {
	t.Parallel()
	r := completeVentaRowRaw(t)
	r.idRaw = "garbage"
	_, err := assembleVenta(r, nil, nil, nil, nil)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "firebird_uuid_invalid", ae.Code)
	assert.Equal(t, "ID", ae.Fields["column"])
}

func TestAssembleVenta_NotaWin1252DecodedWhenValid(t *testing.T) {
	t.Parallel()
	r := completeVentaRowRaw(t)
	// Feed Win1252 bytes (as string) to mimic what the driver hands to
	// sql.NullString from an ISO8859_1 column — assembleVenta decodes through
	// firebird.Win1252.Scan.
	r.nota = sql.NullString{String: utf8Bytes(t, "Una nota con ñ y é"), Valid: true}
	v, err := assembleVenta(r, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, v.Nota())
	assert.Equal(t, "Una nota con ñ y é", *v.Nota())
}

func TestAssembleVenta_OptionalClienteIDPropagates(t *testing.T) {
	t.Parallel()
	r := completeVentaRowRaw(t)
	r.clienteID = sql.NullInt32{Int32: 12345, Valid: true}
	v, err := assembleVenta(r, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, v.ClienteID())
	assert.Equal(t, 12345, *v.ClienteID())
}

func TestAssembleVenta_DireccionAndOptionalsPreserved(t *testing.T) {
	t.Parallel()
	r := completeVentaRowRaw(t)
	r.numeroExterior = sql.NullString{String: utf8Bytes(t, "42-B"), Valid: true}
	r.zonaClienteID = sql.NullInt32{Int32: 7, Valid: true}
	r.telefono = sql.NullString{String: "5551234567", Valid: true}
	r.avalOResponsable = sql.NullString{String: utf8Bytes(t, "Avalista Pérez"), Valid: true}
	v, err := assembleVenta(r, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, v.Direccion().NumeroExterior())
	assert.Equal(t, "42-B", *v.Direccion().NumeroExterior())
	require.NotNil(t, v.Direccion().ZonaClienteID())
	assert.Equal(t, 7, *v.Direccion().ZonaClienteID())
	require.NotNil(t, v.Cliente().Telefono())
	require.NotNil(t, v.Cliente().Aval())
	assert.Equal(t, "Avalista Pérez", v.Cliente().Aval().Value())
}

// ─── Property: datetime round-trip ─────────────────────────────────────────

// TestProperty_DatetimeRoundTrip pins the Firebird datetime contract:
// ScanUTCTime ∘ ToWallClock is the identity on UTC instants whose precision
// fits inside one nanosecond. The mapper relies on this round-trip for every
// CREATED_AT / UPDATED_AT / FECHA_VENTA / APPROVED_AT column.
func TestProperty_DatetimeRoundTrip(t *testing.T) {
	t.Parallel()
	// Year/month/day/hour/minute/second/nanosecond generated within safe
	// bounds (avoid pre-1970 and post-9999, and DST transitions in CDMX —
	// post-2022 CDMX has no DST so any year ≥ 2023 is safe).
	prop := func(daysSince int32, secOfDay, nanos uint32) bool {
		base := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		// Constrain to ~100 years out so we stay inside zoneinfo bounds.
		days := int(daysSince%(365*100)) + 365*100
		secs := int(secOfDay % 86400)
		nanos %= 1_000_000_000
		instant := base.AddDate(0, 0, days).Add(time.Duration(secs)*time.Second + time.Duration(nanos))

		wall := firebird.ToWallClock(instant)
		// ScanUTCTime accepts time.Time directly.
		got, err := firebird.ScanUTCTime(wall)
		if err != nil {
			t.Logf("ScanUTCTime failed for %v: %v", instant, err)
			return false
		}
		// Compare instants — they must represent the same moment.
		return got.Equal(instant)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatal(err)
	}
}
