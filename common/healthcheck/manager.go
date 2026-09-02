package healthcheck

import (
	"context"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/service"
)

var (
	_ adapter.HealthCheckManager = (*Manager)(nil)
	_ adapter.LifecycleService   = (*Manager)(nil)
)

// Manager manages the named health checks declared in the top-level
// `health_checks` option, sharing a single HealthCheck instance per tag
// across all balancer groups, similar to how the HTTP client manager
// shares transports.
type Manager struct {
	ctx    context.Context
	logger log.ContextLogger
	access sync.Mutex

	router  adapter.Router
	om      adapter.OutboundManager
	defines map[string]option.HealthCheck
	checks  map[string]*HealthCheck
	managed []*HealthCheck
}

// NewManager creates a health check manager from the top-level `health_checks`
// declarations. The router and outbound manager are resolved from the context
// here, so this must be called after they have been registered.
func NewManager(ctx context.Context, logger log.ContextLogger, checks []option.HealthCheck) *Manager {
	defines := make(map[string]option.HealthCheck, len(checks))
	for _, check := range checks {
		defines[check.Tag] = check
	}
	return &Manager{
		ctx:     ctx,
		logger:  logger,
		router:  service.FromContext[adapter.Router](ctx),
		om:      service.FromContext[adapter.OutboundManager](ctx),
		defines: defines,
		checks:  make(map[string]*HealthCheck),
	}
}

// Name returns the name of this lifecycle service.
func (m *Manager) Name() string {
	return "health-check"
}

// Start implements adapter.LifecycleService.
func (m *Manager) Start(stage adapter.StartStage) error {
	return nil
}

// Get resolves a health check for a balancer group. If options.Tag is set it
// resolves the named shared health check declared in `health_checks`;
// otherwise it creates an inline health check from the options, like the
// HTTP client manager's ResolveTransport.
func (m *Manager) Get(ctx context.Context, logger logger.ContextLogger, options option.HealthCheckOptions) (adapter.HealthCheck, error) {
	if options.Tag != "" {
		return m.resolveShared(options.Tag)
	}
	check := NewHealthCheck(m.ctx, m.router, m.om, &options, logger)
	if err := check.Start(); err != nil {
		return nil, E.Cause(err, "start health check")
	}
	m.track(check)
	return check, nil
}

func (m *Manager) resolveShared(tag string) (*HealthCheck, error) {
	m.access.Lock()
	defer m.access.Unlock()
	if check, loaded := m.checks[tag]; loaded {
		return check, nil
	}
	define, loaded := m.defines[tag]
	if !loaded {
		return nil, E.New("health check not found: ", tag)
	}
	options := define.Options()
	check := NewHealthCheck(m.ctx, m.router, m.om, &options, m.logger)
	if err := check.Start(); err != nil {
		return nil, E.Cause(err, "start health check[", tag, "]")
	}
	m.checks[tag] = check
	return check, nil
}

func (m *Manager) track(check *HealthCheck) {
	m.access.Lock()
	defer m.access.Unlock()
	m.managed = append(m.managed, check)
}

// Close stops all managed health checks.
func (m *Manager) Close() error {
	m.access.Lock()
	defer m.access.Unlock()
	var err error
	for _, check := range m.checks {
		err = E.Append(err, check.Close(), func(err error) error {
			return E.Cause(err, "close health check")
		})
	}
	for _, check := range m.managed {
		err = E.Append(err, check.Close(), func(err error) error {
			return E.Cause(err, "close health check")
		})
	}
	m.checks = nil
	m.managed = nil
	return err
}
