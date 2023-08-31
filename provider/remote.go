package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/link"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/outbound/outbound"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

var _ adapter.Provider = (*Remote)(nil)

// closedchan is a reusable closed channel.
var closedchan = make(chan struct{})

func init() {
	close(closedchan)
}

// Remote is a remote outbounds provider.
type Remote struct {
	sync.Mutex
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
		s.detour = s.router.DefaultOutbound(N.NetworkTCP)
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
	s.updatedAt = c.updatedAt
	if s.loadedHash == c.hash {
		return nil
	}
	s.loadedHash = c.hash
	opts, err := s.getOutboundsOptions(c.content)
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
		}
		outbounds = append(outbounds, outbound)
		outboundsByTag[tag] = outbound
	}
	s.outbounds = outbounds
	s.outboundsByTag = outboundsByTag
}

func (s *Remote) getOutboundsOptions(content []byte) ([]*option.Outbound, error) {
	opts := make([]*option.Outbound, 0)
	links, err := s.parseLinks(content)
	if err != nil {
		return nil, err
	}
	for _, link := range links {
		opt, err := link.Outbound()
		if err != nil {
			s.logger.Warn("prepare options for link:", err)
			continue
		}
		if !s.selectedByTag(opt.Tag) {
			continue
		}
		s.applyOptions(opt)
		opts = append(opts, opt)
	}
	return opts, nil
}

func (s *Remote) selectedByTag(tag string) bool {
	if s.exclude != nil && s.exclude.MatchString(tag) {
		return false
	}
	if s.include == nil {
		return true
	}
	return s.include.MatchString(tag)
}

func (s *Remote) applyOptions(options *option.Outbound) error {
	// add provider tag as prefix to avoid tag conflict between providers
	options.Tag = s.tag + " " + options.Tag
	switch options.Type {
	case C.TypeSOCKS:
		options.SocksOptions.DialerOptions = s.dialerOptions
	case C.TypeHTTP:
		options.HTTPOptions.DialerOptions = s.dialerOptions
	case C.TypeShadowsocks:
		options.ShadowsocksOptions.DialerOptions = s.dialerOptions
	case C.TypeVMess:
		options.VMessOptions.DialerOptions = s.dialerOptions
	case C.TypeTrojan:
		options.TrojanOptions.DialerOptions = s.dialerOptions
	case C.TypeWireGuard:
		options.WireGuardOptions.DialerOptions = s.dialerOptions
	case C.TypeHysteria:
		options.HysteriaOptions.DialerOptions = s.dialerOptions
	case C.TypeTor:
		options.TorOptions.DialerOptions = s.dialerOptions
	case C.TypeSSH:
		options.SSHOptions.DialerOptions = s.dialerOptions
	case C.TypeShadowTLS:
		options.ShadowTLSOptions.DialerOptions = s.dialerOptions
	case C.TypeShadowsocksR:
		options.ShadowsocksROptions.DialerOptions = s.dialerOptions
	case C.TypeVLESS:
		options.VLESSOptions.DialerOptions = s.dialerOptions
	case C.TypeTUIC:
		options.TUICOptions.DialerOptions = s.dialerOptions
	case C.TypeMixed, C.TypeNaive:
		// do nothing
	default:
		return E.New("unknown outbound type: ", options.Type)
	}
	return nil
}

func (s *Remote) parseLinks(content []byte) ([]link.Link, error) {
	links, err := link.ParseCollection(string(content))
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

type fileContent struct {
	content   []byte
	hash      string
	updatedAt time.Time
}

func (s *Remote) downloadWithCache() (*fileContent, error) {
	content, err := s.download()
	if err == nil {
		updatedAt := time.Now()
		hash := contentHash(content)
		if s.cacheFile != "" {
			if s.loadedHash == hash {
				if err := os.Chtimes(s.cacheFile, updatedAt, updatedAt); err != nil {
					s.logger.Error(E.Cause(err, "update cache file"))
				}
			} else {
				if err := os.WriteFile(s.cacheFile, content, 0o666); err != nil {
					s.logger.Error(E.Cause(err, "write cache file"))
				}
			}
		}
		return &fileContent{
			content:   content,
			hash:      hash,
			updatedAt: updatedAt,
		}, nil
	}
	err = E.Cause(err, "fetch provider")
	if s.loadedHash != "" {
		return nil, err
	}
	if s.cacheFile == "" {
		return nil, err
	}
	s.logger.Error(err)
	s.logger.Info("load cache file: ", s.cacheFile)
	stat, err := os.Stat(s.cacheFile)
	if err != nil {
		return nil, E.Cause(err, "locate cache file")
	}
	updatedAt := stat.ModTime()
	content, err = os.ReadFile(s.cacheFile)
	if err != nil {
		return nil, E.Cause(err, "read cache file")
	}
	return &fileContent{
		content:   content,
		hash:      contentHash(content),
		updatedAt: updatedAt,
	}, nil
}

func (s *Remote) download() ([]byte, error) {
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
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, E.New("unexpected status code: ", resp.StatusCode)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func contentHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}
