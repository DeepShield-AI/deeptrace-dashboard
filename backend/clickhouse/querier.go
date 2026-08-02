package clickhouse

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// Querier is the ClickHouse surface used by the query builders (list/top/
// tracemap). CHService implements it; tests substitute a fake so SQL building
// and result handling can be verified without a live database.
type Querier interface {
	Enabled() bool
	HasColumn(db, table, col string) bool
	// ResolveTable maps a flow_metrics family to the granularity table that
	// actually exists ("" = no resolution, keep the requested name).
	ResolveTable(db, family string, windowSec int64, dataSource string) string
	Query(ctx context.Context, query string, args ...interface{}) (driver.Rows, error)
}

var _ Querier = (*CHService)(nil)
