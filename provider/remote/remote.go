package remote

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/provider"
	"github.com/sagernet/sing-box/common/link"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"
)

// RegisterRemote registers the remote provider.
func RegisterRemote(registry *provider.Registry) {
	provider.Register(registry, C.ProviderHTTP, NewRemote)
}

var _ adapter.Provider = (*Remote)(nil)
var _ adapter.ProviderInfoer = (*Remote)(nil)
var _ adapter.Service = (*Remote)(nil)

// closedchan is a reusable closed channel.
var closedchan = make(chan struct{})

func init() {
	close(closedchan)
}

// Remote is a remote outbounds provider.
type Remote struct {
	parentCtx  context.Context
	router     adapter.Router
	outbound   adapter.OutboundManager
	logFactory log.Factory
	logger     log.ContextLogger
	tag        string

	url            string
	interval       time.Duration
	cacheFile      string
	downloadDetour string
	exclude        *regexp.Regexp
	include        *regexp.Regexp
	dedupHost      bool
	dedupHostPort  bool
	userAgent      string
	disableUA      bool

	sync.Mutex
	*adapter.ProviderInfo
	chReady        chan struct{}
	ctx            context.Context
	cancel         context.CancelFunc
	detour         adapter.Outbound
	loadedHash     string
	updatedAt      time.Time
	outbounds      []adapter.Outbound
	outboundsByTag map[string]adapter.Outbound
}

// NewRemote creates a new remote provider.
func NewRemote(ctx context.Context, router adapter.Router, logFactory log.Factory, tag string, options option.RemoteProviderOptions) (adapter.Provider, error) {
	if tag == "" {
		return nil, E.New("provider tag is required")
	}
	if options.URL == "" {
		return nil, E.New("provider URL is required")
	}
	var (
		err              error
		exclude, include *regexp.Regexp
	)
	if options.Exclude != "" {
		exclude, err = regexp.Compile(options.Exclude)
		if err != nil {
			return nil, err
		}
	}
	if options.Include != "" {
		include, err = regexp.Compile(options.Include)
		if err != nil {
			return nil, err
		}
	}
	interval := time.Duration(options.Interval)
	if interval <= 0 {
		// default to 1 hour
		interval = time.Hour
	}
	if interval < time.Minute {
		// minimum interval is 1 minute
		interval = time.Minute
	}
	ua := "ProxySubscriber/0.6.0  Shadowrocket/2070"
	logger := logFactory.NewLogger(F.ToString("provider/remote", "[", tag, "]"))
	return &Remote{
		router:     router,
		logger:     logger,
		parentCtx:  ctx,
		logFactory: logFactory,
		outbound:   service.FromContext[adapter.OutboundManager](ctx),

		tag:            tag,
		url:            options.URL,
		interval:       interval,
		cacheFile:      options.CacheFile,
		downloadDetour: options.DownloadDetour,
		userAgent:      ua,
		disableUA:      options.DisableUserAgent,
		exclude:        exclude,
		include:        include,
		dedupHost:      options.DedupHost,
		dedupHostPort:  options.DedupHostPort,

		ctx:     ctx,
		chReady: make(chan struct{}),
	}, nil
}

// Type returns the type of the provider.
func (s *Remote) Type() string {
	return C.ProviderHTTP
}

// Tag returns the tag of the provider.
func (s *Remote) Tag() string {
	return s.tag
}

// Info implements Infoer
func (s *Remote) Info() *adapter.ProviderInfo {
	return s.ProviderInfo
}

// Start starts the provider.
func (s *Remote) Start(stage adapter.StartStage) error {
	s.Lock()
	defer s.Unlock()

	switch stage {
	case adapter.StartStateStart:
		if s.cancel != nil {
			return nil
		}
		if s.downloadDetour != "" {
			outbound, loaded := s.outbound.Outbound(s.downloadDetour)
			if !loaded {
				return E.New("detour outbound not found: ", s.downloadDetour)
			}
			s.detour = outbound
		} else {
			s.detour = s.outbound.Default()
		}
		s.ctx, s.cancel = context.WithCancel(s.ctx)
		go s.refreshLoop()
		return nil
	default:
		return nil
	}
}

