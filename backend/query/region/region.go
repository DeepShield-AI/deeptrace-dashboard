// Package region provides the region registry for querier responses.
//
// Regions are queried from deepflow-server's query API (flow_tag.region_map
// dictionary), avoiding a direct MySQL connection. On success the default
// region name also updates clickhouse.QuerierRegion so every query result
// row carries the real region name instead of the hardcoded default.
package region

import (
	"fmt"
	"sync"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/client"
	"deeptrace-backend/logging"
)

// Region is one entry of the region registry.
type Region struct {
	ID     int64
	Name   string
	IconID int64
}

// Service holds the region list loaded from deepflow-server.
// Region data is static in practice (created by cloud sync or admin),
// so it is loaded once at startup.
type Service struct {
	zt *client.ZerotraceService

	mu      sync.RWMutex
	regions []Region
	loaded  bool
}

// New creates a RegionService backed by the deepflow-server query API.
func New(zt *client.ZerotraceService) *Service {
	return &Service{zt: zt}
}

// Load queries the region dictionary via deepflow-server and, on success,
// sets clickhouse.QuerierRegion to the default region name.
// A failed load leaves the hardcoded fallback in place (no error to caller).
func (s *Service) Load() {
	if s.zt == nil {
		return
	}
	rows, err := s.zt.QueryRaw("flow_tag", "SELECT id, name, icon_id FROM region_map")
	if err != nil {
		logging.Warnf("region load failed (keeping fallback): %v", err)
		return
	}
	var regions []Region
	for _, row := range rows.Values {
		if len(row) < 2 {
			continue
		}
		regions = append(regions, Region{
			ID:   clickhouse.ToInt64(row[0]),
			Name: fmt.Sprintf("%v", row[1]),
		})
		if len(row) > 2 {
			regions[len(regions)-1].IconID = clickhouse.ToInt64(row[2])
		}
	}
	if len(regions) == 0 {
		logging.Warnf("region_map returned no rows (keeping fallback)")
		return
	}

	s.mu.Lock()
	s.regions = regions
	s.loaded = true
	s.mu.Unlock()

	// Update the global querier region so all result rows carry the real name.
	clickhouse.QuerierRegion = s.DefaultName()
	logging.Infof("Loaded %d regions from deepflow-server; default=%q", len(regions), clickhouse.QuerierRegion)
}

// Regions returns the region list in the cloud /v2/regions response format,
// or nil when the load failed (caller falls back to hardcoded data).
func (s *Service) Regions() []map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.loaded {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(s.regions))
	for _, r := range s.regions {
		result = append(result, r.toResponseMap())
	}
	return result
}

// DefaultName returns the name of the default region (id=1 "系统默认" when
// present, otherwise the first entry). Empty when not loaded.
func (s *Service) DefaultName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.regions {
		if r.ID == 1 {
			return r.Name
		}
	}
	if len(s.regions) > 0 {
		return s.regions[0].Name
	}
	return ""
}

// FallbackRegions returns the hardcoded region list used when the region
// service could not be loaded from deepflow-server.
func FallbackRegions() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"ID": 1, "NAME": "本地", "LCUUID": "region-local", "SHORT_LCUUID": "local",
			"AZ_COUNT": 1, "EPC_COUNT": 0, "STATUS": 1, "CREATE_METHOD": 0,
			"CREATED_AT": "2024-01-01 00:00:00", "UPDATED_AT": "",
			"DOMAIN_LCUUIDS": []interface{}{}, "DELETED_AT": nil,
			"LATITUDE": nil, "ICON_ID": -4, "LABEL": "",
			"label": "本地", "value": "本地",
		},
	}
}

// toResponseMap converts a Region into the cloud region API response shape
// (all stats fields zeroed — they are computed by deepflow-server's resource
// API which the local zerotrace-server does not expose).
func (r Region) toResponseMap() map[string]interface{} {
	lcuuid := regionLCUUID(r.ID)
	return map[string]interface{}{
		"AZ_COUNT":       0,
		"CREATED_AT":     "",
		"CREATE_METHOD":  0,
		"DELETED_AT":     nil,
		"DOMAIN_LCUUIDS": []interface{}{},
		"EPC_COUNT":      0,
		"ICON_ID":        r.IconID,
		"ID":             r.ID,
		"LABEL":          "",
		"LATITUDE":       nil,
		"LCUUID":         lcuuid,
		"LONGITUDE":      nil,
		"NAME":           r.Name,
		"POD_COUNT":      0,
		"POD_NODE_COUNT": 0,
		// Compatibility fields the frontend expects in this deployment.
		"SHORT_LCUUID": "local",
		"STATUS":       1,
		"SUBNET_COUNT": 0,
		"UPDATED_AT":   "",
		"VM_COUNT":     0,
		"label":        r.Name,
		"value":        r.Name,
	}
}

// regionLCUUID maps a region ID to the LCUUID used by the cloud API.
// id=1 is the built-in default region (ffffffff-...); others get a
// deterministic UUID derived from the ID.
func regionLCUUID(id int64) string {
	if id == 1 {
		return "ffffffff-ffff-ffff-ffff-ffffffffffff"
	}
	h := uint64(id) * 0x9e3779b97f4a7c15
	h = h ^ (h>>30)*0xbf58476d1ce4e5b9
	h = h ^ (h>>27)*0x94d049bb133111eb
	h = h ^ (h >> 31)
	return fmt.Sprintf("%08x-%04x-4%03x-%04x-%012x",
		h&0xFFFFFFFF, (h>>32)&0xFFFF,
		(h>>48)&0x0FFF,
		0x8000|((h>>40)&0x3FFF),
		(h>>16)&0xFFFFFFFFFFFF,
	)
}
