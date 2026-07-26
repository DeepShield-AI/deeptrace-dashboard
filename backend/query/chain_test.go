package query

import (
	"context"
	"errors"
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

	result, err := chain.QueryList(context.Background(), &QuerierListRequest{})

	if err != nil {
		t.Fatalf("QueryList returned error: %v", err)
	}
	if result.Source != "zerotrace" {
		t.Fatalf("source = %q, want zerotrace", result.Source)
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
	ctx := WithSourcePolicy(context.Background(), SourcePolicy{
		ForcedSource: "cache",
		Strict:       true,
	})

	result, err := chain.QueryList(ctx, &QuerierListRequest{})

	if err != nil {
		t.Fatalf("QueryList returned error: %v", err)
	}
	if result.Source != "cache" {
		t.Fatalf("source = %q, want cache", result.Source)
	}
	if zt.calls != 0 {
		t.Fatalf("zerotrace called %d times for forced cache request", zt.calls)
	}
}

func TestQueryListForcedSourceFailsClosed(t *testing.T) {
	zt := &listSourceStub{
		name: "zerotrace",
		err:  errors.New("upstream unavailable"),
	}
	cache := &listSourceStub{
		name:   "cache",
		result: &Result{Data: []map[string]interface{}{{"cached": true}}},
	}
	chain := NewDataSourceChain()
	chain.AddListSource(zt)
	chain.AddListSource(cache)
	ctx := WithSourcePolicy(context.Background(), SourcePolicy{
		ForcedSource: "zerotrace",
		Strict:       true,
	})

	_, err := chain.QueryList(ctx, &QuerierListRequest{})

	if err == nil {
		t.Fatal("QueryList returned nil error for unavailable forced source")
	}
	if cache.calls != 0 {
		t.Fatalf("cache called %d times after forced source failure", cache.calls)
	}
}
