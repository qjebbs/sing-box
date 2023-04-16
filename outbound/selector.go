package outbound

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

var (
	_ adapter.Outbound      = (*Selector)(nil)
	_ adapter.OutboundGroup = (*Selector)(nil)
)

type Selector struct {
	myOutboundGroupAdapter
	defaultTag string

	selected adapter.Outbound
}

func NewSelector(router adapter.Router, logger log.ContextLogger, tag string, options option.SelectorOutboundOptions) (*Selector, error) {
	selector := &Selector{
		myOutboundGroupAdapter: myOutboundGroupAdapter{
			myOutboundAdapter: myOutboundAdapter{
				protocol: C.TypeSelector,
				router:   router,
				logger:   logger,
				tag:      tag,
			},
			options: options.GroupCommonOption,
		},
		defaultTag: options.Default,
	}
	return selector, nil
}

func (s *Selector) Network() []string {
	if s.selected == nil {
		return []string{N.NetworkTCP, N.NetworkUDP}
	}
	return s.selected.Network()
}

func (s *Selector) Start() error {
	if err := s.initProviders(); err != nil {
		return err
	}
	if s.tag != "" {
		if clashServer := s.router.ClashServer(); clashServer != nil && clashServer.StoreSelected() {
			selected := clashServer.CacheFile().LoadSelected(s.tag)
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

	s.selected = s.Outbounds()[0]
	return nil
}

func (s *Selector) Now() string {
	return s.selected.Tag()
}

func (s *Selector) SelectOutbound(tag string) bool {
	detour, loaded := s.Outbound(tag)
	if !loaded {
		return false
	}
	s.selected = detour
	if s.tag != "" {
		if clashServer := s.router.ClashServer(); clashServer != nil && clashServer.StoreSelected() {
			err := clashServer.CacheFile().StoreSelected(s.tag, tag)
			if err != nil {
				s.logger.Error("store selected: ", err)
			}
		}
	}
	return true
}

func (s *Selector) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	return s.selected.DialContext(ctx, network, destination)
}

func (s *Selector) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return s.selected.ListenPacket(ctx, destination)
}

func (s *Selector) NewConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext) error {
	return s.selected.NewConnection(ctx, conn, metadata)
}

func (s *Selector) NewPacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext) error {
	return s.selected.NewPacketConnection(ctx, conn, metadata)
}
