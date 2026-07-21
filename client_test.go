// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	utls "github.com/refraction-networking/utls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/h3"
	"github.com/lemon4ksan/aoni/ja4"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/profiles"
	"github.com/lemon4ksan/aoni/stream"
	"github.com/lemon4ksan/aoni/telemetry"
)

type testPayload struct {
	Message string `json:"message"`
	Status  int    `json:"status"`
}

type errorStruct struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type apiResponse struct {
	Status   string `json:"status"`
	Data     any    `json:"data"`
	ErrorMsg string `json:"error,omitempty"`
}

func (a *apiResponse) IsSuccess() bool  { return a.Status == "success" }
func (a *apiResponse) Error() error     { return errors.New(a.ErrorMsg) }
func (a *apiResponse) SetData(data any) { a.Data = data }

func generateTestCert(t *testing.T) (cert, key []byte, err error) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Test Org"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER := x509.MarshalPKCS1PrivateKey(privateKey)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

func TestClient_Request_Basic(t *testing.T) {
	t.Parallel()
	_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message": "ok"}`))
	})

	result, err := aoni.GetTo[testPayload](t.Context(), client, "/")
	require.NoError(t, err)
	assert.Equal(t, "ok", result.Message)
}

func TestClient_ErrorStatus(t *testing.T) {
	t.Parallel()
	_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "not found"}`))
	})

	_, err := aoni.GetTo[any](t.Context(), client, "/404")
	require.Error(t, err)

	var apiErr *aoni.APIError
	require.ErrorAs(t, err, &apiErr)

	assert.Contains(t, string(apiErr.Body), "not found")
	assert.Contains(t, apiErr.Error(), "404")
}

func TestClient_ContextCancellation(t *testing.T) {
	t.Parallel()
	_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		}
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	r, err := client.Request(ctx, http.MethodGet, "/")
	if err == nil {
		t.Cleanup(func() { aoni.CloseResponse(r) })
		t.Fatal("expected error for canceled context, got nil")
	}
}

func TestClient_BaseResponse(t *testing.T) {
	t.Parallel()

	t.Run("success_response", func(t *testing.T) {
		_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status": "success", "data": {"message": "unwrapped"}}`))
		})

		client = client.With(option.WithBaseResponse(func() aoni.BaseResponse { return &apiResponse{} }))

		result, err := aoni.GetTo[testPayload](t.Context(), client, "/wrapped")
		require.NoError(t, err)
		assert.Equal(t, "unwrapped", result.Message)
	})

	t.Run("error_response", func(t *testing.T) {
		_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status": "fail", "error": "something went wrong"}`))
		})

		client = client.With(option.WithBaseResponse(func() aoni.BaseResponse { return &apiResponse{} }))

		_, err := aoni.GetTo[testPayload](t.Context(), client, "/error")
		assert.ErrorContains(t, err, "something went wrong")
	})
}

func TestClient_WithMultipart(t *testing.T) {
	t.Parallel()
	_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
		err := r.ParseMultipartForm(10 * 1024 * 1024)
		require.NoError(t, err)

		assert.Equal(t, "val1", r.FormValue("field1"))
		assert.Equal(t, "val2", r.FormValue("field2"))

		file, _, err := r.FormFile("file1")
		require.NoError(t, err)
		data, err := io.ReadAll(file)
		require.NoError(t, err)
		assert.Equal(t, "file content", string(data))

		w.WriteHeader(http.StatusOK)
	})

	fields := map[string]string{
		"field1": "val1",
		"field2": "val2",
	}
	files := map[string]io.Reader{
		"file1": strings.NewReader("file content"),
	}

	resp, err := client.Request(t.Context(), http.MethodPost, "/", mod.WithMultipart(fields, files))
	require.NoError(t, err)
	t.Cleanup(func() { aoni.CloseResponse(resp) })
}

func TestClient_ErrorModel(t *testing.T) {
	t.Parallel()
	_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error": "invalid_grant", "error_description": "expired token"}`))
	})

	var errModel errorStruct

	_, err := aoni.GetTo[any](t.Context(), client, "/oauth", mod.WithErrorModel(&errModel))
	require.Error(t, err)

	var apiErr *aoni.APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.NotNil(t, apiErr.Model)

	m, ok := apiErr.Model.(*errorStruct)
	require.True(t, ok)
	assert.Equal(t, "invalid_grant", m.Error)
	assert.Equal(t, "expired token", m.ErrorDescription)
}

func TestClient_ProgressCallbacks(t *testing.T) {
	t.Parallel()

	t.Run("download_progress", func(t *testing.T) {
		_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "10")
			_, _ = w.Write([]byte("1234567890"))
		})

		var (
			downloadCalled bool
			downloadBytes  int64
		)

		downloadProgress := func(current, total int64) {
			downloadCalled = true
			downloadBytes = current

			assert.Equal(t, int64(10), total)
		}

		resp, err := client.Request(
			t.Context(),
			http.MethodGet,
			"/download",
			mod.WithDownloadProgress(downloadProgress),
		)
		require.NoError(t, err)
		t.Cleanup(func() { aoni.CloseResponse(resp) })

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, "1234567890", string(body))
		assert.True(t, downloadCalled)
		assert.Equal(t, int64(10), downloadBytes)
	})
}

