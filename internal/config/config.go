package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Host string `env:"HOST"     default:"0.0.0.0"`
	Port int    `env:"PORT"     default:"8080"`

	QUICPort int `env:"QUIC_PORT" default:"4242"`

	MulticastGroup    string `env:"MULTICAST_GROUP"    default:"224.0.0.251"`
	MulticastPort     int    `env:"MULTICAST_PORT"     default:"5353"`
	DiscoveryInterval time.Duration

	NodeName string `env:"NODE_NAME" default:"lan-node"`

	JWTSecret    string `env:"JWT_SECRET"     default:""`
	JWTExpiryHrs int    `env:"JWT_EXPIRY_HRS" default:"24"`

	DataDir    string `env:"DATA_DIR"    default:"./data"`
	MaxStoreMB int    `env:"MAX_STORE_MB" default:"1024"`

	WSMaxConnsPerIP int `env:"WS_MAX_CONNS_PER_IP" default:"10"`
	HTTPRatePerMin  int `env:"HTTP_RATE_PER_MIN"   default:"120"`

	AllowedOrigins []string `env:"ALLOWED_ORIGINS" default:"http://localhost:5173"`

	BootstrapPeers []string `env:"BOOTSTRAP_PEERS" default:""`

	Env string `env:"APP_ENV" default:"development"`
}

func (c *Config) IsDev() bool {
	return strings.ToLower(c.Env) == "development"
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func (c *Config) QUICAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.QUICPort)
}

func (c *Config) MulticastAddr() string {
	return fmt.Sprintf("%s:%d", c.MulticastGroup, c.MulticastPort)
}

func Load() (*Config, error) {
	cfg := &Config{}

	cfg.Host = envStr("HOST", "0.0.0.0")
	cfg.Port = envInt("PORT", 8080)
	cfg.QUICPort = envInt("QUIC_PORT", 4242)
	cfg.MulticastGroup = envStr("MULTICAST_GROUP", "224.0.0.251")
	cfg.MulticastPort = envInt("MULTICAST_PORT", 5353)
	cfg.NodeName = envStr("NODE_NAME", hostname())
	cfg.JWTSecret = envStr("JWT_SECRET", "")
	cfg.JWTExpiryHrs = envInt("JWT_EXPIRY_HRS", 24)
	cfg.DataDir = envStr("DATA_DIR", "./data")
	cfg.MaxStoreMB = envInt("MAX_STORE_MB", 1024)
	cfg.WSMaxConnsPerIP = envInt("WS_MAX_CONNS_PER_IP", 10)
	cfg.HTTPRatePerMin = envInt("HTTP_RATE_PER_MIN", 120)
	cfg.Env = envStr("APP_ENV", "development")

	bootstrapRaw := envStr("BOOTSTRAP_PEERS", "")
	if bootstrapRaw != "" {
		cfg.BootstrapPeers = splitTrimmed(bootstrapRaw, ",")
	}

	originsRaw := envStr("ALLOWED_ORIGINS", "http://localhost:5173")
	cfg.AllowedOrigins = splitTrimmed(originsRaw, ",")

	cfg.DiscoveryInterval = 5 * time.Second

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

func (c *Config) validate() error {

	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("PORT %d is out of range 1-65535", c.Port)
	}
	if c.QUICPort < 1 || c.QUICPort > 65535 {
		return fmt.Errorf("QUIC_PORT %d is out of range 1-65535", c.QUICPort)
	}
	if c.Port == c.QUICPort {
		return fmt.Errorf("PORT and QUIC_PORT must be different (both are %d)", c.Port)
	}

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

func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "lan-node"
	}
	return name
}
