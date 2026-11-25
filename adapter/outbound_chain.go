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
	sealed    bool
	failed    bool
	onSeal    []func()
}

// NewOutboundChain returns an empty chain.
func NewOutboundChain() *OutboundChain {
	return &OutboundChain{}
}

// WhenSealed calls f once the chain is sealed, right away when it already is. It is how a
// connection is reported, see trafficcontrol.Manager: a connection is only tracked once the
// evidence of what carries it is complete.
func (c *OutboundChain) WhenSealed(f func()) {
	if c == nil {
		return
	}
	c.access.Lock()
	if c.sealed {
		c.access.Unlock()
		f()
		return
	}
	c.onSeal = append(c.onSeal, f)
	c.access.Unlock()
}

// Seal marks the chain as complete because the dial of the connection is over: the
// outbounds that carry it have recorded themselves. A connection that is still being dialed
// holds only a part of the path, and it is not tracked until then, see WhenSealed.
//
// The dial is over when the dialer the connection was handed to returns, not when a hop of
// the path returns: the hops record themselves on the way back, and the connection is only
// carried once the outermost one is.
func (c *OutboundChain) Seal() {
	c.seal()
}

// HandOver marks the chain as complete for a connection that is handed over without a dial
// of its own, to an outbound that carries it itself: there is no dial to wait for, and the
// outbounds have recorded everything they will. See Seal for the dialed case.
func (c *OutboundChain) HandOver() {
	c.seal()
}

// Fail marks the chain as never complete: the dial failed, so the connection is not carried
// by anything and there is no path to report. It is what tells a dial that failed apart from
// a dial path that does not report the connection it carries, see Router.warnUnsealedChain.
func (c *OutboundChain) Fail() {
	if c == nil {
		return
	}
	c.access.Lock()
	c.failed = true
	c.access.Unlock()
}

func (c *OutboundChain) seal() {
	if c == nil {
		return
	}
	c.access.Lock()
	if c.sealed {
		c.access.Unlock()
		return
	}
	c.sealed = true
	callbacks := c.onSeal
	c.onSeal = nil
	c.access.Unlock()
	// The callbacks read the chain back, so they are called once the lock is released.
	for _, callback := range callbacks {
		callback()
	}
}

// Sealed reports whether the chain is complete, see Seal and HandOver.
func (c *OutboundChain) Sealed() bool {
	if c == nil {
		return false
	}
	c.access.Lock()
	defer c.access.Unlock()
	return c.sealed
}

// Failed reports whether the dial of the connection failed, see Fail.
func (c *OutboundChain) Failed() bool {
	if c == nil {
		return false
	}
	c.access.Lock()
	defer c.access.Unlock()
	return c.failed
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

// Len returns the number of outbounds in the chain, without copying it.
func (c *OutboundChain) Len() int {
	if c == nil {
		return 0
	}
	c.access.Lock()
	defer c.access.Unlock()
	return len(c.outbounds)
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
