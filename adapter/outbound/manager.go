package outbound

import (
	"context"
	"os"
	"reflect"
	"strings"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	N "github.com/sagernet/sing/common/network"
)

var _ adapter.OutboundManager = (*Manager)(nil)

type Manager struct {
	registry                adapter.OutboundRegistry
	endpoint                adapter.EndpointManager
	defaultTag              string
	access                  sync.RWMutex
	outbounds               []adapter.Outbound
	outboundByTag           map[string]adapter.Outbound
	defaultOutbound         adapter.Outbound
	defaultOutboundFallback func() (adapter.Outbound, error)

	scope   *adapter.Scope
	started bool
	// isInternalByTag keeps track of which outbounds are internal.
	// In other words, internal outbounds are those managed by the "outbounds" section of configuration,
	// they are created before starting the manager.
	isInternalByTag map[string]struct{}
	confByTag       map[string]*confItem
}

type confItem struct {
	typ     string
	options any
}

func NewManager(registry adapter.OutboundRegistry, endpoint adapter.EndpointManager, defaultTag string) *Manager {
	return &Manager{
		registry:      registry,
		endpoint:      endpoint,
		defaultTag:    defaultTag,
		outboundByTag: make(map[string]adapter.Outbound),

		isInternalByTag: make(map[string]struct{}),
		confByTag:       make(map[string]*confItem),
	}
}

func (m *Manager) Initialize(defaultOutboundFallback func() (adapter.Outbound, error)) {
	m.defaultOutboundFallback = defaultOutboundFallback
}

func (m *Manager) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	m.access.Lock()
	if stage == adapter.StartStateInitialize {
		m.scope = scope
		if m.defaultTag != "" && m.defaultOutbound == nil {
			defaultEndpoint, loaded := m.endpoint.Get(m.defaultTag)
			if !loaded {
				m.access.Unlock()
				return E.New("default outbound not found: ", m.defaultTag)
			}
			m.defaultOutbound = defaultEndpoint
		}
		if m.defaultOutbound == nil {
			directOutbound, err := m.defaultOutboundFallback()
			if err != nil {
				m.access.Unlock()
				return E.Cause(err, "create direct outbound for fallback")
			}
			m.outbounds = append(m.outbounds, directOutbound)
			m.outboundByTag[directOutbound.Tag()] = directOutbound
			// The fallback outbound is created before the manager starts, so it is internal
			// as well and is started stage by stage below.
			m.isInternalByTag[directOutbound.Tag()] = struct{}{}
			m.defaultOutbound = directOutbound
		}
	}
	if stage == adapter.StartStateStart {
		m.started = true
	}
	outbounds := m.outbounds
	m.access.Unlock()
	if stage == adapter.StartStateStart {
		return m.startOutbounds(scope, append(outbounds, common.Map(m.endpoint.Endpoints(), func(it adapter.Endpoint) adapter.Outbound { return it })...))
	}
	// Outbounds created at runtime are already started by Create, which replays every stage
	// in their own child scope. Starting them again here would double-start them and register
	// duplicate cleanups, so this loop only covers internal outbounds.
	for _, outbound := range outbounds {
		if !m.isInternal(outbound.Tag()) {
			continue
		}
		lifecycle, isLifecycle := outbound.(adapter.Lifecycle)
		if !isLifecycle {
			continue
		}
		name := "outbound/" + outbound.Type() + "[" + outbound.Tag() + "]"
		err := scope.Start(name, lifecycle, stage)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) startOutbounds(scope *adapter.Scope, outbounds []adapter.Outbound) error {
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
			if endpoint, isEndpoint := outboundToStart.(adapter.Endpoint); isEndpoint {
				// Upstream's adapter.Endpoint is a structural interface, so every new-style
				// outbound satisfies it. Only the objects actually registered in the endpoint
				// manager should be started through it, the rest are plain outbounds below.
				if registered, loaded := m.endpoint.Get(outboundTag); loaded && registered == endpoint {
					err := m.endpoint.StartEndpoint(endpoint)
					if err != nil {
						return err
					}
					continue
				}
			}
			lifecycle, isLifecycle := outboundToStart.(adapter.Lifecycle)
			if !isLifecycle {
				continue
			}
			name := "outbound/" + outboundToStart.Type() + "[" + outboundTag + "]"
			err := scope.Start(name, lifecycle, adapter.StartStateStart)
			if err != nil {
				return err
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
			problemOutbound := common.Find(outbounds, func(it adapter.Outbound) bool {
				return it.Tag() == problemOutboundTag
			})
			if problemOutbound == nil {
				return E.New("dependency[", problemOutboundTag, "] not found for outbound[", oCurrent.Tag(), "]")
			}
			return lintOutbound(append(oTree, problemOutboundTag), problemOutbound)
		}
		return lintOutbound([]string{currentOutbound.Tag()}, currentOutbound)
	}
	return nil
}

