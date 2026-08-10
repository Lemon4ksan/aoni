// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	stdio "io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/telemetry"
)

type DoerFunc func(req *http.Request) (*http.Response, error)

func (f DoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

type mockDoer struct {
	fn func(req *http.Request) (*http.Response, error)
}

func (m *mockDoer) Do(req *http.Request) (*http.Response, error) {
	if m.fn != nil {
		return m.fn(req)
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       stdio.NopCloser(strings.NewReader("ok")),
		Request:    req,
	}, nil
}

type reallyCloserBody struct {
	stdio.Reader
	reallyClosed bool
}

func (r *reallyCloserBody) Read(p []byte) (int, error) { return r.Reader.Read(p) }
func (r *reallyCloserBody) Close() error               { return nil }
func (r *reallyCloserBody) ReallyClose()               { r.reallyClosed = true }

type mockRequest struct {
	ctx      context.Context
	header   http.Header
	method   string
	urlStr   string
	pathStr  string
	rawQuery string
	body     []byte
}

func newMockRequest(ctx context.Context, method, urlStr string) *mockRequest {
	u, _ := url.Parse(urlStr)
	path := ""

	query := ""
	if u != nil {
		path = u.Path
		query = u.RawQuery
	}

	return &mockRequest{
		ctx:      ctx,
		method:   method,
		urlStr:   urlStr,
		pathStr:  path,
		rawQuery: query,
		header:   make(http.Header),
	}
}

func (r *mockRequest) Context() context.Context       { return r.ctx }
func (r *mockRequest) SetContext(ctx context.Context) { r.ctx = ctx }
func (r *mockRequest) Method() string                 { return r.method }
func (r *mockRequest) SetMethod(m string)             { r.method = m }
func (r *mockRequest) SetMethodBytes(m []byte)        { r.method = string(m) }
func (r *mockRequest) URL() string                    { return r.urlStr }
func (r *mockRequest) SetURL(u string)                { r.urlStr = u }
func (r *mockRequest) SetURIBytes(u []byte)           { r.urlStr = string(u) }
func (r *mockRequest) Path() string                   { return r.pathStr }
func (r *mockRequest) SetPath(p string)               { r.pathStr = p }
func (r *mockRequest) RawQuery() string               { return r.rawQuery }
func (r *mockRequest) SetRawQuery(q string)           { r.rawQuery = q }
func (r *mockRequest) SetRawQueryBytes(q []byte)      { r.rawQuery = string(q) }
func (r *mockRequest) AddQueryParam(_, _ string)      {}
func (r *mockRequest) AddQueryParamBytes(_, _ []byte) {}
func (r *mockRequest) SetQueryParam(_, _ string)      {}
func (r *mockRequest) SetQueryParamBytes(_, _ []byte) {}
func (r *mockRequest) Header(key string) string       { return r.header.Get(key) }
func (r *mockRequest) HeaderBytes(key []byte) []byte  { return []byte(r.header.Get(string(key))) }
func (r *mockRequest) SetHeader(key, val string)      { r.header.Set(key, val) }
func (r *mockRequest) SetHeaderBytes(k, v []byte)     { r.header.Set(string(k), string(v)) }
func (r *mockRequest) AddHeader(key, val string)      { r.header.Add(key, val) }
func (r *mockRequest) AddHeaderBytes(k, v []byte)     { r.header.Add(string(k), string(v)) }
func (r *mockRequest) DelHeader(key string)           { r.header.Del(key) }
func (r *mockRequest) DelHeaderBytes(key []byte)      { r.header.Del(string(key)) }
func (r *mockRequest) ResetHeaders()                  { r.header = make(http.Header) }
func (r *mockRequest) SetBodyBytes(b []byte)          { r.body = b }
func (r *mockRequest) BodyBytes() []byte              { return r.body }
func (r *mockRequest) SetBodyStream(stdio.Reader, int64) {
}
func (r *mockRequest) BodyStream() stdio.Reader { return bytes.NewReader(r.body) }
func (r *mockRequest) HTTPRequest() *http.Request {
	req, _ := http.NewRequestWithContext(r.ctx, r.method, r.urlStr, bytes.NewReader(r.body))
	if req != nil {
		req.Header = r.header.Clone()
	}

	return req
}
func (r *mockRequest) EngineRequest() any { return nil }

var _ Request = (*mockRequest)(nil)

type mockCacheStore struct {
	data map[string][]byte
}

func newMockCacheStore() *mockCacheStore {
	return &mockCacheStore{data: make(map[string][]byte)}
}

func (m *mockCacheStore) Get(_ context.Context, key any) ([]byte, error) {
	kStr := key.(CacheKey).String()
	if val, ok := m.data[kStr]; ok && val != nil {
		return val, nil
	}

	return nil, errors.New("cache miss")
}

func (m *mockCacheStore) Set(_ context.Context, key any, val []byte, _ time.Duration) error {
	kStr := key.(CacheKey).String()
	if val == nil {
		delete(m.data, kStr)
	} else {
		m.data[kStr] = val
	}

	return nil
}

type mockSolver struct {
	solved bool
}

func (m *mockSolver) Solve(_ context.Context, _ error, req *http.Request) (*http.Response, error) {
	m.solved = true

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       stdio.NopCloser(strings.NewReader("solved")),
		Request:    req,
	}, nil
}

