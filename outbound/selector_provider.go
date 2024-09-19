package outbound

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/interrupt"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

var (
	_ adapter.Outbound      = (*SelectorProvider)(nil)
	_ adapter.OutboundGroup = (*SelectorProvider)(nil)
)

type SelectorProvider struct {
	myOutboundGroupAdapter
	ctx                          context.Context
	defaultTag                   string
	selected                     adapter.Outbound
	interruptGroup               *interrupt.Group
	interruptExternalConnections bool
}

func NewSelectorProvider(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.ProviderSelectorOptions) (*SelectorProvider, error) {
	SelectorProvider := &SelectorProvider{
		myOutboundGroupAdapter: myOutboundGroupAdapter{
			myOutboundAdapter: myOutboundAdapter{
				protocol:     C.TypeSelector,
				router:       router,
				logger:       logger,
				tag:          tag,
				dependencies: options.Outbounds,
			},
			options: options.ProviderGroupCommonOption,
		},
		ctx:                          ctx,
		defaultTag:                   options.Default,
		interruptGroup:               interrupt.NewGroup(),
		interruptExternalConnections: options.InterruptExistConnections,
	}
	return SelectorProvider, nil
}

func (s *SelectorProvider) Network() []string {
	if s.selected == nil {
		return []string{N.NetworkTCP, N.NetworkUDP}
	}
	return s.selected.Network()
}

func (s *SelectorProvider) Start() error {
	if err := s.initProviders(); err != nil {
		return err
	}
	if s.tag != "" {
		cacheFile := service.FromContext[adapter.CacheFile](s.ctx)
		if cacheFile != nil {
			selected := cacheFile.LoadSelected(s.tag)
			if selected != "" {
				detour, loaded := s.Outbound(selected)
				if loaded {
					s.selected = detour
					return nil
				}
			}
		}
	}

	if s.defaultTag != "" {
		detour, loaded := s.Outbound(s.defaultTag)
		if !loaded {
			return E.New("default outbound not found: ", s.defaultTag)
		}
		s.selected = detour
		return nil
	}
	return nil
}

func (s *SelectorProvider) Now() string {
	if s.selected == nil {
		return ""
	}
	return s.selected.Tag()
}

func (s *SelectorProvider) SelectOutbound(tag string) bool {
	detour, loaded := s.Outbound(tag)
	if !loaded {
		return false
	}
	if s.selected == detour {
		return true
	}
	s.selected = detour
	if s.tag != "" {
		cacheFile := service.FromContext[adapter.CacheFile](s.ctx)
		if cacheFile != nil {
			err := cacheFile.StoreSelected(s.tag, tag)
			if err != nil {
				s.logger.Error("store selected: ", err)
			}
		}
	}
	s.interruptGroup.Interrupt(s.interruptExternalConnections)
	return true
}

func (s *SelectorProvider) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	if err := s.ensureSelected(); err != nil {
		return nil, err
	}
	conn, err := s.selected.DialContext(ctx, network, destination)
	if err != nil {
		return nil, err
	}
	return s.interruptGroup.NewConn(conn, interrupt.IsExternalConnectionFromContext(ctx)), nil
}

func (s *SelectorProvider) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	if err := s.ensureSelected(); err != nil {
		return nil, err
	}
	conn, err := s.selected.ListenPacket(ctx, destination)
	if err != nil {
		return nil, err
	}
	return s.interruptGroup.NewPacketConn(conn, interrupt.IsExternalConnectionFromContext(ctx)), nil
}

func (s *SelectorProvider) NewConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext) error {
	if err := s.ensureSelected(); err != nil {
		return err
	}
	ctx = interrupt.ContextWithIsExternalConnection(ctx)
	return s.selected.NewConnection(ctx, conn, metadata)
}

func (s *SelectorProvider) NewPacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext) error {
	if err := s.ensureSelected(); err != nil {
		return err
	}
	ctx = interrupt.ContextWithIsExternalConnection(ctx)
	return s.selected.NewPacketConnection(ctx, conn, metadata)
}

func (s *SelectorProvider) ensureSelected() error {
	if s.selected != nil {
		return nil
	}
	all := s.Outbounds()
	if len(all) == 0 {
		// "all" can be empty, only when s.outbounds is empty
		// and s.providers is not empty but not loaded yet.
		return E.New("no outbound available, providers are not loaded yet")
	}
	s.selected = all[0]
	return nil
}
