// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/silicon/clock"
	"github.com/quic-go/quic-go"
	"github.com/valyala/fasthttp"
	"golang.org/x/sys/cpu"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/fast/h2engine"
	"github.com/lemon4ksan/aoni/internal/fast/h3engine"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/netutil"
)

const (
	minH3Cooldown = 5 * time.Minute
	maxH3Cooldown = 48 * time.Hour
)

type brokenH3Entry struct {
	cooldownUntil    time.Time
	consecutiveFails int
}

type protocolState struct {
	h2Clients map[string]*h2engine.Client
	h3Client  *h3engine.Client
	h2Mutex   sync.Mutex
	h3Once    sync.Once
}

type altSvcCache struct {
	mu sync.RWMutex

	_ cpu.CacheLinePad

	hosts  map[string]time.Time
	broken map[string]brokenH3Entry

	_ cpu.CacheLinePad
}

var globalAltSvcCache = &altSvcCache{
	hosts:  make(map[string]time.Time),
	broken: make(map[string]brokenH3Entry),
}

// MarkH3Failed records a failed HTTP/3 connection attempt, applying exponential backoff from 5m up to 48h.
func (c *altSvcCache) MarkH3Failed(host string) {
	if host == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.broken == nil {
		c.broken = make(map[string]brokenH3Entry)
	}

	entry := c.broken[host]
	entry.consecutiveFails++

	backoff := calculateH3Backoff(entry.consecutiveFails)
	entry.cooldownUntil = clock.CoarseTime().Add(backoff)

	c.broken[host] = entry
}

// MarkH3Success clears the broken status and resets consecutive failure counters upon successful H3 request.
func (c *altSvcCache) MarkH3Success(host string) {
	if host == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.broken, host)
}

// IsH3Supported reports whether HTTP/3 is supported for host and not currently in broken backoff cooldown.
func (c *altSvcCache) IsH3Supported(host string) bool {
	if host == "" {
		return false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	now := clock.CoarseTime()
	if entry, ok := c.broken[host]; ok && now.Before(entry.cooldownUntil) {
		return false
	}

	exp, ok := c.hosts[host]
	if !ok || now.After(exp) {
		return false
	}

	return true
}

func calculateH3Backoff(fails int) time.Duration {
	if fails <= 1 {
		return minH3Cooldown
	}

	shift := min(fails-1, 10)
	backoff := minH3Cooldown * time.Duration(1<<shift)

	return min(backoff, maxH3Cooldown)
}

func (c *altSvcCache) Record(host, headerVal string) {
	if host == "" || headerVal == "" {
		return
	}

	if headerVal == "clear" {
		c.mu.Lock()
		delete(c.hosts, host)
		delete(c.broken, host)
		c.mu.Unlock()

		return
	}

	if !strings.Contains(headerVal, "h3") {
		return
	}

	maxAge := parseMaxAge(headerVal)

	c.mu.Lock()
	c.hosts[host] = clock.CoarseTime().Add(maxAge)
	c.mu.Unlock()
}

func parseMaxAge(headerVal string) time.Duration {
	maxAge := 86400 * time.Second

	for p := range strings.SplitSeq(headerVal, ";") {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "ma=") {
			if seconds, err := strconv.ParseInt(p[3:], 10, 64); err == nil && seconds > 0 {
				return time.Duration(seconds) * time.Second
			}
		}
	}

	return maxAge
}

func resolveALPNMode(ctx context.Context, cfg *aoni.Config, fastReq *fasthttp.Request) string {
	reqCfg := aoni.GetRequestConfig(ctx)
	if reqCfg != nil {
		if len(reqCfg.Modifiers) > 0 && len(reqCfg.ALPNOverride) == 0 {
			dummyReq := NewRequest(fastReq)
			dummyReq.SetContext(ctx)

			for _, m := range reqCfg.Modifiers {
				m.Apply(dummyReq)
			}
		}

		if len(reqCfg.ALPNOverride) > 0 {
			first := reqCfg.ALPNOverride[0]
			if first == aoni.AlpnH3 || first == aoni.AlpnH2 || first == aoni.AlpnHTTP {
				return first
			}
		}
	}

	disableAltSvc := reqCfg != nil && reqCfg.DisableAltSvc

	if bytes.EqualFold(fastReq.URI().Scheme(), []byte("https")) {
		host := string(fastReq.URI().Host())
		if host != "" && !disableAltSvc && globalAltSvcCache.IsH3Supported(host) {
			return aoni.AlpnH3
		}

		return aoni.AlpnH2
	}

	if cfg != nil {
		if len(cfg.Fingerprint.HeaderOrder) > 0 ||
			cfg.Fingerprint.H2Settings != nil ||
			cfg.Fingerprint.BrowserID != aoni.BrowserNone {
			return aoni.AlpnH2
		}
	}

	return aoni.AlpnHTTP
}

