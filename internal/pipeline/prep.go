// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/foundation/iokit"
	"github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
	"github.com/lemon4ksan/aoni/netutil/dict"
	"github.com/lemon4ksan/aoni/netutil/netdial"
	"github.com/lemon4ksan/aoni/telemetry"
)

func (p *Pipeline[Req, Resp]) prepareRequest(req any, tx *Tx) *http.Request {
	var stdReq *http.Request

	switch r := req.(type) {
	case *http.Request:
		stdReq = r
	case core.Request:
		stdReq = convertRequestToStd(r)
	default:
		return &http.Request{}
	}

	stdReq = p.prepareRequestContext(req, stdReq)

	if len(p.defaults.BeforeRequest) > 0 {
		stdReq = stageBeforeRequestHooks(p, stdReq, tx)
	}

	if p.fingerprint.PacketPadding != nil {
		stdReq = stagePacketPadding(p, stdReq, tx)
	}

	if p.defaults.RefererAutomaton {
		stdReq = stageRefererHeader(p, stdReq, tx)
	}

	if tx.Flags&FlagRotateUA != 0 {
		stdReq = stageRotateUserAgent(p, stdReq, tx)
	}

	if tx.Flags&FlagDPIJitter != 0 && tx.DPIJitter != nil {
		stdReq = stageDPIJitter(p, stdReq, tx)
	}

	if tx.Flags&FlagRedact != 0 && tx.Redact != nil {
		stdReq = stageRedactSensitiveData(p, stdReq, tx)
	}

	stdReq = stageUploadProgress(p, stdReq, tx)
	stdReq = stageJA4Report(p, stdReq, tx)
	stdReq = stageAvailableDictionary(p, stdReq, tx)

	return stdReq
}

func stageAvailableDictionary[Req, Resp any](p *Pipeline[Req, Resp], req *http.Request, _ *Tx) *http.Request {
	if req == nil || req.URL == nil || !strings.EqualFold(req.URL.Scheme, "https") {
		// RFC 9842 §8: Compression Dictionary Transport MUST only be used in secure contexts (HTTPS).
		return req
	}

	cfg := GetRequestConfig(req.Context())
	if cfg != nil && cfg.DisableDictionaryCompression {
		return req
	}

	if p.defaults.DisableDictionaryCompression {
		return req
	}

	store := p.defaults.DictionaryStore
	if cfg != nil && cfg.DictionaryStore != nil {
		store = cfg.DictionaryStore
	}

	if store == nil {
		return req
	}

	dest := req.Header.Get("Sec-Fetch-Dest")

	matchedDict, ok := store.Match(req.URL, dest)
	if !ok || matchedDict == nil {
		return req
	}

	// RFC 9842 §2.2: Available-Dictionary: :<sha256>:
	req.Header.Set(dict.HeaderAvailableDictionary, dict.FormatAvailableDictionary(matchedDict.Hash))

	// RFC 9842 §2.3: Dictionary-ID
	if matchedDict.ID != "" {
		req.Header.Set(dict.HeaderDictionaryID, strconv.Quote(matchedDict.ID))
	}

	// RFC 9842 §6.1: Accept-Encoding
	ae := req.Header.Get(header.AcceptEncoding)
	if ae != "" {
		if !strings.Contains(strings.ToLower(ae), dict.ContentEncodingDCZ) {
			req.Header.Set(header.AcceptEncoding, ae+", "+dict.ContentEncodingDCB+", "+dict.ContentEncodingDCZ)
		}
	}

	if cfg != nil {
		cfg.AvailableDictionary = matchedDict
	}

	return req
}

func stageBeforeRequestHooks[Req, Resp any](p *Pipeline[Req, Resp], req *http.Request, _ *Tx) *http.Request {
	for _, hook := range p.defaults.BeforeRequest {
		hook(req)
	}

	return req
}

func stagePacketPadding[Req, Resp any](p *Pipeline[Req, Resp], req *http.Request, _ *Tx) *http.Request {
	if p.fingerprint.PacketPadding != nil {
		p.applyPacketPadding(req)
	}

	return req
}

func stageRefererHeader[Req, Resp any](p *Pipeline[Req, Resp], req *http.Request, _ *Tx) *http.Request {
	if p.defaults.RefererAutomaton {
		p.applyRefererHeader(req)
	}

	return req
}

func stageRotateUserAgent[Req, Resp any](p *Pipeline[Req, Resp], req *http.Request, tx *Tx) *http.Request {
	if tx.Flags&FlagRotateUA != 0 {
		p.rotateUserAgentAndHints(req)
	}

	return req
}

func stageDPIJitter[Req, Resp any](p *Pipeline[Req, Resp], req *http.Request, tx *Tx) *http.Request {
	if tx.Flags&FlagDPIJitter != 0 && tx.DPIJitter != nil {
		p.applyDPIJitter(req, tx.DPIJitter)
	}

	return req
}

