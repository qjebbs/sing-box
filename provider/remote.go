package provider

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/link"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/outbound/outbound"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

var _ adapter.Provider = (*Remote)(nil)
var _ Infoer = (*Remote)(nil)

// closedchan is a reusable closed channel.
var closedchan = make(chan struct{})

func init() {
	close(closedchan)
}

// Remote is a remote outbounds provider.
type Remote struct {
	sync.Mutex
	*Information

	chReady chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc

	router     adapter.Router
	parentCtx  context.Context
	logFactory log.Factory
	logger     log.ContextLogger
	tag        string

	url            string
	interval       time.Duration
	cacheFile      string
	downloadDetour string
	exclude        *regexp.Regexp
	include        *regexp.Regexp
	userAgent      string
	disableUA      bool
	dialerOptions  option.DialerOptions

	detour         adapter.Outbound
	loadedHash     string
	updatedAt      time.Time
	outbounds      []adapter.Outbound
	outboundsByTag map[string]adapter.Outbound
}

// NewRemote creates a new remote provider.
func NewRemote(ctx context.Context, router adapter.Router, logger log.ContextLogger, logFactory log.Factory, options option.Provider) (*Remote, error) {
	if options.Tag == "" {
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
	return &Remote{
		router:     router,
		logger:     logger,
		parentCtx:  ctx,
		logFactory: logFactory,

		tag:            options.Tag,
		url:            options.URL,
		interval:       interval,
		cacheFile:      options.CacheFile,
		downloadDetour: options.DownloadDetour,
		userAgent:      ua,
		disableUA:      options.DisableUserAgent,
		exclude:        exclude,
		include:        include,

		dialerOptions: options.DialerOptions,

		ctx:     ctx,
		chReady: make(chan struct{}),
	}, nil
}

// Tag returns the tag of the provider.
func (s *Remote) Tag() string {
	return s.tag
}

// Start starts the provider.
func (s *Remote) Start() error {
	s.Lock()
	defer s.Unlock()

	if s.cancel != nil {
		return nil
	}
	if s.downloadDetour != "" {
		outbound, loaded := s.router.Outbound(s.downloadDetour)
		if !loaded {
			return E.New("detour outbound not found: ", s.downloadDetour)
		}
		s.detour = outbound
	} else {
		detour, err := s.router.DefaultOutbound(N.NetworkTCP)
		if err != nil {
			return err
		}
		s.detour = detour
	}

	_, s.cancel = context.WithCancel(s.ctx)
	go s.refreshLoop()
	return nil
}

// Close closes the service.
func (s *Remote) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
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
	s.Information = c.Information
	if s.loadedHash == c.linksHash {
		return nil
	}
	s.loadedHash = c.linksHash
	opts, err := s.getOutboundsOptions(c.links)
	if err != nil {
		return err
	}
	s.logger.Info(len(opts), " links found")
	s.updateOutbounds(opts)
	return nil
}

func (s *Remote) updateOutbounds(opts []*option.Outbound) {
	outbounds := make([]adapter.Outbound, 0, len(opts))
	outboundsByTag := make(map[string]adapter.Outbound)
	for _, opt := range opts {
		tag := opt.Tag
		outbound, err := outbound.Builder(
			s.parentCtx,
			s.router,
			s.logFactory.NewLogger(F.ToString("provider/", opt.Type, "[", tag, "]")),
			tag,
			*opt,
		)
		if err != nil {
			s.logger.Warn("create [", tag, "]: ", err)
			continue
		}
		outbounds = append(outbounds, outbound)
		outboundsByTag[tag] = outbound
	}
	s.outbounds = outbounds
	s.outboundsByTag = outboundsByTag
}

func (s *Remote) getOutboundsOptions(content string) ([]*option.Outbound, error) {
	opts := make([]*option.Outbound, 0)
	links, err := s.parseLinks(content)
	if err != nil {
		return nil, err
	}
	type dialerOptionsReplacer interface {
		ReplaceDialerOptions(options option.DialerOptions)
	}
	for _, link := range links {
		opt, err := link.Outbound()
		if err != nil {
			s.logger.Warn("prepare options for link:", err)
			continue
		}
		if s.exclude != nil && s.exclude.MatchString(opt.Tag) {
			continue
		}
		if s.include != nil && !s.include.MatchString(opt.Tag) {
			continue
		}
		raw, err := opt.RawOptions()
		if err != nil {
			s.logger.Warn("prepare options for link:", err)
			continue
		}
		if replacer, ok := raw.(dialerOptionsReplacer); ok {
			replacer.ReplaceDialerOptions(s.dialerOptions)
		}
		opts = append(opts, opt)
	}
	return opts, nil
}

func (s *Remote) parseLinks(content string) ([]link.Link, error) {
	links, err := link.ParseCollection(content)
	if len(links) > 0 {
		if err != nil {
			s.logger.Warn("links parsed with error:", err)
		}
		return links, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, E.New("no links found")
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
	err = E.Cause(err, "fetch provider")
	if s.loadedHash != "" {
		return nil, err
	}
	if s.cacheFile == "" {
		return nil, err
	}
	if fc, errCache := loadCache(s.cacheFile); errCache == nil {
		s.logger.Info("cache file loaded due to: ", err)
		return fc, nil
	}
	return nil, err
}

func (s *Remote) download() (*fileContent, error) {
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