// Close closes the service.
func (s *Remote) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	s.Lock()
	defer s.Unlock()
	var err error
	for _, ob := range s.outbounds {
		if err2 := s.outbound.Remove(ob.Tag()); err2 != nil {
			err = E.Append(err, err2, func(err error) error {
				return E.Cause(err, "close outbound [", ob.Tag(), "]")
			})
		}
	}
	s.outbounds = nil
	s.outboundsByTag = nil
	return err
}

// Wait implements adapter.Provider
func (s *Remote) Wait() {
	<-s.Ready()
}

// Ready returns a channel that's closed when provider is ready.
func (s *Remote) Ready() <-chan struct{} {
	s.Lock()
	defer s.Unlock()
	return s.chReady
}

func (s *Remote) refreshLoop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	if err := s.Update(); err != nil {
		s.logger.Error(err)
	}
L:
	for {
		select {
		case <-s.ctx.Done():
			break L
		case <-ticker.C:
			if err := s.Update(); err != nil {
				s.logger.Error(err)
			}
		}
	}
}

// Outbounds returns all the outbounds from the provider.
func (s *Remote) Outbounds() []adapter.Outbound {
	s.Lock()
	defer s.Unlock()
	return s.outbounds
}

// Outbound returns the outbound from the provider.
func (s *Remote) Outbound(tag string) (adapter.Outbound, bool) {
	s.Lock()
	defer s.Unlock()
	if s.outboundsByTag == nil {
		return nil, false
	}
	detour, ok := s.outboundsByTag[tag]
	return detour, ok
}

// UpdatedAt implements adapter.Provider
func (s *Remote) UpdatedAt() time.Time {
	s.Lock()
	defer s.Unlock()
	return s.updatedAt
}

// Update fetches and updates outbounds from the provider.
func (s *Remote) Update() error {
	s.Lock()
	defer s.Unlock()
	if s.chReady != closedchan {
		defer func() {
			close(s.chReady)
			s.chReady = closedchan
		}()
	}
	// cache file is useful in cases that the first fetch will fail,
	// which happens mostly when the network is not ready:
	// - started as a service, and the network is not initilaized yet
	// - disconnected
	// without cache file, the outbounds will not be loaded until next
	// loop, usually 1 hour later.
	c, err := s.downloadWithCache()
	if err != nil {
		return err
	}
	s.updatedAt = c.updated
	s.ProviderInfo = c.ProviderInfo
	if s.loadedHash == c.linksHash {
		return nil
	}
	s.loadedHash = c.linksHash
	s.updateOutbounds(c.links)
	return nil
}

func (s *Remote) updateOutbounds(content string) {
	outbounds := make([]adapter.Outbound, 0)
	outboundsByTag := make(map[string]adapter.Outbound)

	links := s.parseLinks(content, s.dedupHost, s.dedupHostPort)
	for _, link := range links {
		outbound, err := s.createOutbound(link)
		if err != nil {
			s.logger.Warn("line ", link.Line, ": ", err)
			continue
		}
		outbounds = append(outbounds, outbound)
		outboundsByTag[outbound.Tag()] = outbound
	}
	s.logger.Info(len(outbounds), " outbounds available")
	s.outbounds = outbounds
	s.outboundsByTag = outboundsByTag
}

type parsedLink struct {
	Line int
	URL  *url.URL
	Link link.Link
}

