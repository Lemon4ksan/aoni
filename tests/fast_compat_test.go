// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/telemetry"
)

// TestFastClient_BasicGet tests GET request execution and body reading.
func TestFastClient_BasicGet(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
		_, _ = fmt.Fprint(w, "User-agent: go\nDisallow: /something/")
	}))
	t.Cleanup(ts.Close)

	// 1. Direct fast.Client
	c := fast.NewClient(option.WithBaseURL(ts.URL))
	resp, err := c.Request(t.Context(), http.MethodGet, "/")
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.True(t, strings.HasPrefix(string(resp.BodyBytes()), "User-agent:"))
	assert.Equal(t, "Wed, 21 Oct 2015 07:28:00 GMT", resp.Header("Last-Modified"))

	// 2. Bridge stdClient
	stdClient := fast.NewStdClient(c)
	stdResp, err := stdClient.Get(ts.URL)
	require.NoError(t, err)

	defer stdResp.Body.Close()

	body, err := io.ReadAll(stdResp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, stdResp.StatusCode)
	assert.True(t, strings.HasPrefix(string(body), "User-agent:"))
}

// TestFastClient_HeadRequest tests HEAD requests and body emptiness.
func TestFastClient_HeadRequest(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient()
	resp, err := c.Request(t.Context(), http.MethodHead, ts.URL)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, "100", resp.Header("Content-Length"))
	assert.Empty(t, resp.BodyBytes())

	stdClient := fast.NewStdClient(c)
	stdResp, err := stdClient.Head(ts.URL)
	require.NoError(t, err)

	defer stdResp.Body.Close()

	body, err := io.ReadAll(stdResp.Body)
	require.NoError(t, err)

	assert.Equal(t, int64(100), stdResp.ContentLength)
	assert.Empty(t, body)
}

// TestFastClient_PostAndPostFormFormat tests POST payload & Content-Type formatting.
func TestFastClient_PostAndPostFormFormat(t *testing.T) {
	t.Parallel()

	var reqMethod, reqContentType, reqBody string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqMethod = r.Method
		reqContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		reqBody = string(b)

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient()

	// JSON POST
	jsonPayload := `{"key":"value"}`
	resp, err := c.Request(t.Context(), http.MethodPost, ts.URL,
		mod.WithHeader("Content-Type", "application/json"),
		mod.WithBodyBytes([]byte(jsonPayload)),
	)
	require.NoError(t, err)

	resp.Close()

	assert.Equal(t, http.MethodPost, reqMethod)
	assert.Equal(t, "application/json", reqContentType)
	assert.Equal(t, jsonPayload, reqBody)

	// Form POST via stdClient bridge
	stdClient := fast.NewStdClient(c)
	form := url.Values{}
	form.Set("foo", "bar")
	form.Add("foo", "bar2")
	form.Set("bar", "baz")

	stdResp, err := stdClient.PostForm(ts.URL, form)
	require.NoError(t, err)

	stdResp.Body.Close()

	assert.Equal(t, http.MethodPost, reqMethod)
	assert.Equal(t, "application/x-www-form-urlencoded", reqContentType)
	assert.True(t, reqBody == "foo=bar&foo=bar2&bar=baz" || reqBody == "bar=baz&foo=bar&foo=bar2")
}

// TestFastClient_Redirects tests redirect limits, referer headers, and method transformations.
func TestFastClient_Redirects(t *testing.T) {
	t.Parallel()

	var ts *httptest.Server

	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.FormValue("n"))
		if n == 3 {
			if r.Header.Get("Referer") != ts.URL+"/?n=2" {
				t.Errorf("expected referer %q; got %q", ts.URL+"/?n=2", r.Header.Get("Referer"))
			}
		}

		if n < 5 {
			http.Redirect(w, r, fmt.Sprintf("/?n=%d", n+1), http.StatusFound)
			return
		}

		_, _ = fmt.Fprintf(w, "done: n=%d", n)
	}))
	t.Cleanup(ts.Close)

	// 1. fast.Client default follow redirect
	c := fast.NewClient(option.WithRedirectLimit(10))
	resp, err := c.Request(t.Context(), http.MethodGet, ts.URL+"/?n=1")
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, "done: n=5", string(resp.BodyBytes()))

	// 2. Exceeding max redirects limit
	cLimit := fast.NewClient(option.WithRedirectLimit(3))
	_, errLimit := cLimit.Request(t.Context(), http.MethodGet, ts.URL+"/?n=1")
	require.ErrorIs(t, errLimit, fast.ErrMaxRedirectsExceeded)
}

