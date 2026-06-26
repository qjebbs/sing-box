package healthcheck

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	boxService "github.com/sagernet/sing-box/adapter/service"
	boxConstant "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
)

var (
	_ adapter.Service = (*Service)(nil)
)

// RegisterService registers the health checker service to the service registry.
func RegisterService(registry *boxService.Registry) {
	boxService.Register(registry, boxConstant.TypeHealthChecker, NewService)
}

// Service is the standalone health check service that can be shared
// across multiple outbounds. Outbounds submit their full provider list
// via SetProviders; the service merges them into a single immutable
// provider that delegates Outbounds() calls to all registered providers
// with outbound-level deduplication by tag.
type Service struct {
	boxService.Adapter
	*HealthCheck
}

// NewService creates a new health checker service.
func NewService(ctx context.Context, logger log.ContextLogger, tag string, options option.HealthCheckOptions) (adapter.Service, error) {
	return &Service{
		Adapter: boxService.NewAdapter(boxConstant.TypeHealthChecker, tag),
		HealthCheck: NewHealthCheck(
			ctx,
			service.FromContext[adapter.Router](ctx),
			service.FromContext[adapter.OutboundManager](ctx),
			&options,
			logger,
		),
	}, nil
}

// Start starts the health check service. Implements adapter.Service.
func (s *Service) Start(stage adapter.StartStage) error {
	return s.HealthCheck.Start()
}

// Close stops the health check service. Implements adapter.Service.
func (s *Service) Close() error {
	return s.HealthCheck.Close()
}
