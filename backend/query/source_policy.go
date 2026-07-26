package query

import (
	"context"
	"fmt"
	"strings"
)

// SourcePolicy controls data-source selection for verification requests.
// Normal frontend requests use the zero value and retain the configured chain.
type SourcePolicy struct {
	ForcedSource string
	Strict       bool
}

type sourcePolicyContextKey struct{}

// WithSourcePolicy attaches a verification source policy to a request context.
func WithSourcePolicy(ctx context.Context, policy SourcePolicy) context.Context {
	policy.ForcedSource = NormalizeSourceName(policy.ForcedSource)
	return context.WithValue(ctx, sourcePolicyContextKey{}, policy)
}

// SourcePolicyFromContext returns the request-scoped source policy.
func SourcePolicyFromContext(ctx context.Context) SourcePolicy {
	if ctx == nil {
		return SourcePolicy{}
	}
	policy, _ := ctx.Value(sourcePolicyContextKey{}).(SourcePolicy)
	return policy
}

// NormalizeSourceName maps user-facing source aliases to chain source names.
func NormalizeSourceName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "auto":
		return ""
	case "zt", "zerotrace", "deepflow-server":
		return "zerotrace"
	case "ch", "clickhouse":
		return "clickhouse"
	case "cache", "api_cache":
		return "cache"
	case "mock":
		return "mock"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

// IsKnownSource reports whether a source can be requested by verification tools.
func IsKnownSource(name string) bool {
	switch NormalizeSourceName(name) {
	case "zerotrace", "clickhouse", "cache", "mock":
		return true
	default:
		return false
	}
}

func sourceMatches(requested, actual string) bool {
	return NormalizeSourceName(requested) == NormalizeSourceName(actual)
}

func forcedSourceError(source, reason string) error {
	return fmt.Errorf("forced source %q %s", NormalizeSourceName(source), reason)
}
