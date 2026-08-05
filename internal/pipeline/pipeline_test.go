// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"bytes"
	"context"
	"errors"
	stdio "io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/telemetry"
)

type mockRequest struct {
	ctx    context.Context
	method string
	urlStr string
	header http.Header
	body   []byte
	stdReq *http.Request
}

func newMockRequest(method, urlStr string) *mockRequest {
	return &mockRequest{
		ctx:    context.Background(),
		method: method,
		urlStr: urlStr,
		header: make(http.Header),
	}
}

func (m *mockRequest) Context() context.Context       { return m.ctx }
func (m *mockRequest) SetContext(ctx context.Context) { m.ctx = ctx }
func (m *mockRequest) Method() string                 { return m.method }
func (m *mockRequest) SetMethod(method string)        { m.method = method }
func (m *mockRequest) SetMethodBytes(method []byte)   { m.method = string(method) }
func (m *mockRequest) URL() string                    { return m.urlStr }
func (m *mockRequest) SetURL(urlStr string)           { m.urlStr = urlStr }
func (m *mockRequest) SetURIBytes(uri []byte)         { m.urlStr = string(uri) }
func (m *mockRequest) Path() string {
	u, _ := url.Parse(m.urlStr)
	if u != nil {
		return u.Path
	}

	return m.urlStr
}
func (m *mockRequest) SetPath(path string) { m.urlStr = path }
func (m *mockRequest) RawQuery() string {
	u, _ := url.Parse(m.urlStr)
	if u != nil {
		return u.RawQuery
	}

	return ""
}
func (m *mockRequest) SetRawQuery(query string)        { m.urlStr += "?" + query }
func (m *mockRequest) SetRawQueryBytes(query []byte)   { m.SetRawQuery(string(query)) }
func (m *mockRequest) AddQueryParam(key, value string) { m.SetRawQuery(key + "=" + value) }
func (m *mockRequest) AddQueryParamBytes(k, v []byte)  { m.AddQueryParam(string(k), string(v)) }
func (m *mockRequest) SetQueryParam(key, value string) { m.AddQueryParam(key, value) }
func (m *mockRequest) SetQueryParamBytes(k, v []byte)  { m.SetQueryParam(string(k), string(v)) }
func (m *mockRequest) Header(key string) string        { return m.header.Get(key) }
func (m *mockRequest) HeaderBytes(key []byte) []byte   { return []byte(m.Header(string(key))) }
func (m *mockRequest) SetHeader(key, value string)     { m.header.Set(key, value) }
func (m *mockRequest) SetHeaderBytes(k, v []byte)      { m.SetHeader(string(k), string(v)) }
func (m *mockRequest) AddHeader(key, value string)     { m.header.Add(key, value) }
func (m *mockRequest) AddHeaderBytes(k, v []byte)      { m.AddHeader(string(k), string(v)) }
func (m *mockRequest) DelHeader(key string)            { m.header.Del(key) }
func (m *mockRequest) DelHeaderBytes(key []byte)       { m.DelHeader(string(key)) }
func (m *mockRequest) ResetHeaders()                   { m.header = make(http.Header) }
func (m *mockRequest) SetBodyBytes(body []byte)        { m.body = body }
func (m *mockRequest) BodyBytes() []byte               { return m.body }
func (m *mockRequest) SetBodyStream(r stdio.Reader, _ int64) {
	b, _ := stdio.ReadAll(r)
	m.body = b
}
func (m *mockRequest) BodyStream() stdio.Reader { return bytes.NewReader(m.body) }
func (m *mockRequest) HTTPRequest() *http.Request {
	if m.stdReq != nil {
		return m.stdReq
	}

	req, _ := http.NewRequestWithContext(m.ctx, m.method, m.urlStr, bytes.NewReader(m.body))
	req.Header = m.header.Clone()

	return req
}
func (m *mockRequest) EngineRequest() any { return m.HTTPRequest() }

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

type mockCacheStore struct {
	mu    sync.Mutex
	store map[string][]byte
}

func newMockCacheStore() *mockCacheStore {
	return &mockCacheStore{store: make(map[string][]byte)}
}

func (m *mockCacheStore) Get(_ context.Context, key any) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	val, ok := m.store[key.(CacheKey).String()]
	if !ok {
		return nil, errors.New("cache miss")
	}

	return val, nil
}