func (m *Manager) Outbounds() []adapter.Outbound {
	m.access.RLock()
	defer m.access.RUnlock()
	return m.outbounds
}

func (m *Manager) Outbound(tag string) (adapter.Outbound, bool) {
	m.access.RLock()
	outbound, found := m.outboundByTag[tag]
	m.access.RUnlock()
	if found {
		return outbound, true
	}
	return m.endpoint.Get(tag)
}

func (m *Manager) Default() adapter.Outbound {
	m.access.RLock()
	defer m.access.RUnlock()
	return m.defaultOutbound
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
	if !m.started {
		m.isInternalByTag[tag] = struct{}{}
		err = m.register(tag, outbound, outboundType, options)
		m.access.Unlock()
		return err
	}
	if m.isInternal(tag) {
		m.access.Unlock()
		return E.New("cannot create outbound with a internal tag ", tag, " after manager has started")
	}
	m.access.Unlock()
	// Outbounds created at runtime are children of the manager's own scope: Remove(tag)
	// detaches them individually, while the scope tree still closes any leftover on shutdown.
	// StartRuntime also detaches one whose start failed, since the caller here has already
	// dropped its reference.
	// This runs outside of m.access on purpose: Start may call back into the manager or block
	// on other locks of the box, and m.access is not reentrant.
	if lifecycle, isLifecycle := outbound.(adapter.Lifecycle); isLifecycle {
		name := "outbound/" + outbound.Type() + "[" + tag + "]"
		for _, stage := range adapter.ListStartStages {
			err = m.scope.StartRuntime(name, lifecycle, stage)
			if err != nil {
				return err
			}
		}
	}
	// Replace the outbound with the same tag, if any. The old one is detached before the new
	// one is registered, and its teardown is delayed until m.access is released, see below.
	m.access.Lock()
	existsOutbound, replaced := m.take(tag)
	err = m.register(tag, outbound, outboundType, options)
	if err == nil && replaced && existsOutbound == m.defaultOutbound {
		// Keep the default pointing at a live object: the replacement takes its place.
		// It must not be cleared to nil, the default outbound is dereferenced unconditionally.
		m.defaultOutbound = outbound
	}
	m.access.Unlock()
	if err != nil {
		return err
	}
	if !replaced {
		return nil
	}
	// The replaced outbound is torn down outside of m.access, for the same reason the startup
	// above runs unlocked: the cleanups may block for seconds and may call back into the manager.
	existsLifecycle, isLifecycle := existsOutbound.(adapter.Lifecycle)
	if !isLifecycle {
		return nil
	}
	err = m.scope.Remove(existsLifecycle)
	if err != nil {
		return E.Cause(err, "close outbound [", tag, "]")
	}
	return nil
}

// take detaches an outbound from the registry, the caller must hold m.access.
func (m *Manager) take(tag string) (adapter.Outbound, bool) {
	outbound, found := m.outboundByTag[tag]
	if !found {
		return nil, false
	}
	delete(m.outboundByTag, tag)
	index := common.Index(m.outbounds, func(it adapter.Outbound) bool {
		return it == outbound
	})
	if index == -1 {
		panic("invalid outbound index")
	}
	m.outbounds = append(m.outbounds[:index], m.outbounds[index+1:]...)
	return outbound, true
}

