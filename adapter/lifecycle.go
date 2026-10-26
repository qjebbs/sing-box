package adapter

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/sagernet/sing-box/common/taskmonitor"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
)

type SimpleLifecycle interface {
	Start() error
	Close() error
}

type StartStage uint8

const (
	StartStateInitialize StartStage = iota
	StartStateStart
	StartStatePostStart
	StartStateStarted
)

var ListStartStages = []StartStage{
	StartStateInitialize,
	StartStateStart,
	StartStatePostStart,
	StartStateStarted,
}

func (s StartStage) String() string {
	switch s {
	case StartStateInitialize:
		return "initialize"
	case StartStateStart:
		return "start"
	case StartStatePostStart:
		return "post-start"
	case StartStateStarted:
		return "finish-start"
	default:
		panic("unknown stage")
	}
}

type Lifecycle interface {
	Start(stage StartStage, scope *Scope) error
}

type LifecycleService interface {
	Name() string
	Lifecycle
}

// scopedCleanup is a cleanup registered on a scope. component is set when the cleanup
// owns a child scope created by Start, so that Remove can detach it.
type scopedCleanup struct {
	component Lifecycle
	cleanup   func() error
}

// Scope owns the lifetime of the components started in it.
//
// Start lazily creates one child scope per component and registers the cleanup that
// closes it, so the scope tree is torn down bottom-up, in reverse registration order,
// after the scope context has been cancelled. Remove does the same for a single
// component, for objects created and destroyed at runtime.
//
// The rules below keep that model safe, for every Scope and every caller:
//   - A Scope never calls into a component while holding access: Start, Remove and
//     Close all release it before running component code.
//   - Callers must not hold their own locks across Start, Remove or Close: cleanups
//     run component code, which can block for seconds and can call back into the
//     caller, and mutexes are not reentrant.
//   - Add must not be used after Close, such a cleanup never runs.
type Scope struct {
	ctx      context.Context
	cancel   context.CancelFunc
	logger   log.ContextLogger
	access   sync.Mutex
	cleanups []scopedCleanup
	children map[Lifecycle]*Scope
	closed   bool
}

func NewScope(ctx context.Context, logger log.ContextLogger) *Scope {
	ctx, cancel := context.WithCancel(ctx)
	return &Scope{
		ctx:      ctx,
		cancel:   cancel,
		logger:   logger,
		children: make(map[Lifecycle]*Scope),
	}
}

func (s *Scope) Context() context.Context {
	return s.ctx
}

// Add registers a cleanup that runs when the scope is closed, in reverse registration
// order. It is a no-op after Close: the cleanup would never run, so it is dropped with
// a warning instead of leaking silently.
func (s *Scope) Add(cleanup func() error) {
	s.access.Lock()
	if s.closed {
		s.access.Unlock()
		if s.logger != nil {
			s.logger.Warn("cleanup added to a closed scope is ignored")
		}
		return
	}
	s.cleanups = append(s.cleanups, scopedCleanup{cleanup: cleanup})
	s.access.Unlock()
}

// Start runs one stage of component in its own child scope, creating that scope on
// the first stage.
//
// A failing stage is not rolled back here: the child scope stays registered, and the
// half-started component is only closed when the whole scope is closed. That is what
// an aborted startup wants, while callers that create components at runtime use
// StartRuntime, which detaches on failure. Starting on an already closed scope does
// nothing and returns the context error.
func (s *Scope) Start(name string, component Lifecycle, stage StartStage) error {
	s.access.Lock()
	err := s.ctx.Err()
	if err != nil {
		s.access.Unlock()
		return err
	}
	child, loaded := s.children[component]
	if !loaded {
		child = NewScope(s.ctx, s.logger)
		s.children[component] = child
		s.cleanups = append(s.cleanups, scopedCleanup{
			component: component,
			cleanup: func() error {
				done := LogElapsed(s.logger, "close ", name)
				monitor := taskmonitor.New(s.logger, C.StopTimeout)
				monitor.Start("close ", name)
				closeErr := child.Close()
				monitor.Finish()
				done()
				if closeErr != nil {
					return E.Cause(closeErr, "close ", name)
				}
				return nil
			},
		})
	}
	s.access.Unlock()
	done := LogElapsed(s.logger, stage, " ", name)
	monitor := taskmonitor.New(s.logger, C.StartTimeout)
	monitor.Start(stage, " ", name)
	err = component.Start(stage, child)
	monitor.Finish()
	done()
	if err != nil {
		return E.Cause(err, stage, " ", name)
	}
	return nil
}

// StartRuntime runs one stage of a component created and destroyed at runtime, like
// Start, but a failing stage also removes it from the scope: the caller has already
// dropped its own reference while the box keeps running, so nothing else would close
// it before shutdown.
func (s *Scope) StartRuntime(name string, component Lifecycle, stage StartStage) error {
	err := s.Start(name, component, stage)
	if err == nil {
		return nil
	}
	removeErr := s.Remove(component)
	if removeErr != nil {
		return E.Errors(err, E.Cause(removeErr, "remove ", name))
	}
	return err
}

// Remove closes and detaches the child scope created for component by Start. It is
// the counterpart of Start for components created and destroyed at runtime, whose
// lifetime must not last until the whole scope is closed, and the rollback behind
// StartRuntime.
//
// Components that were never started, or already removed, are ignored, so it is
// safe to call unconditionally.
func (s *Scope) Remove(component Lifecycle) error {
	s.access.Lock()
	child, loaded := s.children[component]
	if !loaded {
		s.access.Unlock()
		return nil
	}
	delete(s.children, component)
	for index, cleanup := range s.cleanups {
		if cleanup.component == component {
			s.cleanups = append(s.cleanups[:index], s.cleanups[index+1:]...)
			break
		}
	}
	s.access.Unlock()
	return child.Close()
}

func (s *Scope) Close() error {
	s.access.Lock()
	s.closed = true
	s.cancel()
	cleanups := s.cleanups
	s.cleanups = nil
	s.children = nil
	s.access.Unlock()
	var cleanupErrors []error
	for _, cleanup := range slices.Backward(cleanups) {
		cleanupErrors = append(cleanupErrors, E.Expand(cleanup.cleanup())...)
	}
	return E.Errors(common.Filter(cleanupErrors, func(it error) bool {
		return !E.IsClosed(it) && !E.IsCanceled(it)
	})...)
}

func LogElapsed(logger log.ContextLogger, description ...any) func() {
	prefix := F.ToString(description...)
	startTime := time.Now()
	timer := time.AfterFunc(time.Second, func() {
		logger.Trace(prefix, "...")
	})
	return func() {
		if timer.Stop() {
			return
		}
		logger.Trace(prefix, " completed (", F.Seconds(time.Since(startTime).Seconds()), "s)")
	}
}