func (m *mockCacheStore) Set(_ context.Context, key any, val []byte, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if val == nil {
		delete(m.store, key.(CacheKey).String())
	} else {
		m.store[key.(CacheKey).String()] = val
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

type reallyCloserBody struct {
	stdio.Reader
	reallyClosed bool
}

func (r *reallyCloserBody) Read(p []byte) (int, error) { return r.Reader.Read(p) }
func (r *reallyCloserBody) Close() error               { return nil }
func (r *reallyCloserBody) ReallyClose()               { r.reallyClosed = true }

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

func TestTx_Pool_AcquireAndRelease(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tx := AcquireTx(ctx)
	require.NotNil(t, tx)
	assert.Equal(t, ctx, tx.Ctx)

	tx.Flags = 0xFFFFFFFF
	tx.TargetURL = "http://test.com"
	tx.SizeLimit = 1000

	ReleaseTx(tx)

	// Повторно получаем объект из пула
	tx2 := AcquireTx(ctx)
	assert.Equal(t, uint32(0), tx2.Flags)
	assert.Empty(t, tx2.TargetURL)
	assert.Equal(t, int64(0), tx2.SizeLimit)
	ReleaseTx(tx2)
}

func TestRequestConfig_Lifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx, cfg := AllocRequestConfig(ctx)
	require.NotNil(t, cfg)
	assert.Equal(t, cfg, GetRequestConfig(ctx))

	mReq := newMockRequest("GET", "http://test.com")
	cfg1 := GetOrInitRequestConfig(mReq)
	require.NotNil(t, cfg1)

	stdReq, _ := http.NewRequest("GET", "http://test.com", nil)
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
	stdReqWithCancel, _ := http.NewRequestWithContext(ctx, "GET", "http://test.com", nil)
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

	fingerprint := ClientFingerprint{
		PacketPadding: &fingerprint.PaddingConfig{
			MinPaddingBytes: 5,
			MaxPaddingBytes: 10,
			PaddingHeader:   "X-Custom-Padding",
		},
	}

	pipeEngine := NewPipeline(defaults, fingerprint)

	mReq := newMockRequest("POST", "http://example.com/api")
	mReq.SetBodyBytes([]byte("upload payload"))

	ctx := context.Background()
	ctx, reqCfg := AllocRequestConfig(ctx)

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

func TestPipeline_TraceRequest(t *testing.T) {
	t.Parallel()

	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})
	stdReq, _ := http.NewRequest("GET", "http://example.com", nil)

	tx := AcquireTx(context.Background())
	tx.TraceInfo = &telemetry.TraceInfo{}

	_, traceInfo, traceEnd := pipeEngine.traceRequest(stdReq, tx)
	require.NotNil(t, traceInfo)
	require.NotNil(t, traceEnd)

	resp := &http.Response{ContentLength: 100}
	traceEnd(resp)

	assert.Equal(t, int64(100), traceInfo.ResponseSize)

	ReleaseTx(tx)
}

func TestPipeline_ProxyFailover_And_Hedging(t *testing.T) {
	t.Parallel()

	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})

	var attempts int32

	doerFailover := &mockDoer{
		fn: func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&attempts, 1)

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

	req, _ := http.NewRequest("GET", "http://target.com", nil)
	resp1, err1 := pipeEngine.executeWithProxyFailover(req, doerFailover, failoverCfg, nil)
	require.NoError(t, err1)
	assert.Equal(t, http.StatusOK, resp1.StatusCode)
	_ = resp1.Body.Close()
}

func TestPipeline_PostProcessResponse_Full(t *testing.T) {
	t.Parallel()

	respConflict := &http.Response{
		Header: http.Header{"Content-Length": []string{"100", "200"}},
	}
	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})
	_, errConflict := pipeEngine.postProcessResponse(&http.Request{}, respConflict, AcquireTx(context.Background()))
	assert.ErrorIs(t, errConflict, ErrConflictingContentLength)

	respTooLarge := &http.Response{
		ContentLength: 5000,
		Body:          stdio.NopCloser(strings.NewReader("large body")),
	}
	txSize := AcquireTx(context.Background())
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
	txWAF := AcquireTx(context.Background())
	txWAF.Flags = FlagChallenge

	respWAF := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       stdio.NopCloser(strings.NewReader("<html>cf-challenge</html>")),
	}

	reqWAF, _ := http.NewRequest("GET", "http://waf.com", nil)
	solvedResp, errWAF := pipeWAF.postProcessResponse(reqWAF, respWAF, txWAF)
	require.NoError(t, errWAF)
	assert.True(t, solver.solved)
	assert.Equal(t, http.StatusOK, solvedResp.StatusCode)

	respTranscode := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json; charset=windows-1251"}},
		Body:   stdio.NopCloser(strings.NewReader("\xef\xf0\xe8\xe2\xe5\xf2")),
	}
	applyCharsetTranscoding(respTranscode)
	transcodedBytes, _ := stdio.ReadAll(respTranscode.Body)
	assert.Equal(t, "привет", string(transcodedBytes))
	assert.NotContains(t, respTranscode.Header.Get("Content-Type"), "windows-1251")
}

func TestPipeline_Caching_Full(t *testing.T) {
	t.Parallel()

	cacheStore := newMockCacheStore()
	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})

	cacheCfg := &CacheConfig{
		Store:      cacheStore,
		DefaultTTL: 1 * time.Minute,
	}

	req, _ := http.NewRequest("GET", "http://example.com/cached-route", nil)
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

	reqMismatch, _ := http.NewRequest("GET", "http://example.com/cached-route", nil)
	reqMismatch.Header.Set("Accept-Language", "en-US")
	assert.Nil(t, pipeEngine.tryGetFromCache(reqMismatch, cacheCfg))

	reqPOST, _ := http.NewRequest("POST", "http://example.com/cached-route", nil)
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

func TestPipeline_UnsafePhaseOrder_And_Hooks(t *testing.T) {
	t.Parallel()

	pipeEngine := NewPipeline(ClientDefaults{}, ClientFingerprint{})
	doer := &mockDoer{}

	mReq := newMockRequest("GET", "http://unsafe.com")

	tx := AcquireTx(context.Background())
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

	// Прерывание исполнения при ошибке из Unsafe-хука
	txError := AcquireTx(context.Background())
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

	tx := AcquireTx(context.Background())
	tx.JA4ReportStore = store

	pipeEngine.finalizeJA4Report(tx)

	require.NotNil(t, traceInfo.JA4)
	assert.Equal(t, "t13d1516h2_8daaf6152771_e5627efa2ab1", traceInfo.JA4.JA4)
	assert.Equal(t, "h2", traceInfo.JA4.ALPN)

	ReleaseTx(tx)
}
