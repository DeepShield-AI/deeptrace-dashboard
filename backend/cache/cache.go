package cache

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
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
		log.Printf("⚠️  No api_cache dir: %v", err)
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
	log.Printf("📦 Loaded %d API cache entries", c.len)
	return c
}

// Find looks up a response by METHOD and full URL path (with query string).
func (c *Cache) Find(method, fullPath string) []byte {
	return c.entries[method+" "+fullPath]
}

// FindWithBody performs body‑aware lookup for POST requests.
// It first tries an exact match, then a structured best‑match on
// DATABASE / TABLE / TAG fields (tolerates key‑ordering differences).
func (c *Cache) FindWithBody(method, urlPath, body string) []byte {
	key := method + " " + urlPath

	bodyMap, ok := c.bodyEntries[key]
	if !ok || body == "" {
		if resp, ok2 := c.entries[key]; ok2 {
			return resp
		}
		return nil
	}

	// 1. Exact match.
	if resp, ok2 := bodyMap[body]; ok2 {
		return resp
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
					return resp
				}
			}
		}
	}

	// 3. Structured match on DATABASE / TABLE / TAG fields.
	// Matches for all endpoints (List, Top, DBDescription, etc.) by finding
	// a cached entry with the same DATABASE and TABLE. More specific matches
	// (also matching TAG) take priority.
	var reqMap map[string]interface{}
	if json.Unmarshal([]byte(body), &reqMap) == nil && len(reqMap) > 0 {
		reqDB := fmt.Sprintf("%v", reqMap["DATABASE"])
		reqTable := fmt.Sprintf("%v", reqMap["TABLE"])
		reqTag := fmt.Sprintf("%v", reqMap["TAG"])

		var bestResp []byte
		bestScore := 0
		bestLen := 0
		for cachedBody, resp := range bodyMap {
			var cMap map[string]interface{}
			if json.Unmarshal([]byte(cachedBody), &cMap) != nil {
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
			if score > bestScore || (score == bestScore && score > 0 && len(resp) > bestLen) {
				bestScore = score
				bestLen = len(resp)
				bestResp = resp
			}
		}
		if bestResp != nil {
			return bestResp
		}
	}

	// Fallback to simple path match — only when no body-keyed entries exist.
	// Without this guard, a body-keyed path (e.g., List) that has no matching
	// body still returns the first cached entry, which is almost certainly wrong.
	if _, hasBodyEntries := c.bodyEntries[key]; !hasBodyEntries {
		if resp, ok2 := c.entries[key]; ok2 {
			return resp
		}
	}
	return nil
}

// Len returns the number of cached entries.
func (c *Cache) Len() int {
	return c.len
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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
