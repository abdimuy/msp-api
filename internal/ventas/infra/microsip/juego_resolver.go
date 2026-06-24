// Package microsip — see venta_writer.go for package-level doc.
//
//nolint:misspell // Microsip column names are Spanish by convention.
package microsip

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/nakagami/firebirdsql"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ─── Sentinel errors ──────────────────────────────────────────────────────────

var errComponenteClaveNotFound = apperror.NewInternal(
	"juego_componente_clave_not_found",
	"no se encontró la clave del componente en microsip",
)

// ─── SQL constants ────────────────────────────────────────────────────────────

// selectNextCatalogoID claims the next catalog ID from the Microsip generator
// used for ARTICULOS, CLAVES_ARTICULOS, and other catalog entities.
const selectNextCatalogoID = `SELECT GEN_ID(ID_CATALOGOS, 1) FROM RDB$DATABASE`

// selectClaveArticuloIDByArticulo returns the first CLAVE_ARTICULO_ID for a
// given ARTICULO_ID. Used to populate JUEGOS_DET.CLAVE_ARTICULO_ID.
const selectClaveArticuloIDByArticulo = `SELECT FIRST 1 CLAVE_ARTICULO_ID FROM CLAVES_ARTICULOS WHERE ARTICULO_ID = ? ORDER BY CLAVE_ARTICULO_ID`

// selectJuegosCandidatos finds ARTICULO_IDs of active juegos (ES_JUEGO='S',
// ESTATUS='A') whose JUEGOS_DET has exactly the expected component count.
// Pre-filters candidates before the per-row exact match.
//
//nolint:gosec // SQL constant, not user input.
const selectJuegosCandidatos = `
SELECT jd.ARTICULO_ID
FROM JUEGOS_DET jd
JOIN ARTICULOS a ON a.ARTICULO_ID = jd.ARTICULO_ID
WHERE a.ES_JUEGO = 'S' AND a.ESTATUS = 'A'
GROUP BY jd.ARTICULO_ID
HAVING COUNT(*) = ?`

// selectComponentesDeJuego reads all {COMPONENTE_ID, UNIDADES} pairs for a
// given juego ARTICULO_ID. UNIDADES is NUMERIC(10,4); CAST ensures the driver
// returns the value with the expected scale.
//
//nolint:gosec // SQL constant, not user input.
const selectComponentesDeJuego = `
SELECT COMPONENTE_ID, CAST(UNIDADES AS NUMERIC(18,6)) FROM JUEGOS_DET WHERE ARTICULO_ID = ?`

// insertArticuloJuego inserts the ARTICULOS row for a new juego.
// ES_ALMACENABLE='N' because kits have no own inventory (stock lives on
// components). Columns USUARIO_CREADOR and FECHA_HORA_CREACION carry DB
// defaults and must NOT be passed explicitly (verified in
// docs/microsip-crear-kit-paso-a-paso.md).
//
//nolint:gosec // SQL constant, not user input.
const insertArticuloJuego = `INSERT INTO ARTICULOS
  (ARTICULO_ID, NOMBRE, ES_JUEGO, ES_ALMACENABLE, ESTATUS, LINEA_ARTICULO_ID,
   APLICAR_FACTOR_VENTA, FACTOR_VENTA, RED_PRECIO_CON_IMPTO,
   IMPRIMIR_COMP, CONTENIDO_UNIDAD_COMPRA, PESO_UNITARIO,
   ES_PESO_VARIABLE, SEGUIMIENTO, DIAS_GARANTIA,
   ES_IMPORTADO, ES_SIEMPRE_IMPORTADO, PCTJE_ARANCEL,
   IMPRIMIR_NOTAS_COMPRAS, IMPRIMIR_NOTAS_VENTAS,
   FACTOR_RED_PRECIO_CON_IMPTO)
VALUES
  (?, ?, 'S', 'N', 'A', ?,
   'N', 0, 'N',
   'N', 1, 0,
   'N', 'N', 0,
   'N', 'S', 0,
   'N', 'N',
   0.01)`

