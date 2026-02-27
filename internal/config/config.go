// package config 

// import(
// 	"fmt"
// 	"os"
// 	"strconv"
// 	"strings"
// 	"time"
// )
// type Config struct{
// 	Host string `env:"HOST"`  //default 0.0.0.0
// 	Port int `env:"PORT"`
// 	QUICPort int `env:"QUIC_PORT"`

// }
// Package config holds all runtime configuration for the LAN Suite node.
// It is loaded once at startup and passed down to every subsystem.
// No other package imports this and mutates it — config is read-only after Load().
package config

/*
  CONCEPT: Package declaration
  ──────────────────────────────
  Every .go file starts with "package name".
  Files in the same directory must share the same package name.
  The package name is what you use when importing:
    import "github.com/yourusername/lan-suite/internal/config"
    cfg := config.Load()  ← "config" here is the package name

  Convention: package name = last segment of the import path.
  So internal/config → package config. Always follow this.
*/

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

/*
  CONCEPT: Structs
  ─────────────────
  A struct is a composite type — it groups related fields together.
  This is Go's version of a class (but simpler — no inheritance).

  Field names starting with uppercase = EXPORTED (visible outside package).
  Field names starting with lowercase = unexported (package-private).

  Struct tags (the backtick strings) are metadata read by other packages.
  `json:"host"` tells encoding/json to use "host" as the JSON key.
  `env:"HOST"`  tells our loader to read the HOST env var for this field.
  We're writing our own env loader — you'll see how tags work below.
*/

// Config is the complete configuration for a LAN Suite node.
// Constructed by Load() and passed around as a pointer to avoid copying.
type Config struct {
	// Server — HTTP/WebSocket API
	Host string `env:"HOST"     default:"0.0.0.0"`
	Port int    `env:"PORT"     default:"8080"`

	// QUIC — used for file transfer and drive sync
	QUICPort int `env:"QUIC_PORT" default:"4242"`

	// Discovery — UDP multicast for LAN peer finding
	MulticastGroup   string `env:"MULTICAST_GROUP"    default:"224.0.0.251"`
	MulticastPort    int    `env:"MULTICAST_PORT"     default:"5353"`
	DiscoveryInterval time.Duration // set programmatically below

	// Identity — this node's name, shown to peers
	NodeName string `env:"NODE_NAME" default:"lan-node"`

	// Security
	JWTSecret    string `env:"JWT_SECRET"     default:""`
	JWTExpiryHrs int    `env:"JWT_EXPIRY_HRS" default:"24"`

	// Storage — where files/messages are persisted on disk
	DataDir    string `env:"DATA_DIR"    default:"./data"`
	MaxStoreMB int    `env:"MAX_STORE_MB" default:"1024"`

	// Rate limiting
	WSMaxConnsPerIP int `env:"WS_MAX_CONNS_PER_IP" default:"10"`
	HTTPRatePerMin  int `env:"HTTP_RATE_PER_MIN"   default:"120"`

	// CORS — which frontend origins are allowed to connect
	// Comma-separated list: "http://localhost:5173,http://192.168.1.100:5173"
	AllowedOrigins []string `env:"ALLOWED_ORIGINS" default:"http://localhost:5173"`

	// Environment — "development" or "production"
	Env string `env:"APP_ENV" default:"development"`
}

/*
  CONCEPT: Methods on structs
  ────────────────────────────
  (c *Config) means this function is a "method" on Config.
  The *Config is called the "receiver" — it's like Python's `self`.
  * means it's a pointer receiver — the function can modify the struct.
  (c Config) without * would be a value receiver — gets a copy.

  Rule of thumb: use pointer receivers when:
  - The method modifies the struct
  - The struct is large (avoid copying)
  - You want consistent behavior with other methods on the same type
*/

// IsDev returns true when running in development mode.
// Use this to toggle debug logging, relaxed CORS, etc.
func (c *Config) IsDev() bool {
	return strings.ToLower(c.Env) == "development"
}

// Addr returns the full host:port string for the HTTP server.
// Example: "0.0.0.0:8080"
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// QUICAddr returns the full host:port for the QUIC listener.
func (c *Config) QUICAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.QUICPort)
}

// MulticastAddr returns the multicast group:port for UDP discovery.
func (c *Config) MulticastAddr() string {
	return fmt.Sprintf("%s:%d", c.MulticastGroup, c.MulticastPort)
}

/*
  CONCEPT: Constructor functions
  ────────────────────────────────
  Go has no constructors. Convention: name them New() or Load() or Default().
  They return a pointer to the initialized struct (and optionally an error).

  WHY return a pointer (*Config) not a value (Config)?
  Returning a pointer means all callers share ONE config object.
  Changes by one caller are seen by all others.
  For read-only config this doesn't matter much, but it's conventional
  and avoids copying a potentially large struct on every function call.
*/

