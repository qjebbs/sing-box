package dialer

import (
	"context"

	N "github.com/sagernet/sing/common/network"
)

type detourOverridekey struct{}

// ContextWithDetourOverride returns a new context with the detour override.
func ContextWithDetourOverride(ctx context.Context, detour N.Dialer) context.Context {
	if detour == nil {
		return ctx
	}
	return context.WithValue(ctx, detourOverridekey{}, detour)
}

// DetourOverrideFromContext returns the detour override from the context.
func DetourOverrideFromContext(ctx context.Context) N.Dialer {
	value := ctx.Value(detourOverridekey{})
	if value == nil {
		return nil
	}
	return value.(N.Dialer)
}
