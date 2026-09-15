// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"maps"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/silicon/clock"
	"github.com/lemon4ksan/mach/client/h1"
	"github.com/lemon4ksan/mach/client/h2"
	"github.com/lemon4ksan/mach/client/h3"
	coreh2 "github.com/lemon4ksan/mach/core/h2"
	"golang.org/x/sys/cpu"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/pipeline"
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
	h2Clients map[string]*h2.Client
	h3Client  *h3.Client
	altSvc    *altSvcCache
	h2Mutex   sync.Mutex
	h3Once    sync.Once
}

func newProtocolState() protocolState {
	return protocolState{
		h2Clients: make(map[string]*h2.Client),
		altSvc:    newAltSvcCache(),
	}
}

func (s *protocolState) Clone() protocolState {
	return protocolState{
		h2Clients: make(map[string]*h2.Client),
		altSvc:    s.altSvc.Clone(),
	}
}

type altSvcCache struct {
	mu sync.RWMutex

	_ cpu.CacheLinePad

	hosts  map[string]time.Time
	broken map[string]brokenH3Entry

	_ cpu.CacheLinePad
}

func newAltSvcCache() *altSvcCache {
	return &altSvcCache{
		hosts:  make(map[string]time.Time),
		broken: make(map[string]brokenH3Entry),
	}
}

func (c *altSvcCache) Clone() *altSvcCache {
	if c == nil {
		return newAltSvcCache()
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return &altSvcCache{
		hosts:  maps.Clone(c.hosts),
		broken: maps.Clone(c.broken),
	}
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

func (c *Client) resolveALPNMode(ctx context.Context, fastReq *h1.Request) string {
	return resolveALPNMode(ctx, &c.cfg, fastReq, c.protocolState.altSvc)
}

func resolveALPNMode(ctx context.Context, cfg *aoni.Config, fastReq *h1.Request, altSvc *altSvcCache) string {
	reqCfg := aoni.GetRequestConfig(ctx)
	if reqCfg != nil {
		if len(reqCfg.Modifiers) > 0 && len(reqCfg.ALPNOverride) == 0 {
			dummyFastReq := h1.AcquireRequest()
			dummyReq := NewRequest(dummyFastReq)
			dummyReq.SetContext(ctx)

			for _, m := range reqCfg.Modifiers {
				m.Apply(dummyReq)
			}

			dummyReq.Release()
			ReleaseRequestSafe(dummyFastReq)
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
		host := bytesconv.B2S(fastReq.URI().Host())
		if host != "" && !disableAltSvc && altSvc != nil && altSvc.IsH3Supported(host) {
			return aoni.AlpnH3
		}

		return aoni.AlpnH2
	}

	return aoni.AlpnHTTP
}

func (c *Client) getH3Client() *h3.Client {
	c.protocolState.h3Once.Do(func() {
		tlsCfg := &tls.Config{
			InsecureSkipVerify: c.cfg.Engine.InsecureSkipVerify, //nolint:gosec

		}

		c.protocolState.h3Client = h3.NewClientFromSettings(tlsCfg)
	})

	return c.protocolState.h3Client
}

func (c *Client) getH2Client(host string) *h2.Client {
	c.protocolState.h2Mutex.Lock()
	defer c.protocolState.h2Mutex.Unlock()

	if c.protocolState.h2Clients == nil {
		c.protocolState.h2Clients = make(map[string]*h2.Client)
	}

	if cl, ok := c.protocolState.h2Clients[host]; ok {
		return cl
	}

	dialer := &h2.Dialer{
		Addr: host,
		RawDialContext: func(ctx context.Context, addr string) (net.Conn, error) {
			return c.DialH2(ctx, addr)
		},
	}

	var onRTTCallback func(time.Duration)
	if c.cfg.Network.DynamicHedging != nil && c.cfg.Network.DynamicHedging.Tracker != nil {
		tracker := c.cfg.Network.DynamicHedging.Tracker
		onRTTCallback = func(rtt time.Duration) {
			tracker.Record(rtt)
		}
	}

	var pushHandler func(pushReq *h1.Request, pushResp *h1.Response)
	if cacheCfg := c.cfg.Defaults.Pipeline.Cache; cacheCfg != nil && cacheCfg.Store != nil {
		pushHandler = func(pushReq *h1.Request, pushResp *h1.Response) {
			c.cachePushedResponse(pushReq, pushResp, cacheCfg)
		}
	}

	var settings *coreh2.Settings
	if len(c.cfg.Ext.OverrideH2Settings) > 0 {
		settings = &coreh2.Settings{}
		for k, v := range c.cfg.Ext.OverrideH2Settings {
			switch k {
			case 1:
				settings.SetHeaderTableSize(v)
			case 2:
				settings.SetPush(v != 0)
			case 3:
				settings.SetMaxConcurrentStreams(v)
			case 4:
				settings.SetMaxWindowSize(v)
			case 5:
				settings.SetMaxFrameSize(v)
			case 6:
				settings.SetMaxHeaderListSize(v)
			}
		}
	}

	cl := h2.NewClient(dialer, h2.ClientOpts{
		PingInterval:  15 * time.Second,
		OnRTT:         onRTTCallback,
		OnPushPromise: pushHandler,
		Settings:      settings,
	})

	if len(c.cfg.Ext.HeaderOrder) > 0 {
		cl.SetOrderedHeaders(c.cfg.Ext.HeaderOrder)
	}

	c.protocolState.h2Clients[host] = cl

	return cl
}

func (c *Client) cachePushedResponse(
	fastReq *h1.Request,
	fastResp *h1.Response,
	cacheCfg *aoni.CacheConfig,
) {
	req, err := http.NewRequestWithContext(
		context.Background(),
		bytesconv.B2S(fastReq.Header.Method()),
		bytesconv.B2S(fastReq.URI().FullURI()),
		nil,
	)
	if err != nil {
		return
	}

	for k, v := range fastReq.Header.All() {
		req.Header.Add(string(k), string(v))
	}

	resp := &http.Response{
		StatusCode: fastResp.StatusCode(),
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(fastResp.Body())),
	}

	for k, v := range fastResp.Header.All() {
		resp.Header.Add(string(k), string(v))
	}

	if c.pipeline != nil && cacheCfg != nil {
		var nvs *pipeline.NoVarySearchConfig
		if cacheCfg.NoVarySearch != nil {
			nvs = &pipeline.NoVarySearchConfig{
				IgnoreParams:    cacheCfg.NoVarySearch.IgnoreParams,
				ExceptParams:    cacheCfg.NoVarySearch.ExceptParams,
				IgnoreAllParams: cacheCfg.NoVarySearch.IgnoreAllParams,
			}
		}

		pipeline.SavePushedResponseToCache(req, resp, &pipeline.CacheConfig{
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
