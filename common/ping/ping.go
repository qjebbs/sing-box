package ping

import (
	"context"
	"fmt"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/urltest"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

// Client is the ping client
type Client struct {
	Count    uint
	Interval time.Duration
	Outbound option.Outbound
}

// Ping pings the destination
func (c *Client) Ping(ctx context.Context, destination string) (*Statistics, error) {
	instance, detour, err := newInstance(c.Outbound)
	if err != nil {
		return nil, err
	}
	defer instance.Close()

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
			delay, err := urltest.URLTest(testCtx, destination, detour)
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

func newInstance(outbound option.Outbound) (*box.Box, adapter.Outbound, error) {
	options := option.Options{
		Log: &option.LogOptions{
			Level: log.FormatLevel(log.LevelPanic),
		},
		Outbounds: []option.Outbound{outbound},
	}
	instance, err := box.New(box.Options{
		Options: options,
	})
	if err != nil {
		return nil, nil, err
	}
	err = instance.Start()
	if err != nil {
		return nil, nil, err
	}
	detour := instance.Router().Outbounds()[0]
	return instance, detour, nil
}