func TestClient_AutoTranscoding(t *testing.T) {
	t.Parallel()
	_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=windows-1251")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message": "` + "\xef\xf0\xe8\xe2\xe5\xf2" + `"}`))
	})

	result, err := aoni.GetTo[testPayload](t.Context(), client, "/transcode")
	require.NoError(t, err)
	assert.Equal(t, "привет", result.Message)
}

func TestClient_Hedging_Deterministic(t *testing.T) {
	t.Parallel()

	var requestCount atomic.Int32

	slowRequestStarted := make(chan struct{})
	blockSlowRequest := make(chan struct{})

	_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count == 1 {
			close(slowRequestStarted)
			<-blockSlowRequest
			w.WriteHeader(http.StatusRequestTimeout)

			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message": "hedged"}`))
	})

	client = client.With(option.WithHedging(10 * time.Millisecond))

	type result struct {
		res *testPayload
		err error
	}

	resChan := make(chan result, 1)

	go func() {
		res, err := aoni.GetTo[testPayload](t.Context(), client, "/")
		resChan <- result{res: res, err: err}
	}()

	select {
	case <-slowRequestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("slow request did not start in time")
	}

	select {
	case r := <-resChan:
		require.NoError(t, r.err)
		assert.Equal(t, "hedged", r.res.Message)
	case <-time.After(2 * time.Second):
		t.Fatal("hedged request did not complete in time")
	}

	close(blockSlowRequest)
}

func TestClient_Decompression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		encoding string
		compress func(io.Writer) io.WriteCloser
		want     string
	}{
		{
			name:     "decompress_gzip",
			encoding: "gzip",
			compress: func(w io.Writer) io.WriteCloser { return gzip.NewWriter(w) },
			want:     "decompress-gzip",
		},
		{
			name:     "decompress_brotli",
			encoding: "br",
			compress: func(w io.Writer) io.WriteCloser { return brotli.NewWriter(w) },
			want:     "decompress-brotli",
		},
		{
			name:     "decompress_zstandard",
			encoding: "zstd",
			compress: func(w io.Writer) io.WriteCloser {
				zw, _ := zstd.NewWriter(w)
				return zw
			},
			want: "decompress-zstd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			w := tt.compress(&buf)
			_, _ = w.Write([]byte(`{"message": "` + tt.want + `"}`))
			_ = w.Close()

			_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Content-Encoding", tt.encoding)
				_, _ = w.Write(buf.Bytes())
			})

			result, err := aoni.GetTo[testPayload](t.Context(), client, "/")
			require.NoError(t, err)
			assert.Equal(t, tt.want, result.Message)
		})
	}
}

func TestClient_ContentTypeGuard(t *testing.T) {
	t.Parallel()

	t.Run("html_instead_of_json_returns_error", func(t *testing.T) {
		_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>Hello World</body></html>"))
		})

		_, err := aoni.GetTo[testPayload](t.Context(), client, "/")
		require.Error(t, err)
		assert.ErrorIs(t, err, aoni.ErrUnexpectedContentType)
		assert.Contains(t, err.Error(), "expected structured data but got HTML")
	})

	t.Run("cloudflare_challenge_html_returns_error", func(t *testing.T) {
		_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>cf-challenge and ray id cloudflare</body></html>"))
		})

		_, err := aoni.GetTo[testPayload](t.Context(), client, "/")
		require.Error(t, err)
		assert.ErrorIs(t, err, aoni.ErrCloudflareChallenge)
	})

	t.Run("html_With_raw_decoder_succeeds", func(t *testing.T) {
		_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>Hello World</body></html>"))
		})

		var output []byte

		resp, err := client.Request(t.Context(), http.MethodGet, "/", mod.WithRawDecoder())
		require.NoError(t, err)
		t.Cleanup(func() { aoni.CloseResponse(resp) })

		err = aoni.RawDecoder.Decode(resp.Body, &output)
		require.NoError(t, err)
		assert.Equal(t, "<html><body>Hello World</body></html>", string(output))
	})
}

func TestClient_TraceJA4_WithTLSFingerprint(t *testing.T) {
	t.Parallel()

	cert, key, err := generateTestCert(t)
	require.NoError(t, err)

	tlsCert, err := tls.X509KeyPair(cert, key)
	require.NoError(t, err)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = tlsConfig

	server.StartTLS()
	t.Cleanup(server.Close)

	var callbackReport *ja4.Report

	client := aoni.NewClient(server.Client()).With(
		option.WithTLSFingerprint(aoni.BrowserChrome),
		option.WithJA4Callback(func(r ja4.Report) {
			callbackReport = &r
		}),
	)

	info := &aoni.TraceInfo{}
	_, err = client.Request(
		t.Context(),
		http.MethodGet,
		server.URL,
		aoni.TraceJA4(info),
	)
	require.NoError(t, err)

	require.NotNil(t, info.JA4)
	assert.NotEmpty(t, info.JA4.JA4H)
	assert.NotEmpty(t, info.JA4.JA4)
	assert.Regexp(t, `^t[0-9]{2}[di][0-9]{2}[0-9]{2}[a-z0-9]{2}_[a-f0-9]{12}_[a-f0-9]{12}$`, info.JA4.JA4)

	require.NotNil(t, callbackReport)
	assert.Equal(t, info.JA4.JA4, callbackReport.JA4)
}

