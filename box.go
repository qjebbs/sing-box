package box

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/adapter/provider"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/taskmonitor"
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/experimental"
	"github.com/sagernet/sing-box/experimental/cachefile"
	"github.com/sagernet/sing-box/experimental/libbox/platform"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/route"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
	"github.com/sagernet/sing/common/ntp"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/pause"
)

var _ adapter.Service = (*Box)(nil)

type Box struct {
	createdAt  time.Time
	logFactory log.Factory
	logger     log.ContextLogger
	network    *route.NetworkManager
	endpoint   *endpoint.Manager
	inbound    *inbound.Manager
	outbound   *outbound.Manager
	provider   *provider.Manager
	connection *route.ConnectionManager
	router     *route.Router
	services   []adapter.LifecycleService
	done       chan struct{}
}

type Options struct {
	option.Options
	Context           context.Context
	PlatformLogWriter log.PlatformWriter
}

func Context(
	ctx context.Context,
	inboundRegistry adapter.InboundRegistry,
	outboundRegistry adapter.OutboundRegistry,
	providerRegistry adapter.ProviderRegistry,
	endpointRegistry adapter.EndpointRegistry,
) context.Context {
	if service.FromContext[option.InboundOptionsRegistry](ctx) == nil ||
		service.FromContext[adapter.InboundRegistry](ctx) == nil {
		ctx = service.ContextWith[option.InboundOptionsRegistry](ctx, inboundRegistry)
		ctx = service.ContextWith[adapter.InboundRegistry](ctx, inboundRegistry)
	}
	if service.FromContext[option.OutboundOptionsRegistry](ctx) == nil ||
		service.FromContext[adapter.OutboundRegistry](ctx) == nil {
		ctx = service.ContextWith[option.OutboundOptionsRegistry](ctx, outboundRegistry)
		ctx = service.ContextWith[adapter.OutboundRegistry](ctx, outboundRegistry)
	}
	if service.FromContext[option.ProviderOptionsRegistry](ctx) == nil ||
		service.FromContext[adapter.ProviderRegistry](ctx) == nil {
		ctx = service.ContextWith[option.ProviderOptionsRegistry](ctx, providerRegistry)
		ctx = service.ContextWith[adapter.ProviderRegistry](ctx, providerRegistry)
	}
	if service.FromContext[option.EndpointOptionsRegistry](ctx) == nil ||
		service.FromContext[adapter.EndpointRegistry](ctx) == nil {
		ctx = service.ContextWith[option.EndpointOptionsRegistry](ctx, endpointRegistry)
		ctx = service.ContextWith[adapter.EndpointRegistry](ctx, endpointRegistry)
	}
	return ctx
}