// TestFastClient_RedirectMethods tests HTTP status codes 301/302/303/307/308 method preservation/conversion.
func TestFastClient_RedirectMethods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		statusCode int
		reqMethod  string
		wantMethod string
	}{
		{http.StatusMovedPermanently, http.MethodPost, http.MethodGet},
		{http.StatusFound, http.MethodPost, http.MethodGet},
		{http.StatusSeeOther, http.MethodPost, http.MethodGet},
		{http.StatusTemporaryRedirect, http.MethodPost, http.MethodPost},
		{http.StatusPermanentRedirect, http.MethodPost, http.MethodPost},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d_%s", tt.statusCode, tt.reqMethod), func(t *testing.T) {
			t.Parallel()

			var (
				receivedMethod string
				ts             *httptest.Server
			)

			ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/target" {
					receivedMethod = r.Method

					w.WriteHeader(http.StatusOK)

					return
				}

				http.Redirect(w, r, ts.URL+"/target", tt.statusCode)
			}))
			t.Cleanup(ts.Close)

			c := fast.NewClient(option.WithRedirectLimit(5))
			resp, err := c.Request(t.Context(), tt.reqMethod, ts.URL+"/",
				mod.WithBodyBytes([]byte("payload")),
			)
			require.NoError(t, err)

			resp.Close()

			assert.Equal(t, tt.wantMethod, receivedMethod)
		})
	}
}

// TestFastClient_CrossDomainHeaderScrubbing verifies sensitive headers are stripped on cross-origin redirects.
func TestFastClient_CrossDomainHeaderScrubbing(t *testing.T) {
	t.Parallel()

	var targetHeaders http.Header

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHeaders = r.Header.Clone()

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(targetServer.Close)

	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetServer.URL, http.StatusFound)
	}))
	t.Cleanup(originServer.Close)

	c := fast.NewClient(option.WithRedirectLimit(5))
	resp, err := c.Request(t.Context(), http.MethodGet, originServer.URL,
		mod.WithHeader("Authorization", "Bearer secret-token"),
		mod.WithHeader("Cookie", "session=12345"),
		mod.WithHeader("X-Custom-App", "aoni"),
	)
	require.NoError(t, err)

	resp.Close()

	// Sensitive auth and cookies must be stripped across domains
	assert.Empty(t, targetHeaders.Get("Authorization"))
	assert.Empty(t, targetHeaders.Get("Cookie"))
	// Non-sensitive custom header preserved
	assert.Equal(t, "aoni", targetHeaders.Get("X-Custom-App"))
}

// TestFastClient_HTTPSDowngradeRefererStripping tests Referer stripping on HTTPS->HTTP downgrade.
func TestFastClient_HTTPSDowngradeRefererStripping(t *testing.T) {
	t.Parallel()

	var receivedReferer string

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedReferer = r.Header.Get("Referer")

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(httpServer.Close)

	httpsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, httpServer.URL, http.StatusFound)
	}))
	t.Cleanup(httpsServer.Close)

	c := fast.NewClient(
		option.WithRedirectLimit(5),
		option.WithInsecureSkipVerify(),
	)

	resp, err := c.Request(t.Context(), http.MethodGet, httpsServer.URL,
		mod.WithHeader("Referer", httpsServer.URL+"/secret-path"),
	)
	require.NoError(t, err)

	resp.Close()

	// Referer must be stripped on HTTPS to HTTP downgrade
	assert.Empty(t, receivedReferer)
}

// TestFastClient_UserInfoBasicAuth tests username/password extraction from URL.
func TestFastClient_UserInfoBasicAuth(t *testing.T) {
	t.Parallel()

	var authHeader string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	u, err := url.Parse(ts.URL)
	require.NoError(t, err)

	u.User = url.UserPassword("admin", "secret123")

	c := fast.NewClient()
	resp, err := c.Request(t.Context(), http.MethodGet, u.String())
	require.NoError(t, err)

	resp.Close()

	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:secret123"))
	assert.Equal(t, expectedAuth, authHeader)
}