func TestClient_ResponseSizeGuard(t *testing.T) {
	t.Parallel()

	t.Run("fails_early_on_content_length", func(t *testing.T) {
		_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "20")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("01234567890123456789"))
		})

		client = client.With(option.WithMaxResponseSize(10))
		_, err := client.Request(t.Context(), http.MethodGet, "/")
		require.Error(t, err)
		assert.ErrorIs(t, err, aoni.ErrResponseTooLarge)
	})

	t.Run("fails_during_read_when_limit_exceeded", func(t *testing.T) {
		_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Transfer-Encoding", "chunked")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("01234567890123456789"))
		})

		client = client.With(option.WithMaxResponseSize(10))
		resp, err := client.Request(t.Context(), http.MethodGet, "/")
		require.NoError(t, err)
		t.Cleanup(func() { aoni.CloseResponse(resp) })

		_, err = io.ReadAll(resp.Body)
		require.Error(t, err)
		assert.ErrorIs(t, err, aoni.ErrResponseTooLarge)
	})
}

func TestClient_SensitiveHeaderScrubbing(t *testing.T) {
	t.Parallel()

	t.Run("cross_origin_redirect_scrubs_headers", func(t *testing.T) {
		var redirectedHeaders http.Header

		targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			redirectedHeaders = r.Header

			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(targetServer.Close)

		origServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, targetServer.URL, http.StatusFound)
		}))
		t.Cleanup(origServer.Close)

		client := aoni.NewClient(nil, option.WithRedirectLimit(3))

		reqMod := func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer token123")
			req.Header.Set("Cookie", "session=cookie123")
			req.Header.Set("X-Session-ID", "sess123")
			req.Header.Set("X-Access-Token", "tok123")
			req.Header.Set("X-Safe-Header", "keep-me")
		}

		resp, err := client.Request(t.Context(), http.MethodGet, origServer.URL, reqMod)
		require.NoError(t, err)
		t.Cleanup(func() { aoni.CloseResponse(resp) })

		assert.Empty(t, redirectedHeaders.Get("Authorization"))
		assert.Empty(t, redirectedHeaders.Get("Cookie"))
		assert.Empty(t, redirectedHeaders.Get("X-Session-ID"))
		assert.Empty(t, redirectedHeaders.Get("X-Access-Token"))
		assert.Equal(t, "keep-me", redirectedHeaders.Get("X-Safe-Header"))
	})
}

func TestClient_SSRFGuard(t *testing.T) {
	t.Parallel()

	client := aoni.NewClient(nil, option.WithSSRFGuard())

	t.Run("blocks_loopback_ipv4", func(t *testing.T) {
		_, err := client.Request(t.Context(), http.MethodGet, "http://127.0.0.1:8080/")
		require.Error(t, err)
		assert.ErrorIs(t, err, aoni.ErrSSRFBlocked)
	})

	t.Run("blocks_private_network_ipv4", func(t *testing.T) {
		_, err := client.Request(t.Context(), http.MethodGet, "http://192.168.1.1:8080/")
		require.Error(t, err)
		assert.ErrorIs(t, err, aoni.ErrSSRFBlocked)
	})
}

func TestClient_HappyEyeballs(t *testing.T) {
	t.Parallel()
	_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	client = client.With(option.WithHappyEyeballs(10 * time.Millisecond))
	resp, err := client.Request(t.Context(), http.MethodGet, "/")
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
}

func TestClient_MultiReadBody(t *testing.T) {
	t.Parallel()

	t.Run("in_memory_caching_under_threshold", func(t *testing.T) {
		_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("short body"))
		})

		client = client.With(option.WithMultiReadBody(100))
		resp, err := client.Request(t.Context(), http.MethodGet, "/")
		require.NoError(t, err)
		t.Cleanup(func() { aoni.CloseResponse(resp) })

		body1, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "short body", string(body1))

		_ = resp.Body.Close()

		body2, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "short body", string(body2))
	})

	t.Run("disable_disk_fallback_above_threshold", func(t *testing.T) {
		_, client := aoni.SetupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("long body exceeding threshold"))
		})

		client = client.With(
			option.WithMultiReadBody(10),
			option.WithMultiReadDisableDisk(true),
		)
		_, err := client.Request(t.Context(), http.MethodGet, "/")
		assert.ErrorIs(t, err, aoni.ErrBufferLimitExceeded)
	})
}

func TestClient_SourceIPRotator(t *testing.T) {
	t.Parallel()

	ips := []string{"192.168.1.1", "192.168.1.2"}
	rotator, err := aoni.NewSourceIPRotator(ips)
	require.NoError(t, err)
	require.Equal(t, 2, rotator.Size())

	ip1 := rotator.Next()
	ip2 := rotator.Next()
	ip3 := rotator.Next()

	assert.Equal(t, "192.168.1.1", ip1.String())
	assert.Equal(t, "192.168.1.2", ip2.String())
	assert.Equal(t, "192.168.1.1", ip3.String())

	v4IP := rotator.NextForFamily(true)
	assert.NotNil(t, v4IP)
	assert.True(t, v4IP.To4() != nil)

	err = rotator.UpdatePool([]string{"10.0.0.1"})
	require.NoError(t, err)
	assert.Equal(t, 1, rotator.Size())
	assert.Equal(t, "10.0.0.1", rotator.Next().String())
}

