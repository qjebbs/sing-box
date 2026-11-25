package group

import (
	"context"
	"slices"
	"strings"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group/base"
	"github.com/sagernet/sing-box/protocol/group/selection"
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
// which only quick and idle rank: alive and qualified hand every node of their class to
// the selection, which would then have to break the tie itself.
var urlTestObjectives = []string{selection.ObjectiveQuick, selection.ObjectiveIdle}

// urlTestObjective validates the configured objective and returns it, defaulting to idle.
func urlTestObjective(options option.ProviderURLTestOptions) (string, error) {
	if options.Objective != "" && !slices.Contains(urlTestObjectives, options.Objective) {
		return "", E.New(
			"unsupported objective for ", C.TypeURLTest, ": ", options.Objective,
			", expected one of ", strings.Join(urlTestObjectives, ", "),
		)
	}
	if options.Objective == "" {
		return selection.ObjectiveIdle, nil
	}
	return options.Objective, nil
}

// URLTestProvider is the url test group. It uses one node at a time, and replaces it
// only when another one is better by more than the tolerance: a change takes every new
// connection with it, so a difference that is not worth it keeps the node where it is.
//
// It shares the nodes, their health and the cache with the load balance group, and
// differs in the selection alone, see base.Single.
type URLTestProvider struct {
	*base.Group
	options   option.ProviderURLTestOptions
	objective string
}

func NewURLTestProvider(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.ProviderURLTestOptions) (adapter.Outbound, error) {
	objective, err := urlTestObjective(options)
	if err != nil {
		return nil, err
	}
	return &URLTestProvider{
		Group: base.NewGroup(
			ctx, logger, C.TypeURLTest, tag,
			options.ProviderGroupCommonOption, options.InterruptExistConnections,
		),
		options:   options,
		objective: objective,
	}, nil
}

// Start implements adapter.Lifecycle
func (s *URLTestProvider) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	objective, err := selection.NewObjective(s.objective, selection.ObjectiveOptions{
		Tolerance: s.options.Tolerance.Build(),
	})
	if err != nil {
		return err
	}
	return s.Group.Start(stage, scope, s.options.HealthCheck,
		objective.Name() == selection.ObjectiveIdle,
		selection.NewSingle(objective),
		selection.NodeOptions{
			// A url test group has no upper limit on the round-trip time. The recent
			// failure weight still gates it: a node that just failed is not a candidate
			// until a check passes, which is how a failed test removed a node upstream.
			MaxRTT: 0,
		},
	)
}

// All implements adapter.OutboundGroup
func (s *URLTestProvider) All() []string {
	return common.Map(s.Nodes(N.NetworkTCP), func(node *selection.Node) string {
		return node.Tag()
	})
}