func TestTx_Pool_AcquireAndRelease(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	tx := AcquireTx(ctx)
	require.NotNil(t, tx)
	assert.Equal(t, ctx, tx.Ctx)

	tx.Flags = 0xFFFFFFFF
	tx.TargetURL = "http://test.com"
	tx.SizeLimit = 1000

	ReleaseTx(tx)

	tx2 := AcquireTx(ctx)
	assert.Equal(t, uint32(0), tx2.Flags)
	assert.Empty(t, tx2.TargetURL)
	assert.Equal(t, int64(0), tx2.SizeLimit)
	ReleaseTx(tx2)
}

func TestPipelineConfig_BuildFlags(t *testing.T) {
	t.Parallel()

	pipe := PipelineConfig{
		RotateUA:      true,
		DPIJitter:     &DPIJitterConfig{},
		Redact:        &RedactConfig{},
		Decompress:    true,
		Validate:      true,
		Challenge:     true,
		Cache:         &CacheConfig{},
		ProxyFailover: &ProxyFailoverConfig{},
		Hedging:       &HedgingConfig{},
		Inspect:       true,
		HAR:           &HARConfig{},
	}

	flags := pipe.BuildFlags()
	assert.Equal(t, flags, pipe.PrecomputedFlags)

	assert.NotZero(t, flags&FlagRotateUA)
	assert.NotZero(t, flags&FlagDPIJitter)
	assert.NotZero(t, flags&FlagRedact)
	assert.NotZero(t, flags&FlagDecompress)
	assert.NotZero(t, flags&FlagValidate)
	assert.NotZero(t, flags&FlagChallenge)
	assert.NotZero(t, flags&FlagCache)
	assert.NotZero(t, flags&FlagProxyFailover)
	assert.NotZero(t, flags&FlagHedging)
	assert.NotZero(t, flags&FlagInspect)
	assert.NotZero(t, flags&FlagHAR)

	key := CacheKey{Method: "GET", URL: "http://example.com"}
	assert.Equal(t, "GET:http://example.com", key.String())
}

func TestPipeline_Execute_FastPath(t *testing.T) {
	t.Parallel()

	defaults := ClientDefaults{}
	pipe := NewPipeline(defaults, ClientFingerprint{})

	doer := DoerFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       stdio.NopCloser(bytes.NewReader([]byte("fastpath_ok"))),
			Request:    req,
		}, nil
	})

	req := newMockRequest(t.Context(), http.MethodGet, "http://example.com/fast")
	pipeCfg := PipelineConfig{Decompress: true}

	resp, err := pipe.Execute(t.Context(), req, doer, pipeCfg)
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := stdio.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "fastpath_ok", string(body))
}