// Load reads configuration from environment variables and returns a *Config.
// Values not found in env fall back to their defaults.
// Returns an error if any required value is invalid.
func Load() (*Config, error) {
	cfg := &Config{}

	// Read each field from env using our helper
	cfg.Host             = envStr("HOST",              "0.0.0.0")
	cfg.Port             = envInt("PORT",               8080)
	cfg.QUICPort         = envInt("QUIC_PORT",          4242)
	cfg.MulticastGroup   = envStr("MULTICAST_GROUP",   "224.0.0.251")
	cfg.MulticastPort    = envInt("MULTICAST_PORT",     5353)
	cfg.NodeName         = envStr("NODE_NAME",          hostname())
	cfg.JWTSecret        = envStr("JWT_SECRET",         "")
	cfg.JWTExpiryHrs     = envInt("JWT_EXPIRY_HRS",     24)
	cfg.DataDir          = envStr("DATA_DIR",           "./data")
	cfg.MaxStoreMB       = envInt("MAX_STORE_MB",       1024)
	cfg.WSMaxConnsPerIP  = envInt("WS_MAX_CONNS_PER_IP", 10)
	cfg.HTTPRatePerMin   = envInt("HTTP_RATE_PER_MIN",  120)
	cfg.Env              = envStr("APP_ENV",            "development")

	// Parse comma-separated ALLOWED_ORIGINS
	originsRaw := envStr("ALLOWED_ORIGINS", "http://localhost:5173")
	cfg.AllowedOrigins = splitTrimmed(originsRaw, ",")

	// Hardcoded — not worth making configurable
	cfg.DiscoveryInterval = 5 * time.Second

	/*
	  CONCEPT: Validation
	  ────────────────────
	  "Never trust the user" applies to environment variables too.
	  An operator could set PORT=99999 or JWT_SECRET="" in production.
	  We validate here, once, at startup, so the program fails LOUDLY
	  at boot with a clear message rather than failing silently at runtime.

	  This is called "fail fast" — detect problems as early as possible.
	*/
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// validate checks for invalid or dangerous config values.
func (c *Config) validate() error {
	// Ports must be in valid range
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("PORT %d is out of range 1-65535", c.Port)
	}
	if c.QUICPort < 1 || c.QUICPort > 65535 {
		return fmt.Errorf("QUIC_PORT %d is out of range 1-65535", c.QUICPort)
	}
	if c.Port == c.QUICPort {
		return fmt.Errorf("PORT and QUIC_PORT must be different (both are %d)", c.Port)
	}

	/*
	  CONCEPT: Why enforce JWT_SECRET in production?
	  ─────────────────────────────────────────────────
	  JWT tokens are signed with a secret key.
	  If the secret is empty or weak, anyone can forge a token and
	  authenticate as any user. This is a critical security hole.
	  In development, we use a default — convenient for testing.
	  In production, we REQUIRE the operator to set a real secret.
	  The error message tells them exactly what to do.
	*/
	if !c.IsDev() && len(c.JWTSecret) < 32 {
		return fmt.Errorf(
			"JWT_SECRET must be at least 32 characters in production. "+
				"Generate one with: openssl rand -hex 32\n"+
				"Got length: %d", len(c.JWTSecret),
		)
	}

	if c.MaxStoreMB < 1 {
		return fmt.Errorf("MAX_STORE_MB must be at least 1, got %d", c.MaxStoreMB)
	}

	if len(c.AllowedOrigins) == 0 {
		return fmt.Errorf("ALLOWED_ORIGINS cannot be empty")
	}

	return nil
}

// ── Helper functions ──────────────────────────────────────────────────────────

/*
  CONCEPT: os.Getenv
  ───────────────────
  os.Getenv("KEY") returns the value of env var KEY, or "" if not set.
  We wrap it with a fallback default.

  Why env vars and not a config file?
  Env vars are the 12-factor app standard. They work everywhere:
  - Local dev: .env file loaded by a tool like direnv
  - Docker: -e PORT=8080
  - Kubernetes: env: section in pod spec
  - Systemd: EnvironmentFile=
  No special parsing, no file format to learn, universally supported.
*/

func envStr(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func envInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		// Bad value in env — use default and don't crash here.
		// validate() will catch out-of-range values.
		fmt.Printf("[config] warning: %s=%q is not a valid integer, using default %d\n", key, val, defaultVal)
		return defaultVal
	}
	return n
}

func splitTrimmed(s, sep string) []string {
	parts := strings.Split(s, sep)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// hostname returns the OS hostname as a fallback node name.
// If it fails (rare), we fall back to "lan-node".
func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "lan-node"
	}
	return name
}