package healthcheck

import (
	"context"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
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
	ctx        context.Context
	scope      *adapter.Scope
	logFactory log.Factory
	access     sync.Mutex

	router  adapter.Router
	om      adapter.OutboundManager
	defines map[string]option.HealthCheck
	checks  map[string]*HealthCheck
	managed []*HealthCheck
	// started reports that the post start has passed, so a check created later reads its own
	// states instead of waiting for a start that will not come again.
	started bool
}

// NewManager creates a health check manager from the top-level `health_checks`
// declarations. The router and outbound manager are resolved from the context
// here, so this must be called after they have been registered.
func NewManager(ctx context.Context, logFactory log.Factory, checks []option.HealthCheck) *Manager {
	defines := make(map[string]option.HealthCheck, len(checks))
	for _, check := range checks {
		defines[check.Tag] = check
	}
	return &Manager{
		ctx:        ctx,
		logFactory: logFactory,
		router:     service.FromContext[adapter.Router](ctx),
		om:         service.FromContext[adapter.OutboundManager](ctx),
		defines:    defines,
		checks:     make(map[string]*HealthCheck),
	}
}

// Name returns the name of this lifecycle service.
func (m *Manager) Name() string {
	return "health-check"
}

// Start implements adapter.LifecycleService. The start takes the close of every checker, and
// the post start reads the states the previous run left: the groups have registered their
// providers by then, so one read knows every node they hold. It is the reason SetProviders is
// only called before the start.
func (m *Manager) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	switch stage {
	case adapter.StartStateStart:
		if m.scope != nil {
			return nil
		}
		m.scope = scope
		m.scope.Add(m.Close)
	case adapter.StartStatePostStart:
		for _, check := range m.allCheckers() {
			check.startRestore()
		}
	}
	return nil
}

// allCheckers marks the post start as passed and returns every checker created so far. A
// checker created after this call reads its own states, see track and resolveShared.
func (m *Manager) allCheckers() []*HealthCheck {
	m.access.Lock()
	defer m.access.Unlock()
	m.started = true
	checks := make([]*HealthCheck, 0, len(m.checks)+len(m.managed))
	for _, check := range m.checks {
		checks = append(checks, check)
	}
	return append(checks, m.managed...)
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
	if m.track(check) {
		// the post start has passed, so this check is the one that reads its states
		check.startRestore()
	}
	return check, nil
}

func (m *Manager) resolveShared(tag string) (*HealthCheck, error) {
	m.access.Lock()
	check, loaded := m.checks[tag]
	if loaded {
		m.access.Unlock()
		return check, nil
	}
	define, defined := m.defines[tag]
	if !defined {
		m.access.Unlock()
		return nil, E.New("health check not found: ", tag)
	}
	options := define.Options()
	check = NewHealthCheck(m.ctx, m.router, m.om, &options, m.logFactory.NewLogger(F.ToString("healthcheck[", tag, "]")))
	err := check.Start()
	if err != nil {
		m.access.Unlock()
		return nil, E.Cause(err, "start health check[", tag, "]")
	}
	m.checks[tag] = check
	started := m.started
	m.access.Unlock()
	if started {
		// the post start has passed, so this check is the one that reads its states
		check.startRestore()
	}
	return check, nil
}

// track records a checker so the post start reads its states, and reports whether that start
// has already passed: the caller is then the one that has to read them.
func (m *Manager) track(check *HealthCheck) bool {
	m.access.Lock()
	defer m.access.Unlock()
	m.managed = append(m.managed, check)
	return m.started
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
