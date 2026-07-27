package query

import (
	"log"
	"fmt"
	"sort"
	"strings"
	"time"
)

// buildHistory converts flat rows with time column into HISTORY arrays.
func buildHistory(data []map[string]interface{}, sel string, metrics []string,
	timeStart, timeEnd, interval int64, fill string) []map[string]interface{} {
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
			ti, _ := histToFloat64(g[i]["time"])
			tj, _ := histToFloat64(g[j]["time"])
			return ti > tj
		})
		var h []map[string]interface{}
		seenTOI := map[int64]bool{}
		for _, row := range g {
			tv := row["time"]
			if s, ok := tv.(string); ok {
				if t, err := histParseTime(s); err == nil {
					tv = t.Unix()
				}
			}
			if toi, ok := histToFloat64(tv); ok && interval > 0 {
				tv = float64(int64(toi) / interval * interval)
			}
			toiKey := int64(histToFloat64OrZero(tv))
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
		if fill != "" && interval > 0 && timeEnd > timeStart {
			h = histFill(h, mk, timeStart, timeEnd, interval)
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
	for _, item := range histParseSelectList(sel) {
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

type histSelectItem struct {
	Expr string
	Key  string
}

func histParseSelectList(sel string) []histSelectItem {
	var items []histSelectItem
	if sel == "" {
		return items
	}
	depth := 0
	start := 0
	for i, c := range sel + "," {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(sel[start:i])
				if part != "" {
					item := histSelectItem{Expr: part}
					if idx := strings.LastIndex(strings.ToUpper(part), " AS "); idx >= 0 {
						item.Key = strings.TrimSpace(part[idx+4:])
					} else {
						item.Key = part
					}
					item.Key = strings.Trim(item.Key, "`")
					items = append(items, item)
				}
				start = i + 1
			}
		}
	}
	return items
}

func histToFloat64(v interface{}) (float64, bool) {
	log.Printf("histToFloat64: type=%T val=%v", v, v)
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case uint32:
		return float64(n), true
	default:
		return 0, false
	}
}

func histToFloat64OrZero(v interface{}) float64 {
	if f, ok := histToFloat64(v); ok {
		return f
	}
	return 0
}

func histParseTime(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time: %s", s)
}

func histFill(h []map[string]interface{}, mk map[string]bool, timeStart, timeEnd, interval int64) []map[string]interface{} {
	existing := map[int64]bool{}
	for _, pt := range h {
		if toi, ok := histToFloat64(pt["toi"]); ok {
			existing[int64(toi)] = true
		}
	}
	for t := timeEnd / interval * interval; t > timeStart/interval*interval; t -= interval {
		if existing[t] {
			continue
		}
		pt := map[string]interface{}{"toi": float64(t)}
		for mk2 := range mk {
			pt[mk2] = nil
		}
		h = append(h, pt)
	}
	sort.Slice(h, func(i, j int) bool {
		ti, _ := histToFloat64(h[i]["toi"])
		tj, _ := histToFloat64(h[j]["toi"])
		return ti > tj
	})
	return h
}
