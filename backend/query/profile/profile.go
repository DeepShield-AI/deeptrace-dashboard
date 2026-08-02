package profile

// Profile (flame-graph) query: raw profile.in_process rows → flame-graph
// tree. Each row's profile_location_str is a zstd-compressed stack text like
// "[p] proc;[t] thread;..." (frames separated by ";"). The response contract
// (functions / function_types / function_values / node_values) is verified
// against api_cache — see query/types.go ProfileResult.
//
// Flame-graph semantics (verified against the contract):
//   - each sample = (stack frames [f0..fn], value v); every frame's node gets
//     total += v, the LAST frame's node gets self += v (stacks may truncate at
//     any depth, so any node can be a leaf);
//   - nodes are merged by (parent, frame name);
//   - function_values[i] = Σ over all nodes of function i.

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/logging"
	"deeptrace-backend/query"
)

const (
	profileDB    = "profile"
	profileTable = "in_process"
)

// validColumnRE gates tag_filter keys before they are spliced into SQL.
var validColumnRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validValueRE gates tag_filter values: quoted string literals only allow
// identifiers/numbers — rejects SQL metacharacters (; ' " space) outright.
var validValueRE = regexp.MustCompile(`^[A-Za-z0-9_.:/+-]*$`)

// QueryProfile runs the raw-row scan + flame-graph aggregation.
// M4 semantics: unsupported signature → (nil, nil) so the chain falls through;
// executed-but-empty → non-nil empty ProfileResult; real failures → err.
func QueryProfile(ch clickhouse.Querier, ctx context.Context, req *query.ProfileRequest) (*query.ProfileResult, error) {
	if req.TimeStart == 0 && req.TimeEnd == 0 {
		return nil, nil // unsupported: no time range
	}
	for _, col := range []string{"time", "profile_location_str", "profile_value"} {
		if !ch.HasColumn(profileDB, profileTable, col) {
			return nil, nil // unsupported signature — fall through to cache
		}
	}
	sql, err := buildSQL(req)
	if err != nil {
		return nil, err
	}
	qCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rows, err := ch.Query(qCtx, sql)
	if err != nil {
		return nil, fmt.Errorf("profile query: %w", err)
	}
	defer rows.Close()
	data, err := clickhouse.ScanRows(rows)
	if err != nil {
		return nil, fmt.Errorf("profile scan: %w", err)
	}
	return BuildResult(data), nil
}

// buildSQL assembles the raw-row query. Table and filter columns are fixed
// constants; tag_filter keys are validated before being backticked; string
// values are single-quote escaped (mirrors builder.go newTag escaping).
func buildSQL(req *query.ProfileRequest) (string, error) {
	var wheres []string
	if req.TimeStart > 0 {
		wheres = append(wheres, fmt.Sprintf("time >= %d", req.TimeStart))
	}
	if req.TimeEnd > 0 {
		wheres = append(wheres, fmt.Sprintf("time <= %d", req.TimeEnd))
	}
	// Fixed filters in stable order (map iteration order would be random).
	for _, fc := range []struct{ col, v string }{
		{"profile_language_type", req.ProfileLanguageType},
		{"profile_event_type", req.ProfileEventType},
		{"app_service", req.AppService},
	} {
		if fc.v != "" {
			wheres = append(wheres, fmt.Sprintf("%s = '%s'", fc.col, escapeSingleQuote(fc.v)))
		}
	}
	for _, pair := range strings.Split(req.TagFilter, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, val, ok := strings.Cut(pair, "=")
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if !ok || key == "" {
			continue // tolerate malformed entries
		}
		// The frontend quotes columns/values (e.g. `gprocess_id`=10308 or
		// `pod`='web-1'); strip the quoting before validation.
		key = strings.Trim(key, "`")
		val = strings.Trim(val, "'")
		if !validColumnRE.MatchString(key) || !validValueRE.MatchString(val) {
			return "", fmt.Errorf("%w: %s", clickhouse.ErrUnsupportedColumn, key)
		}
		if n, err := strconv.ParseInt(val, 10, 64); err == nil {
			wheres = append(wheres, fmt.Sprintf("`%s` = %d", key, n)) // gprocess_id=10074
		} else {
			wheres = append(wheres, fmt.Sprintf("`%s` = '%s'", key, escapeSingleQuote(val)))
		}
	}
	sql := "SELECT profile_location_str, profile_value FROM `" + profileDB + "`.`" + profileTable + "`"
	if len(wheres) > 0 {
		sql += " WHERE " + strings.Join(wheres, " AND ")
	}
	return sql, nil
}

func escapeSingleQuote(s string) string { return strings.ReplaceAll(s, "'", "\\'") }

