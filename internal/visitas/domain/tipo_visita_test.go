//nolint:misspell // visitas vocabulary is Spanish per project convention.
package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.org/x/text/unicode/norm"

	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// ─── The measurement, pinned ────────────────────────────────────────────────

// tiposMedidosEnProduccion is the EXACT result of the measurement that closed
// this catalogue, run against MSP_VISITAS (226,746 rows):
//
//	SELECT TIPO_VISITA, COUNT(*) FROM MSP_VISITAS GROUP BY TIPO_VISITA;
//
// It returned 15 groups summing to exactly 226,746 — these 14 non-empty
// values plus the empty string (200 rows, handled separately below).
//
// This list is a REGRESSION GUARD, not a convenience. Every entry is a value
// that real phones really sent. If someone later deletes one from the
// catalogue because it looks redundant or misspelt, this test fails and says
// why: deleting it starts rejecting writes from field devices that still
// send it. Counts are recorded so the cost of a deletion is visible.
var tiposMedidosEnProduccion = []struct {
	valor string
	filas int
}{
	{"No se encontraba", 118514},
	{"No va a dar pago", 38855},
	{"No pagará esta ocasión", 37000},
	{"Dijo que no va a pagar", 16925},
	{"Pidió reagendar visita", 6457},
	{"Se esconde y no sale", 3682},
	{"Se escuchan ruidos pero no abre", 1289},
	{"No responde aunque está", 1272},
	{"Solo había menores", 820},
	{"Casa cerrada con candado", 801},
	{"Se asomó pero no salió", 486},
	{"Tiene dinero pero no quiso pagar", 327},
	{"Fue grosero o agresivo", 112},
	{"Pidió que regrese otro día", 6},
}

// tiposQueMandaLaApp is Constants.kt:31-41 of the Android client, verbatim.
// The catalogue must accept every one of them or the current app breaks.
var tiposQueMandaLaApp = []string{
	"No se encontraba",
	"Casa cerrada con candado",
	"Solo había menores",
	"No pagará esta ocasión",
	"Pidió que regrese otro día",
	"Tiene dinero pero no quiso pagar",
	"Fue grosero o agresivo",
	"Se asomó pero no salió",
	"No responde aunque está",
	"Se escuchan ruidos pero no abre",
	"Pidió reagendar visita",
}

func TestTipoVisita_AceptaTodoValorMedidoEnProduccion(t *testing.T) {
	t.Parallel()

	for _, tc := range tiposMedidosEnProduccion {
		t.Run(tc.valor, func(t *testing.T) {
			t.Parallel()

			got, err := domain.ParseTipoVisita(tc.valor)
			require.NoError(t, err,
				"%q exists in MSP_VISITAS with %d rows; removing it from the catalogue "+
					"would start rejecting writes from phones that still send it",
				tc.valor, tc.filas)
			assert.Equal(t, tc.valor, got.String(), "Parse must round-trip the literal unchanged")
		})
	}
}

func TestTipoVisita_AceptaTodoLiteralQueMandaLaApp(t *testing.T) {
	t.Parallel()

	for _, valor := range tiposQueMandaLaApp {
		t.Run(valor, func(t *testing.T) {
			t.Parallel()

			_, err := domain.ParseTipoVisita(valor)
			require.NoError(t, err, "Constants.kt sends %q; the catalogue must accept it", valor)
		})
	}
}

// TestTipoVisita_CatalogoEsExactamenteLoMedido asserts the catalogue is
// neither larger nor smaller than the measurement. A value nobody has ever
// sent should not be invented here, and a measured one should not vanish.
func TestTipoVisita_CatalogoEsExactamenteLoMedido(t *testing.T) {
	t.Parallel()

	catalogo := domain.TiposVisitaValidos()
	require.Len(t, catalogo, len(tiposMedidosEnProduccion),
		"catalogue size must match the 14 distinct non-empty values measured in MSP_VISITAS")

	enCatalogo := make(map[string]bool, len(catalogo))
	for _, t := range catalogo {
		enCatalogo[t.String()] = true
	}
	for _, tc := range tiposMedidosEnProduccion {
		assert.True(t, enCatalogo[tc.valor], "measured value %q missing from the catalogue", tc.valor)
	}

	// And the app's 11 are a strict subset of it.
	for _, valor := range tiposQueMandaLaApp {
		assert.True(t, enCatalogo[valor], "app literal %q missing from the catalogue", valor)
	}
}