func (s *Remote) parseLinks(content string, dedupHost, dedupHostPort bool) []*parsedLink {
	lines := strings.Split(content, "\n")
	links := make([]*parsedLink, 0, len(lines))
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		u, err := url.Parse(line)
		if err != nil {
			s.logger.Warn("line ", i+1, ": ", err)
			continue
		}
		lnk, err := link.ParseURL(u)
		if err != nil {
			s.logger.Warn("line ", i+1, ": ", err)
			continue
		}
		links = append(links, &parsedLink{
			Line: i + 1,
			URL:  u,
			Link: lnk,
		})
	}
	if !dedupHost && !dedupHostPort {
		return links
	}
	type hostport struct {
		scheme string
		host   string
		port   string
	}
	seen := make(map[hostport]struct{})
	deduped := make([]*parsedLink, 0, len(links))
	// reverse the links to keep the last one when deduping, which will remove:
	// - nodes created duplicated for information display.
	// - nodes with lower index when the same node appears multiple times.
	for i := len(links) - 1; i >= 0; i-- {
		lnk := links[i]
		hp := hostport{
			host: lnk.URL.Hostname(),
			port: lnk.URL.Port(),
		}
		hp.scheme = lnk.URL.Scheme
		hp.host = ""
		if !dedupHostPort {
			hp.port = ""
		}
		if _, ok := seen[hp]; ok {
			continue
		}
		seen[hp] = struct{}{}
		deduped = append(deduped, lnk)
	}
	s.logger.Info(len(links)-len(deduped), " duplicate outbounds removed")
	return common.Reverse(deduped)
}

func (s *Remote) createOutbound(lnk *parsedLink) (adapter.Outbound, error) {
	opt, err := lnk.Link.Outbound()
	if err != nil {
		return nil, E.New("line ", lnk.Line, ": make options:", err)
	}
	tag := s.tag + "/" + opt.Tag
	err = s.outbound.Create(
		s.parentCtx,
		s.router,
		s.logFactory.NewLogger(F.ToString("provider/", opt.Type, "[", tag, "]")),
		tag,
		opt.Type,
		opt.Options,
	)
	if err != nil {
		return nil, err
	}
	outbound, loaded := s.outbound.Outbound(tag)
	if !loaded {
		return nil, E.New("outbound [", tag, "] created but not found")
	}
	return outbound, nil
}

func (s *Remote) downloadWithCache() (*fileContent, error) {
	fc, err := s.download()
	if err == nil {
		if s.cacheFile != "" {
			if err := saveCacheIfNeed(s.cacheFile, fc); err != nil {
				s.logger.Error(E.Cause(err, "save cache file"))
			}
		}
		return fc, nil
	}
	errfetch := E.Cause(err, "fetch provider")
	if s.loadedHash != "" {
		return nil, errfetch
	}
	if s.cacheFile == "" {
		return nil, err
	}
	fc, err = loadCache(s.cacheFile)
	if err == nil {
		s.logger.Info("cache file loaded due to: ", errfetch)
		return fc, nil
	}
	s.logger.Error(E.Cause(err, "load cache file"))
	return nil, err
}

func (s *Remote) download() (*fileContent, error) {
	if s.detour == nil {
		return nil, E.New("no detour available for download")
	}
	client := &http.Client{
		Timeout: time.Second * 30,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return s.detour.DialContext(ctx, network, M.ParseSocksaddr(addr))
			},
			// from http.DefaultTransport
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(s.ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, err
	}
	if !s.disableUA {
		req.Header.Set("User-Agent", s.userAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, E.New("unexpected status code: ", resp.StatusCode)
	}
	defer resp.Body.Close()
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseFileContent(string(content), time.Now())
}

func doBase64DecodeOrNothing(s string) string {
	b, err := base64Decode(s)
	if err != nil {
		return s
	}
	return string(b)
}

func base64Decode(b64 string) ([]byte, error) {
	b64 = strings.TrimSpace(b64)
	stdb64 := b64
	if pad := len(b64) % 4; pad != 0 {
		stdb64 += strings.Repeat("=", 4-pad)
	}

	b, err := base64.StdEncoding.DecodeString(stdb64)
	if err != nil {
		return base64.URLEncoding.DecodeString(b64)
	}
	return b, nil
}
