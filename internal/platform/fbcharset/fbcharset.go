// Package fbcharset holds the charset classification of every Microsip text
// column the repositories read, and the rule that turns a charset into a scan
// target.
//
// WHY THIS PACKAGE EXISTS
// =======================
// Microsip's text columns do not share a character set, and the name of a
// column tells you nothing about which one it has: CIUDADES.NOMBRE is
// ISO8859_1 while ZONAS_CLIENTES.NOMBRE is CHARACTER SET NONE. Measured on the
// live schema (RDB$RELATION_FIELDS × RDB$FIELDS × RDB$CHARACTER_SETS, excluding
// system tables): 2,365 columns are NONE, 155 ASCII, 100 ISO8859_1, 81 UTF8.
//
// The connection is charset=UTF8 in every environment (FB_CHARSET), and that
// splits those columns into exactly two families with OPPOSITE handling:
//
//	FamiliaTransliterada (ISO8859_1, UTF8, ASCII)
//	    Firebird converts the value to UTF-8 on the wire. Go must scan it as a
//	    plain string / sql.NullString. Scanning through firebird.Win1252 applies
//	    a SECOND Windows-1252→UTF-8 decode and "Ñ" becomes "Ã‘".
//
//	FamiliaNone (CHARACTER SET NONE)
//	    Firebird converts nothing — there is no source charset to convert from —
//	    so the stored Windows-1252 bytes arrive verbatim. Go must scan through
//	    firebird.Win1252. A plain string target leaves invalid UTF-8 in the
//	    domain. A '' literal COALESCEd onto such a column also forces a coercion
//	    that fails with "Malformed string"; that restriction is real and applies
//	    ONLY to this family.
//
// Getting this backwards is not cosmetic. The corrupted name is persisted into
// the Meilisearch index, so searching for "FARIÑO" returns 0 results while
// "MUÃ‘OZ" returns 261.
//
// HOW IT IS ENFORCED
// ==================
// Registro below is the single list of what the repositories read and how. Four
// tests in this package hold it honest — see the header of each test file for
// what it covers and, just as importantly, what it does not. Two of them read
// the catalog and the data; two sweep the source, in both directions: every
// firebird.Win1252 target must be classified, and every column classified NONE
// must keep at least one site that decodes it.
//
// THE SCAN TARGET ALSO DEPENDS ON THE JOIN
// ========================================
// Charset decides HOW a value is decoded; the JOIN decides WHETHER it can be
// missing. firebird.Win1252 absorbs NULL silently (nil → ""), so a nullable
// column read through it never hurt — and the day the charset is corrected to a
// bare string, the same LEFT JOIN returns "converting NULL to string is
// unsupported" and takes the WHOLE query down. Look at the FROM clause before
// writing `string`: NOT NULL in the catalog is not the question, the join is.
//
//nolint:misspell // Spanish domain vocabulary (clientes, cobradores, zonas) by project convention.
package fbcharset

import (
	"fmt"
	"sort"
	"strings"
)

// Familia is the charset family of a Microsip text column: the thing that
// decides the scan target. There are only two because, from Go's side, only two
// behaviours exist — Firebird either converted the value or it did not.
type Familia int

const (
	// FamiliaTransliterada covers ISO8859_1, UTF8 and ASCII columns: Firebird
	// delivers UTF-8, Go scans a plain string / sql.NullString.
	FamiliaTransliterada Familia = iota
	// FamiliaNone covers CHARACTER SET NONE columns: Firebird delivers raw
	// Windows-1252 bytes, Go scans through firebird.Win1252.
	FamiliaNone
)

// String renders the family the way the failure messages need it.
func (f Familia) String() string {
	switch f {
	case FamiliaTransliterada:
		return "transliterada (ISO8859_1/UTF8/ASCII)"
	case FamiliaNone:
		return "CHARACTER SET NONE"
	default:
		return fmt.Sprintf("Familia(%d)", int(f))
	}
}

// DestinoDeEscaneo names the Go scan target the family requires. It is what the
// test prints when a column is read the wrong way, so the message tells the
// reader what to change instead of only what broke.
func (f Familia) DestinoDeEscaneo() string {
	switch f {
	case FamiliaTransliterada:
		return "string / sql.NullString"
	case FamiliaNone:
		return "firebird.Win1252"
	default:
		return "?"
	}
}