// TestTipoVisita_LiteralesRetiradosSiguenAceptados names the three retired
// literals explicitly. They are the whole reason the catalogue is a union of
// two sets rather than a copy of Constants.kt.
func TestTipoVisita_LiteralesRetiradosSiguenAceptados(t *testing.T) {
	t.Parallel()

	retirados := []domain.TipoVisita{
		domain.TipoVisitaLegacyNoVaADarPago,
		domain.TipoVisitaLegacyDijoQueNoVaAPagar,
		domain.TipoVisitaLegacySeEscondeYNoSale,
	}
	for _, r := range retirados {
		assert.True(t, r.IsValid(),
			"%q is no longer sent by the current app build but phones in the field still send it", r)
	}

	// The one that proves the point: renamed away on 2025-09-02 by commit
	// f7de5f02, still arriving from unupdated phones on 2026-02-18.
	assert.True(t, domain.TipoVisitaLegacyDijoQueNoVaAPagar.IsValid())
	assert.NotEqual(t, domain.TipoVisitaNoVaADarPago, domain.TipoVisitaLegacyDijoQueNoVaAPagar,
		"the renamed constant and its old value are two distinct catalogue members")
}

// ─── Rejection ──────────────────────────────────────────────────────────────

func TestTipoVisita_RechazaValoresFueraDelCatalogo(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		valor string
	}{
		{"invented value", "cobro"},
		{"the old placeholder", "no_encontrado"},
		{"empty string", ""},
		{"whitespace only", "   "},
		{"wrong case", "no se encontraba"},
		{"uppercased", "NO SE ENCONTRABA"},
		{"trailing space", "No se encontraba "},
		{"leading space", " No se encontraba"},
		{"substring of a member", "No se"},
		{"member plus junk", "No se encontraba; DROP TABLE MSP_VISITAS"},
		{"unaccented spelling", "No pagara esta ocasion"},
		{"sql injection", "'; DROP TABLE MSP_VISITAS; --"},
		{"very long string", strings.Repeat("x", 500)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.ParseTipoVisita(tc.valor)
			require.Error(t, err, "%q must not be accepted", tc.valor)
			require.ErrorIs(t, err, domain.ErrVisitaTipoInvalido,
				"must be the distinguishable catalogue error")
			assert.Empty(t, got, "a rejected parse must return the zero value")
			assert.False(t, domain.TipoVisita(tc.valor).IsValid())
		})
	}
}

// ─── String / accessor mechanics ────────────────────────────────────────────

func TestTipoVisita_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "No se encontraba", domain.TipoVisitaNoSeEncontraba.String())
	assert.Empty(t, domain.TipoVisita("").String())
	assert.Equal(t, "cualquier cosa", domain.TipoVisita("cualquier cosa").String(),
		"String is a plain cast — it does not filter")
}

// TestTiposVisitaValidos_DevuelveCopia proves a caller cannot corrupt the
// catalogue by writing through the returned slice.
func TestTiposVisitaValidos_DevuelveCopia(t *testing.T) {
	t.Parallel()

	primera := domain.TiposVisitaValidos()
	require.NotEmpty(t, primera)
	original := primera[0]

	primera[0] = domain.TipoVisita("envenenado")

	segunda := domain.TiposVisitaValidos()
	assert.Equal(t, original, segunda[0], "mutating the returned slice must not affect the catalogue")
	assert.True(t, original.IsValid())
	assert.False(t, domain.TipoVisita("envenenado").IsValid())
}

// ─── NFC normalization ─────────────────────────────────────────────────────

// TestTipoVisita_NFCEsObligatorioParaAcentos is the trap this catalogue would
// otherwise fall into. "No pagará esta ocasión" carries two accents. Sent in
// NFD (decomposed: "a" + U+0301) it is a DIFFERENT byte sequence from the NFC
// literal in the catalogue and would be rejected — even though it renders
// identically. NewVisita normalizes to NFC before parsing, so the write path
// accepts both forms; ParseTipoVisita alone does not, by design.
func TestTipoVisita_NFCEsObligatorioParaAcentos(t *testing.T) {
	t.Parallel()

	const nfcLiteral = "No pagará esta ocasión"
	nfd := norm.NFD.String(nfcLiteral)
	require.NotEqual(t, nfcLiteral, nfd, "precondition: the two normalizations must differ in bytes")

	// Parse alone is byte-exact and rejects the decomposed form.
	_, err := domain.ParseTipoVisita(nfd)
	require.Error(t, err)
	require.ErrorIs(t, err, domain.ErrVisitaTipoInvalido)

	// NewVisita normalizes first, so the same input is accepted end-to-end.
	p := tipoVisitaParams(nfd)
	v, err := domain.NewVisita(p)
	require.NoError(t, err, "NewVisita must NFC-normalize before parsing")
	assert.Equal(t, nfcLiteral, v.TipoVisita().String(), "stored form is canonical NFC")
}

// TestNewVisita_NormalizaEspaciosAlrededorDelTipo proves the trim in
// requireBounded runs before the catalogue check, so a client that pads the
// value is not rejected.
func TestNewVisita_NormalizaEspaciosAlrededorDelTipo(t *testing.T) {
	t.Parallel()

	v, err := domain.NewVisita(tipoVisitaParams("  No se encontraba  "))
	require.NoError(t, err)
	assert.Equal(t, domain.TipoVisitaNoSeEncontraba, v.TipoVisita())
}

