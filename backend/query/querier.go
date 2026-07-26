package query

import (
	"deeptrace-backend/client"
	"deeptrace-backend/clickhouse"
	"deeptrace-backend/enum"
)


// QuerierService is the single entry point for all query-related business logic.
// data sources and applies post-processing transformations.
type QuerierService struct {
	Chain      *DataSourceChain
	CH         *clickhouse.CHService
	Zerotrace  *client.ZerotraceService
	Enum       *enum.EnumService
}
