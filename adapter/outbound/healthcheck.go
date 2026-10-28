package outbound

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
)

// AttachHealthCheck submits the providers of the group to the health check resolved
// from options, so the nodes of the group are checked, and share the check with the
// other groups using it. listener is called after every check round, so the group
// can update its selection.
//
// InitProviders must be called first: the providers of the group are what the check
// is submitted to.
func (a *GroupAdapter) AttachHealthCheck(
	ctx context.Context,
	logger log.ContextLogger,
	manager adapter.HealthCheckManager,
	options *option.HealthCheckOptions,
	listener func(),
) (*healthcheck.HealthCheck, error) {
	if options == nil || options.IsEmpty() {
		return nil, E.New(a.Type(), " requires 'health_check' in options")
	}
	checker, err := manager.Get(ctx, logger, *options)
	if err != nil {
		return nil, err
	}
	if err = checker.SetProviders(a.Tag(), a.Providers()); err != nil {
		return nil, err
	}
	checker.RegisterPostCheckListener(listener)
	healthCheck, ok := checker.(*healthcheck.HealthCheck)
	if !ok {
		return nil, E.New("health check is not a health check")
	}
	return healthCheck, nil
}

// DetachHealthCheck unregisters the listener and the providers of the group from the
// health check. A nil check is accepted, so it can be called on a group that never
// started.
func (a *GroupAdapter) DetachHealthCheck(check *healthcheck.HealthCheck, listener func()) error {
	if check == nil {
		return nil
	}
	check.UnregisterPostCheckListener(listener)
	return check.RemoveProviders(a.Tag())
}
