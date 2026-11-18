// Package portprobe finds out which outbound ports are allowed by the network
// policy of the exit outbound.
//
// A probe dials a destination through the outbound and watches for any answer.
// A completed handshake and a refusal are both positive signals, because both
// prove that the probe left the outbound. Silence alone proves nothing: the
// outbound may have dropped the probe, or the destination may have dropped it.
// Dialing the same destination directly settles that question, so silence through
// the outbound is blamed on the outbound only when the destination answered
// the direct probe.
package portprobe

import (
	"context"
	"errors"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const (
	// DefaultDestination accepts TCP connections on every port, so a refusal
	// from it can only be produced by the outbound itself.
	DefaultDestination = "portquiz.net"

	// BogonAddress is reserved for documentation and never answers. Probing it
	// reveals how the outbound answers packets it filters out.
	BogonAddress = "192.0.2.1"

	defaultTimeout     = 3 * time.Second
	defaultConcurrency = 8

	// bogonProbePorts are unlikely to be allowed by any outbound policy, they are
	// used to tell a local reject apart from an upstream answer.
	bogonProbePortLow  = 1
	bogonProbePortHigh = 25

	maxDetailLength = 64
)

// SOCKS reply codes, they carry the reason a request was rejected.
const (
	replyCodeFailure            = 1
	replyCodeNotAllowed         = 2
	replyCodeNetworkUnreachable = 3
	replyCodeHostUnreachable    = 4
	replyCodeConnectionRefused  = 5
)

// Verdict is the outcome of a single port probe.
type Verdict uint8

const (
	// VerdictUnknown means no observation was made: the probe was dropped
	// somewhere between the outbound and the destination.
	VerdictUnknown Verdict = iota
	// VerdictAllowed means a positive signal was observed: the destination either
	// completed the handshake or answered with a refusal.
	VerdictAllowed
	// VerdictBlocked means the outbound rejected the probe before it left.
	VerdictBlocked
	// VerdictUnusable means the connection was accepted but the destination never
	// answered, so the port cannot be used through this chain even though the
	// connection itself succeeded.
	VerdictUnusable
)

// rank orders verdicts from the most to the least useful, so a report reads as
// what works first and what could not be determined last.
func (v Verdict) rank() int {
	switch v {
	case VerdictAllowed:
		return 0
	case VerdictUnusable:
		return 1
	case VerdictBlocked:
		return 2
	default:
		return 3
	}
}

func (v Verdict) String() string {
	switch v {
	case VerdictAllowed:
		return "allowed"
	case VerdictBlocked:
		return "blocked"
	case VerdictUnusable:
		return "unusable"
	default:
		return "unknown"
	}
}

// Result is the outcome of a single port probe.
type Result struct {
	Port    int
	Verdict Verdict
	RTT     time.Duration
	Detail  string
	// Verified reports whether the destination answered with data. A connection
	// alone is not proof: a proxied dial returns as soon as the proxy accepts the
	// request, before the destination is known to be reachable.
	Verified bool
}

// Fingerprint describes how the outbound answers packets it filters out.
type Fingerprint struct {
	// Known reports whether the outbound produced an answer for the bogon
	// probe at all.
	Known bool
	// Rejects reports whether the outbound answers filtered packets instead of
	// dropping them.
	Rejects bool
	// Delay is how fast the outbound answered the bogon probe.
	Delay time.Duration
}

// Report is the result of a probe run.
type Report struct {
	Results []Result
	// Reference is the fastest successful probe. A failure that answered much
	// faster than this never made a round trip, so it was rejected locally.
	Reference time.Duration
	// ChainConnected reports whether the outbound answered at least one probe,
	// which means the tunnel itself works.
	ChainConnected bool
	// ChainVerified reports whether any destination answered, which means the
	// whole chain works.
	ChainVerified bool
	// Baseline reports whether the destination was also dialed directly.
	Baseline    bool
	Fingerprint Fingerprint
}

// Client probes outbound ports of an exit outbound.
type Client struct {
	// Destination is the host the probes are sent to.
	Destination string
	// Ports are probed in the given order, so likely candidates can be placed
	// first.
	Ports []int
	// Timeout is the per-probe timeout.
	Timeout time.Duration
	// Concurrency limits the number of probes in flight.
	Concurrency int
	// AllPortsOpen declares that the destination answers on every port, which
	// makes a refusal a local reject by definition.
	AllPortsOpen bool
	// Confirm repeats every probe that did not verify, one at a time, so a busy
	// proxy does not turn a transient stall into a verdict.
	Confirm bool
	// Baseline dials the destination directly from this host in addition to
	// dialing it through the outbound. Without it, silence can never be blamed
	// on the outbound, and every dropped port is reported as unknown.
	Baseline bool
}

type outcome struct {
	port         int
	rtt          time.Duration
	err          error
	ok           bool
	verifyFailed bool
	// baselineOK reports whether dialing the same destination directly from this
	// host produced an answer.
	baselineOK  bool
	baselineRTT time.Duration
}

// Probe dials every port through the given dialer and reports a verdict for each
// of them.
func (c *Client) Probe(ctx context.Context, dialer N.Dialer) (*Report, error) {
	if c.Destination == "" {
		return nil, E.New("missing destination")
	}
	if len(c.Ports) == 0 {
		return nil, E.New("missing ports")
	}
	outcomes := make([]outcome, len(c.Ports))
	indices := make([]int, 0, len(c.Ports))
	for index := range c.Ports {
		indices = append(indices, index)
	}
	c.probeIndexes(ctx, dialer, indices, outcomes, c.concurrency(), c.Baseline)
	var fingerprint Fingerprint
	var fingerprintGroup sync.WaitGroup
	// The fingerprint is always probed: it decides whether a refusal can be blamed
	// on the outbound, so leaving it out would change the verdicts.
	fingerprintGroup.Add(1)
	go func() {
		defer fingerprintGroup.Done()
		fingerprint = c.detectFingerprint(ctx, dialer)
	}()
	fingerprintGroup.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A busy proxy may refuse or stall a probe while it is serving the others,
	// which looks exactly like a blocked port. Failed probes are therefore
	// repeated one at a time before any of them is reported as a verdict.
	if c.Confirm {
		var failed []int
		for index, item := range outcomes {
			if !item.ok {
				failed = append(failed, index)
			}
		}
		if len(failed) > 0 {
			retried := make([]outcome, len(outcomes))
			c.probeIndexes(ctx, dialer, failed, retried, 1, false)
			for _, index := range failed {
				if !retried[index].ok {
					continue
				}
				// The direct probe result does not change between attempts.
				retried[index].baselineOK = retried[index].baselineOK || outcomes[index].baselineOK
				retried[index].baselineRTT = outcomes[index].baselineRTT
				outcomes[index] = retried[index]
			}
		}
	}
	report := &Report{
		Results:     make([]Result, 0, len(outcomes)),
		Reference:   referenceRTT(outcomes),
		Fingerprint: fingerprint,
		Baseline:    c.Baseline,
	}
	for _, item := range outcomes {
		// A reply from the outbound proves the tunnel works, whether it carries
		// a connection or a rejection.
		if item.ok || item.verifyFailed || socksReplyCode(item.err) >= 0 {
			report.ChainConnected = true
		}
		if item.ok ||
			isRefused(item.err) && !c.isLocalReject(item.rtt, report.Reference, fingerprint) {
			report.ChainVerified = true
		}
	}
	for _, item := range outcomes {
		report.Results = append(report.Results, c.classify(item, report.Reference, fingerprint, report.ChainVerified))
	}
	// Group the report by verdict while keeping the probe order inside a group.
	sort.SliceStable(report.Results, func(i int, j int) bool {
		return report.Results[i].Verdict.rank() < report.Results[j].Verdict.rank()
	})
	return report, nil
}

func (c *Client) concurrency() int {
	if c.Concurrency > 0 {
		return c.Concurrency
	}
	return defaultConcurrency
}

func (c *Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return defaultTimeout
}

func (c *Client) probe(ctx context.Context, dialer N.Dialer, host string, port int) outcome {
	probeCtx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	// The whole probe is timed, not just the dial. A proxied dial can return in
	// microseconds when the outbound multiplexes streams over an established
	// connection, so timing the dial alone reports a round trip of zero.
	start := time.Now()
	conn, err := dialer.DialContext(probeCtx, N.NetworkTCP, M.ParseSocksaddrHostPortStr(host, strconv.Itoa(port)))
	if err != nil {
		return outcome{port: port, rtt: time.Since(start), err: err}
	}
	// A proxied dial returns once the proxy accepts the request, so only a reply
	// from the destination proves that the port works.
	err = verifyHTTP(conn, host, probeCtx)
	rtt := time.Since(start)
	conn.Close()
	if err != nil {
		return outcome{port: port, rtt: rtt, err: err, verifyFailed: true}
	}
	return outcome{port: port, rtt: rtt, ok: true}
}

// probeIndexes probes the given outcome indexes, at most concurrency at a time.
func (c *Client) probeIndexes(ctx context.Context, dialer N.Dialer, indices []int, outcomes []outcome, concurrency int, baseline bool) {
	semaphore := make(chan struct{}, concurrency)
	var group sync.WaitGroup
	for _, index := range indices {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				outcomes[index] = outcome{port: c.Ports[index], err: ctx.Err()}
				return
			}
			defer func() {
				<-semaphore
			}()
			port := c.Ports[index]
			item := c.probe(ctx, dialer, c.Destination, port)
			if baseline {
				item.baselineRTT, item.baselineOK = c.probeBaseline(ctx, c.Destination, port)
			}
			outcomes[index] = item
		}(index)
	}
	group.Wait()
}

