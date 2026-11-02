package group

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/healthcheck"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group/balancer"
	"github.com/sagernet/sing/common"
)

func RegisterLoadBalance(registry *outbound.Registry) {
	outbound.Register(registry, C.TypeLoadBalance, NewLoadBalance)
}

var (
	_ adapter.Outbound                = (*LoadBalance)(nil)
	_ adapter.URLTestGroup            = (*LoadBalance)(nil)
	_ adapter.OutboundFailureReporter = (*LoadBalance)(nil)
	_ adapter.Referrer                = (*LoadBalance)(nil)
	_ adapter.Lifecycle               = (*LoadBalance)(nil)
	_ adapter.InterfaceUpdateListener = (*LoadBalance)(nil)
)

// LoadBalance is a group that spreads the connections it carries across the nodes that
// are equally good, and picks one of them for each connection by its strategy.
type LoadBalance struct {
	groupBase
	options option.LoadBalanceOutboundOptions
}

// NewLoadBalance creates a new load balance outbound
func NewLoadBalance(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.LoadBalanceOutboundOptions) (adapter.Outbound, error) {
	return &LoadBalance{
		groupBase: newGroupBase(
			ctx, logger, C.TypeLoadBalance, tag,
			options.ProviderGroupCommonOption, options.InterruptExistConnections,
		),
		options: options,
	}, nil
}

// Start implements adapter.Lifecycle
func (s *LoadBalance) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	return s.start(stage, scope, s.options.HealthCheck, func(check *healthcheck.HealthCheck) (*balancer.Balancer, error) {
		objective, err := balancer.NewObjective(s.options.Pick.Objective, balancer.ObjectiveOptions{
			JitterWeight: s.options.Pick.JitterWeight,
			Tolerance:    s.options.Pick.Tolerance,
			MinNodes:     s.options.Pick.MinNodes,
			MaxNodes:     s.options.Pick.MaxNodes,
		})
		if err != nil {
			return nil, err
		}
		strategy, err := balancer.NewStrategy(s.options.Pick.Strategy, s.options.Pick.Objective)
		if err != nil {
			return nil, err
		}
		return balancer.New(s.logger, &s.GroupAdapter, check, balancer.NodeOptions{
			MaxRTT:  healthcheck.RTT(s.options.Pick.MaxRTT.Build().Milliseconds()),
			MaxFail: s.options.Pick.MaxFail,
			Biases:  s.options.Pick.Biases,
		}, balancer.NewBalancing(objective, strategy))
	})
}

// All implements adapter.OutboundGroup
func (s *LoadBalance) All() []string {
	_, filtered := s.GetNodes()
	return common.Map(filtered, func(node *balancer.Node) string {
		return node.Tag()
	})
}