func TestPipeline_UnsafePhaseOrder_And_Hooks(t *testing.T) {
	t.Parallel()

	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})
	doer := &mockDoer{}

	mReq := newMockRequest(t.Context(), "GET", "http://unsafe.com")

	tx := AcquireTx(t.Context())
	tx.UnsafePhaseOrder = []PhaseID{PhasePrep, PhaseDispatch, PhaseValidate}

	var hookExecuted bool

	tx.UnsafeHooks = map[PhaseID][]UnsafeHook{
		PhaseDispatch: {
			func(_ *Tx, _ *http.Request, _ *http.Response) error {
				hookExecuted = true
				return nil
			},
		},
	}

	resp, err := pipeEngine.executeCustomPhaseOrder(tx, mReq, doer, tx.UnsafePhaseOrder)
	require.NoError(t, err)
	assert.True(t, hookExecuted)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	txError := AcquireTx(t.Context())
	txError.UnsafePhaseOrder = []PhaseID{PhasePrep, PhaseDispatch}
	errExpected := errors.New("unsafe hook abort error")
	txError.UnsafeHooks = map[PhaseID][]UnsafeHook{
		PhasePrep: {
			func(_ *Tx, _ *http.Request, _ *http.Response) error {
				return errExpected
			},
		},
	}

	_, errHook := pipeEngine.executeCustomPhaseOrder(txError, mReq, doer, txError.UnsafePhaseOrder)
	assert.ErrorIs(t, errHook, errExpected)

	ReleaseTx(tx)
	ReleaseTx(txError)
}

func TestPipeline_DisabledFlagsAndLookupDecoder(t *testing.T) {
	t.Parallel()

	ctx, reqCfg := AllocRequestConfig(t.Context())
	reqCfg.DisabledFlags = FlagDecompress | FlagValidate

	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})

	mReq := newMockRequest(t.Context(), "GET", "http://example.com/flags")
	mReq.SetContext(ctx)

	tx := AcquireTx(ctx)
	pipeCfg := PipelineConfig{
		Decompress: true,
		Validate:   true,
		Challenge:  true,
	}

	pipeEngine.initTx(tx, mReq, pipeCfg)

	assert.Equal(t, uint32(0), tx.Flags&FlagDecompress)
	assert.Equal(t, uint32(0), tx.Flags&FlagValidate)
	assert.NotEqual(t, uint32(0), tx.Flags&FlagChallenge)

	ReleaseTx(tx)
}

func TestPipeline_ResponseSizeLimit(t *testing.T) {
	t.Parallel()

	pipe := NewPipeline(ClientDefaults{}, ClientFingerprint{})

	t.Run("exceeds_content_length_fails_early", func(t *testing.T) {
		t.Parallel()

		doer := DoerFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				ContentLength: 2048,
				Body:          stdio.NopCloser(bytes.NewReader(bytes.Repeat([]byte("A"), 2048))),
				Request:       req,
			}, nil
		})

		req := newMockRequest(t.Context(), http.MethodGet, "http://example.com/large")
		pipeCfg := PipelineConfig{SizeLimit: 1024}

		_, err := pipe.Execute(t.Context(), req, doer, pipeCfg)
		require.Error(t, err)
		assert.ErrorIs(t, err, io.ErrResponseTooLarge)
	})

	t.Run("exceeds_stream_bytes_fails_during_read", func(t *testing.T) {
		t.Parallel()

		doer := DoerFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				ContentLength: -1, // Chunked / Unknown size
				Body:          stdio.NopCloser(bytes.NewReader(bytes.Repeat([]byte("B"), 2048))),
				Request:       req,
			}, nil
		})

		req := newMockRequest(t.Context(), http.MethodGet, "http://example.com/chunked")
		pipeCfg := PipelineConfig{SizeLimit: 1024}

		resp, err := pipe.Execute(t.Context(), req, doer, pipeCfg)
		require.NoError(t, err)

		defer resp.Body.Close()

		_, readErr := stdio.ReadAll(resp.Body)
		require.Error(t, readErr)
		assert.ErrorIs(t, readErr, io.ErrResponseTooLarge)
	})
}

func TestPipeline_DecompressionAndExplicitAcceptEncoding(t *testing.T) {
	t.Parallel()

	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})

	var gzBuf bytes.Buffer

	gzWriter := gzip.NewWriter(&gzBuf)
	_, _ = gzWriter.Write([]byte("decompressed_data"))
	_ = gzWriter.Close()

	reqAuto, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/gz", nil)
	respAuto := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Encoding": []string{"gzip"}},
		Body:       stdio.NopCloser(bytes.NewReader(gzBuf.Bytes())),
	}

	respDecompressed := pipeEngine.handleDecompressionAndTranscoding(reqAuto, respAuto)
	bodyBytes, err := stdio.ReadAll(respDecompressed.Body)
	require.NoError(t, err)
	assert.Equal(t, "decompressed_data", string(bodyBytes))
	assert.True(t, respDecompressed.Uncompressed)

	ctx, reqCfg := AllocRequestConfig(t.Context())
	reqCfg.HasExplicitAcceptEncoding = true

	reqExplicit, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/gz", nil)
	respExplicit := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Encoding": []string{"gzip"}},
		Body:       stdio.NopCloser(bytes.NewReader(gzBuf.Bytes())),
	}

	respRaw := pipeEngine.handleDecompressionAndTranscoding(reqExplicit, respExplicit)
	rawBytes, err := stdio.ReadAll(respRaw.Body)
	require.NoError(t, err)
	assert.Equal(t, gzBuf.Bytes(), rawBytes, "Explicit Accept-Encoding should preserve compressed raw bytes")
}

