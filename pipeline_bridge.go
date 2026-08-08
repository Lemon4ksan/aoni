// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"net"
	"net/http"
	"net/url"
	"slices"

	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/internal/tcp"
	"github.com/lemon4ksan/aoni/netutil/fragment"
)

func (c *Client) determineProxy(req *http.Request) (*url.URL, error) {
	if raw, ok := GetProxyOverride(req.Context()).Value(); ok && raw != "" {
		return url.Parse(raw)
	}

	if c.network.ProxyAddr != nil {
		return c.network.ProxyAddr, nil
	}

	return http.ProxyFromEnvironment(req)
}

func applyMSSLimit(conn net.Conn, mss int) net.Conn {
	if mss <= 0 {
		return conn
	}

	if tc, ok := conn.(*net.TCPConn); ok {
		raw, err := tc.SyscallConn()
		if err != nil {
			return conn
		}

		_ = raw.Control(func(fd uintptr) {
			tcp.SetTCPMaxSeg(fd, mss)
		})
	}

	return conn
}

func applyFragmentation(conn net.Conn, cfg fragment.Config) net.Conn {
	return &fragment.FragmentedConn{
		Conn:      conn,
		ChunkSize: cfg.ChunkSize,
		MaxDelay:  cfg.MaxDelay,
	}
}

func (c *Client) resolvePipeline(req *http.Request) PipelineConfig {
	if reqPipe, ok := GetPipeline(req.Context()); ok {
		return reqPipe
	}

	pipe := c.defaults.Pipeline
	if !pipe.RotateUA && len(c.defaults.UARotationProfiles) > 0 {
		pipe.RotateUA = true
	}

	if pipe.SizeLimit == 0 {
		pipe.SizeLimit = c.defaults.MaxResponseSize
	}

	if !pipe.Inspect && c.defaults.Inspector != nil {
		pipe.Inspect = true
	}

	if pipe.Hedging == nil && (c.network.HedgingDelay > 0 || c.network.DynamicHedging != nil) {
		pipe.Hedging = &HedgingConfig{
			DefaultDelay:   c.network.HedgingDelay,
			DynamicHedging: c.network.DynamicHedging,
		}
	}

	return pipe
}

func (c *Client) toPipelineDefaults() pipeline.ClientDefaults {
	return pipeline.ClientDefaults{
		Headers:              c.defaults.Headers,
		BeforeRequest:        c.defaults.BeforeRequest,
		AfterResponse:        c.defaults.AfterResponse,
		Inspector:            c.defaults.Inspector,
		ResponseValidator:    c.defaults.ResponseValidator,
		ChallengeDetector:    c.defaults.ChallengeDetector,
		ChallengeSolver:      c.defaults.ChallengeSolver,
		UARotationProfiles:   c.defaults.toInternalProfiles(),
		RefererState:         c.referer,
		MaxResponseSize:      c.defaults.MaxResponseSize,
		MultiReadThreshold:   c.defaults.MultiReadThreshold,
		MultiReadDisableDisk: c.defaults.MultiReadDisableDisk,
		RefererAutomaton:     c.defaults.RefererAutomaton,
	}
}

func (d ClientDefaults) toInternalProfiles() []pipeline.BrowserProfile {
	if len(d.UARotationProfiles) == 0 {
		return nil
	}
	res := make([]pipeline.BrowserProfile, len(d.UARotationProfiles))
	for i, p := range d.UARotationProfiles {
		res[i] = pipeline.BrowserProfile{
			UserAgent:   p.UserAgent,
			ClientHints: p.ClientHints,
		}
	}
	return res
}