func New(options Options) (*Box, error) {
	createdAt := time.Now()
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = service.ContextWithDefaultRegistry(ctx)

	endpointRegistry := service.FromContext[adapter.EndpointRegistry](ctx)
	inboundRegistry := service.FromContext[adapter.InboundRegistry](ctx)
	outboundRegistry := service.FromContext[adapter.OutboundRegistry](ctx)
	providerRegistry := service.FromContext[adapter.ProviderRegistry](ctx)

	if endpointRegistry == nil {
		return nil, E.New("missing endpoint registry in context")
	}
	if inboundRegistry == nil {
		return nil, E.New("missing inbound registry in context")
	}
	if outboundRegistry == nil {
		return nil, E.New("missing outbound registry in context")
	}

	ctx = pause.WithDefaultManager(ctx)
	experimentalOptions := common.PtrValueOrDefault(options.Experimental)
	applyDebugOptions(common.PtrValueOrDefault(experimentalOptions.Debug))
	var needCacheFile bool
	var needClashAPI bool
	var needV2RayAPI bool
	if experimentalOptions.CacheFile != nil && experimentalOptions.CacheFile.Enabled || options.PlatformLogWriter != nil {
		needCacheFile = true
	}
	if experimentalOptions.ClashAPI != nil || options.PlatformLogWriter != nil {
		needClashAPI = true
	}
	if experimentalOptions.V2RayAPI != nil && experimentalOptions.V2RayAPI.Listen != "" {
		needV2RayAPI = true
	}
	platformInterface := service.FromContext[platform.Interface](ctx)
	var defaultLogWriter io.Writer
	if platformInterface != nil {
		defaultLogWriter = io.Discard
	}
	logFactory, err := log.New(log.Options{
		Context:        ctx,
		Options:        common.PtrValueOrDefault(options.Log),
		Observable:     needClashAPI,
		DefaultWriter:  defaultLogWriter,
		BaseTime:       createdAt,
		PlatformWriter: options.PlatformLogWriter,
	})
	if err != nil {
		return nil, E.Cause(err, "create log factory")
	}

	routeOptions := common.PtrValueOrDefault(options.Route)
	endpointManager := endpoint.NewManager(logFactory.NewLogger("endpoint"), endpointRegistry)
	inboundManager := inbound.NewManager(logFactory.NewLogger("inbound"), inboundRegistry, endpointManager)
	outboundManager := outbound.NewManager(logFactory.NewLogger("outbound"), outboundRegistry, endpointManager, routeOptions.Final)
	providerManager := provider.NewManager(logFactory.NewLogger("provider"), providerRegistry)
	service.MustRegister[adapter.EndpointManager](ctx, endpointManager)
	service.MustRegister[adapter.InboundManager](ctx, inboundManager)
	service.MustRegister[adapter.OutboundManager](ctx, outboundManager)
	service.MustRegister[adapter.ProviderManager](ctx, providerManager)

	networkManager, err := route.NewNetworkManager(ctx, logFactory.NewLogger("network"), routeOptions)
	if err != nil {
		return nil, E.Cause(err, "initialize network manager")
	}
	service.MustRegister[adapter.NetworkManager](ctx, networkManager)
	connectionManager := route.NewConnectionManager(logFactory.NewLogger("connection"))
	service.MustRegister[adapter.ConnectionManager](ctx, connectionManager)
	router, err := route.NewRouter(ctx, logFactory, routeOptions, common.PtrValueOrDefault(options.DNS))
	if err != nil {
		return nil, E.Cause(err, "initialize router")
	}

	ntpOptions := common.PtrValueOrDefault(options.NTP)
	var timeService *tls.TimeServiceWrapper
	if ntpOptions.Enabled {
		timeService = new(tls.TimeServiceWrapper)
		service.MustRegister[ntp.TimeService](ctx, timeService)
	}

	for i, endpointOptions := range options.Endpoints {
		var tag string
		if endpointOptions.Tag != "" {
			tag = endpointOptions.Tag
		} else {
			tag = F.ToString(i)
		}
		endpointCtx := ctx
		if tag != "" {
			// TODO: remove this
			endpointCtx = adapter.WithContext(endpointCtx, &adapter.InboundContext{
				Outbound: tag,
			})
		}
		err = endpointManager.Create(
			endpointCtx,
			router,
			logFactory.NewLogger(F.ToString("endpoint/", endpointOptions.Type, "[", tag, "]")),
			tag,
			endpointOptions.Type,
			endpointOptions.Options,
		)
		if err != nil {
			return nil, E.Cause(err, "initialize inbound[", i, "]")
		}
	}
	for i, inboundOptions := range options.Inbounds {
		var tag string
		if inboundOptions.Tag != "" {
			tag = inboundOptions.Tag
		} else {
			tag = F.ToString(i)
		}
		err = inboundManager.Create(
			ctx,
			router,
			logFactory.NewLogger(F.ToString("inbound/", inboundOptions.Type, "[", tag, "]")),
			tag,
			inboundOptions.Type,
			inboundOptions.Options,
		)
		if err != nil {
			return nil, E.Cause(err, "initialize inbound[", i, "]")
		}
	}
	for i, outboundOptions := range options.Outbounds {
		var tag string
		if outboundOptions.Tag != "" {
			tag = outboundOptions.Tag
		} else {
			tag = F.ToString(i)
		}
		outboundCtx := ctx
		if tag != "" {
			// TODO: remove this
			outboundCtx = adapter.WithContext(outboundCtx, &adapter.InboundContext{
				Outbound: tag,
			})
		}
		err = outboundManager.Create(
			outboundCtx,
			router,
			logFactory.NewLogger(F.ToString("outbound/", outboundOptions.Type, "[", tag, "]")),
			tag,
			outboundOptions.Type,
			outboundOptions.Options,
		)
		if err != nil {
			return nil, E.Cause(err, "initialize outbound[", i, "]")
		}
	}
	outboundManager.Initialize(common.Must1(
		direct.NewOutbound(
			ctx,
			router,
			logFactory.NewLogger("outbound/direct"),
			"direct",
			option.DirectOutboundOptions{},
		),
	))

	for i, opt := range options.Providers {
		var tag string
		if opt.Tag != "" {
			tag = opt.Tag
		} else {
			tag = F.ToString(i)
		}
		err = providerManager.Create(
			ctx,
			router,
			logFactory,
			tag,
			opt.Type,
			opt.Options,
		)
		if err != nil {
			return nil, E.Cause(err, "initialize provider[", i, "]")
		}
	}
	if platformInterface != nil {
		err = platformInterface.Initialize(networkManager)
		if err != nil {
			return nil, E.Cause(err, "initialize platform interface")
		}
	}
	var services []adapter.LifecycleService
	if needCacheFile {
		cacheFile := cachefile.New(ctx, common.PtrValueOrDefault(experimentalOptions.CacheFile))
		service.MustRegister[adapter.CacheFile](ctx, cacheFile)
		services = append(services, cacheFile)
	}
	if needClashAPI {
		clashAPIOptions := common.PtrValueOrDefault(experimentalOptions.ClashAPI)
		clashAPIOptions.ModeList = experimental.CalculateClashModeList(options.Options)
		clashServer, err := experimental.NewClashServer(ctx, logFactory.(log.ObservableFactory), clashAPIOptions)
		if err != nil {
			return nil, E.Cause(err, "create clash-server")
		}
		router.AppendTracker(clashServer)
		service.MustRegister[adapter.ClashServer](ctx, clashServer)
		services = append(services, clashServer)
	}
	if needV2RayAPI {
		v2rayServer, err := experimental.NewV2RayServer(logFactory.NewLogger("v2ray-api"), common.PtrValueOrDefault(experimentalOptions.V2RayAPI))
		if err != nil {
			return nil, E.Cause(err, "create v2ray-server")
		}
		if v2rayServer.StatsService() != nil {
			router.AppendTracker(v2rayServer.StatsService())
			services = append(services, v2rayServer)
			service.MustRegister[adapter.V2RayServer](ctx, v2rayServer)
		}
	}
	if ntpOptions.Enabled {
		ntpDialer, err := dialer.New(ctx, ntpOptions.DialerOptions)
		if err != nil {
			return nil, E.Cause(err, "create NTP service")
		}
		ntpService := ntp.NewService(ntp.Options{
			Context:       ctx,
			Dialer:        ntpDialer,
			Logger:        logFactory.NewLogger("ntp"),
			Server:        ntpOptions.ServerOptions.Build(),
			Interval:      time.Duration(ntpOptions.Interval),
			WriteToSystem: ntpOptions.WriteToSystem,
		})
		timeService.TimeService = ntpService
		services = append(services, adapter.NewLifecycleService(ntpService, "ntp service"))
	}
	return &Box{
		network:    networkManager,
		endpoint:   endpointManager,
		inbound:    inboundManager,
		outbound:   outboundManager,
		provider:   providerManager,
		connection: connectionManager,
		router:     router,
		createdAt:  createdAt,
		logFactory: logFactory,
		logger:     logFactory.Logger(),
		services:   services,
		done:       make(chan struct{}),
	}, nil
}

