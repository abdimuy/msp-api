package config_test

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/config"
)

// setMinimal sets only the variables marked `required` in Config.
//
// Tests in this file do NOT call t.Parallel(): t.Setenv mutates process-wide
// state and must not race with other tests in the same package.
//
// clearAmbientEnv blanks out every env var the loader reads so a test starts
// from a known-empty baseline regardless of what was inherited from the
// caller's shell (typical when developers source .env before `go test`).
// Vars are restored to their previous values automatically at test end via
// t.Setenv's cleanup hook.
func clearAmbientEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"APP_ENV", "APP_LOG_LEVEL", "APP_LOG_FORMAT", "APP_PORT",
		"HTTP_MAX_BODY_SIZE_MB", "HTTP_CORS_ORIGINS",
		"FB_DATABASE", "FB_HOST", "FB_PORT", "FB_USER", "FB_PASSWORD", "FB_CHARSET",
		"FIREBASE_PROJECT_ID", "FIREBASE_DEV_MODE", "FIREBASE_ALLOW_UNCONFIGURED",
		"MEILISEARCH_URL", "MEILISEARCH_API_KEY", "MEILISEARCH_INDEX_NAME",
		"MEILISEARCH_VENTAS_INDEX_NAME",
		"MEILISEARCH_SYNC_INTERVAL", "MEILISEARCH_ALLOW_UNCONFIGURED",
		"LLM_BASE_URL", "LLM_MODEL", "LLM_ENABLED", "LLM_TIMEOUT",
		"STORAGE_DIR",
		"WHATSAPP_ENABLED", "WHATSAPP_TOKEN", "WHATSAPP_PHONE_NUMBER_ID",
		"WHATSAPP_BUSINESS_ACCOUNT_ID", "WHATSAPP_APP_SECRET",
		"WHATSAPP_API_VERSION", "WHATSAPP_TIMEOUT", "WHATSAPP_BASE_URL",
		"FB_SLOW_QUERY_THRESHOLD", "DESEMPENO_STATEMENT_TIMEOUT", "HTTP_WRITE_TIMEOUT",
	} {
		t.Setenv(k, "")
	}
}

func setMinimal(t *testing.T) {
	t.Helper()
	clearAmbientEnv(t)
	t.Setenv("FIREBASE_PROJECT_ID", "test-project")
	// Opt out of Meilisearch in unit tests so they don't require a live server.
	t.Setenv("MEILISEARCH_ALLOW_UNCONFIGURED", "true")
}

func TestLoad_MinimalEnv_Succeeds(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, config.EnvDevelopment, cfg.App.Env)
	assert.Equal(t, 3001, cfg.HTTP.Port)
	assert.Equal(t, "test-project", cfg.Firebase.ProjectID)
}

func TestLoad_MissingRequired_Fails(t *testing.T) { //nolint:paralleltest // env-dependent
	clearAmbientEnv(t)
	_, err := config.Load()
	require.Error(t, err)
}

func TestLoad_RejectsInvalidEnv(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("APP_ENV", "alpha")
	_, err := config.Load()
	require.Error(t, err)
}

func TestLoad_RejectsInvalidLogLevel(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("APP_LOG_LEVEL", "verbose")
	_, err := config.Load()
	require.Error(t, err)
}

func TestLoad_RejectsInvalidLogFormat(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("APP_LOG_FORMAT", "yaml")
	_, err := config.Load()
	require.Error(t, err)
}

func TestLoad_RejectsBadPort(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("APP_PORT", "70000")
	_, err := config.Load()
	require.Error(t, err)
}

func TestLoad_RejectsBadBodySize(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("HTTP_MAX_BODY_SIZE_MB", "0")
	_, err := config.Load()
	require.Error(t, err)
}

