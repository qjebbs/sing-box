package group

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/interrupt"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/atomic"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

func RegisterSelectorProvider(registry *outbound.Registry) {
	outbound.Register[option.ProviderSelectorOptions](registry, C.TypeSelector, NewSelectorProvider)
}

var (
	_ adapter.Outbound      = (*SelectorProvider)(nil)
	_ adapter.OutboundGroup = (*SelectorProvider)(nil)
)

type SelectorProvider struct {
	outbound.GroupAdapter
	ctx                          context.Context
	logger                       log.ContextLogger
	outbound                     adapter.OutboundManager
	provider                     adapter.ProviderManager
	connection                   adapter.ConnectionManager
	defaultTag                   string
	selected                     atomic.TypedValue[adapter.Outbound]
	interruptGroup               *interrupt.Group
	interruptExternalConnections bool
}

func NewSelectorProvider(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.ProviderSelectorOptions) (adapter.Outbound, error) {
	SelectorProvider := &SelectorProvider{
		GroupAdapter:                 outbound.NewGroupAdapter(C.TypeSelector, tag, []string{N.NetworkTCP, N.NetworkUDP}, router, options.ProviderGroupCommonOption),
		ctx:                          ctx,
		logger:                       logger,
		outbound:                     service.FromContext[adapter.OutboundManager](ctx),
		provider:                     service.FromContext[adapter.ProviderManager](ctx),
		connection:                   service.FromContext[adapter.ConnectionManager](ctx),
		defaultTag:                   options.Default,
		interruptGroup:               interrupt.NewGroup(),
		interruptExternalConnections: options.InterruptExistConnections,
	}
	return SelectorProvider, nil
}

func (s *SelectorProvider) Network() []string {
	selected := s.selected.Load()
	if selected == nil {
		return []string{N.NetworkTCP, N.NetworkUDP}
	}
	return selected.Network()
}

func (s *SelectorProvider) Start() error {
	if err := s.InitProviders(s.outbound, s.provider); err != nil {
		return err
	}
	if tag := s.Tag(); tag != "" {
		cacheFile := service.FromContext[adapter.CacheFile](s.ctx)
		if cacheFile != nil {
			selected := cacheFile.LoadSelected(tag)
			if selected != "" {
				detour, loaded := s.Outbound(selected)
				if loaded {
					s.selected.Store(detour)
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
		s.selected.Store(detour)
		return nil
	}
	return nil
}

func (s *SelectorProvider) Now() string {
	selected := s.selected.Load()
	if selected == nil {
		return ""
	}
	return selected.Tag()

}

func (s *SelectorProvider) SelectOutbound(tag string) bool {
	detour, loaded := s.Outbound(tag)
	if !loaded {
		return false
	}
	if s.selected.Swap(detour) == detour {
		return true
	}
	if me := s.Tag(); me != "" {
		cacheFile := service.FromContext[adapter.CacheFile](s.ctx)
		if cacheFile != nil {
			err := cacheFile.StoreSelected(me, tag)
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
	conn, err := s.selected.Load().DialContext(ctx, network, destination)
	if err != nil {
		return nil, err
	}
	return s.interruptGroup.NewConn(conn, interrupt.IsExternalConnectionFromContext(ctx)), nil
}

func (s *SelectorProvider) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	if err := s.ensureSelected(); err != nil {
		return nil, err
	}
	conn, err := s.selected.Load().ListenPacket(ctx, destination)
	if err != nil {
		return nil, err
	}
	return s.interruptGroup.NewPacketConn(conn, interrupt.IsExternalConnectionFromContext(ctx)), nil
}

func (s *SelectorProvider) NewConnectionEx(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	if err := s.ensureSelected(); err != nil {
		s.connection.NewConnection(ctx, newErrDailer(err), conn, metadata, onClose)
		return
	}
	ctx = interrupt.ContextWithIsExternalConnection(ctx)
	selected := s.selected.Load()
	if outboundHandler, isHandler := selected.(adapter.ConnectionHandlerEx); isHandler {
		outboundHandler.NewConnectionEx(ctx, conn, metadata, onClose)
	} else {
		s.connection.NewConnection(ctx, selected, conn, metadata, onClose)
	}
}

func (s *SelectorProvider) NewPacketConnectionEx(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	if err := s.ensureSelected(); err != nil {
		s.connection.NewPacketConnection(ctx, newErrDailer(err), conn, metadata, onClose)
		return
	}
	ctx = interrupt.ContextWithIsExternalConnection(ctx)
	selected := s.selected.Load()
	if outboundHandler, isHandler := selected.(adapter.PacketConnectionHandlerEx); isHandler {
		outboundHandler.NewPacketConnectionEx(ctx, conn, metadata, onClose)
	} else {
		s.connection.NewPacketConnection(ctx, selected, conn, metadata, onClose)
	}
}

func (s *SelectorProvider) ensureSelected() error {
	if s.selected.Load() != nil {
		return nil
	}
	all := s.Outbounds()
	if len(all) == 0 {
		// "all" can be empty, only when s.outbounds is empty
		// and s.providers is not empty but not loaded yet.
		return E.New("no outbound available, providers are not loaded yet")
	}
	s.selected.Store(all[0])
	return nil
}
