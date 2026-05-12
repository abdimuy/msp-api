package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/config"
)

// setMinimal sets only the variables marked `required` in Config.
//
// Tests in this file do NOT call t.Parallel(): t.Setenv mutates process-wide
// state and must not race with other tests in the same package.
func setMinimal(t *testing.T) {
	t.Helper()
	t.Setenv("PG_USER", "msp")
	t.Setenv("PG_PASSWORD", "msp")
	t.Setenv("PG_DATABASE", "msp_dev")
	t.Setenv("FIREBASE_PROJECT_ID", "test-project")
}

func TestLoad_MinimalEnv_Succeeds(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, config.EnvDevelopment, cfg.App.Env)
	assert.Equal(t, 3001, cfg.HTTP.Port)
	assert.Equal(t, "msp", cfg.Postgres.User)
	assert.Equal(t, "test-project", cfg.Firebase.ProjectID)
}

func TestLoad_MissingRequired_Fails(t *testing.T) { //nolint:paralleltest // env-dependent
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

func TestPostgres_DSN(t *testing.T) {
	t.Parallel()
	pg := config.Postgres{
		Host: "db.local", Port: 5432, User: "u", Password: "p",
		Database: "msp", SSLMode: "require",
	}
	assert.Equal(t, "postgres://u:p@db.local:5432/msp?sslmode=require", pg.DSN())
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
	t.Setenv("PG_USER", "msp")
	t.Setenv("PG_PASSWORD", "msp")
	t.Setenv("PG_DATABASE", "msp_dev")
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

func TestPostgres_DSN_IPv6Host(t *testing.T) {
	t.Parallel()
	pg := config.Postgres{
		Host: "::1", Port: 5432, User: "u", Password: "p",
		Database: "msp", SSLMode: "disable",
	}
	// net.JoinHostPort wraps IPv6 in brackets.
	assert.Equal(t, "postgres://u:p@[::1]:5432/msp?sslmode=disable", pg.DSN())
}