// TestFastClient_CookieJarIntegration tests cookie parsing, storage, and transmission across requests.
func TestFastClient_CookieJarIntegration(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/set" {
			http.SetCookie(w, &http.Cookie{Name: "session_id", Value: "xyz123", Path: "/"})
			w.WriteHeader(http.StatusOK)

			return
		}

		cookieVal := r.Header.Get("Cookie")
		_, _ = fmt.Fprint(w, cookieVal)
	}))
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)

	c := fast.NewClient(option.WithCookieJar(jar))

	// Step 1: Set cookie
	resp1, err := c.Request(t.Context(), http.MethodGet, ts.URL+"/set")
	require.NoError(t, err)

	resp1.Close()

	// Step 2: Get cookie on second request
	resp2, err := c.Request(t.Context(), http.MethodGet, ts.URL+"/get")
	require.NoError(t, err)

	defer resp2.Close()

	assert.Equal(t, "session_id=xyz123", string(resp2.BodyBytes()))
}

// TestCookie_ParsingAndAttributes tests parsing and transmission of Cookie attributes via bridge.
func TestCookie_ParsingAndAttributes(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cookie1 := &http.Cookie{
			Name:     "auth_token",
			Value:    "jwt-value-123",
			Path:     "/api",
			Domain:   "127.0.0.1",
			Expires:  time.Now().Add(24 * time.Hour),
			HttpOnly: true,
			Secure:   false,
			SameSite: http.SameSiteLaxMode,
		}
		cookie2 := &http.Cookie{
			Name:  "theme",
			Value: "dark",
		}

		http.SetCookie(w, cookie1)
		http.SetCookie(w, cookie2)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient()
	stdClient := fast.NewStdClient(c)
	stdResp, err := stdClient.Get(ts.URL + "/api/test")
	require.NoError(t, err)

	defer stdResp.Body.Close()

	cookies := stdResp.Cookies()
	require.Len(t, cookies, 2)

	var authCookie, themeCookie *http.Cookie
	for _, ck := range cookies {
		switch ck.Name {
		case "auth_token":
			authCookie = ck
		case "theme":
			themeCookie = ck
		}
	}

	require.NotNil(t, authCookie)
	assert.Equal(t, "jwt-value-123", authCookie.Value)
	assert.Equal(t, "/api", authCookie.Path)
	assert.True(t, authCookie.HttpOnly)

	require.NotNil(t, themeCookie)
	assert.Equal(t, "dark", themeCookie.Value)
}

// TestFastClient_ContextCancellation tests aborting requests upon context cancellation without data races.
func TestFastClient_ContextCancellation(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithCancel(t.Context())

	c := fast.NewClient()

	go func() {
		<-started
		cancel()
	}()

	_, err := c.Request(ctx, http.MethodGet, ts.URL)
	require.Error(t, err)
	assert.True(t, errorsIsContextOrCanceled(err))
}

// TestFastClient_TimeoutOption tests automatic request cancellation upon configured client timeout.
func TestFastClient_TimeoutOption(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient(option.WithTimeout(50 * time.Millisecond))
	_, err := c.Request(t.Context(), http.MethodGet, ts.URL)
	require.Error(t, err)
}

// TestFastClient_TLSConnectionState tests TLS state availability on HTTPS requests.
func TestFastClient_TLSConnectionState(t *testing.T) {
	t.Parallel()

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient(
		option.WithInsecureSkipVerify(),
	)

	stdClient := fast.NewStdClient(c)
	stdResp, err := stdClient.Get(ts.URL)
	require.NoError(t, err)

	defer stdResp.Body.Close()

	assert.Equal(t, http.StatusOK, stdResp.StatusCode)
	assert.NotNil(t, stdResp.TLS)
	assert.True(t, stdResp.TLS.HandshakeComplete)
}

// TestFastClient_Expect100Continue tests Expect: 100-continue handling.
func TestFastClient_Expect100Continue(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Expect") == "100-continue" {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "continued")

			return
		}

		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient()
	resp, err := c.Request(t.Context(), http.MethodPost, ts.URL,
		mod.WithHeader("Expect", "100-continue"),
		mod.WithBodyBytes([]byte("payload")),
	)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
}

