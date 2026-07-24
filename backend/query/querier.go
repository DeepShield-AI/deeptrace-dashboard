package query

import (
	"deeptrace-backend/client"
	"deeptrace-backend/clickhouse"
	"deeptrace-backend/enum"
)

// Aggregator is the interface for reading data files from the data directory.
// Implemented by *aggregator.Aggregator.
type Aggregator interface {
	ReadDataFileJSON(name string) (interface{}, error)
}

// QuerierService is the single entry point for all query-related business logic.
// It orchestrates the DataSourceChain, ClickHouse, Zerotrace, and Aggregator
// data sources and applies post-processing transformations.
type QuerierService struct {
	Chain      *DataSourceChain
	CH         *clickhouse.CHService
	Zerotrace  *client.ZerotraceService
	Aggregator Aggregator
	Enum       *enum.EnumService
}
