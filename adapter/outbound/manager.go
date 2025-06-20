package outbound

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/taskmonitor"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	N "github.com/sagernet/sing/common/network"
)

var _ adapter.OutboundManager = (*Manager)(nil)

type Manager struct {
	logger                  log.ContextLogger
	registry                adapter.OutboundRegistry
	endpoint                adapter.EndpointManager
	defaultTag              string
	access                  sync.Mutex
	started                 bool
	stage                   adapter.StartStage
	outbounds               []adapter.Outbound
	outboundByTag           map[string]adapter.Outbound
	dependByTag             map[string][]string
	defaultOutbound         adapter.Outbound
	defaultOutboundFallback adapter.Outbound

	confByTag map[string]*confItem
}

type confItem struct {
	typ     string
	options any
}

func NewManager(logger logger.ContextLogger, registry adapter.OutboundRegistry, endpoint adapter.EndpointManager, defaultTag string) *Manager {
	return &Manager{
		logger:        logger,
		registry:      registry,
		endpoint:      endpoint,
		defaultTag:    defaultTag,
		outboundByTag: make(map[string]adapter.Outbound),
		dependByTag:   make(map[string][]string),

		confByTag: make(map[string]*confItem),
	}
}

func (m *Manager) Initialize(defaultOutboundFallback adapter.Outbound) {
	m.defaultOutboundFallback = defaultOutboundFallback
}

func (m *Manager) Start(stage adapter.StartStage) error {
	m.access.Lock()
	if m.started && m.stage >= stage {
		panic("already started")
	}
	m.started = true
	m.stage = stage
	outbounds := m.outbounds
	m.access.Unlock()
	if stage == adapter.StartStateStart {
		if m.defaultTag != "" && m.defaultOutbound == nil {
			defaultEndpoint, loaded := m.endpoint.Get(m.defaultTag)
			if !loaded {
				return E.New("default outbound not found: ", m.defaultTag)
			}
			m.defaultOutbound = defaultEndpoint
		}
		return m.startOutbounds(append(outbounds, common.Map(m.endpoint.Endpoints(), func(it adapter.Endpoint) adapter.Outbound { return it })...))
	} else {
		for _, outbound := range outbounds {
			err := adapter.LegacyStart(outbound, stage)
			if err != nil {
				return E.Cause(err, stage, " outbound/", outbound.Type(), "[", outbound.Tag(), "]")
			}
		}
	}
	return nil
}

func (m *Manager) startOutbounds(outbounds []adapter.Outbound) error {
	monitor := taskmonitor.New(m.logger, C.StartTimeout)
	started := make(map[string]bool)
	for {
		canContinue := false
	startOne:
		for _, outboundToStart := range outbounds {
			outboundTag := outboundToStart.Tag()
			if started[outboundTag] {
				continue
			}
			dependencies := outboundToStart.Dependencies()
			for _, dependency := range dependencies {
				if !started[dependency] {
					continue startOne
				}
			}
			started[outboundTag] = true
			canContinue = true
			if starter, isStarter := outboundToStart.(adapter.Lifecycle); isStarter {
				monitor.Start("start outbound/", outboundToStart.Type(), "[", outboundTag, "]")
				err := starter.Start(adapter.StartStateStart)
				monitor.Finish()
				if err != nil {
					return E.Cause(err, "start outbound/", outboundToStart.Type(), "[", outboundTag, "]")
				}
			} else if starter, isStarter := outboundToStart.(interface {
				Start() error
			}); isStarter {
				monitor.Start("start outbound/", outboundToStart.Type(), "[", outboundTag, "]")
				err := starter.Start()
				monitor.Finish()
				if err != nil {
					return E.Cause(err, "start outbound/", outboundToStart.Type(), "[", outboundTag, "]")
				}
			}
		}
		if len(started) == len(outbounds) {
			break
		}
		if canContinue {
			continue
		}
		currentOutbound := common.Find(outbounds, func(it adapter.Outbound) bool {
			return !started[it.Tag()]
		})
		var lintOutbound func(oTree []string, oCurrent adapter.Outbound) error
		lintOutbound = func(oTree []string, oCurrent adapter.Outbound) error {
			problemOutboundTag := common.Find(oCurrent.Dependencies(), func(it string) bool {
				return !started[it]
			})
			if common.Contains(oTree, problemOutboundTag) {
				return E.New("circular outbound dependency: ", strings.Join(oTree, " -> "), " -> ", problemOutboundTag)
			}
			m.access.Lock()
			problemOutbound := m.outboundByTag[problemOutboundTag]
			m.access.Unlock()
			if problemOutbound == nil {
				return E.New("dependency[", problemOutboundTag, "] not found for outbound[", oCurrent.Tag(), "]")
			}
			return lintOutbound(append(oTree, problemOutboundTag), problemOutbound)
		}
		return lintOutbound([]string{currentOutbound.Tag()}, currentOutbound)
	}
	return nil
}

func (m *Manager) Close() error {
	monitor := taskmonitor.New(m.logger, C.StopTimeout)
	m.access.Lock()
	if !m.started {
		m.access.Unlock()
		return nil
	}
	m.started = false
	outbounds := m.outbounds
	m.outbounds = nil
	m.outboundByTag = make(map[string]adapter.Outbound)
	m.access.Unlock()
	var err error
	for _, outbound := range outbounds {
		if closer, isCloser := outbound.(io.Closer); isCloser {
			monitor.Start("close outbound/", outbound.Type(), "[", outbound.Tag(), "]")
			err = E.Append(err, closer.Close(), func(err error) error {
				return E.Cause(err, "close outbound/", outbound.Type(), "[", outbound.Tag(), "]")
			})
			monitor.Finish()
		}
	}
	return nil
}