// insertJuegosDet inserts one component row in JUEGOS_DET.
//
//nolint:gosec // SQL constant, not user input.
const insertJuegosDet = `INSERT INTO JUEGOS_DET
  (ARTICULO_ID, COMPONENTE_ID, CLAVE_ARTICULO_ID, UNIDADES, ES_REEMPLAZABLE, PERMITIR_MODIF_UNID)
VALUES (?, ?, ?, ?, 'N', 'N')`

// insertLibresArticulosJuego inserts the mandatory LIBRES_ARTICULOS extension
// row for the new juego.
//
//nolint:gosec // SQL constant, not user input.
const insertLibresArticulosJuego = `INSERT INTO LIBRES_ARTICULOS
  (ARTICULO_ID, MULTIPLOVENTA, VOLUMEN)
VALUES (?, 0, 0)`

// unidadesReadScale is the scale used when scanning UNIDADES back from
// JUEGOS_DET (matches the CAST in selectComponentesDeJuego).
const unidadesReadScale = 6

// recipeKeyScale is the fixed decimal places used for the canonical string key
// in recipeKey.unidades. Must match domain.firmaScaleDP (=6) so that comparisons
// against domain.Receta.Firma() substrings are consistent.
const recipeKeyScale = 6

// ─── JuegoResolver ────────────────────────────────────────────────────────────

// JuegoResolver implements outbound.MicrosipJuegoResolver against the Microsip
// Firebird database.
type JuegoResolver struct {
	pool *firebird.Pool
}

// NewJuegoResolver builds a JuegoResolver wired to the given Firebird pool.
func NewJuegoResolver(pool *firebird.Pool) *JuegoResolver {
	return &JuegoResolver{pool: pool}
}

// Compile-time check.
var _ outbound.MicrosipJuegoResolver = (*JuegoResolver)(nil)

// Resolve looks up an existing juego whose JUEGOS_DET exactly matches
// in.Receta, or creates a new one if no match is found.
//
// Match algorithm:
//  1. Query JUEGOS_DET for active juegos with exactly len(componentes) rows.
//  2. For each candidate, load its {COMPONENTE_ID, UNIDADES} set and compare
//     against the recipe's canonical pairs. Order-independent (Receta is
//     already sorted by articuloID).
//  3. First exact match → return ArticuloID, Creado=false.
//  4. No match → create via insertJuego. On NOMBRE unique-violation, retry
//     with a suffix derived from the recipe's Firma() hash.
func (r *JuegoResolver) Resolve(ctx context.Context, in outbound.MicrosipJuegoInput) (outbound.MicrosipJuegoResult, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	componentes := in.Receta.Componentes()

	articuloID, err := r.findMatch(ctx, q, componentes)
	if err != nil {
		return outbound.MicrosipJuegoResult{}, fmt.Errorf("juego resolver: find match: %w", err)
	}
	if articuloID > 0 {
		return outbound.MicrosipJuegoResult{ArticuloID: articuloID, Creado: false}, nil
	}

	// No match — create new juego.
	newID, createErr := r.insertJuego(ctx, q, in, componentes, in.NombrePropuesto)
	if createErr == nil {
		return outbound.MicrosipJuegoResult{ArticuloID: newID, Creado: true}, nil
	}

	// NOMBRE unique constraint: detected either via a standard unique-index
	// violation (firebird_unique_violation) or via the trigger-raised user
	// exception EX_LLAVE_DUPLICADA that Microsip fires for duplicate article
	// names. Both cases warrant a suffix retry.
	if !isNombreDuplicado(createErr) {
		return outbound.MicrosipJuegoResult{}, fmt.Errorf("juego resolver: create: %w", createErr)
	}

	nombreConSufijo := in.NombrePropuesto + "-" + firmaSuffix(in.Receta.Firma())
	newID, err = r.insertJuego(ctx, q, in, componentes, nombreConSufijo)
	if err != nil {
		return outbound.MicrosipJuegoResult{}, fmt.Errorf("juego resolver: create (retry nombre): %w", err)
	}
	return outbound.MicrosipJuegoResult{ArticuloID: newID, Creado: true}, nil
}

// ─── Match helpers ────────────────────────────────────────────────────────────

