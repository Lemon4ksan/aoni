// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

// TestClient_BasicGet tests GET request execution and body reading.
func TestClient_BasicGet(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
		_, _ = fmt.Fprint(w, "User-agent: go\nDisallow: /something/")
	}))
	defer ts.Close()

	// 1. Direct fast.Client
	c := fast.NewClient(option.WithBaseURL(ts.URL))
	resp, err := c.Request(context.Background(), "GET", "/")
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

// TestClient_HeadRequest tests HEAD requests and body emptiness.
func TestClient_HeadRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := fast.NewClient()
	resp, err := c.Request(context.Background(), "HEAD", ts.URL)
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

// TestClient_PostAndPostFormFormat tests POST payload & Content-Type formatting.
func TestClient_PostAndPostFormFormat(t *testing.T) {
	var reqMethod, reqContentType, reqBody string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqMethod = r.Method
		reqContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		reqBody = string(b)

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := fast.NewClient()

	// JSON POST
	jsonPayload := `{"key":"value"}`
	resp, err := c.Request(context.Background(), "POST", ts.URL,
		mod.WithHeader("Content-Type", "application/json"),
		mod.WithBodyBytes([]byte(jsonPayload)),
	)
	require.NoError(t, err)
	resp.Close()

	assert.Equal(t, "POST", reqMethod)
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

	assert.Equal(t, "POST", reqMethod)
	assert.Equal(t, "application/x-www-form-urlencoded", reqContentType)
	assert.True(t, reqBody == "foo=bar&foo=bar2&bar=baz" || reqBody == "bar=baz&foo=bar&foo=bar2")
}

// TestClient_Redirects tests redirect limits, referer headers, and method transformations.
func TestClient_Redirects(t *testing.T) {
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
	defer ts.Close()

	// 1. fast.Client default follow redirect
	c := fast.NewClient(option.WithRedirectLimit(10))
	resp, err := c.Request(context.Background(), "GET", ts.URL+"/?n=1")
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, "done: n=5", string(resp.BodyBytes()))

	// 2. Exceeding max redirects limit
	cLimit := fast.NewClient(option.WithRedirectLimit(3))
	_, errLimit := cLimit.Request(context.Background(), "GET", ts.URL+"/?n=1")
	require.ErrorIs(t, errLimit, fast.ErrMaxRedirectsExceeded)
}

// TestClient_RedirectMethods tests HTTP status codes 301/302/303/307/308 method preservation/conversion.
func TestClient_RedirectMethods(t *testing.T) {
	tests := []struct {
		statusCode int
		reqMethod  string
		wantMethod string
	}{
		{http.StatusMovedPermanently, "POST", "GET"},
		{http.StatusFound, "POST", "GET"},
		{http.StatusSeeOther, "POST", "GET"},
		{http.StatusTemporaryRedirect, "POST", "POST"},
		{http.StatusPermanentRedirect, "POST", "POST"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d_%s", tt.statusCode, tt.reqMethod), func(t *testing.T) {
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
			defer ts.Close()

			c := fast.NewClient(option.WithRedirectLimit(5))
			resp, err := c.Request(context.Background(), tt.reqMethod, ts.URL+"/",
				mod.WithBodyBytes([]byte("payload")),
			)
			require.NoError(t, err)
			resp.Close()

			assert.Equal(t, tt.wantMethod, receivedMethod)
		})
	}
}

// TestClient_CrossDomainHeaderScrubbing verifies sensitive headers are stripped on cross-origin redirects.
func TestClient_CrossDomainHeaderScrubbing(t *testing.T) {
	var targetHeaders http.Header

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHeaders = r.Header.Clone()

		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetServer.URL, http.StatusFound)
	}))
	defer originServer.Close()

	c := fast.NewClient(option.WithRedirectLimit(5))
	resp, err := c.Request(context.Background(), "GET", originServer.URL,
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

// TestClient_HTTPSDowngradeRefererStripping tests Referer stripping on HTTPS->HTTP downgrade.
func TestClient_HTTPSDowngradeRefererStripping(t *testing.T) {
	var receivedReferer string

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedReferer = r.Header.Get("Referer")

		w.WriteHeader(http.StatusOK)
	}))
	defer httpServer.Close()

	httpsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, httpServer.URL, http.StatusFound)
	}))
	defer httpsServer.Close()

	c := fast.NewClient(
		option.WithRedirectLimit(5),
		option.WithInsecureSkipVerify(),
	)

	resp, err := c.Request(context.Background(), "GET", httpsServer.URL,
		mod.WithHeader("Referer", httpsServer.URL+"/secret-path"),
	)
	require.NoError(t, err)
	resp.Close()

	// Referer must be stripped on HTTPS to HTTP downgrade
	assert.Empty(t, receivedReferer)
}