func TestClient_ProxyIsolatedCookieJar(t *testing.T) {
	t.Parallel()

	jar := cookie.NewProxyIsolatedJar()
	u, err := url.Parse("https://example.com")
	require.NoError(t, err)

	c := &http.Cookie{Name: "session", Value: "val1"}

	ctx1 := cookie.WithProxyAddress(t.Context(), "http://proxy1:8080")
	subJar1 := jar.GetJar(ctx1)
	subJar1.SetCookies(u, []*http.Cookie{c})

	ctx2 := cookie.WithProxyAddress(t.Context(), "http://proxy2:8080")
	subJar2 := jar.GetJar(ctx2)
	cookies2 := subJar2.Cookies(u)
	assert.Empty(t, cookies2)

	cookies1 := subJar1.Cookies(u)
	require.Len(t, cookies1, 1)
	assert.Equal(t, "session", cookies1[0].Name)
}

func TestClient_HostRewrite(t *testing.T) {
	t.Parallel()

	rules := map[string]string{
		"myapi.local": "127.0.0.1:8080",
	}

	modifier := mod.WithHostRewrite(rules)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://myapi.local/profile", nil)
	require.NoError(t, err)

	modifier(req)

	extracted := aoni.HostRewriteRules(req.Context())
	assert.Equal(t, "127.0.0.1:8080", extracted["myapi.local"])

	appendMod := mod.AppendHostRewrite(map[string]string{"another.local": "10.0.0.2"})
	appendMod(req)

	finalRules := aoni.HostRewriteRules(req.Context())
	assert.Equal(t, "127.0.0.1:8080", finalRules["myapi.local"])
	assert.Equal(t, "10.0.0.2", finalRules["another.local"])
}

func TestClient_PacketPadding(t *testing.T) {
	t.Parallel()

	cfg := aoni.PaddingConfig{
		MinPaddingBytes: 10,
		MaxPaddingBytes: 20,
		PaddingHeader:   "X-Custom-Padding",
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
	require.NoError(t, err)

	mod.WithPadding(cfg)(req)

	headerVal := req.Header.Get("X-Custom-Padding")
	require.NotEmpty(t, headerVal)

	bytesVal, err := hex.DecodeString(headerVal)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(bytesVal), 10)
	assert.LessOrEqual(t, len(bytesVal), 20)
}

func TestClient_RetryMiddleware(t *testing.T) {
	t.Parallel()

	var attempts uint32

	opts := middleware.RetryOptions{
		MaxRetries:     3,
		Backoff:        1 * time.Millisecond,
		JitterStrategy: middleware.JitterFull,
		OnRetry: func(attempt uint32, err error, delay time.Duration) {
			atomic.StoreUint32(&attempts, attempt)
		},
	}

	handlerCount := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&handlerCount, 1)
		if count < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message": "recovered"}`))
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(nil, option.WithBaseURL(server.URL))

	retryMid := middleware.Retry(opts, aoni.RetryOnGatewayErrors())
	doer := retryMid(client.HTTP())

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := doer.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { aoni.CloseResponse(resp) })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, uint32(2), atomic.LoadUint32(&attempts))
}

func TestClient_StreamParsing(t *testing.T) {
	t.Parallel()

	t.Run("ndjson", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(
				[]byte("{\"status\": 200, \"message\": \"msg1\"}\n{\"status\": 200, \"message\": \"msg2\"}\n"),
			)
		}))
		t.Cleanup(srv.Close)

		streamResp, err := stream.Get(t.Context(), aoni.NewClient(nil), srv.URL)
		require.NoError(t, err)

		ch, errs := stream.GetNDJSON[testPayload](t.Context(), streamResp)

		var msgs []string
		for val := range ch {
			msgs = append(msgs, val.Message)
		}

		err = <-errs
		require.NoError(t, err)
		assert.Equal(t, []string{"msg1", "msg2"}, msgs)
	})

	t.Run("sse", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(
				[]byte(
					"event: custom\ndata: {\"message\": \"event1\"}\n\nevent: custom\ndata: {\"message\": \"event2\"}\n\n",
				),
			)
		}))
		t.Cleanup(srv.Close)

		streamResp, err := stream.Get(t.Context(), aoni.NewClient(nil), srv.URL)
		require.NoError(t, err)

		ch, errs := stream.ParseSSE[testPayload](t.Context(), streamResp)

		var msgs []string
		for val := range ch {
			msgs = append(msgs, val.Message)
		}

		err = <-errs
		require.NoError(t, err)
		assert.Equal(t, []string{"event1", "event2"}, msgs)
	})
}

func TestClient_RefererAutomaton(t *testing.T) {
	var lastReferer string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastReferer = r.Header.Get("Referer")

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithRefererAutomaton(true))

	// First request: no referer should be sent
	resp1, err := client.Request(context.Background(), http.MethodGet, server.URL+"/page1")
	require.NoError(t, err)

	_ = resp1.Body.Close()

	assert.Empty(t, lastReferer)

	// Second request: referer should be the URL of the first request
	resp2, err := client.Request(context.Background(), http.MethodGet, server.URL+"/page2")
	require.NoError(t, err)

	_ = resp2.Body.Close()

	assert.Equal(t, server.URL+"/page1", lastReferer)
}