// TestFastClient_EmptyURLAndMalformedURL tests handling of invalid target URLs.
func TestFastClient_EmptyURLAndMalformedURL(t *testing.T) {
	t.Parallel()

	c := fast.NewClient()

	_, errEmpty := c.Request(t.Context(), http.MethodGet, "")
	require.ErrorIs(t, errEmpty, fast.ErrTargetURLEmpty)

	stdClient := fast.NewStdClient(c)
	reqNilURL, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	reqNilURL.URL = nil

	_, errNil := stdClient.Do(reqNilURL)
	require.Error(t, errNil)
}

// TestHeader_MultiValueAndCaseInsensitivity tests case-insensitive header access and multiple header values.
func TestHeader_MultiValueAndCaseInsensitivity(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("X-Custom-Header", "value1")
		w.Header().Add("X-Custom-Header", "value2")
		w.Header().Add("x-custom-header", "value3")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient()
	resp, err := c.Request(t.Context(), http.MethodGet, ts.URL)
	require.NoError(t, err)

	defer resp.Close()

	// Case-insensitive check
	assert.NotEmpty(t, resp.Header("x-custom-header"))
	assert.NotEmpty(t, resp.Header("X-CUSTOM-HEADER"))

	// All header map values
	headers := resp.Headers()
	vals := headers["X-Custom-Header"]
	assert.True(t, len(vals) >= 1)
}

// TestHeader_RequestModifiers verifies applying headers via functional request modifiers.
func TestHeader_RequestModifiers(t *testing.T) {
	t.Parallel()

	var receivedHeaders http.Header

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient()
	resp, err := c.Request(t.Context(), http.MethodGet, ts.URL,
		mod.WithHeader("X-App-Version", "2.5.0"),
		mod.WithHeader("Accept-Language", "en-US,en;q=0.9"),
		mod.WithBearer("my-access-token"),
	)
	require.NoError(t, err)

	resp.Close()

	assert.Equal(t, "2.5.0", receivedHeaders.Get("X-App-Version"))
	assert.Equal(t, "en-US,en;q=0.9", receivedHeaders.Get("Accept-Language"))
	assert.Equal(t, "Bearer my-access-token", receivedHeaders.Get("Authorization"))
}

func TestRequest_URLAndQueryParams(t *testing.T) {
	t.Parallel()

	fastReq := h1engine.AcquireRequest()
	t.Cleanup(func() { h1engine.ReleaseRequest(fastReq) })

	req := fast.NewRequest(fastReq)
	req.SetURL("http://example.com/api/v1/search?q=aoni&page=1")

	assert.Equal(t, http.MethodGet, req.Method())
	assert.Equal(t, "http://example.com/api/v1/search?q=aoni&page=1", req.URL())
	assert.Equal(t, "/api/v1/search", req.Path())
	assert.Equal(t, "q=aoni&page=1", req.RawQuery())

	req.AddQueryParam("sort", "desc")
	assert.Contains(t, req.RawQuery(), "sort=desc")

	req.SetQueryParam("page", "2")
	assert.Contains(t, req.RawQuery(), "page=2")
	assert.NotContains(t, req.RawQuery(), "page=1")
}

func TestRequest_HeaderMutations(t *testing.T) {
	t.Parallel()

	fastReq := h1engine.AcquireRequest()
	t.Cleanup(func() { h1engine.ReleaseRequest(fastReq) })

	req := fast.NewRequest(fastReq)
	req.SetHeader("X-Api-Key", "secret-key-123")
	req.SetHeaderBytes([]byte("X-Engine-Name"), []byte("fast-titanium"))

	assert.Equal(t, "secret-key-123", req.Header("X-Api-Key"))
	assert.Equal(t, "fast-titanium", string(req.HeaderBytes([]byte("X-Engine-Name"))))

	req.DelHeader("X-Api-Key")
	assert.Empty(t, req.Header("X-Api-Key"))

	req.ResetHeaders()
	assert.Empty(t, req.Header("X-Engine-Name"))
}

func TestRequest_BodyBytesAndStream(t *testing.T) {
	t.Parallel()

	fastReq := h1engine.AcquireRequest()
	t.Cleanup(func() { h1engine.ReleaseRequest(fastReq) })

	req := fast.NewRequest(fastReq)

	// Body bytes
	payload := []byte(`{"message":"hello world"}`)
	req.SetBodyBytes(payload)
	assert.Equal(t, payload, req.BodyBytes())

	// Body stream
	streamPayload := bytes.NewReader([]byte("stream-content"))
	req.SetBodyStream(streamPayload, int64(streamPayload.Len()))
	assert.NotNil(t, req.BodyStream())
}