func (c *Client) getH3Client() *h3engine.Client {
	c.protocolState.h3Once.Do(func() {
		tlsCfg := &tls.Config{
			InsecureSkipVerify: c.config.Engine.InsecureSkipVerify, //nolint:gosec
			ClientSessionCache: netutil.ResolveStdSessionCache(c.config.Fingerprint.SessionCache),
		}

		if spec := c.config.Fingerprint.TLSQUICClientHelloSpec; spec != nil && len(spec.CipherSuites) > 0 {
			tlsCfg.CipherSuites = spec.CipherSuites
		}

		quicCfg := &quic.Config{
			EnableDatagrams: true,
		}

		if h3s := c.config.Fingerprint.H3Settings; h3s != nil {
			quicCfg.InitialStreamReceiveWindow = h3s.InitialStreamReceiveWindow
			quicCfg.MaxStreamReceiveWindow = h3s.MaxStreamReceiveWindow
			quicCfg.InitialConnectionReceiveWindow = h3s.InitialConnectionReceiveWindow
			quicCfg.MaxConnectionReceiveWindow = h3s.MaxConnectionReceiveWindow
			quicCfg.MaxIncomingStreams = h3s.MaxIncomingStreams
			quicCfg.MaxIncomingUniStreams = h3s.MaxIncomingUniStreams
			quicCfg.EnableDatagrams = h3s.EnableDatagrams
		}

		c.protocolState.h3Client = h3engine.NewClient(tlsCfg, quicCfg)
	})

	return c.protocolState.h3Client
}

func (c *Client) getH2Client(host string) *h2engine.Client {
	c.protocolState.h2Mutex.Lock()
	defer c.protocolState.h2Mutex.Unlock()

	if c.protocolState.h2Clients == nil {
		c.protocolState.h2Clients = make(map[string]*h2engine.Client)
	}

	if cl, ok := c.protocolState.h2Clients[host]; ok {
		return cl
	}

	dialer := &h2engine.Dialer{
		Addr: host,
		RawDialContext: func(ctx context.Context, addr string) (net.Conn, error) {
			return c.DialH2(ctx, addr)
		},
	}

	var h2s *h2engine.Settings
	if c.config.Fingerprint.H2Settings != nil {
		s := c.config.Fingerprint.H2Settings
		h2s = &h2engine.Settings{}
		h2s.SetHeaderTableSize(s.HeaderTableSize)
		h2s.SetPush(s.EnablePush == 1)
		h2s.SetMaxConcurrentStreams(s.MaxConcurrentStreams)
		h2s.SetMaxWindowSize(s.InitialWindowSize)
		h2s.SetMaxFrameSize(s.MaxFrameSize)
		h2s.SetMaxHeaderListSize(s.MaxHeaderListSize)
	}

	var onRTTCallback func(time.Duration)
	if c.config.Network.DynamicHedging != nil && c.config.Network.DynamicHedging.Tracker != nil {
		tracker := c.config.Network.DynamicHedging.Tracker
		onRTTCallback = func(rtt time.Duration) {
			tracker.Record(rtt)
		}
	}

	var pushHandler func(pushReq *fasthttp.Request, pushResp *fasthttp.Response)
	if cacheCfg := c.config.Defaults.Pipeline.Cache; cacheCfg != nil && cacheCfg.Store != nil {
		pushHandler = func(pushReq *fasthttp.Request, pushResp *fasthttp.Response) {
			c.cachePushedResponse(pushReq, pushResp, cacheCfg)
		}
	}

	cl := h2engine.NewClient(dialer, h2engine.ClientOpts{
		PingInterval:  15 * time.Second,
		OnRTT:         onRTTCallback,
		OnPushPromise: pushHandler,
		Settings:      h2s,
	})

	if len(c.config.Fingerprint.HeaderOrder) > 0 {
		cl.SetOrderedHeaders(c.config.Fingerprint.HeaderOrder)
	}

	c.protocolState.h2Clients[host] = cl

	return cl
}

func (c *Client) cachePushedResponse(
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
	cacheCfg *aoni.CacheConfig,
) {
	req, err := http.NewRequest(string(fastReq.Header.Method()), string(fastReq.URI().FullURI()), nil) //nolint:noctx
	if err != nil {
		return
	}

	fastReq.Header.All()(func(k, v []byte) bool {
		req.Header.Add(string(k), string(v))
		return true
	})

	resp := &http.Response{
		StatusCode: fastResp.StatusCode(),
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(fastResp.Body())),
	}

	fastResp.Header.All()(func(k, v []byte) bool {
		resp.Header.Add(string(k), string(v))
		return true
	})

	pipe := c.pipeline
	if pipe != nil && cacheCfg != nil {
		var nvs *pipeline.NoVarySearchConfig
		if cacheCfg.NoVarySearch != nil {
			nvs = &pipeline.NoVarySearchConfig{
				IgnoreParams:    cacheCfg.NoVarySearch.IgnoreParams,
				ExceptParams:    cacheCfg.NoVarySearch.ExceptParams,
				IgnoreAllParams: cacheCfg.NoVarySearch.IgnoreAllParams,
			}
		}

		pipe.SavePushedResponseToCache(req, resp, &pipeline.CacheConfig{
			Store:         cacheCfg.Store,
			DefaultTTL:    cacheCfg.DefaultTTL,
			NoVarySearch:  nvs,
			CookieIndices: cacheCfg.CookieIndices,
		})
	}
}

func (c *Client) removeH2Client(host string) {
	c.protocolState.h2Mutex.Lock()
	defer c.protocolState.h2Mutex.Unlock()

	if c.protocolState.h2Clients != nil {
		delete(c.protocolState.h2Clients, host)
	}
}
