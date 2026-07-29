package config

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
)

// CHConfig holds ClickHouse connection parameters.
type CHConfig struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
}

// Config is the central configuration for the backend server.
type Config struct {
	Port      string
	StaticDir string
	DataDir   string
	CacheDir  string

	// External services (optional — "" means not configured).
	ZerotraceAddr  string
	AlgorithmsAddr string

	// ClickHouse (optional — nil if not configured).
	ClickHouse *CHConfig
}

// init loads .env file before any config reading.
func init() {
	loadEnvFile()
}

// loadEnvFile reads .env and .env.local files, sets environment variables.
// .env.local takes precedence (loaded after .env, overwriting values).
// Existing OS environment variables always take precedence.
func loadEnvFile() {
	files := []string{".env", ".env.local"}
	for i, name := range files {
		if _, err := os.Stat(name); os.IsNotExist(err) {
			continue
		}
		f, err := os.Open(name)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if key == "" {
				continue
			}
			// .env sets defaults (only if not already in OS env).
			// .env.local always overrides (local developer overrides).
			// Real OS env vars set before process start take precedence over both.
			isLocal := i == 1 // .env.local is the second file
			if isLocal || os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
		_ = f.Close()
		log.Printf("📄 Loaded config from %s", name)
	}
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	cfg := &Config{
		Port:           envStr("PORT", "8888"),
		StaticDir:      envStr("STATIC_DIR", "../cloud.deepflow.yunshan.net"),
		DataDir:        envStr("DATA_DIR", "./data"),
		CacheDir:       envStr("CACHE_DIR", "../api_cache"),
		ZerotraceAddr:  os.Getenv("ZEROTRACE_ADDR"),
		AlgorithmsAddr: os.Getenv("ALGORITHMS_ADDR"),
	}

	if h := os.Getenv("CLICKHOUSE_HOST"); h != "" {
		cfg.ClickHouse = &CHConfig{
			Host:     h,
			Port:     envInt("CLICKHOUSE_PORT", 9000),
			Database: envStr("CLICKHOUSE_DB", "flow_log"),
			User:     envStr("CLICKHOUSE_USER", "default"),
			Password: os.Getenv("CLICKHOUSE_PASSWORD"),
		}
	}

	return cfg
}

// ZerotraceServerURL returns the full base URL for the zerotrace-server, or "" if not configured.
func (c *Config) ZerotraceServerURL() string {
	if c.ZerotraceAddr == "" {
		return ""
	}
	return fmt.Sprintf("http://%s", c.ZerotraceAddr)
}

// AlgorithmsServerURL returns the full base URL for zerotrace-algorithms, or "" if not configured.
func (c *Config) AlgorithmsServerURL() string {
	if c.AlgorithmsAddr == "" {
		return ""
	}
	return fmt.Sprintf("http://%s", c.AlgorithmsAddr)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}