func TestPipeline_PostProcessResponse_Full(t *testing.T) {
	t.Parallel()

	respConflict := &http.Response{
		Header: http.Header{"Content-Length": []string{"100", "200"}},
	}
	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})
	_, errConflict := pipeEngine.postProcessResponse(&http.Request{}, respConflict, AcquireTx(t.Context()))
	assert.ErrorIs(t, errConflict, ErrConflictingContentLength)

	respTooLarge := &http.Response{
		ContentLength: 5000,
		Body:          stdio.NopCloser(strings.NewReader("large body")),
	}
	txSize := AcquireTx(t.Context())
	txSize.SizeLimit = 1000

	_, errSize := pipeEngine.postProcessResponse(&http.Request{}, respTooLarge, txSize)
	assert.Error(t, errSize)

	solver := &mockSolver{}
	defaultsWAF := ClientDefaults{
		ChallengeDetector: func(r *http.Response) (bool, error) {
			return r.StatusCode == http.StatusForbidden, nil
		},
		ChallengeSolver: solver,
	}

	pipeWAF := NewPipeline(defaultsWAF, ClientFingerprint{})
	txWAF := AcquireTx(t.Context())
	txWAF.Flags = FlagChallenge

	respWAF := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       stdio.NopCloser(strings.NewReader("<html>cf-challenge</html>")),
	}

	reqWAF, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://waf.com", nil)
	solvedResp, errWAF := pipeWAF.postProcessResponse(reqWAF, respWAF, txWAF)
	require.NoError(t, errWAF)
	assert.True(t, solver.solved)
	assert.Equal(t, http.StatusOK, solvedResp.StatusCode)

	respTranscode := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json; charset=windows-1251"}},
		Body:   stdio.NopCloser(strings.NewReader("\xef\xf0\xe8\xe2\xe5\xf2")),
	}
	respTranscode.Body = applyCharsetTranscoding(respTranscode, respTranscode.Body)
	transcodedBytes, _ := stdio.ReadAll(respTranscode.Body)
	assert.Equal(t, "привет", string(transcodedBytes))
	assert.NotContains(t, respTranscode.Header.Get("Content-Type"), "windows-1251")
}

func TestPipeline_Hedging_IdempotencyAndBody(t *testing.T) {
	t.Parallel()

	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})

	var calls atomic.Int32

	doerPost := &mockDoer{
		fn: func(req *http.Request) (*http.Response, error) {
			calls.Add(1)

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       stdio.NopCloser(strings.NewReader("ok")),
				Request:    req,
			}, nil
		},
	}

	postReq, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.com/post", nil)
	pipeCfg := PipelineConfig{
		Hedging: &HedgingConfig{
			DefaultDelay: 1 * time.Millisecond,
		},
	}

	resp, err := pipeEngine.executeWithHedging(postReq, doerPost, pipeCfg.Hedging)
	require.NoError(t, err)

	_ = resp.Body.Close()

	assert.Equal(t, int32(1), calls.Load(), "POST should not trigger hedging unless AllowNonReadOnly is true")

	// 2. Поток без повторения (GetBody == nil) возвращает ErrHedgingBodyNonRepeatable
	postReqWithBody := &http.Request{
		Method:  http.MethodPost,
		URL:     postReq.URL,
		Body:    stdio.NopCloser(strings.NewReader("non-repeatable body")),
		GetBody: nil,
	}

	delay := 1 * time.Millisecond
	_, errHedge := pipeEngine.dispatchHedgingAttempts(postReqWithBody, doerPost, delay)
	assert.ErrorIs(t, errHedge, ErrHedgingBodyNonRepeatable)
}

