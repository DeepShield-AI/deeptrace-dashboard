package cache

import (
	"testing"
)

func newTestCache(entries map[string][]byte) *Cache {
	return &Cache{
		entries:     map[string][]byte{},
		bodyEntries: map[string]map[string][]byte{"POST /x": entries},
	}
}

const cachedBody = `{"DATABASE":"flow_log","TABLE":"l7_flow_log","TIME_START":100,"TIME_END":200,` +
	`"QUERIES":[{"SELECT":"count(*)","WHERE":""}]}`

func TestFindWithBodyExactMatch(t *testing.T) {
	c := newTestCache(map[string][]byte{cachedBody: []byte(`{"resp":1}`)})

	resp := c.FindWithBody("POST", "/x", cachedBody)
	if resp == nil {
		t.Fatal("exact body match should hit")
	}
}

func TestFindWithBodyDifferentQueriesDoesNotMatch(t *testing.T) {
	c := newTestCache(map[string][]byte{cachedBody: []byte(`{"resp":1}`)})

	// Same DB/TABLE, different aggregation (SELECT) → must NOT hit.
	other := `{"DATABASE":"flow_log","TABLE":"l7_flow_log","TIME_START":100,"TIME_END":200,` +
		`"QUERIES":[{"SELECT":"max(time)","WHERE":""}]}`
	if resp := c.FindWithBody("POST", "/x", other); resp != nil {
		t.Fatal("different SELECT must not fuzzy-match")
	}

	// Same aggregation, different filter (WHERE) → must NOT hit.
	filtered := `{"DATABASE":"flow_log","TABLE":"l7_flow_log","TIME_START":100,"TIME_END":200,` +
		`"QUERIES":[{"SELECT":"count(*)","WHERE":"l7_protocol = 1"}]}`
	if resp := c.FindWithBody("POST", "/x", filtered); resp != nil {
		t.Fatal("different WHERE must not fuzzy-match")
	}
}

func TestFindWithBodyDisjointTimeWindowDoesNotMatch(t *testing.T) {
	c := newTestCache(map[string][]byte{cachedBody: []byte(`{"resp":1}`)})

	// Window [5000, 6000] does not overlap cached [100, 200].
	disjoint := `{"DATABASE":"flow_log","TABLE":"l7_flow_log","TIME_START":5000,"TIME_END":6000,` +
		`"QUERIES":[{"SELECT":"count(*)","WHERE":""}]}`
	if resp := c.FindWithBody("POST", "/x", disjoint); resp != nil {
		t.Fatal("disjoint time window must not fuzzy-match")
	}

	// Overlapping window [150, 250] → allowed.
	overlap := `{"DATABASE":"flow_log","TABLE":"l7_flow_log","TIME_START":150,"TIME_END":250,` +
		`"QUERIES":[{"SELECT":"count(*)","WHERE":""}]}`
	if resp := c.FindWithBody("POST", "/x", overlap); resp == nil {
		t.Fatal("overlapping time window should fuzzy-match")
	}
}

func TestFindWithBodyMissingTimeWindowsPasses(t *testing.T) {
	c := newTestCache(map[string][]byte{cachedBody: []byte(`{"resp":1}`)})

	// Request without TIME_START/TIME_END — no window info, keep legacy behavior.
	noWindow := `{"DATABASE":"flow_log","TABLE":"l7_flow_log","QUERIES":[{"SELECT":"count(*)","WHERE":""}]}`
	if resp := c.FindWithBody("POST", "/x", noWindow); resp == nil {
		t.Fatal("missing request window should pass the overlap check")
	}
}