func (p PipelineConfig) toInternal() pipeline.PipelineConfig {
	res := pipeline.PipelineConfig{
		SizeLimit:          p.SizeLimit,
		MultiReadThreshold: p.MultiReadThreshold,
		RotateUA:           p.RotateUA,
		Inspect:            p.Inspect,
		Decompress:         p.Decompress,
		Validate:           p.Validate,
		Challenge:          p.Challenge,
	}
	if p.DPIJitter != nil {
		res.DPIJitter = &pipeline.DPIJitterConfig{
			MinDelay: p.DPIJitter.MinDelay,
			MaxDelay: p.DPIJitter.MaxDelay,
		}
	}
	if p.ProxyFailover != nil {
		res.ProxyFailover = &pipeline.ProxyFailoverConfig{
			Proxies:    p.ProxyFailover.Proxies,
			RetryLimit: p.ProxyFailover.RetryLimit,
		}
	}
	if p.Hedging != nil {
		res.Hedging = &pipeline.HedgingConfig{
			DynamicHedging:       p.Hedging.DynamicHedging,
			DefaultDelay:         p.Hedging.DefaultDelay,
			MaxRequestsPerSecond: p.Hedging.MaxRequestsPerSecond,
			AllowNonReadOnly:     p.Hedging.AllowNonReadOnly,
		}
	}
	if p.Cache != nil {
		var nvs *pipeline.NoVarySearchConfig
		if p.Cache.NoVarySearch != nil {
			nvs = &pipeline.NoVarySearchConfig{
				IgnoreParams:    p.Cache.NoVarySearch.IgnoreParams,
				ExceptParams:    p.Cache.NoVarySearch.ExceptParams,
				IgnoreAllParams: p.Cache.NoVarySearch.IgnoreAllParams,
			}
		}
		res.Cache = &pipeline.CacheConfig{
			Store:         p.Cache.Store,
			DefaultTTL:    p.Cache.DefaultTTL,
			NoVarySearch:  nvs,
			CookieIndices: p.Cache.CookieIndices,
		}
	}
	if p.HAR != nil {
		res.HAR = &pipeline.HARConfig{
			Tracker: p.HAR.Tracker,
		}
	}
	if p.Redact != nil {
		res.Redact = &pipeline.RedactConfig{
			Headers:          p.Redact.Headers,
			HeadersToRedact:  p.Redact.HeadersToRedact,
			JSONKeysToRedact: p.Redact.JSONKeysToRedact,
		}
	}
	res.BuildFlags()
	return res
}

func pipelineToAoniConfig(p pipeline.PipelineConfig) PipelineConfig {
	res := PipelineConfig{
		SizeLimit:          p.SizeLimit,
		MultiReadThreshold: p.MultiReadThreshold,
		RotateUA:           p.RotateUA,
		Inspect:            p.Inspect,
		Decompress:         p.Decompress,
		Validate:           p.Validate,
		Challenge:          p.Challenge,
	}
	if p.DPIJitter != nil {
		res.DPIJitter = &DPIJitterConfig{
			MinDelay: p.DPIJitter.MinDelay,
			MaxDelay: p.DPIJitter.MaxDelay,
		}
	}
	if p.ProxyFailover != nil {
		res.ProxyFailover = &ProxyFailoverConfig{
			Proxies:    slices.Clone(p.ProxyFailover.Proxies),
			RetryLimit: p.ProxyFailover.RetryLimit,
		}
	}
	if p.Hedging != nil {
		res.Hedging = &HedgingConfig{
			DynamicHedging:       p.Hedging.DynamicHedging,
			DefaultDelay:         p.Hedging.DefaultDelay,
			MaxRequestsPerSecond: p.Hedging.MaxRequestsPerSecond,
			AllowNonReadOnly:     p.Hedging.AllowNonReadOnly,
		}
	}
	if p.Cache != nil {
		var nvs *NoVarySearchConfig
		if p.Cache.NoVarySearch != nil {
			nvs = &NoVarySearchConfig{
				IgnoreParams:    p.Cache.NoVarySearch.IgnoreParams,
				ExceptParams:    p.Cache.NoVarySearch.ExceptParams,
				IgnoreAllParams: p.Cache.NoVarySearch.IgnoreAllParams,
			}
		}
		res.Cache = &CacheConfig{
			Store:         p.Cache.Store,
			DefaultTTL:    p.Cache.DefaultTTL,
			NoVarySearch:  nvs,
			CookieIndices: p.Cache.CookieIndices,
		}
	}
	if p.HAR != nil {
		res.HAR = &HARConfig{
			Tracker: p.HAR.Tracker,
		}
	}
	if p.Redact != nil {
		res.Redact = &RedactConfig{
			Headers:          p.Redact.Headers,
			HeadersToRedact:  p.Redact.HeadersToRedact,
			JSONKeysToRedact: p.Redact.JSONKeysToRedact,
		}
	}
	return res
}