func (s *Box) PreStart() error {
	err := s.preStart()
	if err != nil {
		// TODO: remove catch error
		defer func() {
			v := recover()
			if v != nil {
				println(err.Error())
				debug.PrintStack()
				panic("panic on early close: " + fmt.Sprint(v))
			}
		}()
		s.Close()
		return err
	}
	s.logger.Info("sing-box pre-started (", F.Seconds(time.Since(s.createdAt).Seconds()), "s)")
	return nil
}

func (s *Box) Start() error {
	err := s.start()
	if err != nil {
		// TODO: remove catch error
		defer func() {
			v := recover()
			if v != nil {
				println(err.Error())
				debug.PrintStack()
				println("panic on early start: " + fmt.Sprint(v))
			}
		}()
		s.Close()
		return err
	}
	s.logger.Info("sing-box started (", F.Seconds(time.Since(s.createdAt).Seconds()), "s)")
	return nil
}

func (s *Box) preStart() error {
	monitor := taskmonitor.New(s.logger, C.StartTimeout)
	monitor.Start("start logger")
	err := s.logFactory.Start()
	monitor.Finish()
	if err != nil {
		return E.Cause(err, "start logger")
	}
	err = adapter.StartNamed(adapter.StartStateInitialize, s.services) // cache-file clash-api v2ray-api
	if err != nil {
		return err
	}
	err = adapter.Start(adapter.StartStateInitialize, s.network, s.connection, s.router, s.provider, s.outbound, s.inbound, s.endpoint)
	if err != nil {
		return err
	}
	err = adapter.Start(adapter.StartStateStart, s.provider, s.outbound, s.network, s.connection, s.router)
	if err != nil {
		return err
	}
	return nil
}