// ─── Write path vs read path ────────────────────────────────────────────────

// TestNewVisita_ErroresDeTipoSonDistinguibles pins which error answers which
// input. A blank tipo is "required", an oversized one is "too long", and only
// a well-formed-but-unknown one is "not in the catalogue". Collapsing these
// into one error would make the API unactionable for a client.
func TestNewVisita_ErroresDeTipoSonDistinguibles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		tipo  string
		quier error
	}{
		{"blank stays 'required'", "", domain.ErrVisitaTipoRequerido},
		{"whitespace-only stays 'required'", "   \t ", domain.ErrVisitaTipoRequerido},
		{"over the column width stays 'too long'", strings.Repeat("z", 101), domain.ErrVisitaTipoDemasiadoLargo},
		{"within width but unknown is 'invalid'", "cobro", domain.ErrVisitaTipoInvalido},
		{"exactly at the width but unknown is 'invalid'", strings.Repeat("z", 100), domain.ErrVisitaTipoInvalido},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := domain.NewVisita(tipoVisitaParams(tc.tipo))
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.quier)
		})
	}
}

// TestRehydrateVisita_AceptaCualquierTipo is the read-path guarantee, and the
// single most important test in this file. MSP_VISITAS holds 226k rows
// written over two years with no catalogue at all. If rehydration validated,
// GET /v2/visitas/{id} would return an error for every one of them that falls
// outside the list — including the 200 rows holding the empty string and the
// ~59k holding retired literals.
//
// Note these values are deliberately ones the WRITE path rejects: the pairing
// is the point.
func TestRehydrateVisita_AceptaCualquierTipo(t *testing.T) {
	t.Parallel()

	// escribible records whether the WRITE path also accepts the value. The
	// retired literals are in the catalogue on purpose, so they are writable
	// too; the rest are read-only shapes that only exist because a backend
	// with no catalogue wrote them years ago. Asserting both directions per
	// case is what makes this a real read/write contrast rather than a list.
	cases := []struct {
		name       string
		tipo       string
		escribible bool
	}{
		{"empty string — 200 real rows carry this", "", false},
		{"retired literal — 38,855 real rows", "No va a dar pago", true},
		{"retired literal — 16,925 real rows", "Dijo que no va a pagar", true},
		{"retired literal — 3,682 real rows", "Se esconde y no sale", true},
		{"a value no catalogue ever had", "cobro", false},
		{"junk that must still read back", "\x7f not even valid business data", false},
		{"longer than the column width", strings.Repeat("q", 300), false},
		{"wrong case", "no se encontraba", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Pin the write path's answer for the same value, so the two
			// paths are compared rather than merely described.
			_, werr := domain.ParseTipoVisita(tc.tipo)
			if tc.escribible {
				require.NoError(t, werr, "%q is a catalogue member and must stay writable", tc.tipo)
			} else {
				require.Error(t, werr, "%q must be rejected by the write path", tc.tipo)
				require.ErrorIs(t, werr, domain.ErrVisitaTipoInvalido)
			}

			v := domain.RehydrateVisita(domain.RehydrateVisitaParams{
				ID:            uuid.New(),
				Cobrador:      "Ramírez García, Jorge",
				CobradorID:    42,
				Fecha:         time.Date(2025, 4, 11, 13, 23, 33, 0, time.UTC),
				FormaCobroID:  87327,
				Lat:           19.432608,
				Lng:           -99.133209,
				TipoVisita:    tc.tipo,
				ZonaClienteID: 7,
				ClienteID:     11486,
			})

			require.NotNil(t, v, "rehydration must never fail — it has no error return by design")
			assert.Equal(t, tc.tipo, v.TipoVisita().String(),
				"the persisted bytes must come back verbatim, unvalidated and unmodified")
		})
	}
}

// ─── helper ─────────────────────────────────────────────────────────────────

// tipoVisitaParams builds otherwise-valid CrearVisitaParams varying only
// TipoVisita, so any error a test observes can only come from that field.
func tipoVisitaParams(tipo string) domain.CrearVisitaParams {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	return domain.CrearVisitaParams{
		ID:            uuid.MustParse("6f2a5c2e-2c8b-4c1a-9b7e-1a2b3c4d5e6f"),
		Cobrador:      "María de los Ángeles Hernández",
		CobradorID:    501,
		Fecha:         now.Add(-time.Hour),
		FormaCobroID:  1,
		Lat:           19.0414,
		Lng:           -98.2063,
		TipoVisita:    tipo,
		ZonaClienteID: 12,
		ClienteID:     24037,
		CreatedBy:     uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Now:           now,
	}
}
