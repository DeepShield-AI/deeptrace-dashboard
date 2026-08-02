package transport

import (
	"context"
	"errors"
	"net/http"

	"deeptrace-backend/logging"
	"deeptrace-backend/query"
)

func handleTop(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parsed, _, err := parseBody[query.QuerierListRequest](r)
		if err != nil {
			if errors.Is(err, ErrBodyRead) {
				writeError(w, "cannot read body")
				return
			}
			logging.Errorf("Top unmarshal error: %v", err)
			writeResult(w, &query.Result{Data: []map[string]interface{}{}})
			return
		}
		req := *parsed
		req.NormalizeQuery()
		result, ok := queryWithProvenance(w, r, func(ctx context.Context) (*query.Result, error) {
			return srv.QueryTop(ctx, &req)
		})
		if !ok {
			return
		}
		writeResult(w, result)
	}
}

// --------------------------------------------------------------------------
// Profile
// --------------------------------------------------------------------------

func handleProfile(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parsed, _, err := parseBody[query.ProfileRequest](r)
		if err != nil {
			writeError(w, decodeErrorMessage(err))
			return
		}
		req := *parsed
		result, ok := queryWithProvenance(w, r, func(ctx context.Context) (*query.ProfileResult, error) {
			return srv.QueryProfile(ctx, &req)
		})
		if !ok {
			return
		}
		// Profile uses its own envelope (result.functions/... — no DATA, no
		// _querier_region), so writeJSON dispatches ProfileResult.Envelope.
		writeJSON(w, result)
	}
}
