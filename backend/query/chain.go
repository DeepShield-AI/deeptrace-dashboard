package query

import (
	"context"
	"fmt"

	"deeptrace-backend/logging"
)

// ---------------------------------------------------------------------------
// DataSource interfaces (strategy pattern)
// ---------------------------------------------------------------------------

// DataSource is the base interface for all data sources.
type DataSource interface {
	// Name returns a human-readable name for logging / metrics.
	Name() string
	// Enabled reports whether this data source is available.
	Enabled() bool
}

// QuerierListSource handles List-style aggregation queries.
type QuerierListSource interface {
	DataSource
	QueryList(ctx context.Context, req *QuerierListRequest) (*Result, error)
}

// QuerierTopSource handles TopN / ranking queries.
type QuerierTopSource interface {
	DataSource
	QueryTop(ctx context.Context, req *QuerierListRequest) (*Result, error)
}

// TraceMapSource handles TraceMap queries.
type TraceMapSource interface {
	DataSource
	QueryTraceMap(ctx context.Context, req *QuerierListRequest) (*TraceMapResult, error)
}

// ProfileSource handles Profile (flame-graph) queries.
type ProfileSource interface {
	DataSource
	QueryProfile(ctx context.Context, req *ProfileRequest) (*ProfileResult, error)
}

// ---------------------------------------------------------------------------
// DataSourceChain — priority-based fallback chain
// ---------------------------------------------------------------------------

// Provenance records which data source actually produced a response.
// The handler attaches one to the request context and reads it back to set
// the X-DeepTrace-Source response header.
type Provenance struct {
	Source string
}

type provenanceContextKey struct{}

// WithProvenance attaches a provenance record to the request context.
func WithProvenance(ctx context.Context, p *Provenance) context.Context {
	return context.WithValue(ctx, provenanceContextKey{}, p)
}

// ProvenanceFromContext returns the provenance record attached by the handler.
func ProvenanceFromContext(ctx context.Context) *Provenance {
	if ctx == nil {
		return nil
	}
	p, _ := ctx.Value(provenanceContextKey{}).(*Provenance)
	return p
}

func recordProvenance(ctx context.Context, name string) {
	if p := ProvenanceFromContext(ctx); p != nil {
		p.Source = NormalizeSourceName(name)
	}
}

// DataSourceChain runs a list of data sources in priority order.
// The first source that returns a non-nil, non-error result wins.
type DataSourceChain struct {
	listSources     []QuerierListSource
	topSources      []QuerierTopSource
	traceMapSources []TraceMapSource
	profileSources  []ProfileSource
}

// NewDataSourceChain creates an empty chain.
func NewDataSourceChain() *DataSourceChain {
	return &DataSourceChain{}
}

// AddListSource appends a List source (lower priority = later).
func (c *DataSourceChain) AddListSource(s QuerierListSource) {
	c.listSources = append(c.listSources, s)
}

// AddTopSource appends a Top source.
func (c *DataSourceChain) AddTopSource(s QuerierTopSource) {
	c.topSources = append(c.topSources, s)
}

// AddTraceMapSource appends a TraceMap source.
func (c *DataSourceChain) AddTraceMapSource(s TraceMapSource) {
	c.traceMapSources = append(c.traceMapSources, s)
}

// AddProfileSource appends a Profile source.
func (c *DataSourceChain) AddProfileSource(s ProfileSource) {
	c.profileSources = append(c.profileSources, s)
}

// Chain semantics (CLAUDE.md M4):
//   result != nil            — the source handled the query (empty or not): stop.
//   result == nil, err == nil — the source does not support this signature: try next.
//   err != nil                — execution failed: log and continue in normal mode,
//                               fail immediately under a forced NoFallback policy.
// Under a forced source, sources that don't match the policy are skipped, and a
// request no source served ends in an error (the handler returns HTTP 502).

