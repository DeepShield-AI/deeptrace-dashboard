package cache

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"deeptrace-backend/logging"
)

// Cache holds pre-loaded API responses from the api_cache/ directory.
// It supports both simple path‑based lookups and body‑aware matching for POST requests.
type Cache struct {
	entries     map[string][]byte            // "METHOD /path?query" → response body
	bodyEntries map[string]map[string][]byte // "METHOD /path?query" → {body → response}
	len         int
}

// apiCacheEntry is the on‑disk format of each cache file.
type apiCacheEntry struct {
	Method       string `json:"method"`
	Path         string `json:"path"`
	ResponseBody string `json:"responseBody"`
	IsBase64     bool   `json:"responseIsBase64"`
}

// New loads cache files from dir and returns a populated Cache.
func New(dir string) *Cache {
	c := &Cache{
		entries:     map[string][]byte{},
		bodyEntries: map[string]map[string][]byte{},
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		logging.Warnf("No api_cache dir: %v", err)
		return c
	}
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".json") || f.Name() == "_index.json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			continue
		}
		var entry apiCacheEntry
		if json.Unmarshal(data, &entry) != nil {
			continue
		}
		var respBody []byte
		if entry.IsBase64 {
			respBody, _ = base64.StdEncoding.DecodeString(entry.ResponseBody)
		} else {
			respBody = []byte(entry.ResponseBody)
		}
		if len(respBody) == 0 {
			continue
		}
		// Preserve full path WITH query string for GET cache key
		// POST keys also include query, but use body for disambiguation
		key := entry.Method + " " + entry.Path

		// For POST requests with body, store with body-awareness.
		reqBody := extractReqBody(data)
		if reqBody != "" && reqBody != "{}" {
			if c.bodyEntries[key] == nil {
				c.bodyEntries[key] = map[string][]byte{}
			}
			c.bodyEntries[key][reqBody] = respBody
		}
		if _, exists := c.entries[key]; !exists {
			c.entries[key] = respBody
			c.len++
		}
	}
	logging.Infof("Loaded %d API cache entries", c.len)
	return c
}

// Find looks up a response by METHOD and full URL path (with query string).
func (c *Cache) Find(method, fullPath string) []byte {
	key := method + " " + fullPath
	if resp, ok := c.entries[key]; ok {
		return hit(key, resp)
	}
	return nil
}

// FindWithBody performs body‑aware lookup for POST requests.
// It first tries an exact match, then a structured best‑match on
// DATABASE / TABLE / TAG fields (tolerates key‑ordering differences).
func (c *Cache) FindWithBody(method, urlPath, body string) []byte {
	key := method + " " + urlPath

	bodyMap, ok := c.bodyEntries[key]
	if !ok || body == "" {
		if resp, ok2 := c.entries[key]; ok2 {
			return hit(key, resp)
		}
		return nil
	}

	// 1. Exact match.
	if resp, ok2 := bodyMap[body]; ok2 {
		return hit(key, resp)
	}

	// 2. Normalized JSON comparison (ignores key ordering).
	var reqMapNormalized map[string]interface{}
	if json.Unmarshal([]byte(body), &reqMapNormalized) == nil && len(reqMapNormalized) > 0 {
		normReq, _ := json.Marshal(reqMapNormalized)
		for cachedBody, resp := range bodyMap {
			var cachedMap map[string]interface{}
			if json.Unmarshal([]byte(cachedBody), &cachedMap) == nil {
				normCached, _ := json.Marshal(cachedMap)
				if string(normReq) == string(normCached) {
					return hit(key, resp)
				}
			}
		}
	}

	// 3. Structured match on DATABASE / TABLE / TAG fields.
	// A candidate is only accepted when TABLE matches (score >= 2) — a
	// DATABASE-only match could return another table's data (e.g. a cached
	// l7_flow_log response for an l4_flow_log request). More specific
	// matches (also matching TAG) take priority.
	//
	// Additionally the candidate's QUERIES signature (SELECT + WHERE of every
	// sub-query) must match the request's, and the time windows must overlap.
	// Otherwise a request with a different aggregation or a different time
	// range would get stale data from an unrelated cached response.
	var reqMap map[string]interface{}
	if json.Unmarshal([]byte(body), &reqMap) == nil && len(reqMap) > 0 {
		reqDB := fmt.Sprintf("%v", reqMap["DATABASE"])
		reqTable := fmt.Sprintf("%v", reqMap["TABLE"])
		reqTag := fmt.Sprintf("%v", reqMap["TAG"])
		// The frontend sends lower-case time_start/time_end; older clients and
		// some cache bodies use upper-case TIME_START/TIME_END. Accept both.
		reqStart := windowValue(reqMap, "TIME_START", "time_start")
		reqEnd := windowValue(reqMap, "TIME_END", "time_end")

		var bestResp []byte
		bestScore := 0
		bestLen := 0
		for cachedBody, resp := range bodyMap {
			var cMap map[string]interface{}
			if json.Unmarshal([]byte(cachedBody), &cMap) != nil {
				continue
			}
			if !queriesCompatible(reqMap, cMap) {
				continue
			}
			if !windowsOverlap(reqStart, reqEnd, cMap) {
				continue
			}
			score := 0
			if fmt.Sprintf("%v", cMap["DATABASE"]) == reqDB && reqDB != "<nil>" {
				score++
			}
			if fmt.Sprintf("%v", cMap["TABLE"]) == reqTable && reqTable != "<nil>" {
				score += 2
			}
			if fmt.Sprintf("%v", cMap["TAG"]) == reqTag && reqTag != "<nil>" {
				score += 4
			}
			if score >= 2 && (score > bestScore || (score == bestScore && len(resp) > bestLen)) {
				bestScore = score
				bestLen = len(resp)
				bestResp = resp
			}
		}
		if bestResp != nil {
			return hit(key, bestResp)
		}
	}

	// Fallback to simple path match — only when no body-keyed entries exist.
	// Without this guard, a body-keyed path (e.g., List) that has no matching
	// body still returns the first cached entry, which is almost certainly wrong.
	if _, hasBodyEntries := c.bodyEntries[key]; !hasBodyEntries {
		if resp, ok2 := c.entries[key]; ok2 {
			return hit(key, resp)
		}
	}
	return nil
}