// TestClient_UserInfoBasicAuth tests username/password extraction from URL.
func TestClient_UserInfoBasicAuth(t *testing.T) {
	var authHeader string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	require.NoError(t, err)

	u.User = url.UserPassword("admin", "secret123")

	c := fast.NewClient()
	resp, err := c.Request(context.Background(), "GET", u.String())
	require.NoError(t, err)
	resp.Close()

	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:secret123"))
	assert.Equal(t, expectedAuth, authHeader)
}

// TestClient_CookieJarIntegration tests cookie parsing, storage, and transmission across requests.
func TestClient_CookieJarIntegration(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/set" {
			http.SetCookie(w, &http.Cookie{Name: "session_id", Value: "xyz123", Path: "/"})
			w.WriteHeader(http.StatusOK)
			return
		}

		cookieVal := r.Header.Get("Cookie")
		_, _ = fmt.Fprint(w, cookieVal)
	}))
	defer ts.Close()

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)

	c := fast.NewClient(option.WithCookieJar(jar))

	// Step 1: Set cookie
	resp1, err := c.Request(context.Background(), "GET", ts.URL+"/set")
	require.NoError(t, err)
	resp1.Close()

	// Step 2: Get cookie on second request
	resp2, err := c.Request(context.Background(), "GET", ts.URL+"/get")
	require.NoError(t, err)

	defer resp2.Close()

	assert.Equal(t, "session_id=xyz123", string(resp2.BodyBytes()))
}

// TestClient_ContextCancellation tests aborting requests upon context cancellation without data races.
func TestClient_ContextCancellation(t *testing.T) {
	started := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())

	c := fast.NewClient()

	go func() {
		<-started
		cancel()
	}()

	_, err := c.Request(ctx, "GET", ts.URL)
	require.Error(t, err)
	assert.True(t, errorsIsContextOrCanceled(err))
}

// TestClient_TimeoutOption tests automatic request cancellation upon configured client timeout.
func TestClient_TimeoutOption(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := fast.NewClient(option.WithTimeout(50 * time.Millisecond))
	_, err := c.Request(context.Background(), "GET", ts.URL)
	require.Error(t, err)
}

// TestClient_TLSConnectionState tests TLS state availability on HTTPS requests.
func TestClient_TLSConnectionState(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

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

// TestClient_Expect100Continue tests Expect: 100-continue handling.
func TestClient_Expect100Continue(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Expect") == "100-continue" {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "continued")
			return
		}

		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	c := fast.NewClient()
	resp, err := c.Request(context.Background(), "POST", ts.URL,
		mod.WithHeader("Expect", "100-continue"),
		mod.WithBodyBytes([]byte("payload")),
	)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
}

// TestClient_EmptyURLAndMalformedURL tests handling of invalid target URLs.
func TestClient_EmptyURLAndMalformedURL(t *testing.T) {
	c := fast.NewClient()

	_, errEmpty := c.Request(context.Background(), "GET", "")
	require.ErrorIs(t, errEmpty, fast.ErrTargetURLEmpty)

	stdClient := fast.NewStdClient(c)
	reqNilURL, err := http.NewRequest("GET", "http://example.com", nil)
	require.NoError(t, err)

	reqNilURL.URL = nil

	_, errNil := stdClient.Do(reqNilURL)
	require.Error(t, errNil)
}

func errorsIsContextOrCanceled(err error) bool {
	if err == nil {
		return false
	}

	return strings.Contains(err.Error(), "canceled") ||
		strings.Contains(err.Error(), "context deadline exceeded")
}
