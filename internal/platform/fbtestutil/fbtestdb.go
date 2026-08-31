package fbtestutil

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Tests run against the REAL Microsip Firebird database — the same one the
// API talks to in dev. We do NOT spin up a clean container because the
// Microsip schema is proprietary and we can't recreate it from scratch.
//
// Safety contract:
//   - Tests must run inside a transaction that ALWAYS rolls back
//     (see fbtesttx.go's WithTestTransaction). Anything that escapes a
//     rollback is a bug in the test.
//   - Tests must never run DDL (CREATE/DROP/ALTER TABLE) — the dev DB
//     mirrors production schema; we only consume what's already there.
//   - Tests touch our own MSP_* tables (added by migrations-firebird/) or
//     run pure-read queries against Microsip's native tables.
//
// Required env vars: FB_DATABASE plus the standard FB_HOST/FB_PORT/FB_USER/
// FB_PASSWORD/FB_CHARSET defaults from .env. If FB_DATABASE is empty the
// helper skips the calling test instead of failing — that's how non-Firebird
// devs run the rest of the suite without a Firebird container.

var (
	fbOnce sync.Once
	fbPool *firebird.Pool
	errFB  error
)

// NewTestFirebirdPool returns a shared *firebird.Pool connected to the dev
// Microsip database described by the FB_* env vars. The pool is opened once
// per process and reused across tests.
//
// Behavior:
//   - FB_DATABASE empty → t.Skip with a clear message.
//   - Pool open or ping fails → t.Fatal.
//   - Otherwise → returns the shared pool.
func NewTestFirebirdPool(tb testing.TB) *firebird.Pool {
	tb.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		tb.Skip("FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests")
	}
	if err := ensurePool(); err != nil {
		tb.Fatalf("fbtestutil: %v", err)
	}
	return fbPool
}

// ensurePool opens the shared pool exactly once. Subsequent calls reuse the
// cached *firebird.Pool / error.
func ensurePool() error {
	fbOnce.Do(func() {
		cfg, err := loadFirebirdConfig()
		if err != nil {
			errFB = err
			return
		}

		pool, err := firebird.New(cfg)
		if err != nil {
			errFB = fmt.Errorf("fbtestutil: open pool: %w", err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := pool.Start(ctx); err != nil {
			errFB = fmt.Errorf("fbtestutil: ping %s: %w", cfg.Host, err)
			return
		}
		fbPool = pool
	})
	return errFB
}

// loadFirebirdConfig reads the FB_* env vars directly so tests don't depend
// on the full config.Load pipeline (which would also require Postgres and
// Firebase env vars). Defaults mirror config.Firebird's struct tags.
func loadFirebirdConfig() (config.Firebird, error) {
	port, err := envInt("FB_PORT", 3050)
	if err != nil {
		return config.Firebird{}, fmt.Errorf("fbtestutil: FB_PORT: %w", err)
	}
	poolSize, err := envInt("FB_POOL_SIZE", 5)
	if err != nil {
		return config.Firebird{}, fmt.Errorf("fbtestutil: FB_POOL_SIZE: %w", err)
	}
	return config.Firebird{
		Host:             envOr("FB_HOST", "localhost"),
		Port:             port,
		Database:         os.Getenv("FB_DATABASE"),
		User:             envOr("FB_USER", "SYSDBA"),
		Password:         os.Getenv("FB_PASSWORD"),
		Charset:          envOr("FB_CHARSET", "UTF8"),
		PoolSize:         poolSize,
		WireCrypt:        envBool("FB_WIRE_CRYPT", true),
		WireCompress:     envBool("FB_WIRE_COMPRESS", false),
		StatementTimeout: envDuration("FB_STATEMENT_TIMEOUT", 10*time.Minute),
	}, nil
}

// TestFirebirdConfig returns the same FB_* configuration the shared pool uses,
// for the rare test that needs a pool of its own (e.g. one that deliberately
// stresses pool capacity and must not disturb the shared one). Skips the
// calling test when FB_DATABASE is unset, exactly like NewTestFirebirdPool.
func TestFirebirdConfig(tb testing.TB) config.Firebird {
	tb.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		tb.Skip("FB_DATABASE not set — point it at the dev Microsip DB to run Firebird tests")
	}
	cfg, err := loadFirebirdConfig()
	if err != nil {
		tb.Fatalf("fbtestutil: %v", err)
	}
	return cfg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

// ─── Nombres verdaderos de los catálogos legados ────────────────────────────

// NombresDeCatalogo lee `<colID>, <colNombre>` de una tabla legada de Microsip
// y devuelve el nombre VERDADERO de cada fila, en UTF-8.
//
// Es el patrón de control positivo para los defectos de codificación: una
// prueba compara lo que devuelve el repositorio contra este mapa, y cualquier
// mojibake ("Ã‘" en vez de "Ñ") o byte crudo sin decodificar rompe la
// comparación. Contra la base de desarrollo completa el barrido pasa por filas
// acentuadas de verdad (CIUDADES trae "CAÑADA MORELOS", ARTICULOS 121 filas,
// CLIENTES 1,615), así que no hace falta sembrar nada para ejercitarlo.
//
// El CAST a WIN1252 es lo que hace que funcione para las DOS familias de
// charset a la vez. Una columna ISO8859_1 se podría leer verbatim y una
// CHARACTER SET NONE no; el CAST convierte cualquiera de las dos a un charset
// declarado y Firebird transliterar el resultado a UTF-8 en el cable. Lo que
// vuelve es, por construcción, independiente de cómo el repositorio bajo prueba
// haya decidido escanear la columna — que es justo lo que se quiere comparar.
//
//nolint:misspell // "defectos" es español, no una errata de "defects".
func NombresDeCatalogo(ctx context.Context, tb testing.TB, q firebird.Querier, tabla, colID, colNombre string) map[int]string {
	tb.Helper()
	//nolint:gosec // tabla/columnas son literales de las pruebas, no entrada externa.
	consulta := fmt.Sprintf(
		"SELECT %s, CAST(%s AS VARCHAR(250) CHARACTER SET WIN1252) FROM %s",
		colID, colNombre, tabla)
	rows, err := q.QueryContext(ctx, consulta)
	if err != nil {
		tb.Fatalf("fbtestutil: leyendo %s.%s: %v", tabla, colNombre, err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[int]string)
	for rows.Next() {
		var (
			id     int
			nombre sql.NullString
		)
		if err := rows.Scan(&id, &nombre); err != nil {
			tb.Fatalf("fbtestutil: escaneando %s.%s: %v", tabla, colNombre, err)
		}
		out[id] = nombre.String
	}
	if err := rows.Err(); err != nil {
		tb.Fatalf("fbtestutil: iterando %s.%s: %v", tabla, colNombre, err)
	}
	if len(out) == 0 {
		tb.Fatalf("fbtestutil: el catálogo %s vino vacío — la prueba no verificaría nada", tabla)
	}
	return out
}

// ContieneNoASCII indica si s trae al menos un byte fuera de ASCII. Las pruebas
// lo usan para exigir que un barrido haya pasado por datos acentuados: sin eso,
// una comparación de nombres ASCII pasa igual con el defecto y sin él.
func ContieneNoASCII(s string) bool {
	for i := range len(s) {
		if s[i] > 127 {
			return true
		}
	}
	return false
}
