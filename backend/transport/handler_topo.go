package transport

import (
	"context"
	"errors"
	"net/http"

	"deeptrace-backend/logging"
	"deeptrace-backend/query"
	"deeptrace-backend/query/topo"
)

func handleTopo(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parsed, _, err := parseBody[query.QuerierListRequest](r)
		if err != nil {
			if errors.Is(err, ErrBodyRead) {
				writeError(w, "cannot read body")
				return
			}
			logging.Errorf("Topo unmarshal error: %v", err)
			writeSuccess(w, topo.EmptyTopoResult())
			return
		}
		req := *parsed
		// flow_metrics topo: query ClickHouse directly.
		if req.Database == "flow_metrics" {
			policy := query.SourcePolicyFromContext(r.Context())
			// flow_metrics topo is only served by ClickHouse — honor the
			// verification protocol (forced source + no-fallback, M4/M5).
			if policy.ForcedSource != "" && policy.ForcedSource != "clickhouse" {
				writeSourceError(w, r, "flow_metrics topo is only served by clickhouse")
				return
			}
			if policy.NoFallback && (srv.CH == nil || !srv.CH.Enabled()) {
				writeSourceError(w, r, "clickhouse not available")
				return
			}
			w.Header().Set(sourceHeader, "clickhouse")
			writeSuccess(w, topo.QueryTopoFlowMetrics(srv.CH, req))
			return
		}
		// flow_log topo: inject auto_service_type columns, query, process result.
		topo.InjectAutoServiceTypeColumns(&req)
		result, ok := queryWithProvenance(w, r, func(ctx context.Context) (*query.Result, error) {
			return srv.QueryTop(ctx, &req)
		})
		if !ok {
			return
		}
		if result == nil || len(result.Data) == 0 {
			writeSuccess(w, topo.EmptyTopoResult())
			return
		}
		writeSuccess(w, topo.BuildFlowLogTopoResponse(result))
	}
}

// handleUniversalHistory handles UniversalHistory (and UniversalPromHistory etc.) endpoints.
func handleUniversalHistory(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parsed, _, err := parseBody[query.QuerierListRequest](r)
		if err != nil {
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		req := *parsed
		result, ok := queryWithProvenance(w, r, func(ctx context.Context) (*query.Result, error) {
			return srv.QueryTop(ctx, &req)
		})
		if !ok {
			return
		}
		writeResult(w, result)
	}
}

func handleUnsupportedTags(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, []interface{}{})
}