func (s *Box) start() error {
	err := s.preStart()
	if err != nil {
		return err
	}
	err = adapter.StartNamed(adapter.StartStateStart, s.services)
	if err != nil {
		return err
	}
	err = s.inbound.Start(adapter.StartStateStart)
	if err != nil {
		return err
	}
	err = adapter.Start(adapter.StartStateStart, s.endpoint)
	if err != nil {
		return err
	}
	err = adapter.Start(adapter.StartStatePostStart, s.provider, s.outbound, s.network, s.connection, s.router, s.inbound, s.endpoint)
	if err != nil {
		return err
	}
	err = adapter.StartNamed(adapter.StartStatePostStart, s.services)
	if err != nil {
		return err
	}
	err = adapter.Start(adapter.StartStateStarted, s.network, s.connection, s.router, s.outbound, s.provider, s.inbound, s.endpoint)
	if err != nil {
		return err
	}
	err = adapter.StartNamed(adapter.StartStateStarted, s.services)
	if err != nil {
		return err
	}
	return nil
}

func (s *Box) Close() error {
	select {
	case <-s.done:
		return os.ErrClosed
	default:
		close(s.done)
	}
	err := common.Close(
		s.provider, s.inbound, s.outbound, s.endpoint, s.router, s.connection, s.network,
	)
	for _, lifecycleService := range s.services {
		err = E.Append(err, lifecycleService.Close(), func(err error) error {
			return E.Cause(err, "close ", lifecycleService.Name())
		})
	}
	err = E.Append(err, s.logFactory.Close(), func(err error) error {
		return E.Cause(err, "close logger")
	})
	return err
}

func (s *Box) Network() adapter.NetworkManager {
	return s.network
}

func (s *Box) Router() adapter.Router {
	return s.router
}

func (s *Box) Inbound() adapter.InboundManager {
	return s.inbound
}

func (s *Box) Outbound() adapter.OutboundManager {
	return s.outbound
}
