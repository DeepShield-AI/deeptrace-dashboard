package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"deeptrace-backend/clickhouse"
)

type svcKey struct{ id, typ string }

// RegisterDimensionResources adds the dimension-resources endpoint.
func RegisterDimensionResources(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("/api/statistics/v1/dimension-resources", handleDimensionResources(deps))
}

// dimReq mirrors the dimension-resources request body.
type dimReq struct {
	Database  string `json:"DATABASE"`
	Table     string `json:"TABLE"`
	TimeStart int64  `json:"time_start"`
	TimeEnd   int64  `json:"time_end"`
	Region    string `json:"REGION"`
	Queries   []struct {
		Where  string `json:"WHERE"`
		Select string `json:"SELECT"`
		TOP    int    `json:"TOP"`
	} `json:"QUERIES"`
}

// resourceEntry holds a resource ID + NAME pair.
type resourceEntry struct {
	ID   interface{} `json:"ID"`
	Name string      `json:"NAME"`
}