// probeBaseline dials the destination directly from this host, bypassing the
// outbound. It answers the only question the outbound cannot: whether the
// destination replies at all, so silence through the outbound can be blamed on
// the outbound.
func (c *Client) probeBaseline(ctx context.Context, host string, port int) (time.Duration, bool) {
	probeCtx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	start := time.Now()
	conn, err := (&net.Dialer{}).DialContext(probeCtx, N.NetworkTCP, net.JoinHostPort(host, strconv.Itoa(port)))
	rtt := time.Since(start)
	if err != nil {
		return rtt, isRefused(err)
	}
	conn.Close()
	return rtt, true
}

// detectFingerprint finds out whether the outbound answers filtered packets or
// drops them. It is the only way to interpret a refusal when no port of the
// destination could be reached at all.
func (c *Client) detectFingerprint(ctx context.Context, dialer N.Dialer) Fingerprint {
	ports := []int{bogonProbePortLow, bogonProbePortHigh}
	items := make([]outcome, len(ports))
	var group sync.WaitGroup
	for index, port := range ports {
		group.Add(1)
		go func(index int, port int) {
			defer group.Done()
			items[index] = c.probe(ctx, dialer, BogonAddress, port)
		}(index, port)
	}
	group.Wait()
	var fingerprint Fingerprint
	for _, item := range items {
		if item.ok || isTimeout(item.err) {
			continue
		}
		// Only a refusal or a rejection is a fingerprint. Any other failure means
		// the tunnel itself did not work, which says nothing about the policy.
		if !isRefused(item.err) && !isNotAllowed(item.err) && !isUnreachable(item.err) {
			continue
		}
		fingerprint.Known = true
		if !fingerprint.Rejects || item.rtt < fingerprint.Delay {
			fingerprint.Rejects = true
			fingerprint.Delay = item.rtt
		}
	}
	return fingerprint
}

