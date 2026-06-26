package balancer

import (
	"errors"
	"regexp"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/service/healthcheck"
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