func TestClient_HTTP3Settings(t *testing.T) {
	client := aoni.NewClient(nil, option.WithHTTP3Settings(h3.ChromeSettings))
	assert.NotNil(t, client)
}

func TestUserAgentAndHintsRotation(t *testing.T) {
	t.Parallel()

	profiles := []aoni.BrowserProfile{
		{
			UserAgent: "BrowserA",
			ClientHints: map[string]string{
				"Sec-CH-UA": "BrandA",
			},
		},
		{
			UserAgent: "BrowserB",
			ClientHints: map[string]string{
				"Sec-CH-UA": "BrandB",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-UA", r.Header.Get("User-Agent"))
		w.Header().Set("X-Hint", r.Header.Get("Sec-CH-UA"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithPipeline(aoni.PipelineConfig{RotateUA: true}),
		option.WithUARotationProfiles(profiles),
	)

	resp1, err := client.Get(t.Context(), "/")
	require.NoError(t, err)

	defer resp1.Body.Close()

	assert.Equal(t, "BrowserA", resp1.Header.Get("X-UA"))
	assert.Equal(t, "BrandA", resp1.Header.Get("X-Hint"))

	resp2, err := client.Get(t.Context(), "/")
	require.NoError(t, err)

	defer resp2.Body.Close()

	assert.Equal(t, "BrowserB", resp2.Header.Get("X-UA"))
	assert.Equal(t, "BrandB", resp2.Header.Get("X-Hint"))
}

func TestDPIJitter(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithPipeline(aoni.PipelineConfig{
			DPIJitter: &aoni.DPIJitterConfig{MinDelay: 10 * time.Millisecond, MaxDelay: 20 * time.Millisecond},
		}),
	)

	start := time.Now()
	resp, err := client.Get(t.Context(), "/")
	require.NoError(t, err)

	defer resp.Body.Close()

	duration := time.Since(start)
	assert.GreaterOrEqual(t, duration, 10*time.Millisecond)
}

func TestProxyFailover(t *testing.T) {
	t.Parallel()

	var attempts int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	proxies := []string{"http://127.0.0.1:9999", server.URL}
	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithPipeline(aoni.PipelineConfig{
			ProxyFailover: &aoni.ProxyFailoverConfig{Proxies: proxies, RetryLimit: 2},
		}),
	)

	resp, err := client.Get(t.Context(), "/")
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(body))
	assert.Equal(t, 1, attempts)
}

func TestCache(t *testing.T) {
	t.Parallel()

	var hits int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++

		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("cached content"))
	}))
	defer server.Close()

	store := aoni.NewInMemoryCacheStore()
	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithPipeline(aoni.PipelineConfig{
			Cache: &aoni.CacheConfig{Store: store, DefaultTTL: 1 * time.Minute},
		}),
	)

	resp1, err := client.Get(t.Context(), "/")
	require.NoError(t, err)

	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	assert.Equal(t, "cached content", string(body1))

	resp2, err := client.Get(t.Context(), "/")
	require.NoError(t, err)

	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	assert.Equal(t, "cached content", string(body2))

	assert.Equal(t, 1, hits)
}

type mockInspector struct {
	capturedReq  *http.Request
	capturedResp *http.Response
}

func (m *mockInspector) Capture(req *http.Request, resp *http.Response, err error, traceInfo *aoni.TraceInfo) {
	m.capturedReq = req
	m.capturedResp = resp
}

func TestSensitiveDataRedactor(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "secret-session")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	inspector := &mockInspector{}
	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithPipeline(aoni.PipelineConfig{
			Redact:  &aoni.RedactConfig{HeadersToRedact: []string{"Authorization", "Set-Cookie"}},
			Inspect: true,
		}),
		option.WithInspector(inspector),
	)

	resp, err := client.Request(t.Context(), "GET", "/", mod.WithHeader("Authorization", "Bearer secretToken"))
	require.NoError(t, err)

	defer resp.Body.Close()

	cfg, ok := inspector.capturedReq.Context().Value(aoni.RedactConfigCtxKey{}).(*aoni.RedactConfig)
	require.True(t, ok)
	require.NotNil(t, cfg)

	_, ok1 := cfg.Headers["authorization"]
	_, ok2 := cfg.Headers["set-cookie"]

	assert.True(t, ok1)
	assert.True(t, ok2)
}

func TestHARGenerator(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("har body"))
	}))
	defer server.Close()

	harGen := telemetry.NewHARGenerator()
	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithPipeline(aoni.PipelineConfig{
			HAR: &aoni.HARConfig{Tracker: harGen},
		}),
	)

	resp, err := client.Get(t.Context(), "/")
	require.NoError(t, err)

	defer resp.Body.Close()

	data, err := harGen.Export()
	require.NoError(t, err)

	harString := string(data)
	assert.Contains(t, harString, "har body")
	assert.Contains(t, harString, "GET")
	assert.Contains(t, harString, server.URL)
}