func referenceRTT(outcomes []outcome) time.Duration {
	var reference time.Duration
	for _, item := range outcomes {
		if !item.ok {
			continue
		}
		if reference == 0 || item.rtt < reference {
			reference = item.rtt
		}
	}
	return reference
}

func (c *Client) classify(item outcome, reference time.Duration, fingerprint Fingerprint, chainVerified bool) Result {
	result := Result{Port: item.port, RTT: item.rtt}
	switch {
	case item.ok:
		result.Verdict = VerdictAllowed
		result.Verified = true
		result.Detail = "http response"
	case item.verifyFailed:
		switch {
		case c.AllPortsOpen:
			// The destination answers on every port, so the silence cannot be its
			// own doing: the port is filtered somewhere on the way.
			result.Verdict = VerdictUnusable
			result.Detail = "no reply, but destination is known to answer"
		case c.Baseline && !item.baselineOK:
			// The destination did not answer the direct dial either, so its silence
			// says nothing about the outbound.
			result.Verdict = VerdictUnknown
			result.Detail = "no reply, and destination silent on direct dial"
		case !c.Baseline:
			result.Verdict = VerdictUnknown
			result.Detail = "no reply, direct dial disabled"
		default:
			// The destination answers on some ports only, so it may simply not speak
			// http on this one.
			result.Verdict = VerdictUnknown
			result.Detail = "no reply, destination may not answer http"
		}
	case isTimeout(item.err):
		switch {
		case !item.baselineOK:
			result.Verdict = VerdictUnknown
			if c.Baseline {
				result.Detail = "dial timeout, and destination silent on direct dial"
			} else {
				result.Detail = "dial timeout, and direct dial disabled"
			}
		case !chainVerified:
			// Nothing reached the destination through the outbound, so the
			// silence may come from a broken outbound rather than from the policy.
			result.Verdict = VerdictUnknown
			result.Detail = "dial timeout, outbound never answered"
		default:
			// The destination answers when it is reached directly, so the probe was
			// dropped on the way. Which hop dropped it is not knowable from here,
			// and an outbound that forwards silently shows the same thing as one
			// that drops it, so this is not reported as a rejection.
			result.Verdict = VerdictUnusable
			result.Detail = "dial timeout, but direct dial answers"
		}
	case isNotAllowed(item.err):
		result.Verdict = VerdictBlocked
		result.Detail = "dial rejected by outbound"
	case isUnreachable(item.err):
		result.Verdict = VerdictBlocked
		result.Detail = "dial unreachable from outbound"
	case isRefused(item.err):
		if c.isLocalReject(item.rtt, reference, fingerprint) {
			result.Verdict = VerdictBlocked
			result.Detail = "dial rejected by outbound"
		} else {
			// A refusal proves the destination answered, but only when it came from
			// the destination rather than from the proxy in front of it.
			result.Verdict = VerdictAllowed
			result.Verified = true
			result.Detail = "refused by peer"
		}
	case outboundReportedFailure(item.err) && item.baselineOK && chainVerified:
		// The outbound answered the request but could not connect, and the
		// destination is reachable, so the port does not work through the outbound.
		result.Verdict = VerdictBlocked
		result.Detail = "dial failed at the outbound"
	default:
		result.Verdict = VerdictUnknown
		result.Detail = shortError(item.err)
	}
	return result
}

