package clickhouse

// QueryListResult holds the result of a List query.
type QueryListResult struct {
	Data   []map[string]interface{}
	Fields map[string]interface{} // SCHEMAS
	Count  int                    // total matching records
}

// QueryTopResult holds the result of a Top query.
type QueryTopResult struct {
	Data   []map[string]interface{}
	Fields map[string]interface{}
}

// QueryFlowLogResult holds the result of a FlowLogDetail query.
type QueryFlowLogResult struct {
	Data []map[string]interface{}
}

// QueryTraceMapResult holds the result of a TraceMap query.
type QueryTraceMapResult struct {
	Data             []map[string]interface{}
	TotalTraces      int
	CalculatedTraces int
}

// MetricExpr holds a parsed metric expression.
type MetricExpr struct {
	Key string
	SQL string
}
