package group

import (
	"context"
	"math"
	"slices"
	"strings"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
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
	_ adapter.Outbound                   = (*URLTestProvider)(nil)
	_ adapter.URLTestGroup               = (*URLTestProvider)(nil)
	_ adapter.OutboundConnectionReporter = (*URLTestProvider)(nil)
	_ adapter.Referrer                   = (*URLTestProvider)(nil)
	_ adapter.InterfaceUpdateListener    = (*URLTestProvider)(nil)
	_ adapter.Lifecycle                  = (*URLTestProvider)(nil)
)

// urlTestObjectives are the objectives this group accepts. The pick is limited to one
// node, and only the least* objectives apply that limit: alive and qualified hand every
// node of their class to the strategy, which would pick a different one per connection.
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

// URLTestProvider is the URL test group: the load balance group under a fixed pick
// profile. It takes the node with the best score, one at a time, and replaces it as
// soon as another node scores better. The tolerance is not part of the pick: it only
// decides whether a node that just left it keeps the connections it already carries.
type URLTestProvider struct {
	*LoadBalance
}

func NewURLTestProvider(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.ProviderURLTestOptions) (adapter.Outbound, error) {
	objective, err := urlTestObjective(options.Objective)
	if err != nil {
		return nil, err
	}
	return &URLTestProvider{
		LoadBalance: newLoadBalance(ctx, router, logger, C.TypeURLTest, tag, option.LoadBalanceOutboundOptions{
			ProviderGroupCommonOption: options.ProviderGroupCommonOption,
			HealthCheck:               options.HealthCheck,
			InterruptExistConnections: options.InterruptExistConnections,
			Pick: option.LoadBalancePickOptions{
				Objective: objective,
				Tolerance: options.Tolerance,
				MinNodes:  1,
				MaxNodes:  1,
				// The failures are already priced into the score of a node, in its own
				// scale, see healthcheck.Stats.Cost. Qualifying on them would rank a
				// node that failed once below one that never failed and is several
				// times slower, which is what max_fail is not for here.
				MaxFail: math.MaxUint32,
				// MaxRTT is set to 0 to indicate that there is no upper limit on the round-trip time.
				MaxRTT: 0,
			},
		}),
	}, nil
}

// All implements adapter.OutboundGroup
func (g *URLTestProvider) All() []string {
	all := g.Nodes(N.NetworkTCP)
	return common.Map(all, func(node *balancer.Node) string {
		return node.Tag()
	})
}
