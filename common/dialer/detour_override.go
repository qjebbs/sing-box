package dialer

import (
	"context"

	N "github.com/sagernet/sing/common/network"
)

type detourOverridekey struct{}

type detourOverride struct {
	detour N.Dialer
	used   bool
}

// ContextWithDetourOverride returns a new context with the detour override.
func ContextWithDetourOverride(parentCtx context.Context, detour N.Dialer) (ctx context.Context, used func() bool) {
	if detour == nil {
		return parentCtx, func() bool { return false }
	}
	value := &detourOverride{detour: detour}
	return context.WithValue(parentCtx, detourOverridekey{}, value), func() bool {
		return value.used
	}
}

// DetourOverrideFromContext returns the detour override from the context.
func DetourOverrideFromContext(ctx context.Context) N.Dialer {
	value := ctx.Value(detourOverridekey{})
	if value == nil {
		return nil
	}
	v := value.(*detourOverride)
	v.used = true
	return v.detour.(N.Dialer)
}
