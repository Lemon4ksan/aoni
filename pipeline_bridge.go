// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"net"
	"net/http"
	"net/url"

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
		if reqPipe.PrecomputedFlags == 0 {
			reqPipe.BuildFlags()
		}

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

	pipe.BuildFlags()

	return pipe
}

func (d ClientDefaults) ToPipelineDefaults() pipeline.ClientDefaults {
	return pipeline.ClientDefaults{
		Headers:              d.Headers,
		BeforeRequest:        d.BeforeRequest,
		AfterResponse:        d.AfterResponse,
		Inspector:            d.Inspector,
		ResponseValidator:    d.ResponseValidator,
		ChallengeDetector:    d.ChallengeDetector,
		ChallengeSolver:      d.ChallengeSolver,
		UARotationProfiles:   d.UARotationProfiles,
		RefererState:         d.RefererState,
		MaxResponseSize:      d.MaxResponseSize,
		MultiReadThreshold:   d.MultiReadThreshold,
		MultiReadDisableDisk: d.MultiReadDisableDisk,
		RefererAutomaton:     d.RefererAutomaton,
	}
}

func (f FingerprintConfig) ToPipelineFingerprint() pipeline.ClientFingerprint {
	return pipeline.ClientFingerprint{
		PacketPadding: f.PacketPadding,
	}
}