func TestEnvironment_IsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		env  config.Environment
		want bool
	}{
		{config.EnvDevelopment, true},
		{config.EnvStaging, true},
		{config.EnvProduction, true},
		{config.EnvTest, true},
		{config.Environment("alpha"), false},
		{config.Environment(""), false},
	}
	for _, tc := range cases {
		t.Run(string(tc.env), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.env.IsValid())
		})
	}
}

func TestFirebird_DSN(t *testing.T) {
	t.Parallel()
	fb := config.Firebird{
		Host: "host", Port: 3050, User: "SYSDBA", Password: "x",
		Database: "/data/msp.fdb", Charset: "UTF8",
		WireCrypt: true, WireCompress: false,
	}
	assert.Equal(
		t,
		"SYSDBA:x@host:3050//data/msp.fdb?charset=UTF8&wire_crypt=true&wire_compress=false",
		fb.DSN(),
	)
}

// ─── Firebase conditional validation ────────────────────────────────────────

// setMinimalNoFirebase sets only Postgres credentials, intentionally leaving
// FIREBASE_PROJECT_ID unset so the conditional Firebase rules drive the test.
func setMinimalNoFirebase(t *testing.T) {
	t.Helper()
	clearAmbientEnv(t)
	t.Setenv("PG_USER", "msp")
	t.Setenv("PG_PASSWORD", "msp")
	t.Setenv("PG_DATABASE", "msp_dev")
	// Opt out of Meilisearch in unit tests so they don't require a live server.
	t.Setenv("MEILISEARCH_ALLOW_UNCONFIGURED", "true")
}

func TestLoad_FirebaseRequiredInProduction(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimalNoFirebase(t)
	t.Setenv("APP_ENV", "production")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FIREBASE_PROJECT_ID")
}

func TestLoad_DevMode_AllowsMissingProjectID(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimalNoFirebase(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("FIREBASE_DEV_MODE", "true")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.Firebase.DevMode)
	assert.Empty(t, cfg.Firebase.ProjectID)
}

func TestLoad_DevMode_RefusedOutsideDevelopment(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimalNoFirebase(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("FIREBASE_DEV_MODE", "true")
	t.Setenv("FIREBASE_PROJECT_ID", "prod-project")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FIREBASE_DEV_MODE")
}

func TestLoad_AllowUnconfigured_BootsWithout(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimalNoFirebase(t)
	t.Setenv("APP_ENV", "staging")
	t.Setenv("FIREBASE_ALLOW_UNCONFIGURED", "true")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.Firebase.AllowUnconfigured)
}

func TestLoad_StagingWithoutEscapeHatch_Fails(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimalNoFirebase(t)
	t.Setenv("APP_ENV", "staging")
	_, err := config.Load()
	require.Error(t, err)
}

// ─── Storage validation ─────────────────────────────────────────────────────

func TestLoad_Storage_DefaultsToLocalDir(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.NotEmpty(t, cfg.Storage.Dir)
}

func TestLoad_Storage_EmptyDirFails(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("STORAGE_DIR", "   ")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "STORAGE_DIR")
}

// ─── ImageProcessor validation ──────────────────────────────────────────────

func TestLoad_ImageProcessor_Defaults(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.ImageProcessor.Enabled)
	assert.Equal(t, 1920, cfg.ImageProcessor.MaxLongSidePx)
	assert.Equal(t, 85, cfg.ImageProcessor.JPEGQuality)
	assert.Equal(t, int64(15*1024*1024), cfg.ImageProcessor.MaxInputBytes)
	assert.True(t, cfg.ImageProcessor.RecompressWebPToJPEG)
	assert.True(t, cfg.ImageProcessor.PreserveSmallImages)
}

func TestLoad_ImageProcessor_DisabledSkipsValidation(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("IMAGEPROCESSOR_ENABLED", "false")
	t.Setenv("IMAGEPROCESSOR_JPEG_QUALITY", "0") // would be invalid if Enabled
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.False(t, cfg.ImageProcessor.Enabled)
}