func TestPipeline_ProxyFailover_And_Hedging(t *testing.T) {
	t.Parallel()

	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})

	var attempts atomic.Int32

	doerFailover := &mockDoer{
		fn: func(req *http.Request) (*http.Response, error) {
			attempts.Add(1)

			proxyAddr := cookie.GetProxyAddress(req.Context())
			if strings.Contains(proxyAddr, "proxy1") {
				return &http.Response{
					StatusCode: http.StatusBadGateway,
					Body:       stdio.NopCloser(strings.NewReader("")),
				}, nil
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       stdio.NopCloser(strings.NewReader("proxy2 ok")),
				Request:    req,
			}, nil
		},
	}

	failoverCfg := &ProxyFailoverConfig{
		Proxies:    []string{"http://proxy1.com:8080", "http://proxy2.com:8080"},
		RetryLimit: 2,
	}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://target.com", nil)
	resp1, err1 := pipeEngine.executeWithProxyFailover(req, doerFailover, failoverCfg, nil)
	require.NoError(t, err1)
	assert.Equal(t, http.StatusOK, resp1.StatusCode)
	_ = resp1.Body.Close()
}

func TestPipeline_Caching_Full(t *testing.T) {
	t.Parallel()

	cacheStore := newMockCacheStore()
	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})

	cacheCfg := &CacheConfig{
		Store:      cacheStore,
		DefaultTTL: 1 * time.Minute,
	}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/cached-route", nil)
	req.Header.Set("Accept-Language", "ru-RU")

	respOriginal := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Cache-Control": []string{"max-age=60"},
			"Vary":          []string{"Accept-Language"},
		},
		Body: stdio.NopCloser(strings.NewReader("cached payload data")),
	}

	pipeEngine.saveToCache(req, respOriginal, cacheCfg)

	cachedResp := pipeEngine.tryGetFromCache(req, cacheCfg)
	require.NotNil(t, cachedResp)
	assert.Equal(t, http.StatusOK, cachedResp.StatusCode)
	cachedBody, _ := stdio.ReadAll(cachedResp.Body)
	assert.Equal(t, "cached payload data", string(cachedBody))

	reqMismatch, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/cached-route", nil)
	reqMismatch.Header.Set("Accept-Language", "en-US")
	assert.Nil(t, pipeEngine.tryGetFromCache(reqMismatch, cacheCfg))

	reqPOST, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.com/cached-route", nil)
	respPOST := &http.Response{StatusCode: http.StatusOK}
	pipeEngine.invalidateCache(reqPOST, respPOST, cacheCfg)

	assert.Nil(t, pipeEngine.tryGetFromCache(req, cacheCfg))

	respFresh1 := &http.Response{Header: http.Header{"Cache-Control": []string{"s-maxage=120"}}}
	dur1, ok1 := parseFreshnessLifetime(respFresh1)
	assert.True(t, ok1)
	assert.Equal(t, 120*time.Second, dur1)

	respFresh2 := &http.Response{
		Header: http.Header{"Expires": []string{time.Now().Add(10 * time.Second).Format(http.TimeFormat)}},
	}
	_, ok2 := parseFreshnessLifetime(respFresh2)
	assert.True(t, ok2)
}

func TestPipeline_TraceInfoCallbacks(t *testing.T) {
	t.Parallel()

	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})
	stdReq, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/trace", nil)

	traceInfo := &telemetry.TraceInfo{}
	tx := AcquireTx(t.Context())
	tx.TraceInfo = traceInfo

	reqWithTrace, info, traceEnd := pipeEngine.traceRequest(stdReq, tx)
	require.NotNil(t, info)
	require.NotNil(t, traceEnd)

	trace := httptrace.ContextClientTrace(reqWithTrace.Context())
	require.NotNil(t, trace)

	trace.DNSStart(httptrace.DNSStartInfo{})
	trace.DNSDone(httptrace.DNSDoneInfo{})
	trace.ConnectStart("tcp", "127.0.0.1:80")
	trace.ConnectDone("tcp", "127.0.0.1:80", nil)
	trace.TLSHandshakeStart()
	trace.TLSHandshakeDone(tls.ConnectionState{}, nil)
	trace.GotConn(httptrace.GotConnInfo{Conn: &net.TCPConn{}})

	resp := &http.Response{ContentLength: 500}
	traceEnd(resp)

	assert.GreaterOrEqual(t, traceInfo.Total, time.Duration(0))
	assert.Equal(t, int64(500), traceInfo.ResponseSize)

	ReleaseTx(tx)
}