func TestResponse_ContractAndHeaders(t *testing.T) {
	t.Parallel()

	fastResp := h1engine.AcquireResponse()
	t.Cleanup(func() { h1engine.ReleaseResponse(fastResp) })

	fastResp.SetStatusCode(http.StatusCreated)
	fastResp.Header.Set("Content-Type", "application/json")
	fastResp.Header.Set("X-Request-ID", "req-999")
	fastResp.SetBodyString(`{"id": 42}`)

	resp := fast.NewResponse(fastResp)
	assert.Equal(t, http.StatusCreated, resp.StatusCode())
	assert.Equal(t, "Created", resp.Status())
	assert.Equal(t, "application/json", resp.Header("Content-Type"))
	assert.Equal(t, "req-999", resp.Header("X-Request-ID"))
	assert.Equal(t, `{"id": 42}`, string(resp.BodyBytes()))

	// Close safety
	assert.NoError(t, resp.Close())
}

func TestResponse_RangeRequests(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "bytes=0-4" {
			w.Header().Set("Content-Range", "bytes 0-4/11")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, "hello")

			return
		}

		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient()
	resp, err := c.Request(t.Context(), http.MethodGet, ts.URL,
		mod.WithHeader("Range", "bytes=0-4"),
	)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusPartialContent, resp.StatusCode())
	assert.Equal(t, "bytes 0-4/11", resp.Header("Content-Range"))
	assert.Equal(t, "hello", string(resp.BodyBytes()))
}

func TestTransport_KeepAliveConnReuse(t *testing.T) {
	t.Parallel()

	var connCount int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "ok")
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient()
	c.Engine().Dial = func(addr string) (net.Conn, error) {
		atomic.AddInt32(&connCount, 1)
		return net.Dial("tcp", addr)
	}

	for range 5 {
		resp, err := c.Request(t.Context(), http.MethodGet, ts.URL)
		require.NoError(t, err)

		resp.Close()
	}

	// Connections should be reused, so count should be 1
	assert.Equal(t, int32(1), atomic.LoadInt32(&connCount))
}

func TestTransport_DisableKeepAlives(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient()
	c.Engine().MaxIdleConnDuration = -1

	for range 3 {
		resp, err := c.Request(t.Context(), http.MethodGet, ts.URL)
		require.NoError(t, err)

		resp.Close()
	}
}

// TestTransport_DecompressionFormats tests transparent decompression for Gzip, Brotli, and Zstd.
func TestTransport_DecompressionFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		encoding string
		compress func(io.Writer) io.WriteCloser
		want     string
	}{
		{
			name:     "gzip_decompression",
			encoding: "gzip",
			compress: func(w io.Writer) io.WriteCloser { return gzip.NewWriter(w) },
			want:     "compressed gzip payload",
		},
		{
			name:     "brotli_decompression",
			encoding: "br",
			compress: func(w io.Writer) io.WriteCloser { return &brotliTestWriter{w: w} },
			want:     "compressed brotli payload",
		},
		{
			name:     "zstd_decompression",
			encoding: "zstd",
			compress: func(w io.Writer) io.WriteCloser {
				return &zstdTestWriter{w: w}
			},
			want: "compressed zstd payload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			cw := tt.compress(&buf)
			_, _ = cw.Write([]byte(tt.want))
			_ = cw.Close()

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Encoding", tt.encoding)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(buf.Bytes())
			}))
			t.Cleanup(ts.Close)

			c := fast.NewClient()

			// Direct fast.Client
			resp, err := c.Request(t.Context(), http.MethodGet, ts.URL)
			require.NoError(t, err)

			defer resp.Close()

			assert.Equal(t, tt.want, string(resp.BodyBytes()))
			assert.True(t, resp.Uncompressed())

			// Standard client bridge
			stdClient := fast.NewStdClient(c)
			stdResp, err := stdClient.Get(ts.URL)
			require.NoError(t, err)

			defer stdResp.Body.Close()

			body, err := io.ReadAll(stdResp.Body)
			require.NoError(t, err)

			assert.Equal(t, tt.want, string(body))
			assert.True(t, stdResp.Uncompressed)
		})
	}
}