func TestClient_QueryEncoder(t *testing.T) {
	t.Parallel()

	var capturedQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	customEncoder := func(s any) (url.Values, error) {
		vals := make(url.Values)
		vals.Set("custom_key", "custom_val")
		return vals, nil
	}

	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithQueryEncoder(customEncoder),
	)

	_, err := client.Get(t.Context(), "/", mod.WithQuery(struct{ Dummy string }{Dummy: "value"}))
	require.NoError(t, err)
	assert.Equal(t, "custom_key=custom_val", capturedQuery)
}

func TestWithClientBrowserProfile(t *testing.T) {
	t.Parallel()

	clientChrome := aoni.NewClient(nil,
		option.WithBrowserProfile(aoni.BrowserChrome, profiles.Windows),
	)

	headersChrome := clientChrome.Defaults().Headers
	assert.Contains(t, headersChrome.Get("User-Agent"), "Chrome")
	assert.Contains(t, headersChrome.Get("Sec-Ch-Ua"), "Google Chrome")
	assert.Equal(t, "?0", headersChrome.Get("Sec-Ch-Ua-Mobile"))
	assert.Equal(t, `"Windows"`, headersChrome.Get("Sec-Ch-Ua-Platform"))

	require.NotNil(t, clientChrome.Fingerprint().H2Settings)
	assert.Equal(t, uint32(65536), clientChrome.Fingerprint().H2Settings.HeaderTableSize)
	assert.Equal(t, uint32(6291456), clientChrome.Fingerprint().H2Settings.InitialWindowSize)

	require.NotNil(t, clientChrome.Fingerprint().H3Settings)
	assert.True(t, clientChrome.Fingerprint().H3Settings.EnableDatagrams)

	require.NotNil(t, clientChrome.Fingerprint().TLSClientHelloSpecProvider)
	spec, err := clientChrome.Fingerprint().TLSClientHelloSpecProvider.ClientHelloSpec()
	require.NoError(t, err)
	require.NotNil(t, spec)
	assert.Contains(t, spec.CipherSuites, uint16(utls.TLS_AES_128_GCM_SHA256))

	clientFirefox := aoni.NewClient(nil,
		option.WithBrowserProfile(aoni.BrowserFirefox, profiles.Windows),
	)

	headersFirefox := clientFirefox.Defaults().Headers
	assert.Contains(t, headersFirefox.Get("User-Agent"), "Firefox")

	require.NotNil(t, clientFirefox.Fingerprint().H2Settings)
	assert.Equal(t, uint32(131072), clientFirefox.Fingerprint().H2Settings.InitialWindowSize)

	require.NotNil(t, clientFirefox.Fingerprint().H3Settings)
	assert.False(t, clientFirefox.Fingerprint().H3Settings.EnableDatagrams)

	require.NotNil(t, clientFirefox.Fingerprint().TLSClientHelloID)
	assert.Equal(t, utls.HelloFirefox_120, *clientFirefox.Fingerprint().TLSClientHelloID)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithBrowserProfile(aoni.BrowserChrome, profiles.Windows),
	)

	reqGet, err := http.NewRequestWithContext(t.Context(), "GET", server.URL, nil)
	require.NoError(t, err)

	reqGet = client.InitRequestConfig(reqGet)
	for _, defaultMod := range client.Defaults().DefaultMods {
		defaultMod(reqGet)
	}

	assert.Contains(t, reqGet.Header.Get("Accept"), "text/html")
	cfgGet := aoni.GetRequestConfig(reqGet.Context())
	require.NotNil(t, cfgGet)
	assert.NotEmpty(t, cfgGet.OrderedHeaders)
	assert.Equal(t, ":method", cfgGet.OrderedHeaders[0])

	reqPost, err := http.NewRequestWithContext(t.Context(), "POST", server.URL, nil)
	require.NoError(t, err)

	reqPost = client.InitRequestConfig(reqPost)
	for _, defaultMod := range client.Defaults().DefaultMods {
		defaultMod(reqPost)
	}

	assert.Equal(t, "*/*", reqPost.Header.Get("Accept"))
	cfgPost := aoni.GetRequestConfig(reqPost.Context())
	require.NotNil(t, cfgPost)
	assert.NotEmpty(t, cfgPost.OrderedHeaders)
	assert.Equal(t, "content-length", cfgPost.OrderedHeaders[4])

	reqMultipart, err := http.NewRequestWithContext(t.Context(), "POST", server.URL, nil)
	require.NoError(t, err)

	reqMultipart = client.InitRequestConfig(reqMultipart)
	for _, defaultMod := range client.Defaults().DefaultMods {
		defaultMod(reqMultipart)
	}

	modMultipart := mod.WithMultipart(map[string]string{"foo": "bar"}, nil)
	modMultipart(reqMultipart)

	contentType := reqMultipart.Header.Get("Content-Type")
	assert.Contains(t, contentType, "multipart/form-data")
	assert.Contains(t, contentType, "----WebKitFormBoundary")
}

