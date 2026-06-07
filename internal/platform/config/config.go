// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
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
	errFirebaseRequired = errors.New("config: FIREBASE_PROJECT_ID is required " +
		"in this environment (set FIREBASE_DEV_MODE=true for development, or " +
		"FIREBASE_ALLOW_UNCONFIGURED=true to explicitly opt into 401 responses)")
	errFirebaseDevModeOnlyInDev = errors.New("config: FIREBASE_DEV_MODE=true is " +
		"only permitted when APP_ENV=development (refusing to boot)")
	errStorageDirRequired = errors.New("config: STORAGE_DIR is required")
	errImageProcessor     = errors.New("config: invalid IMAGEPROCESSOR_* configuration")
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
	App            App
	Cobranza       Cobranza
	HTTP           HTTP
	Postgres       Postgres
	Firebird       Firebird
	Firebase       Firebase
	Sync           Sync
	Storage        Storage
	ImageProcessor ImageProcessor
	Microsip       Microsip
	FailedIntent   FailedIntent
}

// FailedIntent holds blob-store knobs for the failedintent capture pipeline.
//
//   - BlobDir is where multipart bodies land on disk. When empty, it defaults
//     to STORAGE_DIR/failed-intents so a sibling exists by construction.
//   - MaxMultipartBytes caps each individual blob (50 MiB by default). A
//     request whose body exceeds the cap is still captured, but the intent's
//     BodyBlobPath is empty and BodyTruncated is true.
type FailedIntent struct {
	BlobDir           string `env:"FAILEDINTENT_BLOB_DIR"`
	MaxMultipartBytes int64  `env:"FAILEDINTENT_MAX_MULTIPART_BYTES" envDefault:"52428800"`
}

// FailedIntentBlobDir returns the resolved blob directory, falling back to
// a sibling under STORAGE_DIR when FAILEDINTENT_BLOB_DIR is unset.
func (c *Config) FailedIntentBlobDir() string {
	if strings.TrimSpace(c.FailedIntent.BlobDir) != "" {
		return c.FailedIntent.BlobDir
	}
	return filepath.Join(c.Storage.Dir, "failed-intents")
}

// Microsip holds runtime knobs for the read-only microsip catalog module.
type Microsip struct {
	// PriceListIDs are the PRECIO_EMPRESA_IDs filtered into the article
	// list query (legacy default: 42 MUEBLERIAS, 8437, 6925). Keeping the
	// list configurable means a future business decision to swap price
	// lists does not require a recompile.
	PriceListIDs []int `env:"MICROSIP_PRICE_LIST_IDS" envSeparator:"," envDefault:"42,8437,6925"`
}

// Cobranza holds cobranza-module-specific runtime knobs.
type Cobranza struct {
	// SSEEnabled gates the SSE streaming endpoints. Default true: SSE is the
	// designed steady-state delivery channel for the mobile cobranza app and
	// the digest-reconcile path (handlers_sync_reconcile.go) already covers
	// the case where SSE is unavailable. The flag stays as an ops killswitch
	// for emergencies (e.g. proxy buffering, leak): flip COBRANZA_SSE_ENABLED
	// to false in .env and restart, no code change required. Clients
	// gracefully fall through to polling+reconcile when the endpoint returns
	// 503 (see CobranzaSseSubscriber.kt in msp-app-kt).
	SSEEnabled bool `env:"COBRANZA_SSE_ENABLED" envDefault:"true"`
	// SSEPingEvery controls how often the server writes an SSE keep-alive
	// comment. Clients and proxies silently drop the ping; it only exists to
	// keep the TCP connection alive through idle timeouts.
	SSEPingEvery time.Duration `env:"COBRANZA_SSE_PING_INTERVAL" envDefault:"25s"`
}

// ImageProcessor holds the runtime knobs for the
// internal/platform/imageprocessor pipeline. Set Enabled=false to opt out
// at runtime; the package falls back to its NoOp passthrough impl. The
// other fields are only consulted when Enabled is true.
type ImageProcessor struct {
	Enabled              bool  `env:"IMAGEPROCESSOR_ENABLED" envDefault:"true"`
	MaxLongSidePx        int   `env:"IMAGEPROCESSOR_MAX_LONG_SIDE_PX" envDefault:"1920"`
	JPEGQuality          int   `env:"IMAGEPROCESSOR_JPEG_QUALITY" envDefault:"85"`
	PNGCompression       int   `env:"IMAGEPROCESSOR_PNG_COMPRESSION" envDefault:"-1"`
	MaxInputBytes        int64 `env:"IMAGEPROCESSOR_MAX_INPUT_BYTES" envDefault:"15728640"`
	RecompressWebPToJPEG bool  `env:"IMAGEPROCESSOR_WEBP_TO_JPEG" envDefault:"true"`
	PreserveSmallImages  bool  `env:"IMAGEPROCESSOR_PRESERVE_SMALL" envDefault:"true"`
	SmallImageBytes      int64 `env:"IMAGEPROCESSOR_SMALL_IMAGE_BYTES" envDefault:"524288"`
}