func TestPipeline_FinalizeJA4Report(t *testing.T) {
	t.Parallel()

	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})

	pipeEngine.finalizeJA4Report(nil)

	traceInfo := &telemetry.TraceInfo{}
	store := &JA4ReportStore{
		Target: traceInfo,
		Report: &ja4.Report{
			JA4:         "t13d1516h2_8daaf6152771_e5627efa2ab1",
			Protocol:    "t",
			Version:     "13",
			SNI:         "d",
			CipherCount: 15,
			ExtCount:    16,
			ALPN:        "h2",
		},
	}

	tx := AcquireTx(t.Context())
	tx.JA4ReportStore = store

	pipeEngine.finalizeJA4Report(tx)

	require.NotNil(t, traceInfo.JA4)
	assert.Equal(t, "t13d1516h2_8daaf6152771_e5627efa2ab1", traceInfo.JA4.JA4)
	assert.Equal(t, "h2", traceInfo.JA4.ALPN)

	ReleaseTx(tx)
}

func TestRequestConfig_Lifecycle(t *testing.T) {
	t.Parallel()

	ctx, cfg := AllocRequestConfig(t.Context())
	require.NotNil(t, cfg)
	assert.Equal(t, cfg, GetRequestConfig(ctx))

	mReq := newMockRequest(t.Context(), "GET", "http://test.com")
	cfg1 := GetOrInitRequestConfig(mReq)
	require.NotNil(t, cfg1)

	stdReq, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", nil)
	cfg2 := GetOrInitRequestConfig(stdReq)
	require.NotNil(t, cfg2)

	cfg3 := GetOrInitRequestConfig(ctx)
	require.NotNil(t, cfg3)

	cfg4 := GetOrInitRequestConfig("invalid_type")
	require.NotNil(t, cfg4)

	cfg.Decoders = map[string]ResponseDecoder{
		"application/json": ResponseDecoder(nil),
	}
	assert.Nil(t, cfg.LookupDecoder("unknown/type"))

	rcBody := &reallyCloserBody{Reader: strings.NewReader("sample response body")}
	cancelCalled := false
	stdReqWithCancel, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://test.com", nil)
	cfg.RequestTimeoutCancel = func() { cancelCalled = true }

	resp := &http.Response{
		Body:    rcBody,
		Request: stdReqWithCancel,
	}

	CloseResponse(resp)
	assert.True(t, rcBody.reallyClosed)
	assert.True(t, cancelCalled)

	CloseResponse(nil)
}

func TestPipeline_PhasePrep_Full(t *testing.T) {
	t.Parallel()

	var beforeHookCalled bool

	defaults := ClientDefaults{
		BeforeRequest: []func(*http.Request){
			func(_ *http.Request) { beforeHookCalled = true },
		},
		UARotationProfiles: []BrowserProfile{
			{UserAgent: "RotatedUA/1.0", ClientHints: map[string]string{"Sec-CH-UA": "Profile1"}},
		},
		RefererAutomaton: true,
		RefererState:     &RefererState{LastURL: "http://previous.com"},
	}

	fingerprintConfig := ClientFingerprint{
		PacketPadding: &fingerprint.PaddingConfig{
			MinPaddingBytes: 5,
			MaxPaddingBytes: 10,
			PaddingHeader:   "X-Custom-Padding",
		},
	}

	pipeEngine := NewPipeline(defaults, fingerprintConfig)

	mReq := newMockRequest(t.Context(), "POST", "http://example.com/api")
	mReq.SetBodyBytes([]byte("upload payload"))

	ctx, reqCfg := AllocRequestConfig(t.Context())

	var uploadBytesRead int64

	reqCfg.UploadProgress = func(current, _ int64) {
		uploadBytesRead = current
	}
	reqCfg.TimeoutOverride = 5 * time.Second
	reqCfg.ProxyAddr, _ = url.Parse("http://proxy.local:8080")

	mReq.SetHeader("X-Mod-Header", "injected")
	mReq.SetContext(ctx)

	tx := AcquireTx(ctx)
	tx.Flags = FlagRotateUA | FlagDPIJitter | FlagRedact
	tx.DPIJitter = &DPIJitterConfig{MinDelay: 1 * time.Millisecond, MaxDelay: 2 * time.Millisecond}
	tx.Redact = &RedactConfig{HeadersToRedact: []string{"Authorization"}}

	stdReq := pipeEngine.prepareRequest(mReq, tx)

	assert.True(t, beforeHookCalled)
	assert.NotEmpty(t, stdReq.Header.Get("X-Custom-Padding"))
	assert.Equal(t, "http://previous.com", stdReq.Header.Get("Referer"))
	assert.Equal(t, "RotatedUA/1.0", stdReq.Header.Get("User-Agent"))
	assert.Equal(t, "Profile1", stdReq.Header.Get("Sec-CH-UA"))
	assert.Equal(t, "injected", stdReq.Header.Get("X-Mod-Header"))

	_, _ = stdio.ReadAll(stdReq.Body)

	assert.Greater(t, uploadBytesRead, int64(0))

	ReleaseTx(tx)
}

