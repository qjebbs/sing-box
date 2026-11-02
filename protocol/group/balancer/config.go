package balancer

import (
	"errors"
	"regexp"

	"github.com/sagernet/sing-box/common/healthcheck"
	"github.com/sagernet/sing-box/option"
)

type balancerConfig struct {
	maxRTT      healthcheck.RTT
	maxFailRate float32
	pickBiases  []pickBias
}

// NodeOptions describe how the health of a node becomes its status.
type NodeOptions struct {
	// MaxRTT is the highest median delay a node may have and stay qualified, 0 for no
	// limit.
	MaxRTT healthcheck.RTT
	// MaxFail is how many checks of the sampling window may fail before a node is only
	// alive, 0 for any failure.
	MaxFail uint
	// Biases scale the score of the nodes they match.
	Biases []option.LoadBalancePickBias
}

type pickBias struct {
	option.LoadBalancePickBias
	Regexp *regexp.Regexp
}

func configFromOptions(sampling int, options NodeOptions) (balancerConfig, error) {
	if sampling <= 0 {
		return balancerConfig{}, errors.New("sampling must be greater than 0")
	}

	cfg := balancerConfig{
		maxRTT:      options.MaxRTT,
		maxFailRate: float32(options.MaxFail) / float32(sampling),
	}

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
