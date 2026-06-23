package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sagernet/sing-box/common/link"
	"github.com/sagernet/sing-box/common/ping"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json"

	"github.com/spf13/cobra"
)

var commandPing = &cobra.Command{
	Use:   "ping [flags] [tag_or_links]",
	Short: "Link prober",
	Long: `sing-box link prober.
It sends HTTP HEAD requests to the destination via the outbound / outbounds chain,
and print the round trip time (RTT) for each request.

The outbounds can be specified by tags (of configuration file) or links.

Example: 

# ping via the outbound from the configuration file
> sing-box ping -c conifg.json outbound_tag

# ping via the outbound from link
> sing-box ping vmess://...

# ping via an outbounds chain
> sing-box ping -c conifg.json outbound_tag vmess://...
`,
}

var (
	commandPingFlagDest    string
	commandPingFlagCount   uint
	commandPingFlagInteval time.Duration
)

func init() {
	commandPing.Flags().SortFlags = false
	commandPing.Flags().StringVarP(&commandPingFlagDest, "dest", "d", "https://www.gstatic.com/generate_204", "destination")
	commandPing.Flags().DurationVarP(&commandPingFlagInteval, "interval", "i", time.Second, "request interval")
	commandPing.Flags().UintVarP(&commandPingFlagCount, "number", "n", 9999, "number of requests to make")
	commandPing.Run = func(cmd *cobra.Command, args []string) {
		stat, err := runPing()
		if err != nil {
			os.Stderr.WriteString(err.Error() + "\n")
			os.Exit(1)
		}
		if stat.Requests > 0 && stat.Requests == stat.Fails {
			os.Exit(1)
		}
	}
	// commandPing.SetHelpFunc(func(c *cobra.Command, s []string) {
	// 	// hide global flags
	// 	c.Parent().Flags().VisitAll(func(f *pflag.Flag) {
	// 		f.Hidden = true
	// 	})
	// 	c.Parent().HelpFunc()(c, s)
	// })
	mainCommand.AddCommand(commandPing)
}

func runPing() (*ping.Statistics, error) {
	if commandPing.Flags().NArg() == 0 {
		return nil, E.New("no destination specified")
	}
	var (
		tags          []string
		requireConf   bool
		linkOutbounds []option.Outbound
		detour        string
	)
	for i, arg := range commandPing.Flags().Args() {
		uri, err := url.Parse(arg)
		if err != nil || uri.Scheme == "" {
			// a tag
			requireConf = true
			tags = append(tags, arg)
			continue
		}
		link, err := link.Parse(arg)
		if err != nil {
			return nil, err
		}
		out, err := link.Outbound()
		if err != nil {
			return nil, err
		}
		if out.Tag == "" {
			out.Tag = fmt.Sprintf("outbound%d", i+1)
		}
		tags = append(tags, out.Tag)
		linkOutbounds = append(linkOutbounds, *out)
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		encoder.Encode(out)
		os.Stdout.WriteString("\n")
	}
	if len(tags) == 1 {
		detour = tags[0]
	} else {
		detour = strings.Join(tags, " => ")
		chain := option.Outbound{
			Tag:  detour,
			Type: C.TypeChain,
			Options: &option.ChainOptions{
				Outbounds: tags,
			},
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		encoder.Encode(&chain)
		os.Stdout.WriteString("\n")
		linkOutbounds = append(linkOutbounds, chain)
	}

	var options option.Options
	if requireConf {
		o, err := readConfigAndMerge()
		if err != nil {
			return nil, err
		}
		o.Outbounds = append(o.Outbounds, linkOutbounds...)
		options = o
	} else {
		options = option.Options{
			Outbounds: linkOutbounds,
		}
	}

	client := &ping.Client{
		Count:    commandPingFlagCount,
		Interval: commandPingFlagInteval,
		Options:  options,
	}

	ctx, cancel := context.WithCancel(globalCtx)
	go func() {
		osSignals := make(chan os.Signal, 1)
		signal.Notify(osSignals, os.Interrupt, syscall.SIGTERM)
		for {
			select {
			case <-ctx.Done():
				return
			case <-osSignals:
				cancel()
				return
			}
		}
	}()

	os.Stdout.WriteString(fmt.Sprintf(
		"sing-box ping (version %s)\n",
		C.Version,
	))
	stat, err := client.Ping(ctx, detour, commandPingFlagDest)
	cancel()
	if err != nil {
		return nil, err
	}
	statistics := fmt.Sprintf(`
--- ping statistics ---
%d requests made, %d success, total time %v
rtt min/avg/max = %d/%d/%d ms
`,
		stat.Requests, len(stat.RTTs), time.Since(stat.StartAt),
		stat.Min, stat.Average, stat.Max,
	)
	os.Stdout.WriteString(statistics)
	return stat, nil
}
