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
		Database: "/data/msp.fdb", Charset: "WIN1252",
	}
	assert.Equal(t, "SYSDBA:x@host:3050//data/msp.fdb?charset=WIN1252", fb.DSN())
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
