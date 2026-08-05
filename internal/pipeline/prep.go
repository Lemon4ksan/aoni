// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/telemetry"
)

func (p *Pipeline) prepareRequest(req Request, tx *Tx) *http.Request {
	stdReq := req.HTTPRequest()
	if stdReq == nil {
		var err error

		stdReq, err = http.NewRequestWithContext(req.Context(), req.Method(), req.URL(), req.BodyStream())
		if err != nil {
			return &http.Request{}
		}
	}

	stdReq = p.prepareRequestContext(req, stdReq)

	for _, hook := range p.defaults.BeforeRequest {
		hook(stdReq)
	}

	if p.fingerprint.PacketPadding != nil {
		p.applyPacketPadding(stdReq)
	}

	if p.defaults.RefererAutomaton {
		p.applyRefererHeader(stdReq)
	}

	if tx.Flags&FlagRotateUA != 0 {
		p.rotateUserAgentAndHints(stdReq)
	}

	if tx.Flags&FlagDPIJitter != 0 && tx.DPIJitter != nil {
		p.applyDPIJitter(stdReq, tx.DPIJitter)
	}

	if tx.Flags&FlagRedact != 0 && tx.Redact != nil {
		stdReq = p.redactSensitiveData(stdReq, tx.Redact)
	}

	return stdReq
}

func (p *Pipeline) prepareRequestContext(req Request, stdReq *http.Request) *http.Request {
	ctx := stdReq.Context()

	cfg := GetRequestConfig(ctx)
	if cfg == nil {
		return stdReq
	}

	if cfg.TimeoutOverride > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, cfg.TimeoutOverride) //nolint:gosec
		cfg.RequestTimeoutCancel = cancel
	}

	if cfg.ProxyAddr != nil {
		proxyStr := cfg.ProxyAddr.String()

		if cfg.SessionCache != nil {
			cfg.SessionCache.SetProxyKey(proxyStr)
		}

		ctx = cookie.WithProxyAddress(ctx, proxyStr)
	}

	for _, mod := range cfg.Modifiers {
		if mod != nil {
			mod(req)
		}
	}

	return stdReq.WithContext(ctx)
}

func (p *Pipeline) traceRequest(
	stdReq *http.Request,
	tx *Tx,
) (*http.Request, *telemetry.TraceInfo, func(resp *http.Response)) {
	var traceInfo *telemetry.TraceInfo

	switch {
	case tx.TraceInfo != nil:
		traceInfo = tx.TraceInfo

	case tx.Flags&FlagInspect != 0 && p.defaults.Inspector != nil:
		traceInfo = &telemetry.TraceInfo{}

		if tx.JA4ReportStore == nil {
			tx.JA4ReportStore = &JA4ReportStore{Target: traceInfo}
		} else {
			tx.JA4ReportStore.Target = traceInfo
		}

		traceInfo.JA4 = &ja4.Report{JA4H: telemetry.ComputeJA4HFromRequest(stdReq)}
	}

	if traceInfo == nil {
		return stdReq, nil, nil
	}

	trace := &httptrace.ClientTrace{
		DNSStart:          func(_ httptrace.DNSStartInfo) { traceInfo.DNSStart = time.Now() },
		DNSDone:           func(_ httptrace.DNSDoneInfo) { traceInfo.DNSLookup = time.Since(traceInfo.DNSStart) },
		ConnectStart:      func(_, _ string) { traceInfo.ConnectStart = time.Now() },
		ConnectDone:       func(_, _ string, _ error) { traceInfo.TCPConn = time.Since(traceInfo.ConnectStart) },
		TLSHandshakeStart: func() { traceInfo.TLSStart = time.Now() },
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			traceInfo.TLSHandshake = time.Since(traceInfo.TLSStart)
			if err == nil {
				stCopy := state
				traceInfo.TLSState = &stCopy
				traceInfo.PeerCertificates = state.PeerCertificates
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			traceInfo.GotConn = time.Now()
			if info.Conn != nil && info.Conn.RemoteAddr() != nil {
				traceInfo.RemoteAddr = info.Conn.RemoteAddr().String()
			}
		},
		GotFirstResponseByte: func() { traceInfo.ServerProcessing = time.Since(traceInfo.GotConn) },
	}

	stdReq = stdReq.WithContext(httptrace.WithClientTrace(stdReq.Context(), trace))

	return stdReq, traceInfo, traceInfo.Start() //nolint:bodyclose
}

func (p *Pipeline) redactSensitiveData(req *http.Request, redact *RedactConfig) *http.Request {
	headers := make(map[string]struct{}, len(redact.HeadersToRedact))
	for _, h := range redact.HeadersToRedact {
		headers[strings.ToLower(h)] = struct{}{}
	}

	if len(headers) == 0 {
		headers = map[string]struct{}{
			"authorization":       {},
			"proxy-authorization": {},
			"cookie":              {},
			"set-cookie":          {},
			"x-api-key":           {},
		}
	}

	cfg := GetRequestConfig(req.Context())
	if cfg == nil {
		cfg = &RequestConfig{}
	}

	cfg.Redact = &RedactConfig{
		Headers:          headers,
		HeadersToRedact:  redact.HeadersToRedact,
		JSONKeysToRedact: redact.JSONKeysToRedact,
	}

	ctx := context.WithValue(req.Context(), RedactConfigCtxKey{}, cfg.Redact)
	ctx = context.WithValue(ctx, requestConfigKey{}, cfg)

	return req.WithContext(ctx)
}

func (p *Pipeline) rotateUserAgentAndHints(req *http.Request) {
	profiles := p.defaults.UARotationProfiles
	if len(profiles) == 0 {
		return
	}

	idx := atomic.AddUint32(&p.counter, 1) - 1
	prof := profiles[idx%uint32(len(profiles))] //nolint:gosec

	req.Header.Set("User-Agent", prof.UserAgent)

	for k, v := range prof.ClientHints {
		req.Header.Set(k, v)
	}
}

func (p *Pipeline) applyDPIJitter(req *http.Request, cfg *DPIJitterConfig) {
	delay := cfg.MinDelay
	if cfg.MinDelay > 0 && cfg.MaxDelay >= cfg.MinDelay {
		if delta := cfg.MaxDelay - cfg.MinDelay; delta > 0 {
			delay = cfg.MinDelay + time.Duration(time.Now().UnixNano()%int64(delta))
		}
	}

	if delay <= 0 {
		return
	}

	if req.Body != nil && req.Body != http.NoBody {
		req.Body = &io.JitterReader{
			ReadCloser: req.Body,
			Delay:      delay,
		}

		return
	}

	time.Sleep(delay)
}

func (p *Pipeline) applyPacketPadding(req *http.Request) {
	if padding := fingerprint.GeneratePadding(*p.fingerprint.PacketPadding); len(padding) > 0 {
		headerName := fingerprint.PaddingHeaderName(*p.fingerprint.PacketPadding)
		req.Header.Set(headerName, hex.EncodeToString(padding))
	}
}

func (p *Pipeline) applyRefererHeader(req *http.Request) {
	if req.Header.Get("Referer") != "" || p.defaults.RefererState == nil {
		return
	}

	p.defaults.RefererState.Mu.Lock()
	lastURL := p.defaults.RefererState.LastURL
	p.defaults.RefererState.Mu.Unlock()

	if lastURL != "" {
		req.Header.Set("Referer", lastURL)
	}
}
