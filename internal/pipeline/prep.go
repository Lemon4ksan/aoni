// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	stdio "io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/netutil/netdial"
	"github.com/lemon4ksan/aoni/telemetry"
)

func (p *Pipeline[Req, Resp]) prepareRequest(req any, tx *Tx) *http.Request {
	var stdReq *http.Request

	if r, ok := req.(*http.Request); ok {
		stdReq = r
	} else if r, ok := req.(Request); ok {
		stdReq = r.HTTPRequest()
		if stdReq == nil {
			var (
				bodyReader stdio.Reader
				contentLen int64 = -1
			)

			if fastAdapter, ok := r.(interface{ FastHTTPRequest() *fasthttp.Request }); ok {
				fastReq := fastAdapter.FastHTTPRequest()
				if fastReq != nil {
					if cl := fastReq.Header.ContentLength(); cl > 0 {
						contentLen = int64(cl)
					}
				}
			}

			if bs := r.BodyStream(); bs != nil {
				bodyReader = bs
			} else if bb := r.BodyBytes(); len(bb) > 0 {
				bodyReader = bytes.NewReader(bb)
				if contentLen <= 0 {
					contentLen = int64(len(bb))
				}
			}

			var err error

			stdReq, err = http.NewRequestWithContext(r.Context(), r.Method(), r.URL(), bodyReader) //nolint:gosec
			if err != nil {
				return &http.Request{}
			}

			if contentLen > 0 {
				stdReq.ContentLength = contentLen
			}

			if fastAdapter, ok := r.(interface{ FastHTTPRequest() *fasthttp.Request }); ok {
				fastReq := fastAdapter.FastHTTPRequest()
				if fastReq != nil {
					fastReq.Header.All()(func(k, v []byte) bool {
						stdReq.Header.Add(string(k), string(v))
						return true
					})

					if host := string(fastReq.Header.Peek("Host")); host != "" {
						stdReq.Host = host
					}
				}
			}
		}
	} else {
		return &http.Request{}
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

	cfg := GetRequestConfig(stdReq.Context())
	if cfg != nil && cfg.UploadProgress != nil && stdReq.Body != nil && stdReq.Body != http.NoBody {
		progressReader := &io.ProgressReader{
			Reader:     stdReq.Body,
			Total:      stdReq.ContentLength,
			OnProgress: cfg.UploadProgress,
		}
		stdReq.Body = progressReader

		if stdReq.GetBody != nil {
			origGetBody := stdReq.GetBody
			stdReq.GetBody = func() (stdio.ReadCloser, error) {
				rc, err := origGetBody()
				if err != nil {
					return nil, err
				}

				return &io.ProgressReader{
					Reader:     rc,
					Total:      stdReq.ContentLength,
					OnProgress: cfg.UploadProgress,
				}, nil
			}
		}
	}

	if cfg != nil && cfg.JA4ReportStore != nil && cfg.JA4ReportStore.Target != nil {
		if cfg.JA4ReportStore.Target.JA4 == nil {
			cfg.JA4ReportStore.Target.JA4 = &ja4.Report{}
		}

		cfg.JA4ReportStore.Target.JA4.JA4H = telemetry.ComputeJA4HFromRequest(stdReq)
	}

	return stdReq
}

func (p *Pipeline[Req, Resp]) prepareRequestContext(req any, stdReq *http.Request) *http.Request {
	ctx := stdReq.Context()

	cfg := GetRequestConfig(ctx)
	if cfg == nil {
		return stdReq
	}

	if len(cfg.Modifiers) > 0 {
		var reqAdapter Request
		if r, ok := req.(Request); ok {
			reqAdapter = r
		} else {
			reqAdapter = NewStdRequestAdapter(stdReq)
		}

		for _, mod := range cfg.Modifiers {
			if mod != nil {
				mod(reqAdapter)
			}
		}

		cfg.Modifiers = nil
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

	if ctx != stdReq.Context() {
		return stdReq.WithContext(ctx)
	}

	return stdReq
}

func (p *Pipeline[Req, Resp]) traceRequest(
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
			traceInfo.IsReused = info.Reused

			if info.Conn != nil && info.Conn.RemoteAddr() != nil {
				traceInfo.RemoteAddr = info.Conn.RemoteAddr().String()
			}
		},
		GotFirstResponseByte: func() { traceInfo.ServerProcessing = time.Since(traceInfo.GotConn) },
		Got1xxResponse: func(code int, header textproto.MIMEHeader) error {
			if code == 103 {
				ProcessEarlyHints(stdReq.Context(), http.Header(header), p.prewarmTargetOrigin)
			}

			return nil
		},
	}

	stdReq = stdReq.WithContext(httptrace.WithClientTrace(stdReq.Context(), trace))

	return stdReq, traceInfo, traceInfo.Start() //nolint:bodyclose
}

func (p *Pipeline[Req, Resp]) prewarmTargetOrigin(ctx context.Context, targetURL string) {
	if targetURL == "" {
		return
	}

	u, err := url.Parse(targetURL)
	if err != nil || u.Host == "" {
		return
	}

	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	host, port := u.Hostname(), u.Port()
	if port == "" {
		if u.Scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}

	targetAddr := net.JoinHostPort(host, port)

	dialOpts := netdial.DialOptions{
		HappyEyeballs: 250 * time.Millisecond,
	}

	conn, err := netdial.DialL4(dialCtx, "tcp", targetAddr, dialOpts)
	if err != nil {
		return
	}

	if u.Scheme == "https" {
		utlsOpts := netdial.RTLSOptions{
			ALPNOverride: []string{"h2", "http/1.1"},
		}

		uConn, _, handshakeErr := netdial.HandshakeUTLS(dialCtx, conn, host, utlsOpts)
		if handshakeErr != nil {
			_ = conn.Close()
			return
		}

		_ = uConn.Close()

		return
	}

	_ = conn.Close()
}

func (p *Pipeline[Req, Resp]) redactSensitiveData(req *http.Request, redact *RedactConfig) *http.Request {
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

	ctx := req.Context()

	cfg := GetRequestConfig(ctx)
	if cfg == nil {
		ctx, cfg = AllocRequestConfig(ctx)
	}

	cfg.Redact = &RedactConfig{
		Headers:          headers,
		HeadersToRedact:  redact.HeadersToRedact,
		JSONKeysToRedact: redact.JSONKeysToRedact,
	}

	ctx = context.WithValue(ctx, RedactConfigCtxKey{}, cfg.Redact)

	return req.WithContext(ctx)
}

func (p *Pipeline[Req, Resp]) rotateUserAgentAndHints(req *http.Request) {
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

func (p *Pipeline[Req, Resp]) applyDPIJitter(req *http.Request, cfg *DPIJitterConfig) {
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

func (p *Pipeline[Req, Resp]) applyPacketPadding(req *http.Request) {
	if padding := fingerprint.GeneratePadding(*p.fingerprint.PacketPadding); len(padding) > 0 {
		headerName := fingerprint.PaddingHeaderName(*p.fingerprint.PacketPadding)
		req.Header.Set(headerName, hex.EncodeToString(padding))
	}
}

func (p *Pipeline[Req, Resp]) applyRefererHeader(req *http.Request) {
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
