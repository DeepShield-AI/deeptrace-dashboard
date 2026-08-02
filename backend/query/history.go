package query

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"deeptrace-backend/clickhouse"
)

// BuildHistory converts flat rows with time column into HISTORY arrays,
// filling missing time buckets with null when fill param is set.
// Shared by the Top query paths (query root chain and flowlog).
func BuildHistory(data []map[string]interface{}, sel string, metrics []string,
	timeStart, timeEnd, interval int64, fill string,
) []map[string]interface{} {
	if len(data) == 0 {
		return data
	}
	mk := historyParseMetrics(sel, metrics)
	hasTime := false
	for k := range data[0] {
		if k == "time" {
			hasTime = true
			break
		}
	}
	if !hasTime {
		return data
	}
	type gk string
	gs := make(map[gk][]map[string]interface{})
	for _, row := range data {
		var p []string
		for k := range row {
			if k == "time" || k == "_querier_region" {
				continue
			}
			if mk[k] {
				continue
			}
			p = append(p, fmt.Sprintf("%v=%v", k, row[k]))
		}
		sort.Strings(p)
		key := gk(strings.Join(p, "|"))
		gs[key] = append(gs[key], row)
	}
	if len(gs) == len(data) {
		return data
	}
	var r []map[string]interface{}
	for _, g := range gs {
		if len(g) == 0 {
			continue
		}
		b := make(map[string]interface{})
		for k, v := range g[0] {
			if k != "time" {
				b[k] = v
			}
		}
		sort.Slice(g, func(i, j int) bool {
			ti, _ := historyToFloat64(g[i]["time"])
			tj, _ := historyToFloat64(g[j]["time"])
			return ti > tj
		})
		var h []map[string]interface{}
		seenTOI := map[int64]bool{}
		for _, row := range g {
			tv := row["time"]
			if s, ok := tv.(string); ok {
				if t, err := historyParseTime(s); err == nil {
					tv = t.Unix()
				}
			}
			if toi, ok := historyToFloat64(tv); ok && interval > 0 {
				tv = float64(int64(toi) / interval * interval)
			}
			toiKey := int64(historyToFloat64OrZero(tv))
			if seenTOI[toiKey] {
				continue
			}
			seenTOI[toiKey] = true
			pt := map[string]interface{}{"toi": tv}
			for mk2 := range mk {
				if v, ok := row[mk2]; ok {
					pt[mk2] = v
				}
			}
			h = append(h, pt)
		}
		// Fill missing time buckets with null (capped at 30 points).
		// fill="none" means missing buckets are NOT synthesized — consistent
		// with FillNullHistory's three-state semantics (0 / null / none).
		if fill != "" && fill != "none" && interval > 0 && timeEnd > timeStart {
			h = historyFill(h, mk, timeStart, timeEnd, interval)
		}
		b["HISTORY"] = h
		r = append(r, b)
	}
	return r
}

func historyParseMetrics(sel string, metrics []string) map[string]bool {
	keys := map[string]bool{}
	for _, m := range metrics {
		idx := strings.LastIndex(m, " AS ")
		if idx >= 0 {
			a := strings.TrimSpace(m[idx+4:])
			keys[strings.Trim(a, "`")] = true
		} else {
			keys[strings.Trim(m, "`")] = true
		}
	}
	for _, item := range clickhouse.ParseSelectList(sel) {
		lower := strings.ToLower(item.Expr)
		if strings.HasPrefix(lower, "newtag(") ||
			strings.HasPrefix(lower, "enum(") ||
			strings.HasPrefix(lower, "node_type(") ||
			strings.HasPrefix(lower, "icon_id(") {
			continue
		}
		if strings.Contains(item.Expr, "(") {
			keys[item.Key] = true
		}
	}
	return keys
}

func historyToFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f, true
		}
	}
	return 0, false
}

func historyToFloat64OrZero(v interface{}) float64 {
	if f, ok := historyToFloat64(v); ok {
		return f
	}
	return 0
}

func historyParseTime(s string) (time.Time, error) {
	formats := []string{"2006-01-02T15:04:05-07:00", "2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05", "2006-01-02T15:04:05", time.RFC3339}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time: %s", s)
}

// historyFill fills missing time buckets with null, capped at 30 points
// (from timeEnd down to timeStart, truncating the start when the range exceeds 30 buckets).
func historyFill(h []map[string]interface{}, mk map[string]bool, timeStart, timeEnd, interval int64) []map[string]interface{} {
	if interval <= 0 {
		interval = 1
	}
	existing := map[int64]bool{}
	for _, pt := range h {
		if toi, ok := historyToFloat64(pt["toi"]); ok {
			existing[int64(toi)] = true
		}
	}
	fillEnd := timeEnd - timeEnd%interval
	fillStart := timeStart - timeStart%interval
	maxPts := int64(30) * interval
	if fillEnd-fillStart > maxPts {
		fillStart = fillEnd - maxPts
	}
	for t := fillEnd; t >= fillStart; t -= interval {
		if existing[t] {
			continue
		}
		pt := map[string]interface{}{"toi": t}
		for mk2 := range mk {
			pt[mk2] = nil
		}
		h = append(h, pt)
	}
	sort.Slice(h, func(i, j int) bool {
		ti, _ := historyToFloat64(h[i]["toi"])
		tj, _ := historyToFloat64(h[j]["toi"])
		return ti > tj
	})
	return h
}
