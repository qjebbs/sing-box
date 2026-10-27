package option

import "github.com/sagernet/sing/common/json/badoption"

// LoadBalanceOutboundOptions is the options for balancer outbound
type LoadBalanceOutboundOptions struct {
	ProviderGroupCommonOption
	HealthCheck               *HealthCheckOptions         `json:"health_check,omitempty"`
	Pick                      LoadBalancePickOptions      `json:"pick,omitempty"`
	Profiles                  []LoadBalanceProfileOptions `json:"profiles,omitempty"`
	LogHealth                 bool                        `json:"log_health,omitempty"`
	InterruptExistConnections bool                        `json:"interrupt_exist_connections,omitempty"`
}

// LoadBalanceProfileOutboundOptions is the options for load balance profile
type LoadBalanceProfileOutboundOptions struct {
	LoadBalanceTag string                 `json:"loadbalance_tag,omitempty"`
	Exclude        string                 `json:"exclude,omitempty"`
	Include        string                 `json:"include,omitempty"`
	Pick           LoadBalancePickOptions `json:"pick,omitempty"`
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
	// tolerance is the maximum relative difference to the best node, e.g. 0.2 means
	// nodes up to 20% worse than the best one are selected too. Default is 0.2.
	Tolerance float32 `json:"tolerance,omitempty"`
	// min_nodes is the minimum number of nodes to select. Default is 1.
	MinNodes uint `json:"min_nodes,omitempty"`
	// max_nodes is the maximum number of nodes to select, 0 means unlimited.
	MaxNodes uint `json:"max_nodes,omitempty"`
	// jitter_weight scales the standard deviation added to the score. Default is 1,
	// it is not used by leastload, whose score is the standard deviation itself.
	JitterWeight float32 `json:"jitter_weight,omitempty"`
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
