package outbound

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

// Builder is the outbound builder
var Builder func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.Outbound) (adapter.Outbound, error)
