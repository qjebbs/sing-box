package group

import (
	"context"
	"math"
	"slices"
	"strings"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/healthcheck"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group/balancer"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	N "github.com/sagernet/sing/common/network"
)

func RegisterURLTestProvider(registry *outbound.Registry) {
	outbound.Register(registry, C.TypeURLTest, NewURLTestProvider)
}

var (
	_ adapter.Outbound                = (*URLTestProvider)(nil)
	_ adapter.URLTestGroup            = (*URLTestProvider)(nil)
	_ adapter.OutboundFailureReporter = (*URLTestProvider)(nil)
	_ adapter.Referrer                = (*URLTestProvider)(nil)
	_ adapter.InterfaceUpdateListener = (*URLTestProvider)(nil)
	_ adapter.Lifecycle               = (*URLTestProvider)(nil)
)

// urlTestObjectives are the objectives this group accepts. It uses one node at a time,
// which only the least* objectives rank: alive and qualified hand every node of their
// class to the selection, which would then have to break the tie itself.
var urlTestObjectives = []string{balancer.ObjectiveLeastPing, balancer.ObjectiveLeastLoad}

// urlTestObjective validates the configured objective, defaulting to leastping.
func urlTestObjective(objective string) (string, error) {
	if objective == "" {
		return balancer.ObjectiveLeastPing, nil
	}
	if !slices.Contains(urlTestObjectives, objective) {
		return "", E.New(
			"unsupported objective for ", C.TypeURLTest, ": ", objective,
			", expected one of ", strings.Join(urlTestObjectives, ", "),
		)
	}
	return objective, nil
}

// URLTestProvider is the url test group. It uses one node at a time, and replaces it
// only when another one is better by more than the tolerance: a change takes every new
// connection with it, so a difference that is not worth it keeps the node where it is.
//
// It shares the nodes, their health and the cache with the load balance group, and
// differs in the selection alone, see balancer.Single.
type URLTestProvider struct {
	groupBase
	options   option.ProviderURLTestOptions
	objective string
}

func NewURLTestProvider(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.ProviderURLTestOptions) (adapter.Outbound, error) {
	objective, err := urlTestObjective(options.Objective)
	if err != nil {
		return nil, err
	}
	return &URLTestProvider{
		groupBase: newGroupBase(
			ctx, logger, C.TypeURLTest, tag,
			options.ProviderGroupCommonOption, options.InterruptExistConnections,
		),
		options:   options,
		objective: objective,
	}, nil
}

// Start implements adapter.Lifecycle
func (s *URLTestProvider) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	return s.start(stage, scope, s.options.HealthCheck, func(check *healthcheck.HealthCheck) (*balancer.Balancer, error) {
		objective, err := balancer.NewObjective(s.objective, balancer.ObjectiveOptions{
			Tolerance: s.options.Tolerance,
		})
		if err != nil {
			return nil, err
		}
		return balancer.New(s.logger, &s.GroupAdapter, check, balancer.NodeOptions{
			// The failures are already priced into the score of a node, in its own
			// scale, see healthcheck.Stats.Cost. Qualifying on them would rank a node
			// that failed once below one that never failed and is several times
			// slower, which is what max_fail is not for here.
			MaxFail: math.MaxUint32,
			// A url test group has no upper limit on the round-trip time.
			MaxRTT: 0,
		}, balancer.NewSingle(objective, s.options.Tolerance))
	})
}

// All implements adapter.OutboundGroup
func (s *URLTestProvider) All() []string {
	return common.Map(s.Nodes(N.NetworkTCP), func(node *balancer.Node) string {
		return node.Tag()
	})
}
