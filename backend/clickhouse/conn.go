package clickhouse

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"deeptrace-backend/config"
)

// CHService wraps the ClickHouse connection pool.
type CHService struct {
	conn    driver.Conn
	enabled bool
}

// New creates a CHService. Returns nil if ClickHouse is not configured.
func New(cfg *config.CHConfig) *CHService {
	if cfg == nil {
		return nil
	}

	ctx := context.Background()
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)},
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.User,
			Password: cfg.Password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		DialTimeout:      time.Second * 10,
		MaxOpenConns:     10,
		MaxIdleConns:     5,
		ConnMaxLifetime:  time.Hour,
		ConnOpenStrategy: clickhouse.ConnOpenInOrder,
	})
	if err != nil {
		log.Printf("⚠️  ClickHouse not available: %v", err)
		return &CHService{enabled: false}
	}

	if err := conn.Ping(ctx); err != nil {
		log.Printf("⚠️  ClickHouse ping failed: %v", err)
		return &CHService{enabled: false}
	}

	log.Printf("✅ Connected to ClickHouse at %s:%d (db=%s, user=%s)",
		cfg.Host, cfg.Port, cfg.Database, cfg.User)
	return &CHService{conn: conn, enabled: true}
}

// Enabled reports whether ClickHouse is available.
func (s *CHService) Enabled() bool {
	return s != nil && s.enabled
}

// Query runs a ClickHouse query and returns rows.
func (s *CHService) Query(ctx context.Context, query string, args ...interface{}) (driver.Rows, error) {
	if !s.Enabled() {
		return nil, fmt.Errorf("clickhouse not connected")
	}
	return s.conn.Query(ctx, query, args...)
}

// Exec runs a ClickHouse statement.
func (s *CHService) Exec(ctx context.Context, query string, args ...interface{}) error {
	if !s.Enabled() {
		return fmt.Errorf("clickhouse not connected")
	}
	return s.conn.Exec(ctx, query, args...)
}

// QueryRow runs a query returning a single row.
func (s *CHService) QueryRow(ctx context.Context, query string, args ...interface{}) driver.Row {
	if !s.Enabled() {
		return nil
	}
	return s.conn.QueryRow(ctx, query, args...)
}
