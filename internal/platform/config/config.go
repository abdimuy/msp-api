// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Sentinel errors so each validation produces a wrapped, comparable error.
var (
	errInvalidAppEnv    = errors.New("config: invalid APP_ENV")
	errInvalidLogLevel  = errors.New("config: invalid APP_LOG_LEVEL")
	errInvalidLogFormat = errors.New("config: invalid APP_LOG_FORMAT")
	errInvalidPort      = errors.New("config: invalid APP_PORT")
	errInvalidBodySize  = errors.New("config: invalid HTTP_MAX_BODY_SIZE_MB")
)

// Environment is the runtime environment.
type Environment string

// Recognized values for Environment.
const (
	EnvDevelopment Environment = "development"
	EnvStaging     Environment = "staging"
	EnvProduction  Environment = "production"
	EnvTest        Environment = "test"
)

// IsValid reports whether the environment is one of the recognized values.
func (e Environment) IsValid() bool {
	switch e {
	case EnvDevelopment, EnvStaging, EnvProduction, EnvTest:
		return true
	}
	return false
}

// Config aggregates all runtime configuration.
type Config struct {
	App      App
	HTTP     HTTP
	Postgres Postgres
	Firebird Firebird
	Firebase Firebase
	Sync     Sync
}

// App holds general app settings.
type App struct {
	Env       Environment `env:"APP_ENV" envDefault:"development"`
	LogLevel  string      `env:"APP_LOG_LEVEL" envDefault:"info"`
	LogFormat string      `env:"APP_LOG_FORMAT" envDefault:"text"`
}

// HTTP holds HTTP server settings.
type HTTP struct {
	Port            int           `env:"APP_PORT" envDefault:"3001"`
	ReadTimeout     time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"10s"`
	WriteTimeout    time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"30s"`
	IdleTimeout     time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"120s"`
	ShutdownTimeout time.Duration `env:"HTTP_SHUTDOWN_TIMEOUT" envDefault:"30s"`
	MaxBodySizeMB   int           `env:"HTTP_MAX_BODY_SIZE_MB" envDefault:"10"`
	CORSOrigins     []string      `env:"CORS_ALLOWED_ORIGINS" envSeparator:"," envDefault:"http://localhost:3000"`
}

// Postgres holds Postgres connection settings.
type Postgres struct {
	Host         string `env:"PG_HOST" envDefault:"localhost"`
	Port         int    `env:"PG_PORT" envDefault:"5432"`
	User         string `env:"PG_USER,required"`
	Password     string `env:"PG_PASSWORD,required"`
	Database     string `env:"PG_DATABASE,required"`
	SSLMode      string `env:"PG_SSLMODE" envDefault:"disable"`
	MaxOpenConns int32  `env:"PG_MAX_OPEN_CONNS" envDefault:"25"`
	MaxIdleConns int32  `env:"PG_MAX_IDLE_CONNS" envDefault:"5"`
}

// DSN returns the connection string for pgx.
func (p Postgres) DSN() string {
	hostPort := net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
	return fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=%s",
		p.User, p.Password, hostPort, p.Database, p.SSLMode)
}

// Firebird holds Firebird connection settings (Microsip database).
type Firebird struct {
	Host     string `env:"FB_HOST" envDefault:"localhost"`
	Port     int    `env:"FB_PORT" envDefault:"3050"`
	Database string `env:"FB_DATABASE"`
	User     string `env:"FB_USER" envDefault:"SYSDBA"`
	Password string `env:"FB_PASSWORD"`
	Charset  string `env:"FB_CHARSET" envDefault:"WIN1252"`
	PoolSize int    `env:"FB_POOL_SIZE" envDefault:"10"`
}

// DSN returns the connection string for the Firebird driver.
func (f Firebird) DSN() string {
	hostPort := net.JoinHostPort(f.Host, strconv.Itoa(f.Port))
	return fmt.Sprintf("%s:%s@%s/%s?charset=%s",
		f.User, f.Password, hostPort, f.Database, f.Charset)
}

// Firebase holds Firebase Admin SDK settings.
type Firebase struct {
	ProjectID          string `env:"FIREBASE_PROJECT_ID,required"`
	ServiceAccountPath string `env:"FIREBASE_SERVICE_ACCOUNT_PATH" envDefault:"./serviceAccountKey.json"`
}

// Sync holds Microsip sync worker settings.
type Sync struct {
	Enabled   bool          `env:"MICROSIP_SYNC_ENABLED" envDefault:"false"`
	Interval  time.Duration `env:"MICROSIP_SYNC_INTERVAL" envDefault:"5s"`
	BatchSize int           `env:"MICROSIP_SYNC_BATCH_SIZE" envDefault:"500"`
}

// Load parses environment variables into a Config and validates it.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("config: parse env: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config: validate: %w", err)
	}
	return cfg, nil
}

func (c *Config) validate() error {
	var errs []error

	if !c.App.Env.IsValid() {
		errs = append(errs, fmt.Errorf("%w: %q", errInvalidAppEnv, c.App.Env))
	}

	switch strings.ToLower(c.App.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("%w: %q", errInvalidLogLevel, c.App.LogLevel))
	}

	switch strings.ToLower(c.App.LogFormat) {
	case "text", "json":
	default:
		errs = append(errs, fmt.Errorf("%w: %q", errInvalidLogFormat, c.App.LogFormat))
	}

	if c.HTTP.Port < 1 || c.HTTP.Port > 65535 {
		errs = append(errs, fmt.Errorf("%w: %d", errInvalidPort, c.HTTP.Port))
	}

	if c.HTTP.MaxBodySizeMB < 1 {
		errs = append(errs, fmt.Errorf("%w: %d", errInvalidBodySize, c.HTTP.MaxBodySizeMB))
	}

	return errors.Join(errs...)
}
