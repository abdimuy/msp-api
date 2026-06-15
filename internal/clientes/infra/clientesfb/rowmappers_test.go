//nolint:misspell // Spanish domain vocabulary by project convention.
package clientesfb

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
)

// ─── tipoVentaFromStr ─────────────────────────────────────────────────────────

func TestTipoVentaFromStr_Credito(t *testing.T) {
	t.Parallel()
	assert.Equal(t, domain.TipoVentaCredito, tipoVentaFromStr("CREDITO"))
}

func TestTipoVentaFromStr_Contado(t *testing.T) {
	t.Parallel()
	assert.Equal(t, domain.TipoVentaContado, tipoVentaFromStr("CONTADO"))
}

func TestTipoVentaFromStr_EmptyFallsToContado(t *testing.T) {
	t.Parallel()
	// Unexpected value defaults to CONTADO (safe: show less credit data rather
	// than incorrect credit contract)
	assert.Equal(t, domain.TipoVentaContado, tipoVentaFromStr(""))
}

func TestTipoVentaFromStr_UnknownFallsToContado(t *testing.T) {
	t.Parallel()
	assert.Equal(t, domain.TipoVentaContado, tipoVentaFromStr("OTRO"))
}

// ─── collectVendedores ────────────────────────────────────────────────────────

func TestCollectVendedores_AllThree(t *testing.T) {
	t.Parallel()
	got := collectVendedores("RUTA01", "RUTA02", "RUTA03")
	require.Len(t, got, 3)
	assert.Equal(t, []string{"RUTA01", "RUTA02", "RUTA03"}, got)
}

func TestCollectVendedores_SomeEmpty(t *testing.T) {
	t.Parallel()
	got := collectVendedores("RUTA01", "", "RUTA03")
	require.Len(t, got, 2)
	assert.Equal(t, []string{"RUTA01", "RUTA03"}, got)
}

func TestCollectVendedores_AllEmpty(t *testing.T) {
	t.Parallel()
	got := collectVendedores("", "", "")
	assert.Empty(t, got)
}

func TestCollectVendedores_Duplicates_Deduped(t *testing.T) {
	t.Parallel()
	got := collectVendedores("RUTA01", "RUTA01", "RUTA01")
	require.Len(t, got, 1)
	assert.Equal(t, "RUTA01", got[0])
}

func TestCollectVendedores_Whitespace_Trimmed(t *testing.T) {
	t.Parallel()
	got := collectVendedores("  RUTA01  ", "", "")
	require.Len(t, got, 1)
	assert.Equal(t, "RUTA01", got[0])
}

// ─── nullableIntVal ───────────────────────────────────────────────────────────

func TestNullableIntVal_Valid(t *testing.T) {
	t.Parallel()
	v := nullableInt64(42)
	assert.Equal(t, 42, nullableIntVal(v))
}

func TestNullableIntVal_Invalid(t *testing.T) {
	t.Parallel()
	v := nullableInt64Invalid()
	assert.Equal(t, 0, nullableIntVal(v))
}

// ─── searchDocRaw.assembleSearchDoc ──────────────────────────────────────────

func TestSearchDocRaw_AllFields(t *testing.T) {
	t.Parallel()
	raw := searchDocRaw{
		clienteID:  12345,
		nombreRaw:  "GARCIA MARTINEZ JOSE",
		calleRaw:   "CALLE INDEPENDENCIA",
		coloniaRaw: "CENTRO",
		poblRaw:    "TEHUACAN",
	}
	doc := raw.assembleSearchDoc()
	assert.Equal(t, 12345, doc.ClienteID)
	assert.Equal(t, "GARCIA MARTINEZ JOSE CALLE INDEPENDENCIA CENTRO TEHUACAN", doc.Texto)
}

func TestSearchDocRaw_EmptyFields_Skipped(t *testing.T) {
	t.Parallel()
	raw := searchDocRaw{
		clienteID:  99,
		nombreRaw:  "JUAN PEREZ",
		calleRaw:   "",
		coloniaRaw: "  ", // whitespace only → trimmed to empty → skipped
		poblRaw:    "PUEBLA",
	}
	doc := raw.assembleSearchDoc()
	assert.Equal(t, "JUAN PEREZ PUEBLA", doc.Texto)
}

func TestSearchDocRaw_OnlyNombre(t *testing.T) {
	t.Parallel()
	raw := searchDocRaw{
		clienteID: 1,
		nombreRaw: "SOLO NOMBRE",
	}
	doc := raw.assembleSearchDoc()
	assert.Equal(t, "SOLO NOMBRE", doc.Texto)
}

// ─── scanIntFromAny ───────────────────────────────────────────────────────────

func TestScanIntFromAny_Int32(t *testing.T) {
	t.Parallel()
	v, err := scanIntFromAny(int32(2026))
	require.NoError(t, err)
	assert.Equal(t, 2026, v)
}

func TestScanIntFromAny_Int64(t *testing.T) {
	t.Parallel()
	v, err := scanIntFromAny(int64(12))
	require.NoError(t, err)
	assert.Equal(t, 12, v)
}

func TestScanIntFromAny_Float64(t *testing.T) {
	t.Parallel()
	v, err := scanIntFromAny(float64(6))
	require.NoError(t, err)
	assert.Equal(t, 6, v)
}

func TestScanIntFromAny_Nil(t *testing.T) {
	t.Parallel()
	v, err := scanIntFromAny(nil)
	require.NoError(t, err)
	assert.Equal(t, 0, v)
}

// ─── scanNullDecimalOrZero ────────────────────────────────────────────────────

func TestScanNullDecimalOrZero_Nil_ReturnsZero(t *testing.T) {
	t.Parallel()
	d, err := scanNullDecimalOrZero(nil)
	require.NoError(t, err)
	assert.True(t, d.IsZero())
}

func TestScanNullDecimalOrZero_Int64_ScalesCorrectly(t *testing.T) {
	t.Parallel()
	// Driver hands back int64(850) for NUMERIC(_,2) column with value 8.50
	d, err := scanNullDecimalOrZero(int64(850))
	require.NoError(t, err)
	assert.Equal(t, "8.5", d.String())
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func nullableInt64(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: true}
}

func nullableInt64Invalid() sql.NullInt64 {
	return sql.NullInt64{}
}
