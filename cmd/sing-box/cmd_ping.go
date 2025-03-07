package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sagernet/sing/common/json"

	"github.com/sagernet/sing-box/common/link"
	"github.com/sagernet/sing-box/common/ping"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var commandPing = &cobra.Command{
	Use:   "ping [flags] [link]",
	Short: "Link prober",
	Long:  `sing-box link prober`,
	Args:  cobra.MaximumNArgs(1),
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
	commandPing.SetHelpFunc(func(c *cobra.Command, s []string) {
		// hide global flags
		c.Parent().Flags().VisitAll(func(f *pflag.Flag) {
			f.Hidden = true
		})
		c.Parent().HelpFunc()(c, s)
	})
	mainCommand.AddCommand(commandPing)
}

func runPing() (*ping.Statistics, error) {
	var outbound *option.Outbound
	if commandPing.Flags().NArg() > 0 {
		u, err := url.Parse(commandPing.Flags().Arg(0))
		if err != nil {
			return nil, err
		}
		link, err := link.Parse(u)
		if err != nil {
			return nil, err
		}
		out, err := link.Outbound()
		if err != nil {
			return nil, err
		}
		outbound = out
	} else {
		outbound = &option.Outbound{
			Type: C.TypeDirect,
		}
	}

	client := &ping.Client{
		Count:    commandPingFlagCount,
		Interval: commandPingFlagInteval,
		Outbound: outbound,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.Encode(outbound)
	os.Stdout.WriteString("\n")

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
	stat, err := client.Ping(ctx, commandPingFlagDest)
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