func TestLoad_ImageProcessor_RejectsBadJPEGQuality(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("IMAGEPROCESSOR_JPEG_QUALITY", "0")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IMAGEPROCESSOR_JPEG_QUALITY")
}

func TestLoad_ImageProcessor_RejectsBadMaxLongSide(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("IMAGEPROCESSOR_MAX_LONG_SIDE_PX", "-1")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IMAGEPROCESSOR_MAX_LONG_SIDE_PX")
}

func TestLoad_ImageProcessor_RejectsBadMaxInputBytes(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("IMAGEPROCESSOR_MAX_INPUT_BYTES", "0")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IMAGEPROCESSOR_MAX_INPUT_BYTES")
}

func TestLoad_ImageProcessor_RejectsBadPNGCompression(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("IMAGEPROCESSOR_PNG_COMPRESSION", "42")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IMAGEPROCESSOR_PNG_COMPRESSION")
}

func TestLoad_ImageProcessor_RejectsNegativeSmallImageBytes(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("IMAGEPROCESSOR_SMALL_IMAGE_BYTES", "-1")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IMAGEPROCESSOR_SMALL_IMAGE_BYTES")
}

func TestLoad_ImageProcessor_AcceptsPNGCompressionBestCompression(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("IMAGEPROCESSOR_PNG_COMPRESSION", "-3") // png.BestCompression
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, -3, cfg.ImageProcessor.PNGCompression)
}

func TestLoad_ImageProcessor_AcceptsPNGCompressionNoCompression(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("IMAGEPROCESSOR_PNG_COMPRESSION", "0") // png.NoCompression
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 0, cfg.ImageProcessor.PNGCompression)
}

// ─── MicrosipVenta.Juegos validation ────────────────────────────────────────

func TestLoad_MicrosipVenta_JuegosDisabled_OK(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	// Default: JuegosEnabled=false, JuegosLineaArticuloID=0 → valid.
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.False(t, cfg.MicrosipVenta.JuegosEnabled)
	assert.Equal(t, 0, cfg.MicrosipVenta.JuegosLineaArticuloID)
}

func TestLoad_MicrosipVenta_JuegosEnabled_LineaSet_OK(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("MICROSIP_VENTA_JUEGOS_ENABLED", "true")
	t.Setenv("MICROSIP_VENTA_JUEGOS_LINEA_ARTICULO_ID", "11774")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.MicrosipVenta.JuegosEnabled)
	assert.Equal(t, 11774, cfg.MicrosipVenta.JuegosLineaArticuloID)
}

func TestLoad_MicrosipVenta_JuegosEnabled_LineaZero_Fails(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("MICROSIP_VENTA_JUEGOS_ENABLED", "true")
	// MICROSIP_VENTA_JUEGOS_LINEA_ARTICULO_ID left at default (0) → must fail.
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MICROSIP_VENTA_JUEGOS_LINEA_ARTICULO_ID")
}

// ─── MicrosipVenta.ReactivarClienteEnabled ──────────────────────────────────

func TestLoad_MicrosipVenta_ReactivarClienteEnabled_DefaultFalse(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.False(t, cfg.MicrosipVenta.ReactivarClienteEnabled)
}

func TestLoad_MicrosipVenta_ReactivarClienteEnabled_True(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("MICROSIP_REACTIVAR_CLIENTE_ENABLED", "true")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.MicrosipVenta.ReactivarClienteEnabled)
}

// ─── Firebird.SlowQueryThreshold / Desempeno.StatementTimeout ───────────────

func TestLoad_SlowQueryThreshold_DefaultOneSecond(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, time.Second, cfg.Firebird.SlowQueryThreshold)
}

func TestLoad_SlowQueryThreshold_Override(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("FB_SLOW_QUERY_THRESHOLD", "250ms")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 250*time.Millisecond, cfg.Firebird.SlowQueryThreshold)
}