// frameType maps a frame to its contract type. The no-prefix rule is
// positional: the root frame (first frame of a sample) → "P", elsewhere → "A"
// (e.g. "__vdso_clock_gettime" is "A"). Verified against the contract:
// [p]/bare-root → P, [t] → T, [k] → K, [l] → L, [/... → "?", else "A".
func frameType(frame string, isRoot bool) string {
	switch {
	case strings.HasPrefix(frame, "[p] "), isRoot && !strings.HasPrefix(frame, "["):
		return "P"
	case strings.HasPrefix(frame, "[t] "):
		return "T"
	case strings.HasPrefix(frame, "[k] "):
		return "K"
	case strings.HasPrefix(frame, "[l] "):
		return "L"
	case strings.HasPrefix(frame, "[/"):
		return "?"
	default:
		return "A"
	}
}

// profileNode is one flame-graph node: a frame occurrence under a parent.
// Children merge on the exact frame name (trie over (parent, frame)).
type profileNode struct {
	functionID int
	self       float64
	total      float64
	children   map[string]*profileNode
}

// BuildResult aggregates scanned rows (profile_location_str + profile_value)
// into the flame-graph result. Malformed rows (empty location, zstd decode
// failure, empty frame list) are skipped with a warn — never fatal.
func BuildResult(data []map[string]interface{}) *query.ProfileResult {
	dec, err := zstd.NewReader(nil)
	if err != nil {
		logging.Errorf("profile: zstd init failed: %v", err)
		return query.EmptyProfileResult()
	}
	defer dec.Close()

	var functions []string
	var funcTypes []string
	funcID := map[string]int{}
	getFunc := func(frame string, isRoot bool) int {
		if id, ok := funcID[frame]; ok {
			return id
		}
		id := len(functions)
		functions = append(functions, frame)
		funcTypes = append(funcTypes, frameType(frame, isRoot))
		funcID[frame] = id
		return id
	}

	// Multiple root frames (different first frames, e.g. mixed processes) are
	// each emitted as a parent_node_id=-1 node. The contract samples are
	// single-root after app_service filtering; log if that is violated.
	var rootOrder []*profileNode
	roots := map[string]*profileNode{} // merged by first frame
	for _, row := range data {
		raw := clickhouse.GetStr(row, "profile_location_str")
		if raw == "" {
			continue
		}
		decoded, err := dec.DecodeAll([]byte(raw), nil)
		if err != nil {
			logging.Warnf("profile: zstd decode failed: %v", err)
			continue
		}
		var frames []string
		for _, p := range strings.Split(string(decoded), ";") {
			if p = strings.TrimSpace(p); p != "" {
				frames = append(frames, p)
			}
		}
		if len(frames) == 0 {
			continue
		}
		v := clickhouse.Get[float64](row, "profile_value")
		cur := roots[frames[0]]
		if cur == nil {
			cur = &profileNode{functionID: getFunc(frames[0], true), children: map[string]*profileNode{}}
			roots[frames[0]] = cur
			rootOrder = append(rootOrder, cur)
		}
		cur.total += v // every frame on the path contributes to total
		for i := 1; i < len(frames); i++ {
			f := frames[i]
			child := cur.children[f]
			if child == nil {
				child = &profileNode{functionID: getFunc(f, false), children: map[string]*profileNode{}}
				cur.children[f] = child
			}
			cur = child
			cur.total += v
		}
		cur.self += v // only the last frame carries self
	}

	if len(rootOrder) == 0 {
		return query.EmptyProfileResult()
	}
	if len(rootOrder) > 1 {
		logging.Warnf("profile: %d root frames in one response", len(rootOrder))
	}

	type nodeRow struct {
		functionID  int
		parentIndex int
		self        float64
		total       float64
	}
	var rows []nodeRow
	var visit func(n *profileNode, parentIndex int)
	visit = func(n *profileNode, parentIndex int) {
		idx := len(rows)
		rows = append(rows, nodeRow{n.functionID, parentIndex, n.self, n.total})
		children := make([]*profileNode, 0, len(n.children))
		for _, c := range n.children {
			children = append(children, c)
		}
		sort.Slice(children, func(i, j int) bool { return children[i].total > children[j].total })
		for _, c := range children {
			visit(c, idx)
		}
	}
	for _, r := range rootOrder {
		visit(r, -1)
	}

	funcSelf := make([]float64, len(functions))
	funcTotal := make([]float64, len(functions))
	for _, r := range rows {
		funcSelf[r.functionID] += r.self
		funcTotal[r.functionID] += r.total
	}
	fv := make([][]interface{}, len(functions))
	for i := range functions {
		fv[i] = []interface{}{funcSelf[i], funcTotal[i]}
	}
	nv := make([][]interface{}, len(rows))
	for i, r := range rows {
		nv[i] = []interface{}{r.functionID, r.parentIndex, r.self, r.total}
	}

	return &query.ProfileResult{
		Functions:     functions,
		FunctionTypes: funcTypes,
		FunctionValues: query.ProfileValueTable{
			Columns: []string{"self_value", "total_value"},
			Values:  fv,
		},
		NodeValues: query.ProfileValueTable{
			Columns: []string{"function_id", "parent_node_id", "self_value", "total_value"},
			Values:  nv,
		},
	}
}
