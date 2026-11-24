package adapter

import (
	"context"
	"slices"
	"strings"
	"sync"
)

// OutboundChain is the chain of outbounds that carry a connection, in the order they are
// entered, ending at the outbound that dials.
//
// A connection is routed before it is dialed, so the node that serves it is only known
// when the outbound that carries it picks one. The chain is therefore held by a value
// both of them can reach, instead of being passed between them: it is empty until the
// dial, and stays empty for a connection that never reaches an outbound, e.g. one that is
// rejected.
type OutboundChain struct {
	access    sync.Mutex
	outbounds []Outbound
}

// NewOutboundChain returns an empty chain.
func NewOutboundChain() *OutboundChain {
	return &OutboundChain{}
}

// Set replaces the chain, in the order the outbounds are entered.
func (c *OutboundChain) Set(outbounds []Outbound) {
	if c == nil {
		return
	}
	c.access.Lock()
	c.outbounds = slices.Clone(outbounds)
	c.access.Unlock()
}

// Outbounds returns the chain in the order it was recorded. A connection that no outbound
// was told about has no chain, and the methods of a nil chain answer for it.
func (c *OutboundChain) Outbounds() []Outbound {
	if c == nil {
		return nil
	}
	c.access.Lock()
	defer c.access.Unlock()
	return slices.Clone(c.outbounds)
}

// Tags returns the tags of the chain, in the order it was recorded.
func (c *OutboundChain) Tags() []string {
	outbounds := c.Outbounds()
	tags := make([]string, len(outbounds))
	for i, outbound := range outbounds {
		tags[i] = outbound.Tag()
	}
	return tags
}

// String returns the tags of the chain as a path, from the outbound that starts it to the
// one that dials. It is what a log line prints, and it is only built when it is printed.
func (c *OutboundChain) String() string {
	return strings.Join(c.Tags(), " -> ")
}

// Append adds the outbounds to the chain, in the order they are entered, skipping the ones
// the chain already holds: a connection passes through an outbound once, and a group
// records itself and the node it picks, which its caller may have recorded already.
func (c *OutboundChain) Append(outbounds ...Outbound) {
	if c == nil {
		return
	}
	c.access.Lock()
	defer c.access.Unlock()
	for _, outbound := range outbounds {
		if outbound == nil || slices.ContainsFunc(c.outbounds, func(it Outbound) bool {
			return it.Tag() == outbound.Tag()
		}) {
			continue
		}
		c.outbounds = append(c.outbounds, outbound)
	}
}

type outboundChainKey struct{}

// WithOutboundChain returns a context carrying the chain the connection is recorded into.
//
// The chain travels in the context next to the metadata rather than in it: every outbound
// the connection passes through copies the metadata, and one of them may replace it, while
// the chain they all record into is the one the router created for the connection.
func WithOutboundChain(ctx context.Context, chain *OutboundChain) context.Context {
	return context.WithValue(ctx, outboundChainKey{}, chain)
}

// OutboundChainFrom returns the chain of the connection, nil when it records none.
func OutboundChainFrom(ctx context.Context) *OutboundChain {
	chain, _ := ctx.Value(outboundChainKey{}).(*OutboundChain)
	return chain
}

// RecordOutboundChain records the outbounds that carry a connection, in the order they are
// entered. It does nothing for a connection that records no chain, so an outbound can
// record unconditionally.
func RecordOutboundChain(ctx context.Context, outbounds ...Outbound) {
	chain := OutboundChainFrom(ctx)
	if chain == nil {
		return
	}
	chain.Append(outbounds...)
}