// recipeKey is the canonical lookup key for a {componente, unidades} pair.
// Using StringFixed(recipeKeyScale) avoids floating-point comparison hazards.
type recipeKey struct {
	articuloID int
	unidades   string // decimal.StringFixed(recipeKeyScale)
}

// findMatch scans JUEGOS_DET candidate juegos pre-filtered by component count
// and returns the first ARTICULO_ID whose recipe is an exact numeric match.
// Returns 0 when no match is found.
func (r *JuegoResolver) findMatch(
	ctx context.Context,
	q firebird.Querier,
	componentes []domain.RecetaComponente,
) (int, error) {
	n := len(componentes)
	rows, err := q.QueryContext(ctx, selectJuegosCandidatos, n)
	if err != nil {
		return 0, fmt.Errorf("query candidatos: %w", firebird.MapError(err))
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is best-effort after iteration

	recipeSet := buildRecipeSet(componentes)

	for rows.Next() {
		var candidatoID int
		if err := rows.Scan(&candidatoID); err != nil {
			return 0, fmt.Errorf("scan candidato_id: %w", firebird.MapError(err))
		}
		matched, err := r.matchesRecipe(ctx, q, candidatoID, recipeSet, n)
		if err != nil {
			return 0, err
		}
		if matched {
			return candidatoID, nil
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("candidatos rows: %w", firebird.MapError(err))
	}
	return 0, nil
}

// buildRecipeSet converts a slice of RecetaComponente into a set for O(1)
// membership tests.
func buildRecipeSet(componentes []domain.RecetaComponente) map[recipeKey]struct{} {
	s := make(map[recipeKey]struct{}, len(componentes))
	for _, c := range componentes {
		s[recipeKey{
			articuloID: c.ArticuloID(),
			unidades:   c.Unidades().StringFixed(recipeKeyScale),
		}] = struct{}{}
	}
	return s
}

// matchesRecipe loads JUEGOS_DET for candidatoID and verifies that every row
// is present in recipeSet and the total count matches expectedCount.
func (r *JuegoResolver) matchesRecipe(
	ctx context.Context,
	q firebird.Querier,
	candidatoID int,
	recipeSet map[recipeKey]struct{},
	expectedCount int,
) (bool, error) {
	rows, err := q.QueryContext(ctx, selectComponentesDeJuego, candidatoID)
	if err != nil {
		return false, fmt.Errorf("query componentes juego_id=%d: %w", candidatoID, firebird.MapError(err))
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is best-effort after iteration

	seen := 0
	for rows.Next() {
		var componenteID int
		var rawUnidades any
		if err := rows.Scan(&componenteID, &rawUnidades); err != nil {
			return false, fmt.Errorf("scan componente juego_id=%d: %w", candidatoID, firebird.MapError(err))
		}
		unidades, err := firebird.ScanDecimal(rawUnidades, unidadesReadScale)
		if err != nil {
			return false, fmt.Errorf("scan unidades juego_id=%d: %w", candidatoID, err)
		}
		key := recipeKey{
			articuloID: componenteID,
			unidades:   unidades.StringFixed(recipeKeyScale),
		}
		if _, ok := recipeSet[key]; !ok {
			return false, nil // extra or mismatched pair → no match
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("componentes rows juego_id=%d: %w", candidatoID, firebird.MapError(err))
	}
	return seen == expectedCount, nil
}

// ─── Create helpers ───────────────────────────────────────────────────────────

// insertJuego creates a new juego in ARTICULOS + JUEGOS_DET + LIBRES_ARTICULOS
// within the ambient transaction. Returns the newly assigned ARTICULO_ID.
func (r *JuegoResolver) insertJuego(
	ctx context.Context,
	q firebird.Querier,
	in outbound.MicrosipJuegoInput,
	componentes []domain.RecetaComponente,
	nombre string,
) (int, error) {
	// Phase 1: claim ARTICULO_ID from the catalog generator.
	articuloID, err := nextCatalogoID(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("claim articulo_id: %w", err)
	}

	// Phase 2: INSERT ARTICULOS. NOMBRE is ISO8859_1 in the Microsip schema.
	// The API always connects with FB_CHARSET=UTF8, so the driver sends string
	// values as UTF-8 and Firebird auto-transliterates UTF-8 → ISO8859_1 for
	// columns declared as ISO8859_1. Passing a raw string (not Win1252) is
	// correct for a UTF-8 connection — Win1252 sends raw bytes that the server
	// rejects as "Malformed string" when the connection charset is UTF-8.
	if _, err := q.ExecContext(ctx, insertArticuloJuego,
		articuloID, nombre, in.LineaArticuloID,
	); err != nil {
		return 0, firebird.MapError(err)
	}

	// Phase 3: INSERT JUEGOS_DET — one row per component.
	for _, c := range componentes {
		claveID, err := r.lookupClaveArticuloID(ctx, q, c.ArticuloID())
		if err != nil {
			return 0, fmt.Errorf("lookup clave componente_id=%d: %w", c.ArticuloID(), err)
		}
		if _, err := q.ExecContext(ctx, insertJuegosDet,
			articuloID,
			c.ArticuloID(),
			claveID,
			c.Unidades().StringFixed(unidadesReadScale),
		); err != nil {
			return 0, fmt.Errorf("insert juegos_det componente_id=%d: %w", c.ArticuloID(), firebird.MapError(err))
		}
	}

	// Phase 4: INSERT LIBRES_ARTICULOS (extension row, always required).
	if _, err := q.ExecContext(ctx, insertLibresArticulosJuego, articuloID); err != nil {
		return 0, fmt.Errorf("insert libres_articulos: %w", firebird.MapError(err))
	}

	return articuloID, nil
}

// lookupClaveArticuloID returns the first CLAVE_ARTICULO_ID for a component
// articuloID. Returns errComponenteClaveNotFound when no clave row exists.
func (r *JuegoResolver) lookupClaveArticuloID(ctx context.Context, q firebird.Querier, articuloID int) (int, error) {
	var claveID int
	err := q.QueryRowContext(ctx, selectClaveArticuloIDByArticulo, articuloID).Scan(&claveID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errComponenteClaveNotFound.WithField("articulo_id", articuloID)
	}
	if err != nil {
		return 0, firebird.MapError(err)
	}
	return claveID, nil
}

// ─── Pure helpers ─────────────────────────────────────────────────────────────

// nextCatalogoID claims the next value from the ID_CATALOGOS generator.
func nextCatalogoID(ctx context.Context, q firebird.Querier) (int, error) {
	var id int
	if err := q.QueryRowContext(ctx, selectNextCatalogoID).Scan(&id); err != nil {
		return 0, fmt.Errorf("GEN_ID(ID_CATALOGOS): %w", firebird.MapError(err))
	}
	return id, nil
}

// firmaSuffix returns the first 8 hex characters of the SHA-256 of the recipe
// Firma string. Used to make a NOMBRE unique on collision.
func firmaSuffix(firma string) string {
	h := sha256.Sum256([]byte(firma))
	return hex.EncodeToString(h[:4]) // 4 bytes → 8 hex chars
}

// isNombreDuplicado reports whether err represents a duplicate ARTICULOS.NOMBRE
// condition. Two error shapes are possible:
//
//  1. Standard unique-index violation (GDS 335544349 / 335544665) → MapError
//     maps this to apperror Code "firebird_unique_violation".
//  2. Trigger EX_LLAVE_DUPLICADA raised by ARTICULOS_BEFINSUPD — the trigger
//     fires a user-defined EXCEPTION instead of relying on the DB constraint.
//     MapError maps this to the generic "firebird_error" code, so we inspect
//     the raw FbError message to detect this case.
func isNombreDuplicado(err error) bool {
	if err == nil {
		return false
	}
	// Case 1: standard unique violation handled by MapError.
	if appErr, ok := apperror.As(err); ok && appErr.Code == "firebird_unique_violation" {
		return true
	}
	// Case 2: Microsip trigger exception. The raw FbError message contains
	// "EX_LLAVE_DUPLICADA" raised by ARTICULOS_BEFINSUPD.
	var fbErr *firebirdsql.FbError
	if errors.As(err, &fbErr) && strings.Contains(fbErr.Error(), "EX_LLAVE_DUPLICADA") {
		return true
	}
	return false
}
