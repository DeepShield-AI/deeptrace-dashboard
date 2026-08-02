package clickhouse

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"deeptrace-backend/logging"
)

// ColumnChecker reports whether a physical column exists in a table.
// BuildSelectSQL uses it to pre-check columns so unsupported signatures fail
// fast (and fall through the chain to cache) instead of hitting ClickHouse
// with "Missing columns" errors.
type ColumnChecker interface {
	HasColumn(db, table, col string) bool
}

// TableResolver is the optional extension of ColumnChecker that resolves a
// flow_metrics family to the granularity table actually present (CHService
// implements it via the schema registry).
type TableResolver interface {
	ResolveTable(db, family string, windowSec int64, dataSource string) string
}

// ResolveFlowMetricsTable resolves a flow_metrics family through check when
// it supports table resolution; returns "" otherwise (keep requested name).
func ResolveFlowMetricsTable(check ColumnChecker, db, family string, windowSec int64, dataSource string) string {
	if r, ok := check.(TableResolver); ok {
		return r.ResolveTable(db, family, windowSec, dataSource)
	}
	return ""
}

// ColumnRegistry caches the physical column set of each table, loaded lazily
// from system.columns on first access per table.
type ColumnRegistry struct {
	ch     *CHService
	mu     sync.Mutex
	cols   map[string]map[string]bool
	tables map[string]string // "db.family" → resolved table name ("" = none)
}

// NewColumnRegistry creates a registry for the given CH service.
func NewColumnRegistry(ch *CHService) *ColumnRegistry {
	return &ColumnRegistry{ch: ch, cols: map[string]map[string]bool{}, tables: map[string]string{}}
}

// ResolveTable returns the real granularity table for a flow_metrics family
// (e.g. "application_map" → "application_map.1h" for a 1-day window when the
// environment retains the .1h/.1d tables). The frontend sends a DATA_SOURCE
// granularity that the environment may not retain — a missing .1h/.1m table
// made every column lookup fail and the whole request fall back to cache.
// The choice honors the requested DATA_SOURCE first (1m/1h/1s — the frontend
// granularity drives the table when it exists), then the query window: fine
// tables (.1s/.1m) have short TTLs, so a wide window must not land on a
// table whose data is already expired. Returns "" when no family table
// exists (callers keep their requested name).
func (r *ColumnRegistry) ResolveTable(db, family string, windowSec int64, dataSource string) string {
	if r.ch == nil || !r.ch.Enabled() {
		return ""
	}
	key := fmt.Sprintf("%s.%s.%d.%s", db, family, windowSec, dataSource)
	r.mu.Lock()
	defer r.mu.Unlock()
	if name, ok := r.tables[key]; ok {
		return name
	}
	name := r.loadFamilyTable(db, family, windowSec, dataSource)
	r.tables[key] = name
	return name
}

// granularityRank orders granularity suffixes from fine to coarse.
func granularityRank(suffix string) int {
	switch suffix {
	case "1s":
		return 0
	case "1m":
		return 1
	case "5m":
		return 2
	case "1h":
		return 3
	case "6h":
		return 4
	case "1d":
		return 5
	case "1w":
		return 6
	}
	return 100 // unknown granularity
}