// isLocalReject tells a refusal produced by the outbound apart from a refusal
// produced by the destination. The former answers without a round trip, so it is
// much faster than a successful probe to the same destination.
func (c *Client) isLocalReject(rtt time.Duration, reference time.Duration, fingerprint Fingerprint) bool {
	if c.AllPortsOpen {
		return true
	}
	// The bogon probe is direct evidence about who speaks: an outbound that stayed
	// silent there never answers a request itself, so a refusal it returns can
	// only come from the destination.
	if fingerprint.Known && !fingerprint.Rejects {
		return false
	}
	if reference > 0 {
		return rtt < reference/2
	}
	if fingerprint.Rejects {
		return rtt <= fingerprint.Delay*3
	}
	return rtt < 10*time.Millisecond
}

// outboundReportedFailure reports whether the outbound answered the request with
// a generic failure instead of connecting.
func outboundReportedFailure(err error) bool {
	return socksReplyCode(err) == replyCodeFailure
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, os.ErrDeadlineExceeded) ||
		E.IsTimeout(err) ||
		strings.Contains(strings.ToLower(err.Error()), "timeout")
}

func isRefused(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.ECONNREFUSED) ||
		strings.Contains(strings.ToLower(err.Error()), "refused") ||
		socksReplyCode(err) == replyCodeConnectionRefused
}

func isUnreachable(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		strings.Contains(strings.ToLower(err.Error()), "unreachable") ||
		socksReplyCode(err) == replyCodeNetworkUnreachable ||
		socksReplyCode(err) == replyCodeHostUnreachable
}

func isNotAllowed(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return errors.Is(err, syscall.EACCES) ||
		strings.Contains(message, "administratively prohibited") ||
		strings.Contains(message, "not allowed") ||
		strings.Contains(message, "permission denied") ||
		socksReplyCode(err) == replyCodeNotAllowed
}

// socksReplyCode extracts the reply code reported by a SOCKS server, which
// carries the reason the request was rejected.
func socksReplyCode(err error) int {
	const marker = "code="
	message := err.Error()
	index := strings.Index(message, marker)
	if index == -1 {
		return -1
	}
	start := index + len(marker)
	end := start
	for end < len(message) && message[end] >= '0' && message[end] <= '9' {
		end++
	}
	if end == start {
		return -1
	}
	code, parseErr := strconv.Atoi(message[start:end])
	if parseErr != nil {
		return -1
	}
	return code
}

func shortError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ReplaceAll(err.Error(), "\n", " ")
	if len(message) > maxDetailLength {
		message = message[:maxDetailLength] + "..."
	}
	return message
}
