package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sagernet/sing-box/common/portprobe"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"

	"github.com/spf13/cobra"
)

var commandPortProbe = &cobra.Command{
	Use:   "portprobe [flags] tag_or_link",
	Short: "Outbound port prober",
	Args:  cobra.ExactArgs(1),
	Long: `sing-box outbound port prober.
It dials the destination on every given port through the exit outbound, and
reports whether its network policy allows the port.

A refusal from the destination and a reply from it are both positive signals.
A connection alone is not: a proxied dial returns as soon as the proxy accepts
the request, so a working connection only proves that the proxy forwarded it,
not that the destination is reachable.

Every probe therefore ends with an HTTP request, and only a destination that
answers it counts as allowed. The destination is required to answer HTTP on the
probed ports, which is why the default destination is portquiz.net: it answers on
every port, so a port that stays silent was dropped before it got there.

Silence alone proves nothing either, for the same reason the connection is not
enough. The destination is also dialed directly from this host, so silence
through the exit is reported as blocked once the destination is known to answer.

The exit outbound is specified by a tag of the configuration file, or by a link.

Example:

# probe the exit outbound from the configuration file
> sing-box tools portprobe -c config.json exit_tag

# probe the exit outbound from a link
> sing-box tools portprobe vmess://...

Every port is reported as one of:

  allowed   the destination answered, so the port works
  blocked   the outbound rejected the probe before the destination could answer,
            for example with a refusal, a rejection code or an unreachable error
  unusable  no answer, while the destination is known to answer, so the port
            cannot be used through this outbound
  unknown   nothing was observed, so no conclusion can be drawn

Exit code is 0 when at least one port is allowed, 1 when every port is blocked or
unusable, and 2 when the result is indeterminate because no probe was answered.
`,
}

var (
	commandPortProbeFlagDest        string
	commandPortProbeFlagPorts       string
	commandPortProbeFlagTimeout     time.Duration
	commandPortProbeFlagConcurrency int
	commandPortProbeFlagAllPorts    bool
	commandPortProbeFlagBaseline    bool
	commandPortProbeFlagConfirm     bool
	commandPortProbeFlagJSON        bool
)

func init() {
	commandPortProbe.Flags().SortFlags = false
	commandPortProbe.Flags().StringVarP(&commandPortProbeFlagDest, "dest", "d", portprobe.DefaultDestination, "destination host")
	commandPortProbe.Flags().StringVarP(&commandPortProbeFlagPorts, "ports", "p", "common", "ports to probe, a comma separated list of ports and ranges, or \"common\" for standard service ports, their alternatives and proxy ports")
	commandPortProbe.Flags().DurationVarP(&commandPortProbeFlagTimeout, "timeout", "t", 3*time.Second, "timeout of a single probe")
	commandPortProbe.Flags().IntVar(&commandPortProbeFlagConcurrency, "concurrency", 8, "number of probes in flight")
	commandPortProbe.Flags().BoolVar(&commandPortProbeFlagAllPorts, "all-ports-open", false, "assume the destination accepts every port, a refusal is then a local reject (defaults to true for the default destination)")
	commandPortProbe.Flags().BoolVar(&commandPortProbeFlagBaseline, "baseline", true, "also dial the destination directly, so a dropped port can be blamed on the outbound")
	commandPortProbe.Flags().BoolVar(&commandPortProbeFlagConfirm, "confirm", true, "repeat failed probes one at a time, so a busy proxy is not mistaken for a blocked port")
	commandPortProbe.Flags().BoolVar(&commandPortProbeFlagJSON, "json", false, "output results as JSON")
	commandPortProbe.Run = func(cmd *cobra.Command, args []string) {
		exitCode, err := runPortProbe(cmd, args)
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(exitCode)
	}
	commandTools.AddCommand(commandPortProbe)
}

func runPortProbe(cmd *cobra.Command, args []string) (int, error) {
	tags, linkOutbounds, requireConfig, err := parseLinkOutbounds(args)
	if err != nil {
		return 1, err
	}
	detour := tags[0]
	ports, err := portprobe.ParsePorts(commandPortProbeFlagPorts)
	if err != nil {
		return 1, err
	}
	isDefaultDestination := strings.EqualFold(commandPortProbeFlagDest, portprobe.DefaultDestination)
	allPortsOpen := commandPortProbeFlagAllPorts
	if !cmd.Flags().Changed("all-ports-open") {
		allPortsOpen = isDefaultDestination
	}
	options, err := portProbeOptions(requireConfig, linkOutbounds)
	if err != nil {
		return 1, err
	}
	instance, err := newPreStartedClient(options)
	if err != nil {
		return 1, err
	}
	defer instance.Close()
	dialer, err := createDialer(instance, detour)
	if err != nil {
		return 1, err
	}
	client := &portprobe.Client{
		Destination:  commandPortProbeFlagDest,
		Ports:        ports,
		Timeout:      commandPortProbeFlagTimeout,
		Concurrency:  commandPortProbeFlagConcurrency,
		AllPortsOpen: allPortsOpen,
		Baseline:     commandPortProbeFlagBaseline,
		Confirm:      commandPortProbeFlagConfirm,
	}
	if !commandPortProbeFlagJSON {
		os.Stdout.WriteString(fmt.Sprintf("sing-box tools portprobe (version %s)\n", C.Version))
		os.Stdout.WriteString(fmt.Sprintf("probing %s via %s\n", commandPortProbeFlagDest, detour))
	}
	report, err := client.Probe(globalCtx, dialer)
	if err != nil {
		return 1, err
	}
	if commandPortProbeFlagJSON {
		err = writePortProbeJSON(report)
		if err != nil {
			return 1, err
		}
	} else {
		writePortProbeText(report)
	}
	return portProbeExitCode(report), nil
}

