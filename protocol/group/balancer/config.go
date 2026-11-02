package balancer

import (
	"errors"
	"regexp"

	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
)

type balancerConfig struct {
	option.LoadBalancePickOptions
	maxRTT      healthcheck.RTT
	maxFailRate float32
	pickBiases  []pickBias
}

type pickBias struct {
	option.LoadBalancePickBias
	Regexp *regexp.Regexp
}

// Profile is how a group picks its node, next to the options that rank them.
type Profile struct {
	// Single is a group that uses one node at a time. It keeps the node it chose while
	// another one is not better by more than the tolerance, because a change takes every
	// new connection with it. A load balance group spreads the connections across the
	// nodes instead, and replaces each of them on its own, so it has no choice to keep.
	Single bool
}

func configFromOptions(sampling int, options option.LoadBalancePickOptions) (balancerConfig, error) {
	if sampling <= 0 {
		return balancerConfig{}, errors.New("sampling must be greater than 0")
	}
	if options.Strategy == "" {
		options.Strategy = StrategyRandom
	}
	if options.Objective == "" {
		options.Objective = ObjectiveAlive
	}

	var cfg balancerConfig
	cfg.LoadBalancePickOptions = options
	cfg.maxFailRate = float32(options.MaxFail) / float32(sampling)
	cfg.maxRTT = healthcheck.RTT(options.MaxRTT.Build().Milliseconds())

	for _, bias := range options.Biases {
		var re *regexp.Regexp
		if bias.Regexp != "" {
			var err error
			re, err = regexp.Compile(bias.Regexp)
			if err != nil {
				return cfg, err
			}
		}
		cfg.pickBiases = append(cfg.pickBiases, pickBias{
			LoadBalancePickBias: bias,
			Regexp:              re,
		})
	}
	return cfg, nil
}
