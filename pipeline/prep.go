// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/iokit"
	"github.com/lemon4ksan/foundation/net/http/header"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/netutil/dict"
	"github.com/lemon4ksan/aoni/netutil/netdial"
	"github.com/lemon4ksan/aoni/telemetry"
)

func (h *StdHandler) Prepare(stdReq *http.Request, tx *Tx) *http.Request {
	stdReq = h.prepareRequestContext(stdReq, stdReq)

	if len(h.defaults.BeforeRequest) > 0 {
		stdReq = stageBeforeRequestHooks(h, stdReq, tx)
	}

	if h.defaults.RefererAutomaton {
		stdReq = stageRefererHeader(h, stdReq, tx)
	}

	if tx.Flags&FlagRedact != 0 && tx.Redact != nil {
		stdReq = stageRedactSensitiveData(h, stdReq, tx)
	}

	stdReq = stageUploadProgress(h, stdReq, tx)
	stdReq = stageAvailableDictionary(h, stdReq, tx)

	return stdReq
}

func stageAvailableDictionary(h *StdHandler, req *http.Request, _ *Tx) *http.Request {
	if req == nil || req.URL == nil || !strings.EqualFold(req.URL.Scheme, "https") {
		// RFC 9842 §8: Compression Dictionary Transport MUST only be used in secure contexts (HTTPS).
		return req
	}

	cfg := GetRequestConfig(req)
	if cfg != nil && cfg.DisableDictionaryCompression {
		return req
	}

	if h.defaults.DisableDictionaryCompression {
		return req
	}

	store := h.defaults.DictionaryStore
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

func stageBeforeRequestHooks(h *StdHandler, req *http.Request, _ *Tx) *http.Request {
	for _, hook := range h.defaults.BeforeRequest {
		hook(req)
	}

	return req
}

func stageRefererHeader(h *StdHandler, req *http.Request, _ *Tx) *http.Request {
	if h.defaults.RefererAutomaton {
		h.applyRefererHeader(req)
	}

	return req
}

func stageRedactSensitiveData(h *StdHandler, req *http.Request, tx *Tx) *http.Request {
	if tx.Flags&FlagRedact != 0 && tx.Redact != nil {
		return h.redactSensitiveData(req, tx.Redact)
	}

	return req
}

func stageUploadProgress(h *StdHandler, req *http.Request, _ *Tx) *http.Request {
	cfg := GetRequestConfig(req)
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

func (h *StdHandler) prepareRequestContext(req any, stdReq *http.Request) *http.Request {
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

		ctx = cookie.WithProxyAddress(ctx, proxyStr)
	}

	if ctx != stdReq.Context() {
		return stdReq.WithContext(ctx)
	}

	return stdReq
}

func (h *StdHandler) TraceRequest(
	stdReq *http.Request,
	tx *Tx,
) (*http.Request, *telemetry.TraceInfo, func(resp *http.Response)) {
	var traceInfo *telemetry.TraceInfo

	switch {
	case tx.TraceInfo != nil:
		traceInfo = tx.TraceInfo

	case tx.Flags&FlagInspect != 0 && h.defaults.Inspector != nil:
		traceInfo = &telemetry.TraceInfo{}
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
				ProcessEarlyHints(stdReq.Context(), http.Header(header), h.prewarmTargetOrigin)
			}

			return nil
		},
	}

	stdReq = stdReq.WithContext(httptrace.WithClientTrace(stdReq.Context(), trace))

	return stdReq, traceInfo, traceInfo.Start() //nolint:bodyclose
}

func (h *StdHandler) prewarmTargetOrigin(ctx context.Context, targetURL string) {
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
		tlsCfg := &tls.Config{ServerName: host, NextProtos: []string{"h2", "http/1.1"}}
		uConn := tls.Client(conn, tlsCfg)

		handshakeErr := uConn.HandshakeContext(dialCtx)
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

func (h *StdHandler) redactSensitiveData(req *http.Request, redact *RedactConfig) *http.Request {
	var headers map[string]struct{}
	if len(redact.HeadersToRedact) > 0 {
		if redact.Headers != nil && len(redact.Headers) == len(redact.HeadersToRedact) {
			headers = redact.Headers
		} else {
			headers = make(map[string]struct{}, len(redact.HeadersToRedact))
			for _, h := range redact.HeadersToRedact {
				headers[strings.ToLower(h)] = struct{}{}
			}

			redact.Headers = headers
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

func (h *StdHandler) applyRefererHeader(req *http.Request) {
	if req.Header == nil {
		req.Header = make(http.Header)
	}

	if req.Header.Get(header.Referer) != "" || h.defaults.RefererState == nil {
		return
	}

	if lastURL := h.defaults.RefererState.LastURL.Get(); lastURL != "" {
		req.Header.Set(header.Referer, lastURL)
	}
}
