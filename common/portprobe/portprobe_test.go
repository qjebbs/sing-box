package portprobe

import (
	"context"
	"fmt"
	"reflect"
	"syscall"
	"testing"
	"time"
)

func TestParsePorts(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name          string
		specification string
		want          []int
		wantErr       bool
	}{
		{name: "empty", specification: "", want: CommonPorts},
		{name: "common", specification: "common", want: CommonPorts},
		{name: "common case insensitive", specification: " Common ", want: CommonPorts},
		{name: "single", specification: "443", want: []int{443}},
		{name: "list", specification: "443, 8080", want: []int{443, 8080}},
		{name: "duplicate", specification: "443,443", want: []int{443}},
		{name: "range", specification: "1-3", want: []int{1, 2, 3}},
		{name: "mixed", specification: "443,8080-8082,53", want: []int{443, 8080, 8081, 8082, 53}},
		{name: "common plus port", specification: "common,1234", want: append(append([]int(nil), CommonPorts...), 1234)},
		{name: "zero", specification: "0", wantErr: true},
		{name: "too large", specification: "65536", wantErr: true},
		{name: "reversed range", specification: "5-3", wantErr: true},
		{name: "not a number", specification: "https", wantErr: true},
		{name: "empty range end", specification: "100-", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ports, err := ParsePorts(testCase.specification)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", ports)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(ports, testCase.want) {
				t.Fatalf("expected %v, got %v", testCase.want, ports)
			}
		})
	}
}

func TestSocksReplyCode(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		message string
		want    int
	}{
		{"socks5: request rejected, code=5", 5},
		{"socks5: request rejected, code=2", 2},
		{"connection refused", -1},
		{"code=", -1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.message, func(t *testing.T) {
			t.Parallel()
			got := socksReplyCode(fmt.Errorf("%s", testCase.message))
			if got != testCase.want {
				t.Fatalf("expected %d, got %d", testCase.want, got)
			}
		})
	}
}