func TestTransport_CustomDialer(t *testing.T) {
	t.Parallel()

	var (
		dialedAddr   string
		dialerCalled bool
	)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient()
	c.Engine().Dial = func(addr string) (net.Conn, error) {
		dialerCalled = true
		dialedAddr = addr

		return net.Dial("tcp", addr)
	}

	resp, err := c.Request(t.Context(), http.MethodGet, ts.URL)
	require.NoError(t, err)

	resp.Close()

	assert.True(t, dialerCalled)
	assert.NotEmpty(t, dialedAddr)
}

func TestTransport_ProxyServer(t *testing.T) {
	t.Parallel()

	var proxyReceivedTarget string

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyReceivedTarget = r.RequestURI

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "proxied ok")
	}))
	t.Cleanup(proxyServer.Close)

	proxyURL, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)

	c := fast.NewClient(option.WithProxy(proxyURL))
	resp, err := c.Request(t.Context(), http.MethodGet, "http://example.com/target-path")
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, "proxied ok", string(resp.BodyBytes()))
	assert.NotEmpty(t, proxyReceivedTarget)
}

func TestTransport_ChunkedTransferEncoding(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)

		w.Header().Set("Transfer-Encoding", "chunked")
		w.WriteHeader(http.StatusOK)

		_, _ = io.WriteString(w, "chunk-1|")

		flusher.Flush()
		time.Sleep(20 * time.Millisecond)

		_, _ = io.WriteString(w, "chunk-2|")

		flusher.Flush()
		time.Sleep(20 * time.Millisecond)

		_, _ = io.WriteString(w, "chunk-3")

		flusher.Flush()
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient()
	resp, err := c.Request(t.Context(), http.MethodGet, ts.URL)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, "chunk-1|chunk-2|chunk-3", string(resp.BodyBytes()))
}

func TestTransport_ResponseHeaderTimeout(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient(option.WithTimeout(50 * time.Millisecond))
	_, err := c.Request(t.Context(), http.MethodGet, ts.URL)
	require.Error(t, err)
}

// TestFastClient_WithCloningAndIsolation tests option isolation on cloned fast clients.
func TestFastClient_WithCloningAndIsolation(t *testing.T) {
	t.Parallel()

	parent := fast.NewClient(
		option.WithHeader("X-Parent", "parent-val"),
		option.WithTimeout(10*time.Second),
	)

	child := parent.With(
		option.WithHeader("X-Child", "child-val"),
		option.WithTimeout(2*time.Second),
	)

	assert.Equal(t, "parent-val", parent.Config().Defaults.Headers.Get("X-Parent"))
	assert.Empty(t, parent.Config().Defaults.Headers.Get("X-Child"))
	assert.Equal(t, 10*time.Second, parent.Config().Engine.Timeout)

	assert.Equal(t, "parent-val", child.Config().Defaults.Headers.Get("X-Parent"))
	assert.Equal(t, "child-val", child.Config().Defaults.Headers.Get("X-Child"))
	assert.Equal(t, 2*time.Second, child.Config().Engine.Timeout)
}

// TestFastClient_PooledResponseLifecycle verifies creation and clean recycling of PooledResponse instances.
func TestFastClient_PooledResponseLifecycle(t *testing.T) {
	t.Parallel()

	fastReq := h1engine.AcquireRequest()
	fastResp := h1engine.AcquireResponse()

	fastResp.SetStatusCode(http.StatusOK)
	fastResp.SetBodyString("pooled-data")

	pooled := fast.NewPooledResponse(fastReq, fastResp)
	require.NotNil(t, pooled)

	assert.Equal(t, http.StatusOK, pooled.StatusCode())
	assert.Equal(t, "pooled-data", string(pooled.BodyBytes()))

	// Test Close safety & pool recycling
	assert.NoError(t, pooled.Close())
	// Repeated close must be a no-op
	assert.NoError(t, pooled.Close())
}

// TestFastClient_WSDialerContract tests WebSocket dialing methods on fast.Client.
func TestFastClient_WSDialerContract(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient()
	u, err := url.Parse(ts.URL)
	require.NoError(t, err)

	conn, err := c.DialPlainForWS(t.Context(), u.Host)
	require.NoError(t, err)

	t.Cleanup(func() { _ = conn.Close() })

	assert.NotNil(t, conn)
}