// Remove detaches and closes an outbound, m.access must not be held.
func (m *Manager) Remove(tag string) error {
	m.access.Lock()
	outbound, found := m.outboundByTag[tag]
	if !found {
		m.access.Unlock()
		return os.ErrInvalid
	}
	if m.isInternal(tag) {
		m.access.Unlock()
		return E.New("cannot remove internal outbound with tag ", tag)
	}
	m.take(tag)
	started := m.started
	m.access.Unlock()
	if !started {
		return nil
	}
	// The outbound lives in the manager's scope: removing it cancels its context and runs
	// its cleanups in reverse order, so there is no separate Close to call here.
	//
	// This is done outside of m.access on purpose: the cleanups may block for seconds (closing
	// transports, waiting for goroutines) and may call back into this manager, and m.access is
	// not reentrant.
	lifecycle, isLifecycle := outbound.(adapter.Lifecycle)
	if !isLifecycle {
		return nil
	}
	if err := m.scope.Remove(lifecycle); err != nil {
		return E.Cause(err, "close outbound [", tag, "]")
	}
	return nil
}

// register records a created (and already started) outbound, the caller must hold m.access.
func (m *Manager) register(tag string, outbound adapter.Outbound, outboundType string, options any) error {
	_, loaded := m.outboundByTag[tag]
	if loaded {
		return E.New("duplicate outbound tag: ", tag)
	}
	m.outbounds = append(m.outbounds, outbound)
	m.outboundByTag[tag] = outbound
	if tag == m.defaultTag || (m.defaultTag == "" && m.defaultOutbound == nil) {
		m.defaultOutbound = outbound
	}
	m.confByTag[tag] = &confItem{
		typ:     outboundType,
		options: options,
	}
	return nil
}

func (m *Manager) isInternal(tag string) bool {
	_, internal := m.isInternalByTag[tag]
	return internal
}

// CreateDetour creates a copy of the outbound with the specified tag, dialing through detour
// instead of its configured detour. The copy is not managed by the manager: it lives in the scope
// provided by the caller, which is responsible for closing it. When its start fails it is
// detached from that scope right away instead, it is not a running component the caller could
// use.
//
// The original outbound is not affected: the options are copied, so neither the configuration nor
// the running instance is shared. The detour is handed over as a dialer rather than as a tag,
// because the copy is not registered and has no tag to be looked up by.
func (m *Manager) CreateDetour(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, detour N.Dialer, scope *adapter.Scope) (adapter.Outbound, error) {
	m.access.RLock()
	conf, found := m.confByTag[tag]
	m.access.RUnlock()
	if !found {
		return nil, E.New("[", tag, "] is not an outbound that can be chained")
	}
	if detour == nil {
		return nil, E.New("missing detour for [", tag, "]")
	}
	options, err := conf.detourOptions(detour)
	if err != nil {
		return nil, err
	}
	outbound, err := m.registry.CreateOutbound(ctx, router, logger, tag, conf.typ, options)
	if err != nil {
		return nil, err
	}
	if lifecycle, isLifecycle := outbound.(adapter.Lifecycle); isLifecycle {
		name := "outbound/" + outbound.Type() + "[" + tag + "]"
		for _, stage := range adapter.ListStartStages {
			err = scope.StartRuntime(name, lifecycle, stage)
			if err != nil {
				return nil, err
			}
		}
	}
	return outbound, nil
}

// detourOptions copies the options of the outbound and points their detour at detour.
func (c *confItem) detourOptions(detour N.Dialer) (any, error) {
	value := reflect.ValueOf(c.options)
	if value.Kind() != reflect.Ptr || value.IsNil() {
		return nil, E.New("outbound type ", c.typ, " has no options to set a detour on")
	}
	// Only the top level is copied: the detour is the single field replaced, so the nested
	// values can keep being shared with the configuration the copy came from.
	cloned := reflect.New(value.Type().Elem())
	cloned.Elem().Set(value.Elem())
	dialerOptionsWrapper, isDialerOptionsWrapper := cloned.Interface().(option.DialerOptionsWrapper)
	if !isDialerOptionsWrapper {
		return nil, &adapter.NoDialFieldsError{OutboundType: c.typ}
	}
	dialerOptions := dialerOptionsWrapper.TakeDialerOptions()
	dialerOptions.Detour = ""
	dialerOptions.DetourDialer = detour
	dialerOptionsWrapper.ReplaceDialerOptions(dialerOptions)
	return cloned.Interface(), nil
}