// Regression: the frontend sends lower-case time_start/time_end — the window
// check must honor them, or a disjoint window would fuzzy-match the cached
// response and return stale data (the Profile-page List "fake data" bug).
func TestFindWithBodyLowercaseTimeWindowDoesNotMatchDisjoint(t *testing.T) {
	c := newTestCache(map[string][]byte{cachedBody: []byte(`{"resp":1}`)})

	// Request window [5000, 6000] does not overlap cached [100, 200].
	disjoint := `{"DATABASE":"flow_log","TABLE":"l7_flow_log","time_start":5000,"time_end":6000,` +
		`"QUERIES":[{"SELECT":"count(*)","WHERE":""}]}`
	if resp := c.FindWithBody("POST", "/x", disjoint); resp != nil {
		t.Fatal("lower-case disjoint time window must not fuzzy-match")
	}

	// Overlapping window [150, 250] → allowed.
	overlap := `{"DATABASE":"flow_log","TABLE":"l7_flow_log","time_start":150,"time_end":250,` +
		`"QUERIES":[{"SELECT":"count(*)","WHERE":""}]}`
	if resp := c.FindWithBody("POST", "/x", overlap); resp == nil {
		t.Fatal("lower-case overlapping time window should fuzzy-match")
	}
}

// Cached bodies that only carry lower-case time_start/time_end must also
// participate in the overlap check (they are part of the same bug class).
func TestFindWithBodyLowercaseCachedWindowDoesNotMatchDisjoint(t *testing.T) {
	lowercaseCached := `{"DATABASE":"flow_log","TABLE":"l7_flow_log","time_start":100,"time_end":200,` +
		`"QUERIES":[{"SELECT":"count(*)","WHERE":""}]}`
	c := newTestCache(map[string][]byte{lowercaseCached: []byte(`{"resp":1}`)})

	disjoint := `{"DATABASE":"flow_log","TABLE":"l7_flow_log","TIME_START":5000,"TIME_END":6000,` +
		`"QUERIES":[{"SELECT":"count(*)","WHERE":""}]}`
	if resp := c.FindWithBody("POST", "/x", disjoint); resp != nil {
		t.Fatal("lower-case cached window must not match a disjoint request window")
	}
}

func TestQueriesCompatibleHandlesNoQueriesSide(t *testing.T) {
	// Request with QUERIES vs cache without QUERIES → incompatible.
	req := map[string]interface{}{
		"QUERIES": []interface{}{map[string]interface{}{"SELECT": "count(*)", "WHERE": ""}},
	}
	if queriesCompatible(req, map[string]interface{}{}) {
		t.Fatal("request with QUERIES must not match cache without QUERIES")
	}
	// Request without QUERIES → legacy tolerance.
	if !queriesCompatible(map[string]interface{}{}, req) {
		t.Fatal("request without QUERIES should pass (cannot compare)")
	}
}

func TestFindWithBodyDifferentHavingDoesNotMatch(t *testing.T) {
	// Regression: a request with a HAVING filter must not fuzzy-match a
	// cached response of the same SELECT/WHERE but no HAVING.
	base := `{"DATABASE":"flow_log","TABLE":"l7_flow_log","TIME_START":100,"TIME_END":200,` +
		`"QUERIES":[{"SELECT":"count(*)","WHERE":""}]}`
	c := newTestCache(map[string][]byte{base: []byte(`{"resp":1}`)})

	withHaving := `{"DATABASE":"flow_log","TABLE":"l7_flow_log","TIME_START":100,"TIME_END":200,` +
		`"QUERIES":[{"SELECT":"count(*)","WHERE":"","HAVING":"count(*) > 10"}]}`
	if resp := c.FindWithBody("POST", "/x", withHaving); resp != nil {
		t.Fatal("request with HAVING must not match cache without HAVING")
	}

	sameHaving := `{"DATABASE":"flow_log","TABLE":"l7_flow_log","TIME_START":100,"TIME_END":200,` +
		`"QUERIES":[{"SELECT":"count(*)","WHERE":"","HAVING":"count(*) > 10"}]}`
	c2 := newTestCache(map[string][]byte{withHaving: []byte(`{"resp":2}`)})
	if resp := c2.FindWithBody("POST", "/x", sameHaving); resp == nil {
		t.Fatal("identical HAVING should match")
	}
}