// hit logs a cache hit at warn level (yellow) and returns the response.
func hit(key string, resp []byte) []byte {
	logging.Warnf("api cache hit: %s", key)
	return resp
}

// Len returns the number of cached entries.
func (c *Cache) Len() int {
	return c.len
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// numValue coerces a JSON number to int64 (0 when absent/unparseable).
func numValue(v interface{}) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i
		}
	case string:
		var i int64
		if _, err := fmt.Sscanf(n, "%d", &i); err == nil {
			return i
		}
	}
	return 0
}

// queriesCompatible reports whether the request and cached body select/filter
// the same data: every QUERIES entry must have an identical SELECT + WHERE
// (the aggregation and filter shape). When the request carries no QUERIES we
// cannot compare, so the candidate passes (legacy bodies).
func queriesCompatible(reqMap, cMap map[string]interface{}) bool {
	reqQueries, reqOK := reqMap["QUERIES"].([]interface{})
	cacheQueries, cacheOK := cMap["QUERIES"].([]interface{})
	if !reqOK || len(reqQueries) == 0 {
		return true
	}
	if !cacheOK || len(cacheQueries) != len(reqQueries) {
		return false
	}
	for i := range reqQueries {
		rq, rk := reqQueries[i].(map[string]interface{})
		cq, ck := cacheQueries[i].(map[string]interface{})
		if !rk || !ck {
			return false
		}
		rSel, _ := rq["SELECT"].(string)
		rWh, _ := rq["WHERE"].(string)
		rHav, _ := rq["HAVING"].(string)
		cSel, _ := cq["SELECT"].(string)
		cWh, _ := cq["WHERE"].(string)
		cHav, _ := cq["HAVING"].(string)
		if strings.TrimSpace(rSel) != strings.TrimSpace(cSel) ||
			strings.TrimSpace(rWh) != strings.TrimSpace(cWh) ||
			strings.TrimSpace(rHav) != strings.TrimSpace(cHav) {
			return false
		}
	}
	return true
}

// windowValue reads a time key that may appear upper- or lower-cased in the
// request/cached body (frontend: time_start; legacy clients: TIME_START).
func windowValue(m map[string]interface{}, upper, lower string) int64 {
	if v := numValue(m[upper]); v > 0 {
		return v
	}
	return numValue(m[lower])
}

// windowsOverlap reports whether the request's time window intersects the
// cached request's window. Both sides must have a full window; otherwise we
// have no information and pass.
func windowsOverlap(reqStart, reqEnd int64, cMap map[string]interface{}) bool {
	cStart := windowValue(cMap, "TIME_START", "time_start")
	cEnd := windowValue(cMap, "TIME_END", "time_end")
	if reqStart <= 0 || reqEnd <= 0 || cStart <= 0 || cEnd <= 0 {
		return true
	}
	return reqStart <= cEnd && cStart <= reqEnd
}

// extractReqBody reads requestBody from the raw cache file JSON.
func extractReqBody(raw []byte) string {
	var full map[string]interface{}
	if json.Unmarshal(raw, &full) != nil {
		return ""
	}
	if rb, ok := full["requestBody"]; ok {
		if s, ok2 := rb.(string); ok2 {
			return s
		}
	}
	return ""
}