func (c *DataSourceChain) allowedByPolicy(policy SourcePolicy, name string) bool {
	return policy.ForcedSource == "" || NormalizeSourceName(name) == policy.ForcedSource
}

func (c *DataSourceChain) noSourceError() error {
	return fmt.Errorf("no data source served the query")
}

// QueryList walks the list sources in priority order.
func (c *DataSourceChain) QueryList(ctx context.Context, req *QuerierListRequest) (*Result, error) {
	policy := SourcePolicyFromContext(ctx)
	for _, s := range c.listSources {
		if !s.Enabled() || !c.allowedByPolicy(policy, s.Name()) {
			continue
		}
		result, err := s.QueryList(ctx, req)
		if err != nil {
			logging.Warnf("chain.List: source %s failed: %v", s.Name(), err)
			if policy.NoFallback {
				return nil, err
			}
			continue
		}
		if result == nil {
			continue // not supported by this source
		}
		recordProvenance(ctx, s.Name())
		return result, nil
	}
	if policy.NoFallback {
		return nil, c.noSourceError()
	}
	return &Result{
		Data: []map[string]interface{}{},
		Type: "Application_Detail_List",
	}, nil
}

// QueryTop walks the top sources in priority order.
func (c *DataSourceChain) QueryTop(ctx context.Context, req *QuerierListRequest) (*Result, error) {
	policy := SourcePolicyFromContext(ctx)
	for _, s := range c.topSources {
		if !s.Enabled() || !c.allowedByPolicy(policy, s.Name()) {
			continue
		}
		result, err := s.QueryTop(ctx, req)
		if err != nil {
			logging.Warnf("chain.Top: source %s failed: %v", s.Name(), err)
			if policy.NoFallback {
				return nil, err
			}
			continue
		}
		if result == nil {
			continue
		}
		recordProvenance(ctx, s.Name())
		return result, nil
	}
	if policy.NoFallback {
		return nil, c.noSourceError()
	}
	return &Result{
		Data: []map[string]interface{}{},
	}, nil
}

// QueryTraceMap walks the trace-map sources.
func (c *DataSourceChain) QueryTraceMap(ctx context.Context, req *QuerierListRequest) (*TraceMapResult, error) {
	policy := SourcePolicyFromContext(ctx)
	for _, s := range c.traceMapSources {
		if !s.Enabled() || !c.allowedByPolicy(policy, s.Name()) {
			continue
		}
		result, err := s.QueryTraceMap(ctx, req)
		if err != nil {
			logging.Warnf("chain.TraceMap: source %s failed: %v", s.Name(), err)
			if policy.NoFallback {
				return nil, err
			}
			continue
		}
		if result == nil {
			continue
		}
		recordProvenance(ctx, s.Name())
		return result, nil
	}
	if policy.NoFallback {
		return nil, c.noSourceError()
	}
	return &TraceMapResult{
		NodeData: []map[string]interface{}{},
		ProgressInfo: map[string]interface{}{
			"total_traces_count":      0,
			"calculated_traces_count": 0,
		},
	}, nil
}

// QueryProfile walks the profile sources in priority order (same M4
// three-state semantics as QueryTop).
func (c *DataSourceChain) QueryProfile(ctx context.Context, req *ProfileRequest) (*ProfileResult, error) {
	policy := SourcePolicyFromContext(ctx)
	for _, s := range c.profileSources {
		if !s.Enabled() || !c.allowedByPolicy(policy, s.Name()) {
			continue
		}
		result, err := s.QueryProfile(ctx, req)
		if err != nil {
			logging.Warnf("chain.Profile: source %s failed: %v", s.Name(), err)
			if policy.NoFallback {
				return nil, err
			}
			continue
		}
		if result == nil {
			continue
		}
		recordProvenance(ctx, s.Name())
		return result, nil
	}
	if policy.NoFallback {
		return nil, c.noSourceError()
	}
	return EmptyProfileResult(), nil
}
