package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"deeptrace-backend/logging"
)

// CHConfig holds ClickHouse connection parameters.
type CHConfig struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
}

// MysqlConfig holds optional deepflow metadb (MySQL) connection parameters.
// When configured, auth handlers read the real user/org from t_user/t_org
// instead of the hardcoded fallback.
type MysqlConfig struct {
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
	CacheDir  string

	// External services (optional — "" means not configured).
	ZerotraceAddr  string
	AlgorithmsAddr string

	// ClickHouse (optional — nil if not configured).
	ClickHouse *CHConfig

	// MySQL metadb (optional — nil keeps the hardcoded auth identities).
	MySQL *MysqlConfig

	// VerifySourceControl enables client-selected data sources via
	// X-DeepTrace-Force-Source. Local verification only — must stay false
	// on public deployments.
	VerifySourceControl bool
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
		logging.Infof("Loaded config from %s", name)
	}
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	cfg := &Config{
		Port:                envStr("PORT", "8888"),
		StaticDir:           envStr("STATIC_DIR", "../cloud.deepflow.yunshan.net"),
		CacheDir:            envStr("CACHE_DIR", "../api_cache"),
		ZerotraceAddr:       os.Getenv("ZEROTRACE_ADDR"),
		AlgorithmsAddr:      os.Getenv("ALGORITHMS_ADDR"),
		VerifySourceControl: os.Getenv("VERIFY_SOURCE_CONTROL") == "true",
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

	if h := os.Getenv("MYSQL_HOST"); h != "" {
		cfg.MySQL = &MysqlConfig{
			Host:     h,
			Port:     envInt("MYSQL_PORT", 3306),
			Database: envStr("MYSQL_DATABASE", "deepflow"),
			User:     envStr("MYSQL_USER", "root"),
			Password: os.Getenv("MYSQL_PASSWORD"),
		}
	}

	return cfg
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
