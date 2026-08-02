package query

import (
	"context"
	"testing"
)

type listSourceStub struct {
	name   string
	result *Result
	err    error
	calls  int
}

func (s *listSourceStub) Name() string  { return s.name }
func (s *listSourceStub) Enabled() bool { return true }
func (s *listSourceStub) QueryList(context.Context, *QuerierListRequest) (*Result, error) {
	s.calls++
	return s.result, s.err
}

type topSourceStub struct {
	listSourceStub
}

func (s *topSourceStub) QueryTop(ctx context.Context, req *QuerierListRequest) (*Result, error) {
	s.calls++
	return s.result, s.err
}

func TestQueryListStopsOnHandledEmptyResult(t *testing.T) {
	first := &listSourceStub{
		name:   "zerotrace",
		result: &Result{Data: []map[string]interface{}{}},
	}
	second := &listSourceStub{
		name:   "cache",
		result: &Result{Data: []map[string]interface{}{{"stale": true}}},
	}
	chain := NewDataSourceChain()
	chain.AddListSource(first)
	chain.AddListSource(second)

	prov := &Provenance{}
	ctx := WithProvenance(context.Background(), prov)
	result, err := chain.QueryList(ctx, &QuerierListRequest{})
	if err != nil {
		t.Fatalf("QueryList returned error: %v", err)
	}
	if prov.Source != "zerotrace" {
		t.Fatalf("provenance source = %q, want zerotrace", prov.Source)
	}
	if result == nil || len(result.Data) != 0 {
		t.Fatalf("result = %+v, want handled empty result", result)
	}
	if second.calls != 0 {
		t.Fatalf("cache called %d times after a handled empty result", second.calls)
	}
}

func TestQueryListForcedSourceSkipsOtherSources(t *testing.T) {
	zt := &listSourceStub{
		name:   "zerotrace",
		result: &Result{Data: []map[string]interface{}{{"live": true}}},
	}
	cache := &listSourceStub{
		name:   "cache",
		result: &Result{Data: []map[string]interface{}{{"cached": true}}},
	}
	chain := NewDataSourceChain()
	chain.AddListSource(zt)
	chain.AddListSource(cache)

	ctx := WithSourcePolicy(context.Background(), SourcePolicy{ForcedSource: "cache"})
	prov := &Provenance{}
	ctx = WithProvenance(ctx, prov)
	result, err := chain.QueryList(ctx, &QuerierListRequest{})
	if err != nil {
		t.Fatalf("QueryList returned error: %v", err)
	}
	if zt.calls != 0 {
		t.Fatalf("zerotrace called %d times under forced cache", zt.calls)
	}
	if cache.calls != 1 {
		t.Fatalf("cache called %d times, want 1", cache.calls)
	}
	if prov.Source != "cache" {
		t.Fatalf("provenance source = %q, want cache", prov.Source)
	}
	if result.Data[0]["cached"] != true {
		t.Fatalf("result = %+v, want cached data", result.Data)
	}
}

func TestQueryListUnsupportedFallsThrough(t *testing.T) {
	zt := &listSourceStub{name: "zerotrace"} // nil, nil → not supported
	cache := &listSourceStub{
		name:   "cache",
		result: &Result{Data: []map[string]interface{}{{"cached": true}}},
	}
	chain := NewDataSourceChain()
	chain.AddListSource(zt)
	chain.AddListSource(cache)

	prov := &Provenance{}
	ctx := WithProvenance(context.Background(), prov)
	result, err := chain.QueryList(ctx, &QuerierListRequest{})
	if err != nil {
		t.Fatalf("QueryList returned error: %v", err)
	}
	if prov.Source != "cache" {
		t.Fatalf("provenance source = %q, want cache", prov.Source)
	}
	if result == nil || len(result.Data) != 1 {
		t.Fatalf("result = %+v, want cache data", result)
	}
}

func TestQueryListNoFallbackFailsClosed(t *testing.T) {
	chain := NewDataSourceChain() // no sources at all
	ctx := WithSourcePolicy(context.Background(), SourcePolicy{ForcedSource: "zerotrace", NoFallback: true})

	result, err := chain.QueryList(ctx, &QuerierListRequest{})

	if err == nil {
		t.Fatalf("expected error under no-fallback with no serving source, got result %+v", result)
	}
	if result != nil {
		t.Fatalf("result = %+v, want nil on failure", result)
	}
}

func TestQueryListSourceErrorContinuesInNormalMode(t *testing.T) {
	failing := &listSourceStub{
		name: "zerotrace",
		err:  context.DeadlineExceeded,
	}
	cache := &listSourceStub{
		name:   "cache",
		result: &Result{Data: []map[string]interface{}{{"cached": true}}},
	}
	chain := NewDataSourceChain()
	chain.AddListSource(failing)
	chain.AddListSource(cache)

	result, err := chain.QueryList(context.Background(), &QuerierListRequest{})
	if err != nil {
		t.Fatalf("normal mode should continue past a failed source: %v", err)
	}
	if result == nil || len(result.Data) != 1 {
		t.Fatalf("result = %+v, want cache fallback data", result)
	}
}