func TestPipeline_Cloudflare403WAF(t *testing.T) {
	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})

	var attempts atomic.Int32

	doer := DoerFunc(func(req *http.Request) (*http.Response, error) {
		attempts.Add(1)

		hdr := make(http.Header)
		hdr.Set("cf-mitigated", "challenge")

		return &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     hdr,
			Body:       stdio.NopCloser(strings.NewReader("Cloudflare WAF Challenge")),
			Request:    req,
		}, nil
	})

	mReq := newMockRequest(t.Context(), "GET", "http://example.com/protected")
	resp, err := pipeEngine.Execute(t.Context(), mReq, doer, PipelineConfig{})
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "challenge", resp.Header.Get("cf-mitigated"))
}

func TestPipeline_MisdirectedRequest421(t *testing.T) {
	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})

	var attempts atomic.Int32

	doer := DoerFunc(func(req *http.Request) (*http.Response, error) {
		attempts.Add(1)

		return &http.Response{
			StatusCode: http.StatusMisdirectedRequest,
			Header:     make(http.Header),
			Body:       stdio.NopCloser(strings.NewReader("421 Misdirected Request")),
			Request:    req,
		}, nil
	})

	mReq := newMockRequest(t.Context(), "GET", "http://example.com/misdirected")
	resp, err := pipeEngine.Execute(t.Context(), mReq, doer, PipelineConfig{})
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusMisdirectedRequest, resp.StatusCode)
}

func TestPipeline_TooManyRequests429_RetryAfter(t *testing.T) {
	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})

	doer := DoerFunc(func(req *http.Request) (*http.Response, error) {
		hdr := make(http.Header)
		hdr.Set("Retry-After", "1")

		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     hdr,
			Body:       stdio.NopCloser(strings.NewReader("Rate Limited")),
			Request:    req,
		}, nil
	})

	mReq := newMockRequest(t.Context(), "GET", "http://example.com/rate-limited")
	resp, err := pipeEngine.Execute(t.Context(), mReq, doer, PipelineConfig{})
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, "1", resp.Header.Get("Retry-After"))
}

func TestPipeline_ServiceUnavailable503(t *testing.T) {
	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})

	doer := DoerFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     make(http.Header),
			Body:       stdio.NopCloser(strings.NewReader("503 Service Unavailable")),
			Request:    req,
		}, nil
	})

	mReq := newMockRequest(t.Context(), "GET", "http://example.com/down")
	resp, err := pipeEngine.Execute(t.Context(), mReq, doer, PipelineConfig{})
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestAltSvcCache_RecordingAndLookup(t *testing.T) {
	cache := NewAltSvcCache()
	cache.ParseAndStore("example.com", `h3=":443"; ma=3600`)

	assert.True(t, cache.HasH3Support("example.com"))

	cache.RecordH3Failure("example.com", 5*time.Minute)
	assert.False(t, cache.HasH3Support("example.com"))
}

func TestRefererAutomaton_PolicyTransitions(t *testing.T) {
	auto := NewRefererAutomaton(PolicyNoRefererWhenDowngrade)
	u1, _ := url.Parse("https://origin.com/page1")
	u2, _ := url.Parse("https://origin.com/page2")

	auto.UpdateLastURL(u1)
	ref := auto.ComputeReferer(u2)
	assert.Equal(t, "https://origin.com/page1", ref)
}