func (m *Manager) Outbounds() []adapter.Outbound {
	m.access.Lock()
	defer m.access.Unlock()
	return m.outbounds
}

func (m *Manager) Outbound(tag string) (adapter.Outbound, bool) {
	m.access.Lock()
	outbound, found := m.outboundByTag[tag]
	m.access.Unlock()
	if found {
		return outbound, true
	}
	return m.endpoint.Get(tag)
}

func (m *Manager) Default() adapter.Outbound {
	m.access.Lock()
	defer m.access.Unlock()
	if m.defaultOutbound != nil {
		return m.defaultOutbound
	} else {
		return m.defaultOutboundFallback
	}
}

func (m *Manager) Remove(tag string) error {
	m.access.Lock()
	outbound, found := m.outboundByTag[tag]
	if !found {
		m.access.Unlock()
		return os.ErrInvalid
	}
	delete(m.outboundByTag, tag)
	index := common.Index(m.outbounds, func(it adapter.Outbound) bool {
		return it == outbound
	})
	if index == -1 {
		panic("invalid inbound index")
	}
	m.outbounds = append(m.outbounds[:index], m.outbounds[index+1:]...)
	started := m.started
	if m.defaultOutbound == outbound {
		if len(m.outbounds) > 0 {
			m.defaultOutbound = m.outbounds[0]
			m.logger.Info("updated default outbound to ", m.defaultOutbound.Tag())
		} else {
			m.defaultOutbound = nil
		}
	}
	dependBy := m.dependByTag[tag]
	if len(dependBy) > 0 {
		return E.New("outbound[", tag, "] is depended by ", strings.Join(dependBy, ", "))
	}
	dependencies := outbound.Dependencies()
	for _, dependency := range dependencies {
		if len(m.dependByTag[dependency]) == 1 {
			delete(m.dependByTag, dependency)
		} else {
			m.dependByTag[dependency] = common.Filter(m.dependByTag[dependency], func(it string) bool {
				return it != tag
			})
		}
	}
	m.access.Unlock()
	if started {
		return common.Close(outbound)
	}
	return nil
}

func (m *Manager) Create(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, outboundType string, options any) error {
	if tag == "" {
		return os.ErrInvalid
	}
	outbound, err := m.registry.CreateOutbound(ctx, router, logger, tag, outboundType, options)
	if err != nil {
		return err
	}
	m.access.Lock()
	defer m.access.Unlock()
	if m.started {
		for _, stage := range adapter.ListStartStages {
			err = adapter.LegacyStart(outbound, stage)
			if err != nil {
				return E.Cause(err, stage, " outbound/", outbound.Type(), "[", outbound.Tag(), "]")
			}
		}
	}
	if existsOutbound, loaded := m.outboundByTag[tag]; loaded {
		if m.started {
			err = common.Close(existsOutbound)
			if err != nil {
				return E.Cause(err, "close outbound/", existsOutbound.Type(), "[", existsOutbound.Tag(), "]")
			}
		}
		existsIndex := common.Index(m.outbounds, func(it adapter.Outbound) bool {
			return it == existsOutbound
		})
		if existsIndex == -1 {
			panic("invalid inbound index")
		}
		m.outbounds = append(m.outbounds[:existsIndex], m.outbounds[existsIndex+1:]...)
	}
	m.outbounds = append(m.outbounds, outbound)
	m.outboundByTag[tag] = outbound
	dependencies := outbound.Dependencies()
	for _, dependency := range dependencies {
		m.dependByTag[dependency] = append(m.dependByTag[dependency], tag)
	}
	if tag == m.defaultTag || (m.defaultTag == "" && m.defaultOutbound == nil) {
		m.defaultOutbound = outbound
		if m.started {
			m.logger.Info("updated default outbound to ", outbound.Tag())
		}
	}
	m.confByTag[tag] = &confItem{
		typ:     outboundType,
		options: options,
	}
	return nil
}

// DupOverrideDetour duplicates the outbound with the specified tag and sets the override and detour for the duplicated outbound.
// The original outbound is not affected.
// The duplicated outbound is not managed by the manager, you should close it manually.
func (m *Manager) DupOverrideDetour(ctx context.Context, router adapter.Router, tag string, logger log.ContextLogger, detour N.Dialer) (adapter.Outbound, error) {
	m.access.Lock()
	defer m.access.Unlock()
	conf, found := m.confByTag[tag]
	if !found {
		return nil, os.ErrInvalid
	}
	// It's hacky here, works only if all outbound creations invoke dialer.New()
	ctx, used := dialer.ContextWithDetourOverride(ctx, detour)
	outbound, err := m.registry.CreateOutbound(ctx, router, logger, tag, conf.typ, conf.options)
	if err != nil {
		return nil, err
	}
	if !used() {
		return nil, E.New("[" + tag + "] detour not overridable")
	}
	for _, stage := range adapter.ListStartStages {
		err = adapter.LegacyStart(outbound, stage)
		if err != nil {
			return nil, E.Cause(err, stage, " outbound/", outbound.Type(), "[", outbound.Tag(), "]")
		}
	}
	return outbound, nil
}