// validate enforces shape invariants on the knob ranges. The
// imageprocessor package re-validates after mapping to its Options type;
// this layer catches obvious misconfigs before the package is even
// invoked.
func (i ImageProcessor) validate() error {
	if !i.Enabled {
		return nil
	}
	if i.MaxLongSidePx < 0 {
		return fmt.Errorf("%w: IMAGEPROCESSOR_MAX_LONG_SIDE_PX must be >= 0 (got %d)", errImageProcessor, i.MaxLongSidePx)
	}
	if i.JPEGQuality < 1 || i.JPEGQuality > 100 {
		return fmt.Errorf("%w: IMAGEPROCESSOR_JPEG_QUALITY must be in [1,100] (got %d)", errImageProcessor, i.JPEGQuality)
	}
	if i.MaxInputBytes < 1 {
		return fmt.Errorf("%w: IMAGEPROCESSOR_MAX_INPUT_BYTES must be >= 1 (got %d)", errImageProcessor, i.MaxInputBytes)
	}
	switch i.PNGCompression {
	case -1, 0, -2, -3:
	default:
		return fmt.Errorf("%w: IMAGEPROCESSOR_PNG_COMPRESSION must be one of {-1,0,-2,-3} (got %d)", errImageProcessor, i.PNGCompression)
	}
	if i.SmallImageBytes < 0 {
		return fmt.Errorf("%w: IMAGEPROCESSOR_SMALL_IMAGE_BYTES must be >= 0 (got %d)", errImageProcessor, i.SmallImageBytes)
	}
	return nil
}

// Storage holds blob-storage configuration for the ventas module's image
// uploads. The deployment target writes blobs to a local directory; cloud
// object storage is intentionally not part of this configuration.
type Storage struct {
	Dir string `env:"STORAGE_DIR" envDefault:"./var/uploads"`
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
//
// Charset defaults to UTF8: the firebirdsql driver delegates encoding
// translation to the Firebird server and Go strings stay UTF-8 native.
// Set FB_CHARSET=WIN1252 only when running against a server that does not
// transcode (legacy Microsip installs) and you can tolerate the parallel-write
// scenarios that motivated the old Node API choice.
type Firebird struct {
	Host         string `env:"FB_HOST" envDefault:"localhost"`
	Port         int    `env:"FB_PORT" envDefault:"3050"`
	Database     string `env:"FB_DATABASE"`
	User         string `env:"FB_USER" envDefault:"SYSDBA"`
	Password     string `env:"FB_PASSWORD"`
	Charset      string `env:"FB_CHARSET" envDefault:"UTF8"`
	PoolSize     int    `env:"FB_POOL_SIZE" envDefault:"10"`
	WireCrypt    bool   `env:"FB_WIRE_CRYPT" envDefault:"true"`
	WireCompress bool   `env:"FB_WIRE_COMPRESS" envDefault:"false"`
}

// DSN returns the connection string for the Firebird driver.
func (f Firebird) DSN() string {
	hostPort := net.JoinHostPort(f.Host, strconv.Itoa(f.Port))
	return fmt.Sprintf(
		"%s:%s@%s/%s?charset=%s&wire_crypt=%t&wire_compress=%t",
		f.User, f.Password, hostPort, f.Database, f.Charset,
		f.WireCrypt, f.WireCompress,
	)
}

// Firebase holds Firebase Admin SDK settings.
//
// ProjectID is conditionally required by [Config.validate]:
//
//   - APP_ENV=production: required (no escape hatch).
//   - APP_ENV=development with FIREBASE_DEV_MODE=true: not required;
//     the DevMode token client takes over and accepts "dev:<uid>" tokens.
//   - Anywhere else: required unless FIREBASE_ALLOW_UNCONFIGURED=true,
//     which boots with the NotConfigured client that returns 401 on every
//     authenticated request. Use only for staging-without-auth or
//     internal-tooling-only deployments.
type Firebase struct {
	ProjectID          string `env:"FIREBASE_PROJECT_ID"`
	ServiceAccountPath string `env:"FIREBASE_SERVICE_ACCOUNT_PATH" envDefault:"./serviceAccountKey.json"`
	DevMode            bool   `env:"FIREBASE_DEV_MODE" envDefault:"false"`
	AllowUnconfigured  bool   `env:"FIREBASE_ALLOW_UNCONFIGURED" envDefault:"false"`
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

	if err := c.Firebase.validate(c.App.Env); err != nil {
		errs = append(errs, err)
	}

	if err := c.Storage.validate(); err != nil {
		errs = append(errs, err)
	}

	if err := c.ImageProcessor.validate(); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// validate enforces that the storage configuration is internally
// consistent.
func (s Storage) validate() error {
	if strings.TrimSpace(s.Dir) == "" {
		return errStorageDirRequired
	}
	return nil
}

// validate enforces the conditional rules around Firebase configuration.
// See the [Firebase] type doc for the matrix.
func (f Firebase) validate(appEnv Environment) error {
	if f.DevMode && appEnv != EnvDevelopment {
		return errFirebaseDevModeOnlyInDev
	}
	if f.ProjectID != "" {
		return nil
	}
	switch appEnv {
	case EnvProduction:
		return errFirebaseRequired
	case EnvDevelopment:
		if f.DevMode {
			return nil
		}
	case EnvStaging, EnvTest:
		// fall through to the AllowUnconfigured escape hatch.
	}
	if f.AllowUnconfigured {
		return nil
	}
	return errFirebaseRequired
}