func TestClassify(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name          string
		client        Client
		item          outcome
		reference     time.Duration
		fingerprint   Fingerprint
		chainVerified bool
		want          Verdict
	}{
		{
			name: "connected with an http response",
			item: outcome{port: 443, ok: true, rtt: 150 * time.Millisecond},
			want: VerdictAllowed,
		},
		{
			name: "timeout",
			item: outcome{port: 443, rtt: 3 * time.Second, err: context.DeadlineExceeded},
			want: VerdictUnknown,
		},
		{
			name:          "timeout with answering destination",
			item:          outcome{port: 8000, rtt: 3 * time.Second, err: context.DeadlineExceeded, baselineOK: true},
			chainVerified: true,
			want:          VerdictUnusable,
		},
		{
			name: "timeout with answering destination but unverified chain",
			item: outcome{port: 8000, rtt: 3 * time.Second, err: context.DeadlineExceeded, baselineOK: true},
			want: VerdictUnknown,
		},
		{
			name:          "timeout with silent destination",
			item:          outcome{port: 8000, rtt: 3 * time.Second, err: context.DeadlineExceeded},
			chainVerified: true,
			want:          VerdictUnknown,
		},
		{
			name:      "refused by peer",
			item:      outcome{port: 8080, rtt: 190 * time.Millisecond, err: fmt.Errorf("connect: connection refused")},
			reference: 200 * time.Millisecond,
			want:      VerdictAllowed,
		},
		{
			name:      "refused too fast",
			item:      outcome{port: 8080, rtt: 5 * time.Millisecond, err: fmt.Errorf("connect: connection refused")},
			reference: 200 * time.Millisecond,
			want:      VerdictBlocked,
		},
		{
			name:        "refused with rejecting outbound",
			item:        outcome{port: 8080, rtt: 3 * time.Millisecond, err: fmt.Errorf("connect: connection refused")},
			fingerprint: Fingerprint{Known: true, Rejects: true, Delay: 2 * time.Millisecond},
			want:        VerdictBlocked,
		},
		{
			name:        "refused with silent outbound",
			item:        outcome{port: 8080, rtt: 150 * time.Millisecond, err: fmt.Errorf("connect: connection refused")},
			fingerprint: Fingerprint{Known: true},
			want:        VerdictAllowed,
		},
		{
			name:   "refused by all ports open destination",
			client: Client{AllPortsOpen: true},
			item:   outcome{port: 8080, rtt: 150 * time.Millisecond, err: fmt.Errorf("connect: connection refused")},
			want:   VerdictBlocked,
		},
		{
			name: "unreachable",
			item: outcome{port: 8080, rtt: 2 * time.Millisecond, err: fmt.Errorf("connect: %w", syscall.EHOSTUNREACH)},
			want: VerdictBlocked,
		},
		{
			name: "socks not allowed",
			item: outcome{port: 8080, rtt: 2 * time.Millisecond, err: fmt.Errorf("socks5: request rejected, code=2")},
			want: VerdictBlocked,
		},
		{
			name:      "socks refused",
			item:      outcome{port: 8080, rtt: 190 * time.Millisecond, err: fmt.Errorf("socks5: request rejected, code=5")},
			reference: 200 * time.Millisecond,
			want:      VerdictAllowed,
		},
		{
			name:   "connected without http response",
			client: Client{},
			item:   outcome{port: 8080, rtt: 150 * time.Millisecond, err: fmt.Errorf("read response: EOF"), verifyFailed: true},
			want:   VerdictUnknown,
		},
		{
			name:   "connected without http response on an all ports open destination",
			client: Client{AllPortsOpen: true},
			item:   outcome{port: 8000, rtt: 150 * time.Millisecond, err: fmt.Errorf("read response: EOF"), verifyFailed: true, baselineOK: true},
			want:   VerdictUnusable,
		},
		{
			name:   "connected without http response and a silent destination",
			client: Client{Baseline: true},
			item:   outcome{port: 8000, rtt: 150 * time.Millisecond, err: fmt.Errorf("read response: EOF"), verifyFailed: true},
			want:   VerdictUnknown,
		},
		{
			name: "unknown error",
			item: outcome{port: 8080, rtt: 150 * time.Millisecond, err: fmt.Errorf("unexpected EOF")},
			want: VerdictUnknown,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			client := testCase.client
			result := client.classify(testCase.item, testCase.reference, testCase.fingerprint, testCase.chainVerified)
			if result.Verdict != testCase.want {
				t.Fatalf("expected %v, got %v (%s)", testCase.want, result.Verdict, result.Detail)
			}
			if result.Port != testCase.item.port {
				t.Fatalf("expected port %d, got %d", testCase.item.port, result.Port)
			}
		})
	}
}

func TestVerifiedSemantics(t *testing.T) {
	t.Parallel()
	connected := outcome{port: 443, ok: true, rtt: 150 * time.Millisecond}

	checked := (&Client{}).classify(connected, 0, Fingerprint{}, true)
	if !checked.Verified {
		t.Fatal("expected a connection with an http response to be verified")
	}

	refused := fmt.Errorf("connect: connection refused")
	fakeEntry := (&Client{}).classify(
		outcome{port: 8080, rtt: 190 * time.Millisecond, err: refused},
		200*time.Millisecond, Fingerprint{}, true,
	)
	if !fakeEntry.Verified {
		t.Fatal("expected a refusal from the peer to be verified")
	}
}

func TestCommonPortsUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[int]struct{}, len(CommonPorts))
	for _, port := range CommonPorts {
		if _, loaded := seen[port]; loaded {
			t.Fatalf("port %d is listed twice", port)
		}
		seen[port] = struct{}{}
		if PortName(port) == "" {
			t.Fatalf("port %d has no service name", port)
		}
	}
}

func TestPortName(t *testing.T) {
	t.Parallel()
	if got := PortName(443); got != "https" {
		t.Fatalf("expected %q, got %q", "https", got)
	}
	if got := PortName(12345); got != "" {
		t.Fatalf("expected no name for an unknown port, got %q", got)
	}
}
