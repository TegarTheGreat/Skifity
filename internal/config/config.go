// Package config loads panel configuration from a file and the environment.
//
// Precedence, lowest to highest: built-in defaults, the config file, environment
// variables. Everything an operator can change at runtime lives in the database
// and is edited in the panel; this package only covers what must be known before
// the database can be opened.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"skifity/internal/version"
)

// EnvPrefix is the prefix of every environment variable this package reads.
const EnvPrefix = "SKIFITY_"

// Config is the panel's startup configuration.
type Config struct {
	// Listen is the address the HTTP server binds to.
	Listen string `json:"listen" toml:"listen"`
	// DatabasePath is the SQLite file backing the panel.
	DatabasePath string `json:"database_path" toml:"database_path"`
	// MasterKeyPath is the file holding the master encryption key. It is kept
	// outside the database on purpose: see ADR-0006.
	MasterKeyPath string `json:"master_key_path" toml:"master_key_path"`
	// KubeconfigPath is empty when running inside the cluster, where the
	// ServiceAccount token is used instead.
	KubeconfigPath string `json:"kubeconfig_path" toml:"kubeconfig_path"`
	// Namespace is the namespace the panel itself runs in.
	Namespace string `json:"namespace" toml:"namespace"`
	// LogLevel is one of debug, info, warn, error.
	LogLevel string `json:"log_level" toml:"log_level"`
	// LogFormat is json or text.
	LogFormat string `json:"log_format" toml:"log_format"`
	// DevMode serves the frontend from a Vite dev server instead of the embedded
	// files and relaxes cookie security so http://localhost works.
	DevMode bool `json:"dev_mode" toml:"dev_mode"`
	// DevFrontendURL is the Vite dev server to proxy to when DevMode is on.
	DevFrontendURL string `json:"dev_frontend_url" toml:"dev_frontend_url"`
	// PublicURL is how users reach the panel. Used for OAuth callbacks, webhook
	// URLs and links in notifications. Empty means "derive from the request".
	PublicURL string `json:"public_url" toml:"public_url"`
	// TrustedProxyCount is how many reverse proxies sit in front of the panel.
	// Only that many entries are trusted from X-Forwarded-For.
	TrustedProxyCount int `json:"trusted_proxy_count" toml:"trusted_proxy_count"`
	// SessionTTL is how long a login lasts without activity.
	SessionTTL time.Duration `json:"session_ttl" toml:"session_ttl"`
	// ShutdownGrace is how long in-flight requests get during shutdown.
	ShutdownGrace time.Duration `json:"shutdown_grace" toml:"shutdown_grace"`
	// SetupTokenPath holds the one-time token the installer generates. The file
	// is deleted once setup completes.
	SetupTokenPath string `json:"setup_token_path" toml:"setup_token_path"`
}

// Default returns the configuration used when nothing overrides it.
func Default() Config {
	return Config{
		Listen:            ":8080",
		DatabasePath:      filepath.Join(version.DataDir, "panel.db"),
		MasterKeyPath:     filepath.Join(version.ConfigDir, "master.key"),
		Namespace:         "skifity-system",
		LogLevel:          "info",
		LogFormat:         "json",
		DevFrontendURL:    "http://127.0.0.1:5173",
		TrustedProxyCount: 1,
		SessionTTL:        7 * 24 * time.Hour,
		ShutdownGrace:     20 * time.Second,
		SetupTokenPath:    filepath.Join(version.ConfigDir, "setup-token"),
	}
}

// Load builds a Config from defaults, then the file at path when it is non-empty
// and exists, then the environment.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		if err := cfg.applyFile(path); err != nil {
			return cfg, err
		}
	}
	cfg.applyEnv(os.Getenv)
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c *Config) applyFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read config %s: %w", path, err)
	}
	kv, err := parseSimpleTOML(string(data))
	if err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	c.applyEnv(func(key string) string {
		return kv[strings.ToLower(strings.TrimPrefix(key, EnvPrefix))]
	})
	return nil
}

// applyEnv reads every setting through lookup, which lets the same mapping serve
// both the environment and the config file.
func (c *Config) applyEnv(lookup func(string) string) {
	str := func(key string, dst *string) {
		if v := lookup(EnvPrefix + key); v != "" {
			*dst = v
		}
	}
	str("LISTEN", &c.Listen)
	str("DATABASE_PATH", &c.DatabasePath)
	str("MASTER_KEY_PATH", &c.MasterKeyPath)
	str("KUBECONFIG", &c.KubeconfigPath)
	str("NAMESPACE", &c.Namespace)
	str("LOG_LEVEL", &c.LogLevel)
	str("LOG_FORMAT", &c.LogFormat)
	str("DEV_FRONTEND_URL", &c.DevFrontendURL)
	str("PUBLIC_URL", &c.PublicURL)
	str("SETUP_TOKEN_PATH", &c.SetupTokenPath)

	if v := lookup(EnvPrefix + "DEV_MODE"); v != "" {
		c.DevMode = truthy(v)
	}
	if v := lookup(EnvPrefix + "TRUSTED_PROXY_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			c.TrustedProxyCount = n
		}
	}
	if v := lookup(EnvPrefix + "SESSION_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			c.SessionTTL = d
		}
	}
	if v := lookup(EnvPrefix + "SHUTDOWN_GRACE"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			c.ShutdownGrace = d
		}
	}
}

// Validate reports configuration that would fail later in a confusing way.
func (c *Config) Validate() error {
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("listen %q is not host:port: %w", c.Listen, err)
	}
	if c.DatabasePath == "" {
		return errors.New("database_path must not be empty")
	}
	if c.MasterKeyPath == "" {
		return errors.New("master_key_path must not be empty")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log_level %q must be one of debug, info, warn, error", c.LogLevel)
	}
	switch c.LogFormat {
	case "json", "text":
	default:
		return fmt.Errorf("log_format %q must be json or text", c.LogFormat)
	}
	if c.PublicURL != "" && !strings.HasPrefix(c.PublicURL, "http://") && !strings.HasPrefix(c.PublicURL, "https://") {
		return fmt.Errorf("public_url %q must start with http:// or https://", c.PublicURL)
	}
	if c.SessionTTL < time.Minute {
		return fmt.Errorf("session_ttl %s is shorter than a minute", c.SessionTTL)
	}
	return nil
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// parseSimpleTOML understands the flat `key = "value"` subset we need, plus
// `#` comments. Pulling in a TOML dependency to read eight keys is not worth it.
func parseSimpleTOML(src string) (map[string]string, error) {
	out := map[string]string{}
	for i, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			return nil, fmt.Errorf("line %d: sections are not supported in the panel config file", i+1)
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected key = value", i+1)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if idx := strings.Index(value, " #"); idx >= 0 {
			value = strings.TrimSpace(value[:idx])
		}
		value = strings.Trim(value, `"'`)
		out[key] = value
	}
	return out, nil
}