func TestWithClientProfileVariant_Custom(t *testing.T) {
	t.Parallel()

	customCache := profiles.NewHeaderCache(
		map[string][]string{"GET": {"custom-header", "user-agent"}},
		map[string][]string{"GET": {"user-agent", "custom-header"}},
	)

	customVariant := &profiles.Variant{
		HelloID: utls.ClientHelloID{
			Client:  "Firefox",
			Version: "123",
		},
		BoundaryFunc: func() string {
			return "----CustomBoundary123"
		},
		ConfigureH2: func(s *profiles.H2Settings) {
			s.InitialWindowSize = 99999
		},
		BuildHeaders: func(os profiles.OSKey) []profiles.HeaderEntry {
			return []profiles.HeaderEntry{
				{Name: "User-Agent", Value: "CustomUA/1.0"},
				{Name: "custom-header", Value: "custom-val"},
			}
		},
		HeaderCache: customCache,
	}

	client := aoni.NewClient(nil,
		option.WithProfileVariant(customVariant, profiles.Windows),
	)

	// Verify custom headers
	headers := client.Defaults().Headers
	assert.Equal(t, "CustomUA/1.0", headers.Get("User-Agent"))
	assert.Equal(t, "custom-val", headers.Get("custom-header"))

	// Verify custom H2 Settings
	require.NotNil(t, client.Fingerprint().H2Settings)
	assert.Equal(t, uint32(99999), client.Fingerprint().H2Settings.InitialWindowSize)

	// Verify custom HelloID
	require.NotNil(t, client.Fingerprint().TLSClientHelloID)
	assert.Equal(t, "Firefox", client.Fingerprint().TLSClientHelloID.Client)
	assert.Equal(t, "123", client.Fingerprint().TLSClientHelloID.Version)

	// Verify custom OrderedHeaders
	req, err := http.NewRequestWithContext(t.Context(), "GET", "http://example.com", nil)
	require.NoError(t, err)

	req = client.InitRequestConfig(req)
	for _, defaultMod := range client.Defaults().DefaultMods {
		defaultMod(req)
	}

	cfg := aoni.GetRequestConfig(req.Context())
	require.NotNil(t, cfg)
	assert.Equal(t, []string{"custom-header", "user-agent"}, cfg.OrderedHeaders)

	// Verify custom boundary
	reqMultipart, err := http.NewRequestWithContext(t.Context(), "POST", "http://example.com", nil)
	require.NoError(t, err)

	reqMultipart = client.InitRequestConfig(reqMultipart)
	for _, defaultMod := range client.Defaults().DefaultMods {
		defaultMod(reqMultipart)
	}

	modMultipart := mod.WithMultipart(map[string]string{"foo": "bar"}, nil)
	modMultipart(reqMultipart)

	contentType := reqMultipart.Header.Get("Content-Type")
	assert.Contains(t, contentType, "multipart/form-data")
	assert.Contains(t, contentType, "----CustomBoundary123")
}

func TestWithClientTCPDelay(t *testing.T) {
	t.Parallel()

	client := aoni.NewClient(nil,
		option.WithTCPDelay(10*time.Millisecond, 20*time.Millisecond),
	)

	// Test GET request uses client-level default TCP delay
	reqGet, err := http.NewRequestWithContext(t.Context(), "GET", "http://example.com", nil)
	require.NoError(t, err)

	reqGet = client.InitRequestConfig(reqGet)
	for _, defaultMod := range client.Defaults().DefaultMods {
		defaultMod(reqGet)
	}

	cfgGet := aoni.GetRequestConfig(reqGet.Context())
	require.NotNil(t, cfgGet)
	assert.Equal(t, 10*time.Millisecond, cfgGet.TCPDelay.Min)
	assert.Equal(t, 20*time.Millisecond, cfgGet.TCPDelay.Max)

	// Test GET request With per-request override
	reqOverride, err := http.NewRequestWithContext(t.Context(), "GET", "http://example.com", nil)
	require.NoError(t, err)

	reqOverride = client.InitRequestConfig(reqOverride)
	for _, defaultMod := range client.Defaults().DefaultMods {
		defaultMod(reqOverride)
	}

	// Apply per-request override
	mod.WithTCPDelay(100*time.Millisecond, 200*time.Millisecond)(reqOverride)

	cfgOverride := aoni.GetRequestConfig(reqOverride.Context())
	require.NotNil(t, cfgOverride)
	assert.Equal(t, 100*time.Millisecond, cfgOverride.TCPDelay.Min)
	assert.Equal(t, 200*time.Millisecond, cfgOverride.TCPDelay.Max)
}

