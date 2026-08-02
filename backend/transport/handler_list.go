package transport

import (
	"context"
	"errors"
	"net/http"

	"deeptrace-backend/logging"
	"deeptrace-backend/query"
)

func handleList(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parsed, _, err := parseBody[query.QuerierListRequest](r)
		if err != nil {
			writeError(w, decodeErrorMessage(err))
			return
		}
		req := *parsed
		req.NormalizeQuery()
		result, ok := queryWithProvenance(w, r, func(ctx context.Context) (*query.Result, error) {
			return srv.QueryList(ctx, &req)
		})
		if !ok {
			return
		}
		writeResult(w, result)
	}
}

// --------------------------------------------------------------------------
// MultiPromList
// --------------------------------------------------------------------------

func handleMultiPromList(srv *query.QuerierService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type multiPromRequest struct {
			TimeStart int64    `json:"time_start"`
			TimeEnd   int64    `json:"time_end"`
			PromQLs   []string `json:"PROMQLS"`
			Metrics   []string `json:"METRICS"`
			TOP       int      `json:"TOP"`
		}
		if _, _, err := parseBody[multiPromRequest](r); err != nil {
			if errors.Is(err, ErrBodyRead) {
				writeError(w, "cannot read body")
				return
			}
			logging.Errorf("MultiPromList unmarshal error: %v", err)
			writeJSON(w, map[string]interface{}{
				"OPT_STATUS": "SUCCESS", "TYPE": "Multi_Prom_List", "DATA": []interface{}{},
			})
			return
		}
		writeJSON(w, map[string]interface{}{
			"OPT_STATUS": "SUCCESS",
			"TYPE":       "Multi_Prom_List",
			"DATA":       []interface{}{},
		})
	}
}

// --------------------------------------------------------------------------
// Top
// --------------------------------------------------------------------------