func stageRedactSensitiveData[Req, Resp any](p *Pipeline[Req, Resp], req *http.Request, tx *Tx) *http.Request {
	if tx.Flags&FlagRedact != 0 && tx.Redact != nil {
		return p.redactSensitiveData(req, tx.Redact)
	}

	return req
}

func stageUploadProgress[Req, Resp any](_ *Pipeline[Req, Resp], req *http.Request, _ *Tx) *http.Request {
	cfg := GetRequestConfig(req.Context())
	if cfg != nil && cfg.UploadProgress != nil && req.Body != nil && req.Body != http.NoBody {
		progressReader := &iokit.ProgressReader{
			Reader:     req.Body,
			Total:      req.ContentLength,
			OnProgress: cfg.UploadProgress,
		}
		req.Body = progressReader

		if req.GetBody != nil {
			origGetBody := req.GetBody
			req.GetBody = func() (io.ReadCloser, error) {
				rc, err := origGetBody()
				if err != nil {
					return nil, err
				}

				return &iokit.ProgressReader{
					Reader:     rc,
					Total:      req.ContentLength,
					OnProgress: cfg.UploadProgress,
				}, nil
			}
		}
	}

	return req
}

func stageJA4Report[Req, Resp any](_ *Pipeline[Req, Resp], req *http.Request, _ *Tx) *http.Request {
	cfg := GetRequestConfig(req.Context())
	if cfg != nil && cfg.JA4ReportStore != nil && cfg.JA4ReportStore.Target != nil {
		if cfg.JA4ReportStore.Target.JA4 == nil {
			cfg.JA4ReportStore.Target.JA4 = &ja4.Report{}
		}

		cfg.JA4ReportStore.Target.JA4.JA4H = telemetry.ComputeJA4HFromRequest(req)
	}

	return req
}

func (p *Pipeline[Req, Resp]) prepareRequestContext(req any, stdReq *http.Request) *http.Request {
	ctx := stdReq.Context()

	cfg := GetRequestConfig(ctx)
	if cfg == nil {
		return stdReq
	}

	if len(cfg.Modifiers) > 0 {
		if r, ok := req.(core.Request); ok {
			for _, mod := range cfg.Modifiers {
				mod.Apply(r)
			}
		} else {
			for _, mod := range cfg.Modifiers {
				mod.ApplyStd(stdReq)
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

	dialCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
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

	conn, err := netdial.DialL4(dialCtx, netdial.NetworkTCP.String(), targetAddr, dialOpts)
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

var defaultRedactHeaders = map[string]struct{}{
	"authorization":       {},
	"proxy-authorization": {},
	"cookie":              {},
	"set-cookie":          {},
	"x-api-key":           {},
}

func (p *Pipeline[Req, Resp]) redactSensitiveData(req *http.Request, redact *RedactConfig) *http.Request {
	var headers map[string]struct{}
	if len(redact.HeadersToRedact) > 0 {
		headers = make(map[string]struct{}, len(redact.HeadersToRedact))
		for _, h := range redact.HeadersToRedact {
			headers[strings.ToLower(h)] = struct{}{}
		}
	} else {
		headers = defaultRedactHeaders
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

	req.Header.Set(header.UserAgent, prof.UserAgent)

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
		req.Body = &iokit.JitterReader{
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
	if req.Header == nil {
		req.Header = make(http.Header)
	}

	if req.Header.Get(header.Referer) != "" || p.defaults.RefererState == nil {
		return
	}

	if lastURL := p.defaults.RefererState.LastURL.Get(); lastURL != "" {
		req.Header.Set(header.Referer, lastURL)
	}
}

func convertRequestToStd(r core.Request) *http.Request {
	if stdReq := r.HTTPRequest(); stdReq != nil {
		return stdReq
	}

	var (
		bodyReader io.Reader
		contentLen int64 = -1
	)

	fastAdapter, isFast := r.(interface{ FastHTTPRequest() *h1engine.Request })
	if isFast {
		if fastReq := fastAdapter.FastHTTPRequest(); fastReq != nil {
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

	stdReq, err := http.NewRequestWithContext(r.Context(), r.Method(), r.URL(), bodyReader) //nolint:gosec
	if err != nil {
		return &http.Request{}
	}

	if contentLen > 0 {
		stdReq.ContentLength = contentLen
	}

	if isFast {
		if fastReq := fastAdapter.FastHTTPRequest(); fastReq != nil {
			for k, v := range fastReq.Header.All() {
				stdReq.Header.Add(string(k), string(v))
			}

			if host := bytesconv.B2S(fastReq.Header.Peek(header.Host)); host != "" {
				stdReq.Host = host
			}
		}
	} else if r.Headers() != nil {
		for k, v := range r.Headers() {
			stdReq.Header.Add(string(k), string(v))
		}
	}

	return stdReq
}
