package query

import (
	"context"
	"strings"
)

// SourcePolicy controls data-source selection for verification requests.
// Normal frontend requests use the zero value and retain the configured chain.
type SourcePolicy struct {
	ForcedSource string
	// NoFallback disables the cache/fallback layer: a forced source that
	// cannot serve the request must fail the request instead.
	NoFallback bool
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
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

// IsKnownSource reports whether a source can be requested by verification tools.
func IsKnownSource(name string) bool {
	switch NormalizeSourceName(name) {
	case "zerotrace", "clickhouse", "cache":
		return true
	default:
		return false
	}
}