func TestQueryListNoFallbackStopsOnSourceError(t *testing.T) {
	failing := &listSourceStub{
		name: "zerotrace",
		err:  context.DeadlineExceeded,
	}
	chain := NewDataSourceChain()
	chain.AddListSource(failing)

	ctx := WithSourcePolicy(context.Background(), SourcePolicy{ForcedSource: "zerotrace", NoFallback: true})
	result, err := chain.QueryList(ctx, &QuerierListRequest{})
	if err == nil {
		t.Fatalf("expected error under no-fallback, got result %+v", result)
	}
	if result != nil {
		t.Fatalf("result = %+v, want nil on failure", result)
	}
}

func TestQueryTopStopsOnHandledResult(t *testing.T) {
	first := &topSourceStub{listSourceStub: listSourceStub{
		name:   "zerotrace",
		result: &Result{Data: []map[string]interface{}{{"live": true}}},
	}}
	second := &topSourceStub{listSourceStub: listSourceStub{
		name:   "cache",
		result: &Result{Data: []map[string]interface{}{{"stale": true}}},
	}}
	chain := NewDataSourceChain()
	chain.AddTopSource(first)
	chain.AddTopSource(second)

	prov := &Provenance{}
	ctx := WithProvenance(context.Background(), prov)
	result, err := chain.QueryTop(ctx, &QuerierListRequest{})
	if err != nil {
		t.Fatalf("QueryTop returned error: %v", err)
	}
	if prov.Source != "zerotrace" {
		t.Fatalf("provenance source = %q, want zerotrace", prov.Source)
	}
	if second.calls != 0 {
		t.Fatalf("cache called %d times after a handled result", second.calls)
	}
	if result.Data[0]["live"] != true {
		t.Fatalf("result = %+v, want live data", result.Data)
	}
}

// ---------------------------------------------------------------------------
// Profile chain
// ---------------------------------------------------------------------------

type profileSourceStub struct {
	name   string
	result *ProfileResult
	err    error
	calls  int
}

func (s *profileSourceStub) Name() string  { return s.name }
func (s *profileSourceStub) Enabled() bool { return true }
func (s *profileSourceStub) QueryProfile(context.Context, *ProfileRequest) (*ProfileResult, error) {
	s.calls++
	return s.result, s.err
}

func TestQueryProfileStopsOnHandledEmptyResult(t *testing.T) {
	ch := &profileSourceStub{name: "clickhouse", result: EmptyProfileResult()}
	cache := &profileSourceStub{name: "cache", result: &ProfileResult{Functions: []string{"stale"}}}
	chain := NewDataSourceChain()
	chain.AddProfileSource(ch)
	chain.AddProfileSource(cache)

	prov := &Provenance{}
	ctx := WithProvenance(context.Background(), prov)
	result, err := chain.QueryProfile(ctx, &ProfileRequest{})
	if err != nil {
		t.Fatalf("QueryProfile returned error: %v", err)
	}
	if prov.Source != "clickhouse" {
		t.Fatalf("provenance source = %q, want clickhouse", prov.Source)
	}
	if result == nil || result.Functions != nil && len(result.Functions) != 0 {
		t.Fatalf("result = %+v, want handled empty result", result)
	}
	if cache.calls != 0 {
		t.Fatalf("cache called %d times after a handled empty result", cache.calls)
	}
}

func TestQueryProfileUnsupportedFallsThrough(t *testing.T) {
	ch := &profileSourceStub{name: "clickhouse"} // nil, nil → not supported
	cache := &profileSourceStub{name: "cache", result: &ProfileResult{Functions: []string{"cached"}}}
	chain := NewDataSourceChain()
	chain.AddProfileSource(ch)
	chain.AddProfileSource(cache)

	prov := &Provenance{}
	ctx := WithProvenance(context.Background(), prov)
	result, err := chain.QueryProfile(ctx, &ProfileRequest{})
	if err != nil {
		t.Fatalf("QueryProfile returned error: %v", err)
	}
	if prov.Source != "cache" {
		t.Fatalf("provenance source = %q, want cache", prov.Source)
	}
	if result == nil || len(result.Functions) != 1 {
		t.Fatalf("result = %+v, want cache data", result)
	}
}

func TestQueryProfileNoFallbackFailsClosed(t *testing.T) {
	chain := NewDataSourceChain() // no sources at all
	ctx := WithSourcePolicy(context.Background(), SourcePolicy{ForcedSource: "clickhouse", NoFallback: true})

	result, err := chain.QueryProfile(ctx, &ProfileRequest{})

	if err == nil {
		t.Fatalf("expected error under no-fallback with no serving source, got result %+v", result)
	}
	if result != nil {
		t.Fatalf("result = %+v, want nil on failure", result)
	}
}