func TestClient_WithClientInsecureSkipVerify(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(nil, option.WithInsecureSkipVerify())
	resp, err := client.Request(t.Context(), http.MethodGet, server.URL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestClient_WithClientResponseValidator(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	errClientVal := errors.New("client validator error")
	errReqVal := errors.New("request validator error")

	t.Run("Client-level only - failure", func(t *testing.T) {
		client := aoni.NewClient(nil, option.WithResponseValidator(func(resp *http.Response) error {
			return errClientVal
		}))
		_, err := client.Request(t.Context(), http.MethodGet, server.URL)
		require.Error(t, err)
		assert.ErrorIs(t, err, errClientVal)
	})

	t.Run("Client-level and request-level both fail - request-level wins", func(t *testing.T) {
		client := aoni.NewClient(nil, option.WithResponseValidator(func(resp *http.Response) error {
			return errClientVal
		}))
		_, err := client.Request(
			t.Context(),
			http.MethodGet,
			server.URL,
			mod.WithResponseValidator(func(resp *http.Response) error {
				return errReqVal
			}),
		)
		require.Error(t, err)
		assert.ErrorIs(t, err, errReqVal)
	})

	t.Run("Client-level fails, request-level succeeds - request-level override passes", func(t *testing.T) {
		client := aoni.NewClient(nil, option.WithResponseValidator(func(resp *http.Response) error {
			return errClientVal
		}))
		resp, err := client.Request(
			t.Context(),
			http.MethodGet,
			server.URL,
			mod.WithResponseValidator(func(resp *http.Response) error {
				return nil
			}),
		)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestCertificatePinning(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	// Extract port from the test server URL
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(u.Host)
	require.NoError(t, err)

	// Extract the leaf certificate of the test server
	require.NotEmpty(t, server.TLS.Certificates)
	leafCert, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
	require.NoError(t, err)

	// Compute SPKI fingerprints
	spkiHash := sha256.Sum256(leafCert.RawSubjectPublicKeyInfo)
	correctPinBase64 := base64.StdEncoding.EncodeToString(spkiHash[:])
	correctPinHex := hex.EncodeToString(spkiHash[:])
	correctPinPrefixed := "sha256/" + correctPinBase64

	incorrectPin := "sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

	t.Run("Standard Client - Correct Pin Base64", func(t *testing.T) {
		client := aoni.NewClient(server.Client())
		resp, err := client.Request(t.Context(), http.MethodGet, server.URL,
			mod.WithCertificatePin("127.0.0.1", correctPinBase64),
		)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Standard Client - Correct Pin Hex", func(t *testing.T) {
		client := aoni.NewClient(server.Client())
		resp, err := client.Request(t.Context(), http.MethodGet, server.URL,
			mod.WithCertificatePin("127.0.0.1", correctPinHex),
		)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Standard Client - Correct Pin Prefixed", func(t *testing.T) {
		client := aoni.NewClient(server.Client())
		resp, err := client.Request(t.Context(), http.MethodGet, server.URL,
			mod.WithCertificatePin("127.0.0.1", correctPinPrefixed),
		)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Standard Client - Incorrect Pin", func(t *testing.T) {
		client := aoni.NewClient(server.Client())
		_, err := client.Request(t.Context(), http.MethodGet, server.URL,
			mod.WithCertificatePin("127.0.0.1", incorrectPin),
		)
		require.Error(t, err)
		assert.True(
			t,
			errors.Is(err, aoni.ErrCertificatePinning) ||
				strings.Contains(err.Error(), "certificate pinning validation failed"),
		)
	})

	t.Run("UTLS Client - Correct Pin", func(t *testing.T) {
		client := aoni.NewClient(nil, option.WithTLSFingerprint(aoni.BrowserChrome))
		resp, err := client.Request(t.Context(), http.MethodGet, server.URL,
			mod.WithCertificatePin("127.0.0.1", correctPinBase64),
			mod.WithInsecureSkipVerify(),
		)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("UTLS Client - Incorrect Pin", func(t *testing.T) {
		client := aoni.NewClient(nil, option.WithTLSFingerprint(aoni.BrowserChrome))
		_, err := client.Request(t.Context(), http.MethodGet, server.URL,
			mod.WithCertificatePin("127.0.0.1", incorrectPin),
			mod.WithInsecureSkipVerify(),
		)
		require.Error(t, err)
		assert.True(
			t,
			errors.Is(err, aoni.ErrCertificatePinning) ||
				strings.Contains(err.Error(), "certificate pinning validation failed"),
		)
	})

	t.Run("Wildcard Domain Match - Correct Pin", func(t *testing.T) {
		client := aoni.NewClient(server.Client())
		// Map api.example.com to our local test server port
		targetURL := "https://api.example.com/test"
		resp, err := client.Request(t.Context(), http.MethodGet, targetURL,
			mod.WithHostRewrite(map[string]string{"api.example.com": "127.0.0.1:" + port}),
			mod.WithCertificatePin("*.example.com", correctPinBase64),
		)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Suffix Domain Match - Correct Pin", func(t *testing.T) {
		client := aoni.NewClient(server.Client())
		// Map api.example.com to our local test server port
		targetURL := "https://api.example.com/test"
		resp, err := client.Request(t.Context(), http.MethodGet, targetURL,
			mod.WithHostRewrite(map[string]string{"api.example.com": "127.0.0.1:" + port}),
			mod.WithCertificatePin(".example.com", correctPinBase64),
		)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Multiple Pins - One Correct", func(t *testing.T) {
		client := aoni.NewClient(server.Client())
		resp, err := client.Request(t.Context(), http.MethodGet, server.URL,
			mod.WithCertificatePin("127.0.0.1", incorrectPin),
			mod.WithCertificatePin("127.0.0.1", correctPinBase64),
		)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Client-Level Correct Pin", func(t *testing.T) {
		client := aoni.NewClient(server.Client(), option.WithCertificatePin("127.0.0.1", correctPinBase64))
		resp, err := client.Request(t.Context(), http.MethodGet, server.URL)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Client-Level Incorrect Pin", func(t *testing.T) {
		client := aoni.NewClient(server.Client(), option.WithCertificatePin("127.0.0.1", incorrectPin))
		_, err := client.Request(t.Context(), http.MethodGet, server.URL)
		require.Error(t, err)
		assert.True(
			t,
			errors.Is(err, aoni.ErrCertificatePinning) ||
				strings.Contains(err.Error(), "certificate pinning validation failed"),
		)
	})
}