// FamiliaDeCharset maps a RDB$CHARACTER_SETS name onto its family.
// An unknown charset name is reported rather than guessed: a new charset in the
// schema is exactly the kind of change that should stop the build.
func FamiliaDeCharset(charset string) (Familia, error) {
	switch strings.TrimSpace(strings.ToUpper(charset)) {
	case "NONE":
		return FamiliaNone, nil
	case "ISO8859_1", "UTF8", "ASCII", "WIN1252":
		return FamiliaTransliterada, nil
	default:
		//nolint:err113 // the offending charset name is the whole point of the message.
		return 0, fmt.Errorf("fbcharset: charset %q no está clasificado; agréguelo a FamiliaDeCharset o corrija la columna", charset)
	}
}

// Columna is one Microsip text column the repositories read, with the family
// the reading code assumes and the code sites that read it.
type Columna struct {
	// Tabla and Columna are the RDB$ names, uppercase, exactly as stored.
	Tabla   string
	Columna string
	// Familia is what the READING CODE assumes. The catalog test compares it
	// against RDB$FIELDS and fails on a mismatch.
	Familia Familia
	// LeidoPor lists the packages that read the column. It is documentation for
	// whoever has to fix a mismatch: it tells them where to go.
	LeidoPor []string
	// Nota records anything non-obvious about the column.
	Nota string
}

// Clave is "TABLA.COLUMNA", the key used by the source sweep and by the
// failure messages.
func (c Columna) Clave() string { return c.Tabla + "." + c.Columna }

// minimoDeColumnas is the floor the tests assert before they trust a sweep.
// Its job is to make an EMPTY result impossible to mistake for a pass: a
// refactor that stops finding columns fails loudly instead of quietly
// verifying nothing. Raise it when the registry grows; never lower it to make
// a test pass.
const minimoDeColumnas = 20

// MinimoDeColumnas is minimoDeColumnas, exported for the tests.
func MinimoDeColumnas() int { return minimoDeColumnas }

