package outbound

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type testOptions struct {
	option.DialerOptions
	Value string `json:"value,omitempty"`
}

type groupOptions struct{}

type captureOutbound struct {
	tag      string
	detour   N.Dialer
	value    string
	dialed   bool
	packeted bool
}

func (o *captureOutbound) Type() string           { return "capture" }
func (o *captureOutbound) Tag() string            { return o.tag }
func (o *captureOutbound) Network() []string      { return nil }
func (o *captureOutbound) Dependencies() []string { return nil }

func (o *captureOutbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	o.dialed = true
	return nil, nil
}

func (o *captureOutbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	o.packeted = true
	return nil, nil
}

var _ adapter.Outbound = (*captureOutbound)(nil)

type stubDialer struct {
	id string
}

func (d *stubDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	return nil, nil
}

func (d *stubDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return nil, nil
}

var _ N.Dialer = (*stubDialer)(nil)

func newCaptureManager(t *testing.T) *Manager {
	t.Helper()
	registry := NewRegistry()
	Register[testOptions](registry, "capture", func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options testOptions) (adapter.Outbound, error) {
		return &captureOutbound{tag: tag, detour: options.DetourDialer, value: options.Value}, nil
	})
	Register[groupOptions](registry, "group", func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options groupOptions) (adapter.Outbound, error) {
		return &captureOutbound{tag: tag}, nil
	})
	return NewManager(registry, endpoint.NewManager(nil), "")
}

func TestCreateDetourPointsTheCopyAtTheGivenDetour(t *testing.T) {
	t.Parallel()
	manager := newCaptureManager(t)
	ctx := context.Background()
	logger := log.NewNOPFactory().Logger()
	scope := adapter.NewScope(ctx, logger)
	original := &testOptions{DialerOptions: option.DialerOptions{Detour: "configured"}, Value: "kept"}
	if err := manager.Create(ctx, nil, logger, "node", "capture", original); err != nil {
		t.Fatal(err)
	}
	detour := &stubDialer{id: "detour"}
	detoured, err := manager.CreateDetour(ctx, nil, logger, "node", detour, scope)
	if err != nil {
		t.Fatal(err)
	}
	created := detoured.(*captureOutbound)
	if created.detour != N.Dialer(detour) {
		t.Fatal("want the copy to dial through the given detour")
	}
	if created.value != "kept" {
		t.Fatalf("want the other options preserved, got %q", created.value)
	}
	if created.tag != "node" {
		t.Fatalf("want the copy to keep the tag of the outbound it copies, got %q", created.tag)
	}
	if original.DetourDialer != nil || original.Detour != "configured" {
		t.Fatal("want the configured options left untouched")
	}
}

func TestCreateDetourReportsAnOutboundWithoutDialFields(t *testing.T) {
	t.Parallel()
	manager := newCaptureManager(t)
	ctx := context.Background()
	logger := log.NewNOPFactory().Logger()
	if err := manager.Create(ctx, nil, logger, "node", "group", &groupOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err := manager.CreateDetour(ctx, nil, logger, "node", &stubDialer{id: "detour"}, adapter.NewScope(ctx, logger))
	var noDialFields *adapter.NoDialFieldsError
	if !errors.As(err, &noDialFields) {
		t.Fatalf("want a NoDialFieldsError, got %v", err)
	}
	if noDialFields.OutboundType != "group" {
		t.Fatalf("want the type of the outbound reported, got %q", noDialFields.OutboundType)
	}
}

func TestCreateDetourReportsAnUnknownTag(t *testing.T) {
	t.Parallel()
	manager := newCaptureManager(t)
	ctx := context.Background()
	logger := log.NewNOPFactory().Logger()
	_, err := manager.CreateDetour(ctx, nil, logger, "missing", &stubDialer{id: "detour"}, adapter.NewScope(ctx, logger))
	if err == nil {
		t.Fatal("want an error for a tag that is not an outbound")
	}
}
