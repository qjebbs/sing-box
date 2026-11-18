package ping

import (
	"context"
	"fmt"
	"time"

	"github.com/sagernet/sing-box/common/urltest"
	C "github.com/sagernet/sing-box/constant"
	N "github.com/sagernet/sing/common/network"
)

// Client is the ping client
type Client struct {
	Count    uint
	Interval time.Duration
	// Dialer is the outbound the requests are sent through.
	Dialer N.Dialer
}

// Ping pings the destination through the client dialer
func (c *Client) Ping(ctx context.Context, destination string) (*Statistics, error) {
	startAt := time.Now()
	rtts := make([]uint16, 0)
	round := uint(0)
L:
	for {
		round++
		chDelay := make(chan uint16)
		go func() {
			testCtx, cancel := context.WithTimeout(ctx, C.TCPTimeout)
			defer cancel()
			delay, err := urltest.URLTest(testCtx, destination, c.Dialer)
			if ctx.Err() != nil {
				// if context is canceled, ignore the test
				return
			}
			if err != nil {
				fmt.Printf("Ping %s: seq=%d err %v\n", destination, round, err)
				chDelay <- 0
				return
			}
			fmt.Printf("Ping %s: seq=%d time=%d ms\n", destination, round, delay)
			chDelay <- delay
		}()

		select {
		case delay := <-chDelay:
			if delay > 0 {
				rtts = append(rtts, delay)
			}
		case <-ctx.Done():
			break L
		}
		if round == c.Count {
			break L
		}
		select {
		case <-time.After(c.Interval):
		case <-ctx.Done():
			break L
		}
	}
	return getStatistics(startAt, round, rtts), nil
}
