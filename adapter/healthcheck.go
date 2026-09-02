package adapter

import (
	"context"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/logger"
)

// HealthCheck is the health checker shared by balancer groups.
type HealthCheck interface {
	SetProviders(namespace string, providers []Provider) error
	RemoveProviders(namespace string) error
	RegisterPostCheckListener(listener func())
	UnregisterPostCheckListener(listener func())
	CheckAll(ctx context.Context, namespace string) (map[string]uint16, error)
	ReportFailure(outbound Outbound)
}

// HealthCheckManager manages named health checks from the top-level
// `health_checks` option and resolves them for balancer groups. Get resolves
// either a named health check (when options.Tag is set) or an inline health
// check built from the options, similar to how the HTTP client manager
// resolves transports.
//
// A health check must be configured explicitly on each balancer group;
// there is no implicit default.
type HealthCheckManager interface {
	Get(ctx context.Context, logger logger.ContextLogger, options option.HealthCheckOptions) (HealthCheck, error)
}