// loadFamilyTable queries system.tables for the family's granularity tables.
// The requested DATA_SOURCE granularity wins when the table exists; otherwise
// the window picks the closest finer-or-equal granularity:
//
//	window <= 1h  → prefer 1s/1m
//	window <= 6h  → prefer 1m/1h
//	window <= 1d  → prefer 1h
//	window >  1d  → prefer 1d (or 6h)
func (r *ColumnRegistry) loadFamilyTable(db, family string, windowSec int64, dataSource string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ValidateTableName(db); err != nil {
		logging.Warnf("schema registry: invalid db %q", db)
		return ""
	}
	if err := ValidateTableName(family); err != nil {
		logging.Warnf("schema registry: invalid family %q", family)
		return ""
	}
	rows, err := r.ch.Query(ctx,
		"SELECT name FROM system.tables WHERE database = '"+db+"' AND name LIKE '"+family+".%'")
	if err != nil {
		logging.Warnf("schema registry: cannot list tables for %s.%s: %v", db, family, err)
		return ""
	}
	defer rows.Close()
	var candidates []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) != nil {
			continue
		}
		candidates = append(candidates, name)
	}
	if len(candidates) == 0 {
		return ""
	}
	// Requested DATA_SOURCE wins when the exact table exists (the frontend
	// chooses the granularity — 1m for fine detail, 1h for coarser).
	if dataSource != "" && granularityRank(dataSource) <= 100 {
		exact := family + "." + dataSource
		for _, name := range candidates {
			if name == exact {
				logging.Debugf("schema registry: %s.%s uses requested %s", db, family, name)
				return name
			}
		}
	}
	// Target granularity rank for the window.
	target := 5 // 1d
	switch {
	case windowSec <= 3600:
		target = 1 // 1m
	case windowSec <= 6*3600:
		target = 3 // 1h
	case windowSec <= 24*3600:
		target = 3 // 1h
	}
	best := ""
	bestDist := 999
	for _, name := range candidates {
		suffix := strings.TrimPrefix(name, family+".")
		rank := granularityRank(suffix)
		if rank > target {
			continue // too coarse for this window
		}
		dist := target - rank
		if dist < bestDist {
			bestDist = dist
			best = name
		}
	}
	if best == "" {
		// No fine-enough table: fall back to the coarsest candidate.
		for _, name := range candidates {
			if best == "" || granularityRank(strings.TrimPrefix(name, family+".")) > granularityRank(strings.TrimPrefix(best, family+".")) {
				best = name
			}
		}
	}
	if best != "" {
		logging.Debugf("schema registry: %s.%s (window %ds) resolved to %s", db, family, windowSec, best)
	}
	return best
}

// HasColumn reports whether the table has the physical column.
// If the table schema cannot be loaded (CH down / table gone) it returns
// true so queries are attempted rather than silently dropped.
func (r *ColumnRegistry) HasColumn(db, table, col string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := db + "." + table
	set, ok := r.cols[key]
	if !ok {
		set = r.load(db, table)
		// deepflow granularity tables (application_map.1h) are Distributed;
		// some environments expose their column metadata under the plain
		// family name (application_map) instead. Fall back before giving up,
		// so a metadata quirk doesn't drop the request to cache.
		if set == nil || !set[col] {
			if plain := strings.TrimSuffix(table, "."+granularitySuffix(table)); plain != table {
				if plainSet, ok2 := r.cols[db+"."+plain]; ok2 {
					if plainSet[col] {
						r.cols[key] = plainSet
						return true
					}
				} else if plainSet = r.load(db, plain); plainSet != nil {
					r.cols[db+"."+plain] = plainSet
					if plainSet[col] {
						r.cols[key] = plainSet
						return true
					}
				}
			}
		}
		r.cols[key] = set
	}
	if set == nil {
		return true
	}
	return set[col]
}

// granularitySuffix returns the ".1m"/".1h"/".1d" style suffix of a table
// name, or "" when the name has no such suffix.
func granularitySuffix(table string) string {
	i := strings.LastIndex(table, ".")
	if i < 0 || i == len(table)-1 {
		return ""
	}
	suffix := table[i+1:]
	if len(suffix) >= 2 && suffix[0] == '1' && (suffix[1] == 's' || suffix[1] == 'm' || suffix[1] == 'h' || suffix[1] == 'd' || suffix[1] == 'w') {
		return suffix
	}
	return ""
}

func (r *ColumnRegistry) load(db, table string) map[string]bool {
	if r.ch == nil || !r.ch.Enabled() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ValidateTableName(db); err != nil {
		logging.Warnf("schema registry: invalid db %q", db)
		return nil
	}
	if err := ValidateTableName(table); err != nil {
		logging.Warnf("schema registry: invalid table %q", table)
		return nil
	}
	rows, err := r.ch.Query(ctx,
		"SELECT name FROM system.columns WHERE database = '"+db+"' AND table = '"+table+"'")
	if err != nil {
		logging.Warnf("schema registry: cannot load columns for %s.%s: %v", db, table, err)
		return nil
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		set[name] = true
	}
	if len(set) == 0 {
		logging.Warnf("schema registry: no columns found for %s.%s", db, table)
		return nil
	}
	return set
}
