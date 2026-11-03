package option

import "github.com/sagernet/sing/common/json/badoption"

// LoadBalanceOutboundOptions is the options for balancer outbound
type LoadBalanceOutboundOptions struct {
	ProviderGroupCommonOption
	HealthCheck               *HealthCheckOptions         `json:"health_check,omitempty"`
	Pick                      LoadBalancePickOptions      `json:"pick,omitempty"`
	Profiles                  []LoadBalanceProfileOptions `json:"profiles,omitempty"`
	InterruptExistConnections bool                        `json:"interrupt_exist_connections,omitempty"`
}

// LoadBalancePickOptions is the options for balancer outbound picking
type LoadBalancePickOptions struct {
	// load balance objective
	Objective string `json:"objective,omitempty"`
	// pick strategy
	Strategy string `json:"strategy,omitempty"`
	// max acceptable failures
	MaxFail uint `json:"max_fail,omitempty"`
	// max acceptable rtt. defalut 0
	MaxRTT badoption.Duration `json:"max_rtt,omitempty"`
	// tolerance is the maximum absolute difference to the best node, for the objectives
	// that rank the nodes by a score: a node whose score is at most this far above the
	// best one is selected with it, and a node that just left the selection is kept while
	// the excess stays within the noise. A duration, e.g. 50ms. Default is 10ms for
	// steady and 50ms for quick. Requires the objective to be set to quick or steady.
	Tolerance badoption.Duration `json:"tolerance,omitempty"`
	// min_nodes is the minimum number of nodes to select. Default is 1.
	MinNodes uint `json:"min_nodes,omitempty"`
	// max_nodes is the maximum number of nodes to select, 0 means unlimited.
	MaxNodes uint `json:"max_nodes,omitempty"`
	// pick biases
	Biases []LoadBalancePickBias `json:"biases,omitempty"`
}

// LoadBalanceProfileOptions is the options for load balance profile
type LoadBalanceProfileOptions struct {
	Tag     string `json:"tag,omitempty"`
	Exclude string `json:"exclude,omitempty"`
	Include string `json:"include,omitempty"`
	LoadBalancePickOptions
}

// LoadBalancePickBias is the bias for load balance picking
type LoadBalancePickBias struct {
	MatchCondition
	RTTScale float32 `json:"rtt_scale,omitempty"`
}

// MatchCondition is the condition to match a node tag
type MatchCondition struct {
	Contains string `json:"contains,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	Suffix   string `json:"suffix,omitempty"`
	Regexp   string `json:"regexp,omitempty"`
}
