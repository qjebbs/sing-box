package remote

import (
	"context"
	"encoding/base64"
	"io"
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
	"github.com/sagernet/sing-box/experimental/deprecated"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/service"
)

// RegisterRemote registers the remote provider.
func RegisterRemote(registry *provider.Registry) {
	provider.Register(registry, C.ProviderHTTP, NewRemote)
}

var _ adapter.Provider = (*Remote)(nil)
var _ adapter.Lifecycle = (*Remote)(nil)
var _ adapter.ProviderInfoer = (*Remote)(nil)
var _ adapter.Service = (*Remote)(nil)

// Remote is a remote outbounds provider.
type Remote struct {
	parentCtx  context.Context
	router     adapter.Router
	outbound   adapter.OutboundManager
	logFactory log.Factory
	logger     log.ContextLogger
	tag        string
	userAgent  string
	exclude    *regexp.Regexp
	include    *regexp.Regexp
	options    option.RemoteProviderOptions

	// access guards the state below. It is never held over a download, nor over the creation
	// of the outbounds: both are slow, and a connection reads the nodes through it.
	access sync.Mutex
	info   *adapter.ProviderInfo
	// ready is closed by the first finished fetch, whatever it returned. It is created once,
	// so Ready needs no lock, and readyClosed is what keeps it to one close.
	ready          chan struct{}
	readyClosed    bool
	closed         bool
	ctx            context.Context
	cancel         context.CancelFunc
	httpClient     *http.Client
	loadedHash     string
	updatedAt      time.Time
	outbounds      []adapter.Outbound
	outboundsByTag map[string]adapter.Outbound
	// update serializes the updates themselves, which are not run under access: the download
	// takes as long as the subscription server does.
	update sync.Mutex
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
		err                          error
		excludeRegexp, includeRegexp *regexp.Regexp
	)
	if options.Exclude != "" {
		excludeRegexp, err = regexp.Compile(options.Exclude)
		if err != nil {
			return nil, err
		}
	}
	if options.Include != "" {
		includeRegexp, err = regexp.Compile(options.Include)
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
	options.Interval = badoption.Duration(interval)
	ua := "ProxySubscriber/0.6.0  Shadowrocket/2070"
	logger := logFactory.NewLogger(F.ToString("provider/remote", "[", tag, "]"))
	return &Remote{
		router:     router,
		logger:     logger,
		parentCtx:  ctx,
		logFactory: logFactory,
		outbound:   service.FromContext[adapter.OutboundManager](ctx),

		tag:       tag,
		userAgent: ua,
		exclude:   excludeRegexp,
		include:   includeRegexp,
		options:   options,

		ctx:   ctx,
		ready: make(chan struct{}),
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

// Info implements Infoer. It returns a copy: every update replaces the record, and a reader
// holding the pointer would see it change under itself.
func (s *Remote) Info() *adapter.ProviderInfo {
	s.access.Lock()
	defer s.access.Unlock()
	if s.info == nil {
		return nil
	}
	info := *s.info
	return &info
}

// Start starts the provider.
func (s *Remote) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	switch stage {
	case adapter.StartStateInitialize:
		s.access.Lock()
		defer s.access.Unlock()
		if s.cancel != nil {
			return nil
		}
		transport, err := s.resolveTransport()
		if err != nil {
			return E.Cause(err, "create provider http client")
		}
		s.httpClient = &http.Client{
			Timeout:   time.Second * 30,
			Transport: transport,
		}
		// The runtime context derives from the scope: closing the scope cancels it first
		// (aborting downloads and the refresh loop) and then runs the registered cleanups.
		s.ctx, s.cancel = context.WithCancel(scope.Context())
	case adapter.StartStateStart:
		scope.Add(s.Close)
		s.quickStart()
		go s.refreshLoop()
	}
	return nil
}

// Close closes the service.
func (s *Remote) Close() error {
	s.access.Lock()
	if s.closed {
		s.access.Unlock()
		return nil
	}
	s.closed = true
	cancel := s.cancel
	httpClient := s.httpClient
	outbounds := s.outbounds
	s.outboundsByTag = nil
	s.outbounds = nil
	s.access.Unlock()
	// The context is cancelled outside the lock, and before the nodes are removed: it aborts
	// the download an update may be waiting on, so the update gives up instead of putting its
	// nodes back into a provider that is going away.
	if cancel != nil {
		cancel()
	}
	if httpClient != nil {
		httpClient.CloseIdleConnections()
	}
	return s.removeOutbounds(outbounds)
}

// removeOutbounds takes the nodes out of the outbound manager. It is what a close does with the
// nodes it holds, and what an update does with the ones it built for a provider that was closed
// under it.
func (s *Remote) removeOutbounds(outbounds []adapter.Outbound) error {
	if s.outbound == nil {
		return nil
	}
	var err error
	for _, ob := range outbounds {
		if err2 := s.outbound.Remove(ob.Tag()); err2 != nil {
			err = E.Append(err, err2, func(err error) error {
				return E.Cause(err, "close outbound [", ob.Tag(), "]")
			})
		}
	}
	return err
}

// Wait implements adapter.Provider
func (s *Remote) Wait() {
	<-s.Ready()
}

// Ready returns a channel that's closed when provider is ready.
func (s *Remote) Ready() <-chan struct{} {
	return s.ready
}

// closeReadyLocked marks the first fetch as finished, whatever it returned: the nodes are then
// either loaded or known to be unavailable, and a reader waiting for them has an answer.
func (s *Remote) closeReadyLocked() {
	if s.readyClosed {
		return
	}
	s.readyClosed = true
	close(s.ready)
}

func (s *Remote) refreshLoop() {
	ticker := time.NewTicker(time.Duration(s.options.Interval))
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
	s.access.Lock()
	defer s.access.Unlock()
	return s.outbounds
}

// Outbound returns the outbound from the provider.
func (s *Remote) Outbound(tag string) (adapter.Outbound, bool) {
	s.access.Lock()
	defer s.access.Unlock()
	if s.outboundsByTag == nil {
		return nil, false
	}
	detour, ok := s.outboundsByTag[tag]
	return detour, ok
}

// UpdatedAt implements adapter.Provider
func (s *Remote) UpdatedAt() time.Time {
	s.access.Lock()
	defer s.access.Unlock()
	return s.updatedAt
}

// quickStart loads the outbounds of the cache file, so the provider has nodes before its first
// fetch finishes, which is what a start without a network needs.
func (s *Remote) quickStart() {
	if s.options.CacheFile == "" {
		return
	}
	fc, err := loadCache(s.options.CacheFile)
	if err != nil {
		return
	}
	outbounds, outboundsByTag := s.buildOutbounds(fc.links)
	s.access.Lock()
	defer s.access.Unlock()
	s.applyContent(fc, outbounds, outboundsByTag)
	s.closeReadyLocked()
}

// Update fetches and updates outbounds from the provider.
//
// The download and the creation of the outbounds are the slow parts of it, and both run without
// the lock: a connection reads the nodes of the provider through it, and the first fetch of a
// slow subscription would hold them for as long as the server takes. update keeps two updates
// apart instead, and the swap that follows is all that needs the lock.
func (s *Remote) Update() error {
	s.update.Lock()
	defer s.update.Unlock()
	s.access.Lock()
	loadedHash := s.loadedHash
	s.access.Unlock()
	// cache file is useful in cases that the first fetch will fail,
	// which happens mostly when the network is not ready:
	// - started as a service, and the network is not initilaized yet
	// - disconnected
	// without cache file, the outbounds will not be loaded until next
	// loop, usually 1 hour later.
	c, err := s.downloadWithCache(loadedHash)
	if err != nil {
		s.access.Lock()
		s.closeReadyLocked()
		s.access.Unlock()
		return err
	}
	var (
		outbounds      []adapter.Outbound
		outboundsByTag map[string]adapter.Outbound
	)
	changed := c.linksHash != loadedHash
	if changed {
		outbounds, outboundsByTag = s.buildOutbounds(c.links)
	}
	s.access.Lock()
	defer s.access.Unlock()
	s.closeReadyLocked()
	if s.closed {
		// the close took the nodes while this update was in flight: the ones just built are
		// not going to be used either
		s.removeOutbounds(outbounds)
		return nil
	}
	if !changed {
		s.applyInfo(c)
		return nil
	}
	s.applyContent(c, outbounds, outboundsByTag)
	return nil
}

// applyContent puts the outbounds of a subscription in place. It runs under access.
func (s *Remote) applyContent(c *fileContent, outbounds []adapter.Outbound, outboundsByTag map[string]adapter.Outbound) {
	s.applyInfo(c)
	s.loadedHash = c.linksHash
	s.outbounds = outbounds
	s.outboundsByTag = outboundsByTag
}

// applyInfo records what the subscription says about itself, which is not the nodes: it is
// stored even when the content did not change, because the counters of a subscription move on
// their own. It runs under access.
func (s *Remote) applyInfo(c *fileContent) {
	s.updatedAt = c.updated
	s.info = c.ProviderInfo
}

// buildOutbounds creates the outbounds of the links of a subscription. It reads only what a
// start sets, so it runs without the lock: creating hundreds of outbounds is slow, and the set
// it replaces stays readable while it does.
func (s *Remote) buildOutbounds(content string) ([]adapter.Outbound, map[string]adapter.Outbound) {
	outbounds := make([]adapter.Outbound, 0)
	outboundsByTag := make(map[string]adapter.Outbound)

	links := s.parseLinks(content, s.options.DedupHost, s.options.DedupHostPort)
	for _, link := range links {
		opt, err := link.Link.Outbound(s.options.OutboundsDefault)
		if err != nil {
			s.logger.Warn("line ", link.Line, ": make options:", err)
			continue
		}
		if !s.matchExcludeInclude(opt.Tag) {
			s.logger.Info("outbound ", opt.Tag, " filtered out by exclude/include")
			continue
		}
		outbound, err := s.createOutbound(opt)
		if err != nil {
			s.logger.Warn("line ", link.Line, ": ", err)
			continue
		}
		outbounds = append(outbounds, outbound)
		outboundsByTag[outbound.Tag()] = outbound
	}
	s.logger.Info(len(outbounds), " outbounds available")
	return outbounds, outboundsByTag
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
			scheme: lnk.URL.Scheme,
			host:   lnk.URL.Hostname(),
			port:   lnk.URL.Port(),
		}
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

// matchExcludeInclude reports whether a node with the given tag should be
// kept, according to the exclude and include regular expressions.
// A node is kept only when it does not match exclude and (if include is
// configured) matches include. The priority of exclude is higher than include.
func (s *Remote) matchExcludeInclude(tag string) bool {
	if s.exclude != nil && s.exclude.MatchString(tag) {
		return false
	}
	if s.include != nil && !s.include.MatchString(tag) {
		return false
	}
	return true
}

func (s *Remote) createOutbound(opt *option.Outbound) (adapter.Outbound, error) {
	tag := s.tag + "/" + opt.Tag
	err := s.outbound.Create(
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

// downloadWithCache fetches the subscription, and falls back to the cache file when the fetch
// fails and this run holds nothing yet. loadedHash is the content this run already has, read
// under the lock by the caller: it is what tells a failed fetch that the nodes are worth
// keeping.
func (s *Remote) downloadWithCache(loadedHash string) (*fileContent, error) {
	fc, err := s.download()
	if err == nil {
		if s.options.CacheFile != "" {
			if err := saveCacheIfNeed(s.options.CacheFile, fc); err != nil {
				s.logger.Error(E.Cause(err, "save cache file"))
			}
		}
		return fc, nil
	}
	errfetch := E.Cause(err, "fetch provider")
	if loadedHash != "" {
		return nil, errfetch
	}
	if s.options.CacheFile == "" {
		return nil, err
	}
	fc, err = loadCache(s.options.CacheFile)
	if err == nil {
		s.logger.Info("cache file loaded due to: ", errfetch)
		return fc, nil
	}
	s.logger.Error(E.Cause(err, "load cache file"))
	return nil, err
}

func (s *Remote) download() (*fileContent, error) {
	if s.httpClient == nil {
		return nil, E.New("http client not initialized")
	}
	defer s.httpClient.CloseIdleConnections()
	req, err := http.NewRequestWithContext(s.ctx, http.MethodGet, s.options.URL, nil)
	if err != nil {
		return nil, err
	}
	if !s.options.DisableUserAgent {
		req.Header.Set("User-Agent", s.userAgent)
	}
	resp, err := s.httpClient.Do(req)
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

func (s *Remote) resolveTransport() (adapter.HTTPTransport, error) {
	httpClientManager := service.FromContext[adapter.HTTPClientManager](s.ctx)
	if s.options.HTTPClient != nil && !s.options.HTTPClient.IsEmpty() {
		if s.options.DownloadDetour != "" { //nolint:staticcheck
			return nil, E.New("http_client is conflict with deprecated download_detour field")
		}
		return httpClientManager.ResolveTransport(s.ctx, s.logger, *s.options.HTTPClient)
	}
	if s.options.DownloadDetour != "" { //nolint:staticcheck
		deprecated.Report(s.ctx, deprecated.OptionLegacyProviderDownloadDetour)
		return httpClientManager.ResolveTransport(s.ctx, s.logger, option.HTTPClientOptions{
			DialerOptions: option.DialerOptions{
				Detour: s.options.DownloadDetour, //nolint:staticcheck
			},
			DisableEmptyDirectCheck: true,
		})
	}
	defaultTransport := httpClientManager.DefaultTransport()
	if defaultTransport == nil {
		return nil, E.New("default http client transport is not initialized")
	}
	return defaultTransport, nil
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