// Same rule as the dashboard ceiling: a logging knob never stops the API.
func TestLoad_SlowQueryThreshold_NegativeIsCorrectedNotFatal(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("FB_SLOW_QUERY_THRESHOLD", "-1s")
	buf := captureWarn(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, time.Second, cfg.Firebird.SlowQueryThreshold)
	assert.Contains(t, buf.String(), "FB_SLOW_QUERY_THRESHOLD")
}

func TestLoad_DesempenoStatementTimeout_DefaultBelowHTTPWriteTimeout(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 25*time.Second, cfg.Desempeno.StatementTimeout)
	assert.Less(t, cfg.Desempeno.StatementTimeout, cfg.HTTP.WriteTimeout,
		"el servidor debe abortar la sentencia antes de que el HTTP corte")
}

func TestLoad_DesempenoStatementTimeout_Override(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("DESEMPENO_STATEMENT_TIMEOUT", "12s")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 12*time.Second, cfg.Desempeno.StatementTimeout)
}

// captureWarn swaps the default slog logger for the duration of the test.
// Safe here: tests in this file never run in parallel (t.Setenv).
func captureWarn(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// The default is derived from HTTP_WRITE_TIMEOUT so a production box with a
// shorter HTTP budget than ours still gets a ceiling below it — and boots.
func TestLoad_DesempenoStatementTimeout_DefaultDerivesFromHTTPWriteTimeout(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	for _, c := range []struct {
		write, want string
	}{
		{"30s", "25s"}, // min(25 s, 30−5)
		{"60s", "25s"},
		{"20s", "15s"}, // 20−5
		{"6s", "1s"},
		{"3s", "1s"},  // floor of 1 s
		{"0s", "25s"}, // no HTTP deadline at all
	} {
		t.Run(c.write, func(t *testing.T) {
			setMinimal(t)
			t.Setenv("HTTP_WRITE_TIMEOUT", c.write)
			buf := captureWarn(t)
			cfg, err := config.Load()
			require.NoError(t, err, "un tope derivado nunca impide arrancar")
			want, _ := time.ParseDuration(c.want)
			assert.Equal(t, want, cfg.Desempeno.StatementTimeout)
			assert.NotContains(t, buf.String(), "DESEMPENO_STATEMENT_TIMEOUT", "el derivado no es un error de configuración")
		})
	}
}

// An explicit value that would not fit the HTTP budget is CORRECTED with a
// WARN, never a refusal to boot: phones depend on this API being up.
func TestLoad_DesempenoStatementTimeout_InvalidExplicitIsCorrectedNotFatal(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	for _, c := range []struct {
		write, value, want string
	}{
		{"30s", "500ms", "25s"},
		{"30s", "30s", "25s"},
		{"30s", "45s", "25s"},
		{"20s", "25s", "15s"},
		{"30s", "-3s", "25s"},
	} {
		t.Run(c.write+"/"+c.value, func(t *testing.T) {
			setMinimal(t)
			t.Setenv("HTTP_WRITE_TIMEOUT", c.write)
			t.Setenv("DESEMPENO_STATEMENT_TIMEOUT", c.value)
			buf := captureWarn(t)
			cfg, err := config.Load()
			require.NoError(t, err, "un valor inválido nunca impide arrancar")
			want, _ := time.ParseDuration(c.want)
			assert.Equal(t, want, cfg.Desempeno.StatementTimeout)
			assert.Contains(t, buf.String(), "level=WARN")
			assert.Contains(t, buf.String(), "DESEMPENO_STATEMENT_TIMEOUT")
		})
	}
}

func TestLoad_DesempenoStatementTimeout_ValidExplicitDoesNotWarn(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("HTTP_WRITE_TIMEOUT", "0s")
	t.Setenv("DESEMPENO_STATEMENT_TIMEOUT", "40s")
	buf := captureWarn(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 40*time.Second, cfg.Desempeno.StatementTimeout, "sin tope HTTP cualquier valor >= 1 s vale")
	assert.Empty(t, buf.String())
}