// portProbeOptions builds the options a prober needs. The configuration file is
// only read when an outbound tag is referenced, so a link-only run stays fast and
// does not start providers or bind ports.
func portProbeOptions(requireConfig bool, linkOutbounds []option.Outbound) (option.Options, error) {
	var options option.Options
	if requireConfig {
		configured, err := readConfigAndMerge()
		if err != nil {
			return option.Options{}, err
		}
		options = configured
	}
	options.Outbounds = append(options.Outbounds, linkOutbounds...)
	return dialOnlyOptions(options), nil
}

func portProbeExitCode(report *portprobe.Report) int {
	var failed int
	for _, result := range report.Results {
		switch result.Verdict {
		case portprobe.VerdictAllowed:
			return 0
		case portprobe.VerdictBlocked, portprobe.VerdictUnusable:
			failed++
		}
	}
	if failed > 0 {
		return 1
	}
	return 2
}

func writePortProbeText(report *portprobe.Report) {
	if report.Fingerprint.Rejects {
		os.Stdout.WriteString(fmt.Sprintf("exit answers filtered packets in %dms\n", report.Fingerprint.Delay.Milliseconds()))
	} else if report.Fingerprint.Known {
		os.Stdout.WriteString("exit stays silent for filtered packets\n")
	}
	switch {
	case !report.ChainConnected:
		os.Stdout.WriteString("warning: the outbound answered nothing, so check that it works before\n")
		os.Stdout.WriteString("         reading these results as a port policy\n")
	case !report.ChainVerified:
		os.Stdout.WriteString("warning: the outbound accepted the requests but the destination never replied,\n")
		os.Stdout.WriteString("         so it cannot reach the destination whatever the port policy is\n")
	}
	if !report.Baseline && !report.ChainVerified {
		os.Stdout.WriteString("warning: baseline disabled, silence is reported as unknown\n")
	}
	os.Stdout.WriteString("\n")
	var allowed, blocked, unusable, unknown int
	os.Stdout.WriteString(fmt.Sprintf("%-6s%-14s%-10s%-9s%s\n", "PORT", "SERVICE", "VERDICT", "RTT", "EVIDENCE"))
	for _, result := range report.Results {
		switch result.Verdict {
		case portprobe.VerdictAllowed:
			allowed++
		case portprobe.VerdictBlocked:
			blocked++
		case portprobe.VerdictUnusable:
			unusable++
		default:
			unknown++
		}
		service := portprobe.PortName(result.Port)
		if service == "" {
			service = "-"
		}
		os.Stdout.WriteString(fmt.Sprintf(
			"%-6d%-14s%-10s%-9s%s\n",
			result.Port, service, result.Verdict, fmt.Sprintf("%dms", result.RTT.Milliseconds()), result.Detail,
		))
	}
	os.Stdout.WriteString("\n--- portprobe statistics ---\n")
	os.Stdout.WriteString(fmt.Sprintf(
		"%d ports probed, %d allowed, %d unusable, %d blocked, %d unknown\n",
		len(report.Results), allowed, unusable, blocked, unknown,
	))
}

type portProbeJSONOutput struct {
	Destination string                   `json:"destination"`
	Fingerprint portProbeJSONFingerprint `json:"fingerprint"`
	Results     []portProbeJSONResult    `json:"results"`
}

type portProbeJSONFingerprint struct {
	Known       bool  `json:"known"`
	Rejects     bool  `json:"rejects"`
	DelayMillis int64 `json:"delay_ms"`
}

type portProbeJSONResult struct {
	Port      int    `json:"port"`
	Service   string `json:"service,omitempty"`
	Verdict   string `json:"verdict"`
	Verified  bool   `json:"verified"`
	RTTMillis int64  `json:"rtt_ms"`
	Detail    string `json:"detail,omitempty"`
}

func writePortProbeJSON(report *portprobe.Report) error {
	output := portProbeJSONOutput{
		Destination: commandPortProbeFlagDest,
		Fingerprint: portProbeJSONFingerprint{
			Known:       report.Fingerprint.Known,
			Rejects:     report.Fingerprint.Rejects,
			DelayMillis: report.Fingerprint.Delay.Milliseconds(),
		},
		Results: make([]portProbeJSONResult, 0, len(report.Results)),
	}
	for _, result := range report.Results {
		output.Results = append(output.Results, portProbeJSONResult{
			Port:      result.Port,
			Service:   portprobe.PortName(result.Port),
			Verdict:   result.Verdict.String(),
			Verified:  result.Verified,
			RTTMillis: result.RTT.Milliseconds(),
			Detail:    result.Detail,
		})
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(&output)
}
