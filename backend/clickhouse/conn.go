package clickhouse

import (
	"context"
	"fmt"
	"time"

	"deeptrace-backend/config"
	"deeptrace-backend/logging"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// CHService wraps the ClickHouse connection pool.
type CHService struct {
	conn    driver.Conn
	enabled bool
	reg     *ColumnRegistry
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
		logging.Errorf("ClickHouse not available: %v", err)
		return &CHService{enabled: false}
	}

	if err := conn.Ping(ctx); err != nil {
		logging.Errorf("ClickHouse ping failed: %v", err)
		return &CHService{enabled: false}
	}

	logging.Infof("Connected to ClickHouse at %s:%d (db=%s, user=%s)",
		cfg.Host, cfg.Port, cfg.Database, cfg.User)
	s := &CHService{conn: conn, enabled: true}
	s.reg = NewColumnRegistry(s)
	return s
}

// HasColumn implements ColumnChecker via the schema registry.
func (s *CHService) HasColumn(db, table, col string) bool {
	if s.reg == nil {
		return true
	}
	return s.reg.HasColumn(db, table, col)
}

// ResolveTable resolves a flow_metrics family to the actual granularity table
// present in the database (see ColumnRegistry.ResolveTable).
func (s *CHService) ResolveTable(db, family string, windowSec int64, dataSource string) string {
	if s.reg == nil {
		return ""
	}
	return s.reg.ResolveTable(db, family, windowSec, dataSource)
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