// TestFastClient_MiddlewareChain verifies wrapping fast.Client with middleware layers.
func TestFastClient_MiddlewareChainCompat(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := attempts.Add(1)
		if count < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient(option.WithBaseURL(ts.URL))

	retryMid := middleware.Retry(middleware.RetryOptions{
		MaxRetries: 3,
		Backoff:    1 * time.Millisecond,
	}, middleware.RetryOnGatewayErrors())

	doer := middleware.Chain(c, retryMid)

	req := fast.NewRequest(nil)
	t.Cleanup(req.Release)

	req.SetContext(t.Context())
	req.SetMethod(http.MethodGet)
	req.SetURL(ts.URL + "/")

	resp, err := doer.Do(req)
	require.NoError(t, err)

	t.Cleanup(func() { _ = resp.Close() })

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, int32(3), attempts.Load())
}

// TestFastClient_DoMethod tests the Do method executing prepared aoni.Request objects.
func TestFastClient_DoMethod(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "custom-agent-v1", r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("do-ok"))
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient()

	req := fast.NewRequest(nil)
	t.Cleanup(req.Release)

	req.SetContext(t.Context())
	req.SetMethod(http.MethodGet)
	req.SetURL(ts.URL)
	req.SetHeader("User-Agent", "custom-agent-v1")

	resp, err := c.Do(req)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, "do-ok", string(resp.BodyBytes()))
}

// TestFastClient_HTTPDoerAdapter tests executing requests through c.HTTP() adapter.
func TestFastClient_HTTPDoerAdapter(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Echo-Len", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(ts.Close)

	c := fast.NewClient()
	doer := c.HTTP()

	stdReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL, strings.NewReader("adapter-payload"))
	require.NoError(t, err)

	stdResp, err := doer.Do(stdReq)
	require.NoError(t, err)

	defer stdResp.Body.Close()

	body, err := io.ReadAll(stdResp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, stdResp.StatusCode)
	assert.Equal(t, "15", stdResp.Header.Get("X-Echo-Len"))
	assert.Equal(t, "adapter-payload", string(body))
}

type mockFastInspector struct {
	capturedReq  *http.Request
	capturedResp *http.Response
}

func (m *mockFastInspector) Capture(req *http.Request, resp *http.Response, _ error, _ *telemetry.TraceInfo) {
	m.capturedReq = req
	m.capturedResp = resp
}

func TestFast_Integration_Telemetry(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Server-Telemetry", "active")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "telemetry payload ok")
	}))
	defer ts.Close()

	trafficInspector := &mockFastInspector{}
	c := fast.NewClient(option.WithInspector(trafficInspector))

	resp, err := c.Request(context.Background(), "GET", ts.URL)
	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	require.NotNil(t, trafficInspector.capturedReq)
	require.NotNil(t, trafficInspector.capturedResp)
	assert.Equal(t, "GET", trafficInspector.capturedReq.Method)
	assert.Equal(t, http.StatusOK, trafficInspector.capturedResp.StatusCode)
}

func TestFast_Integration_AntiDPIAndHeaderOrdering(t *testing.T) {
	var receivedHeaders []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k := range r.Header {
			receivedHeaders = append(receivedHeaders, k)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := fast.NewClient()

	resp, err := c.Request(
		context.Background(),
		"GET",
		ts.URL,
		mod.WithHeader("X-Anti-DPI", "bypassed"),
		mod.WithUserAgent("Aoni-AntiDPI-Agent/1.0"),
	)
	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.NotEmpty(t, receivedHeaders)
}

func TestFast_Integration_TLSEvasionAndBrowserProfiles(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "evasion ok")
	}))
	defer ts.Close()

	c := fast.NewClient(
		option.WithTLSFingerprint(aoni.BrowserChrome),
		option.WithInsecureSkipVerify(),
		option.WithTimeout(5*time.Second),
	)

	resp, err := c.Request(
		context.Background(),
		"GET",
		ts.URL,
		mod.WithHeader("Accept-Language", "en-US,en;q=0.9"),
	)
	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, "evasion ok", string(resp.BodyBytes()))
}

func errorsIsContextOrCanceled(err error) bool {
	if err == nil {
		return false
	}

	return strings.Contains(err.Error(), "canceled") ||
		strings.Contains(err.Error(), "context deadline exceeded")
}