// registro is the authoritative classification. It is hand-maintained on
// purpose: a column enters this list only when a human has looked it up in the
// catalog, and the catalog test then keeps that lookup honest forever.
var registro = []Columna{
	// ── CHARACTER SET NONE — scanned through firebird.Win1252 ────────────────
	{
		"CLIENTES", "NOTAS", FamiliaNone,
		[]string{"clientes/infra/clientesfb", "analytics/infra/analyticsfb", "reactivacion/infra/reactivacionfb"},
		"BLOB SUB_TYPE TEXT. 3,458 de 20,000 filas muestreadas traen no-ASCII.",
	},
	{
		"ZONAS_CLIENTES", "NOMBRE", FamiliaNone,
		[]string{"clientes/infra/clientesfb", "microsip/infra/microsipfb", "config/infra/configfb", "rutas/infra/rutasfb", "analytics/infra/analyticsfb"},
		"Hoy 0 de 46 filas traen no-ASCII: el defecto sería invisible en datos, no en el catálogo.",
	},
	{
		"DIRS_CLIENTES", "NOMBRE_CALLE", FamiliaNone,
		[]string{"clientes/infra/clientesfb"},
		"198 de 20,000 filas muestreadas traen no-ASCII.",
	},
	{
		"DIRS_CLIENTES", "COLONIA", FamiliaNone,
		[]string{"clientes/infra/clientesfb"},
		"",
	},
	{
		"DIRS_CLIENTES", "POBLACION", FamiliaNone,
		[]string{"clientes/infra/clientesfb"},
		"287 de 20,000 filas muestreadas traen no-ASCII.",
	},
	{
		"DIRS_CLIENTES", "TELEFONO1", FamiliaNone,
		[]string{"analytics/infra/analyticsfb", "reactivacion/infra/reactivacionfb", "clientes/infra/clientesfb"},
		"Hoy 0 filas con no-ASCII: mismo caso latente que ZONAS_CLIENTES.NOMBRE.",
	},
	{
		"LISTAS_ATRIBUTOS", "VALOR_DESPLEGADO", FamiliaNone,
		[]string{"clientes/infra/clientesfb", "config/infra/configfb"},
		"371 de 1,246 filas traen no-ASCII.",
	},
	{
		"FORMAS_COBRO", "NOMBRE", FamiliaNone,
		[]string{"clientes/infra/clientesfb"},
		"1 de 4 filas trae no-ASCII (\"Crédito\").",
	},
	{
		"CONCEPTOS_CC", "NOMBRE", FamiliaNone,
		[]string{"clientes/infra/clientesfb"},
		"13 de 34 filas traen no-ASCII (\"Interés moratorio\").",
	},
	{
		"DOCTOS_CC", "DESCRIPCION", FamiliaNone,
		[]string{"clientes/infra/clientesfb", "cobranza/infra/ventfb"},
		"",
	},
	{
		"FORMAS_COBRO_DOCTOS", "REFERENCIA", FamiliaNone,
		[]string{"clientes/infra/clientesfb"},
		"",
	},
	{
		"DOCTOS_PV", "FOLIO", FamiliaNone,
		[]string{"rutas/infra/rutasfb"},
		"CHAR(9) generado por el sistema; 0 filas con no-ASCII.",
	},
	{
		"CAJAS", "NOMBRE", FamiliaNone,
		[]string{"config/infra/configfb"},
		"",
	},

	// ── Transliteradas (ISO8859_1) — scanned as string / sql.NullString ──────
	{
		"CLIENTES", "NOMBRE", FamiliaTransliterada,
		[]string{"clientes/infra/clientesfb", "rutas/infra/rutasfb", "analytics/infra/analyticsfb", "reactivacion/infra/reactivacionfb"},
		"1,615 de 43,834 filas traen no-ASCII; 99.7% son Ñ. Leerla con Win1252 corrompía el nombre hasta Meilisearch.",
	},
	{
		"COBRADORES", "NOMBRE", FamiliaTransliterada,
		[]string{"clientes/infra/clientesfb", "microsip/infra/microsipfb", "config/infra/configfb", "rutas/infra/rutasfb"},
		"",
	},
	{
		"ESTADOS", "NOMBRE", FamiliaTransliterada,
		[]string{"clientes/infra/clientesfb", "microsip/infra/microsipfb"},
		"",
	},
	{
		"ALMACENES", "NOMBRE", FamiliaTransliterada,
		[]string{"clientes/infra/clientesfb", "microsip/infra/microsipfb"},
		"1 de 50 filas trae no-ASCII (\"Mercancía en tránsito\").",
	},
	{
		"ARTICULOS", "NOMBRE", FamiliaTransliterada,
		[]string{"clientes/infra/clientesfb", "cobranza/infra/ventfb", "microsip/infra/microsipfb", "analytics/infra/analyticsfb"},
		"121 de 6,113 filas traen no-ASCII.",
	},
	{
		"CIUDADES", "NOMBRE", FamiliaTransliterada,
		[]string{"microsip/infra/microsipfb", "ventas/infra/ventfb"},
		"2 de 70 filas traen no-ASCII (\"CAÑADA MORELOS\"). Contraejemplo del nombre: es ISO8859_1 aunque ZONAS_CLIENTES.NOMBRE sea NONE.",
	},
	{
		"LINEAS_ARTICULOS", "NOMBRE", FamiliaTransliterada,
		[]string{"microsip/infra/microsipfb"},
		"",
	},
	{
		"CAJEROS", "NOMBRE", FamiliaTransliterada,
		[]string{"config/infra/configfb"},
		"",
	},
	{
		"VENDEDORES", "NOMBRE", FamiliaTransliterada,
		[]string{"config/infra/configfb"},
		"",
	},
	{
		"PRECIOS_EMPRESA", "NOMBRE", FamiliaTransliterada,
		[]string{"microsip/infra/microsipfb"},
		"Llega concatenada dentro de LIST(...) — la expresión hereda el charset de la columna.",
	},
}

// Registro returns the classification, sorted by "TABLA.COLUMNA" so failure
// output is stable across runs.
func Registro() []Columna {
	out := make([]Columna, len(registro))
	copy(out, registro)
	sort.Slice(out, func(i, j int) bool { return out[i].Clave() < out[j].Clave() })
	return out
}

// PorClave indexes Registro by "TABLA.COLUMNA".
func PorClave() map[string]Columna {
	out := make(map[string]Columna, len(registro))
	for _, c := range registro {
		out[c.Clave()] = c
	}
	return out
}
