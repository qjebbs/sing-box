package option

import "github.com/sagernet/sing/common/json/badoption"

// LoadBalanceOutboundOptions is the options for balancer outbound
type LoadBalanceOutboundOptions struct {
	ProviderGroupCommonOption
	Checker  string                      `json:"checker,omitempty"`
	Pick     LoadBalancePickOptions      `json:"pick,omitempty"`
	Profiles []LoadBalanceProfileOptions `json:"profiles,omitempty"`
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
	// expected nodes count to select
	Expected uint `json:"expected,omitempty"`
	// ping rtt baselines
	Baselines []badoption.Duration `json:"baselines,omitempty"`
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
