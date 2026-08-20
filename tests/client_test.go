// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
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

	"github.com/klauspost/compress/gzip"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	utls "github.com/refraction-networking/utls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/codec/values"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/h3"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/fingerprint/profiles"
	"github.com/lemon4ksan/aoni/fluent"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/digest"
	"github.com/lemon4ksan/aoni/netutil/netdial"
	"github.com/lemon4ksan/aoni/netutil/proxy"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/realtime/stream"
	"github.com/lemon4ksan/aoni/request"
	"github.com/lemon4ksan/aoni/resiliency/cache"
	"github.com/lemon4ksan/aoni/resiliency/challenge"
	"github.com/lemon4ksan/aoni/telemetry"
	"github.com/lemon4ksan/foundation/net/ip"
)

type mockBaseResponse struct {
	Success  bool  `json:"success"`
	Data     any   `json:"data"`
	ErrorVal error `json:"-"`
}

func (b *mockBaseResponse) IsSuccess() bool { return b.Success }
func (b *mockBaseResponse) Error() error    { return b.ErrorVal }
func (b *mockBaseResponse) SetData(d any)   { b.Data = d }

type mockBaseProvider struct {
	request.Requester
	provider func() aoni.BaseResponse
}

func (m *mockBaseProvider) BaseResponse() aoni.BaseResponse {
	return m.provider()
}

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

func setupTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *aoni.Client) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := aoni.NewClient(nil, option.WithBaseURL(server.URL))

	return server, client
}

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
		DNSNames:              []string{"localhost", "api.example.com", "*.example.com"},
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

func TestClient_Request_StatusCodesAndErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		status           int
		respBody         string
		expectErr        bool
		expectMessage    string
		expectStatusCode int
	}{
		{
			name:             "success_200_ok",
			status:           http.StatusOK,
			respBody:         `{"message": "ok"}`,
			expectErr:        false,
			expectMessage:    "ok",
			expectStatusCode: http.StatusOK,
		},
		{
			name:             "error_404_not_found",
			status:           http.StatusNotFound,
			respBody:         `{"error": "not found"}`,
			expectErr:        true,
			expectMessage:    "not found",
			expectStatusCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.respBody))
			})

			res, err := request.GetTo[testPayload](t.Context(), client, "/")
			if tt.expectErr {
				require.Error(t, err)

				var apiErr *aoni.APIError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, tt.expectStatusCode, apiErr.StatusCode)
				assert.Contains(t, string(apiErr.Body), tt.expectMessage)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectMessage, res.Message)
			}
		})
	}
}

func TestClient_ContextCancellation(t *testing.T) {
	t.Parallel()

	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
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

	t.Run("unwrapping_options", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			respBody   string
			expectErr  string
			expectData string
		}{
			{
				name:       "success_unwrapped_data",
				respBody:   `{"status": "success", "data": {"message": "unwrapped"}}`,
				expectData: "unwrapped",
			},
			{
				name:      "error_something_went_wrong",
				respBody:  `{"status": "fail", "error": "something went wrong"}`,
				expectErr: "something went wrong",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(tt.respBody))
				})

				client = client.With(option.WithBaseResponse(func() aoni.BaseResponse { return &apiResponse{} }))

				result, err := request.GetTo[testPayload](t.Context(), client, "/")
				if tt.expectErr != "" {
					assert.ErrorContains(t, err, tt.expectErr)
				} else {
					require.NoError(t, err)
					assert.Equal(t, tt.expectData, result.Message)
				}
			})
		}
	})

	t.Run("disable_base_response_per_request", func(t *testing.T) {
		t.Parallel()

		_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"message": "raw_direct_payload"}`))
		})

		client = client.With(option.WithBaseResponse(func() aoni.BaseResponse { return &apiResponse{} }))

		result, err := request.GetTo[testPayload](t.Context(), client, "/", mod.WithoutBaseResponse())
		require.NoError(t, err)
		assert.Equal(t, "raw_direct_payload", result.Message)
	})

	t.Run("provider_interface", func(t *testing.T) {
		t.Parallel()

		_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"message":"provider_response"}}`))
		})

		providerClient := &mockBaseProvider{
			Requester: client,
			provider: func() aoni.BaseResponse {
				return &mockBaseResponse{}
			},
		}

		result, err := request.GetTo[testPayload](t.Context(), providerClient, "/provider")
		require.NoError(t, err)
		assert.Equal(t, "provider_response", result.Message)
	})
}

func TestAllowedDomainsRedirectPolicy(t *testing.T) {
	t.Parallel()

	policy := aoni.AllowedDomainsRedirectPolicy("example.com", "*.trusted.org")

	reqAllowed, _ := http.NewRequest("GET", "https://example.com/login", nil)
	assert.NoError(t, policy(reqAllowed, []*http.Request{reqAllowed}))

	reqSubdomainAllowed, _ := http.NewRequest("GET", "https://api.trusted.org/v1", nil)
	assert.NoError(t, policy(reqSubdomainAllowed, []*http.Request{reqSubdomainAllowed}))

	reqForbidden, _ := http.NewRequest("GET", "https://evil-phishing.com/steal", nil)
	err := policy(reqForbidden, []*http.Request{reqForbidden})
	assert.ErrorIs(t, err, aoni.ErrRedirectDomainForbidden)
}

func TestClient_ErrorModel(t *testing.T) {
	t.Parallel()

	_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "invalid_grant", "error_description": "expired token"}`))
	})

	var errModel errorStruct

	_, err := request.GetTo[any](t.Context(), client, "/oauth", mod.WithErrorModel(&errModel))
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

func TestClient_WithMultipart(t *testing.T) {
	t.Parallel()

	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
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

	fields := map[string]string{"field1": "val1", "field2": "val2"}
	files := map[string]io.Reader{"file1": strings.NewReader("file content")}

	resp, err := client.Request(t.Context(), http.MethodPost, "/", mod.WithMultipart(fields, files))
	require.NoError(t, err)
	t.Cleanup(func() { aoni.CloseResponse(resp) })
}

func TestClient_SSRFGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{name: "blocks_loopback_ipv4", url: "http://127.0.0.1:8080/"},
		{name: "blocks_private_network_ipv4", url: "http://192.168.1.1:8080/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := aoni.NewClient(nil, option.WithSSRFGuard())
			_, err := client.Request(t.Context(), http.MethodGet, tt.url)
			assert.ErrorIs(t, err, netdial.ErrSSRFBlocked)
		})
	}
}

func TestClient_ResponseSizeGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		headers        map[string]string
		payload        string
		maxSize        int64
		expectEarlyErr bool
	}{
		{
			name:           "fails_early_on_content_length",
			headers:        map[string]string{"Content-Length": "20"},
			payload:        "01234567890123456789",
			maxSize:        10,
			expectEarlyErr: true,
		},
		{
			name:           "fails_during_read_chunked",
			headers:        map[string]string{"Transfer-Encoding": "chunked"},
			payload:        "01234567890123456789",
			maxSize:        10,
			expectEarlyErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tt.headers {
					w.Header().Set(k, v)
				}

				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.payload))
			})

			client = client.With(option.WithMaxResponseSize(tt.maxSize))
			resp, err := client.Request(t.Context(), http.MethodGet, "/")

			if tt.expectEarlyErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, aoni.ErrResponseTooLarge)
				return
			}

			require.NoError(t, err)
			t.Cleanup(func() { aoni.CloseResponse(resp) })

			_, err = io.ReadAll(resp.Body)
			require.Error(t, err)
			assert.ErrorIs(t, err, aoni.ErrResponseTooLarge)
		})
	}
}

func TestClient_ContentTypeGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		mod       aoni.RequestModifier
		expectErr error
	}{
		{
			name:      "html_instead_of_json_returns_error",
			body:      "<html><body>Hello World</body></html>",
			expectErr: request.ErrUnexpectedContentType,
		},
		{
			name:      "cloudflare_challenge_html_returns_error",
			body:      "<html><body>cf-challenge and ray id cloudflare</body></html>",
			expectErr: challenge.ErrCloudflareDetected,
		},
		{
			name:      "html_with_raw_decoder_succeeds",
			body:      "<html><body>Hello World</body></html>",
			mod:       decode.WithRaw(),
			expectErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte(tt.body))
			})

			if !tt.mod.IsZero() {
				var output []byte

				resp, err := client.Request(t.Context(), http.MethodGet, "/", tt.mod)
				require.NoError(t, err)
				t.Cleanup(func() { aoni.CloseResponse(resp) })

				err = decode.RawDecoder.Decode(resp.Body, &output)
				require.NoError(t, err)
				assert.Equal(t, tt.body, string(output))

				return
			}

			_, err := request.GetTo[testPayload](t.Context(), client, "/")
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.expectErr)
		})
	}
}

func TestClient_AutoTranscoding(t *testing.T) {
	t.Parallel()

	_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=windows-1251")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message": "` + "\xef\xf0\xe8\xe2\xe5\xf2" + `"}`))
	})

	result, err := request.GetTo[testPayload](t.Context(), client, "/transcode")
	require.NoError(t, err)
	assert.Equal(t, "привет", result.Message)
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

			_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Content-Encoding", tt.encoding)
				_, _ = w.Write(buf.Bytes())
			})

			result, err := request.GetTo[testPayload](t.Context(), client, "/")
			require.NoError(t, err)
			assert.Equal(t, tt.want, result.Message)
		})
	}
}

func TestClient_CertificatePinning(t *testing.T) {
	t.Parallel()

	cert, key, err := generateTestCert(t)
	require.NoError(t, err)

	tlsCert, err := tls.X509KeyPair(cert, key)
	require.NoError(t, err)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	server.StartTLS()
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(u.Host)
	require.NoError(t, err)

	leafCert, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
	require.NoError(t, err)

	spkiHash := sha256.Sum256(leafCert.RawSubjectPublicKeyInfo)
	correctPinBase64 := base64.StdEncoding.EncodeToString(spkiHash[:])
	correctPinHex := hex.EncodeToString(spkiHash[:])
	correctPinPrefixed := "sha256/" + correctPinBase64
	incorrectPin := "sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

	tests := []struct {
		name        string
		useUTLS     bool
		clientPins  []string
		reqPins     []string
		hostRewrite map[string]string
		targetPath  string
		insecure    bool
		expectErr   bool
	}{
		{
			name:       "correct_pin_base64",
			reqPins:    []string{correctPinBase64},
			targetPath: server.URL,
		},
		{
			name:       "correct_pin_hex",
			reqPins:    []string{correctPinHex},
			targetPath: server.URL,
		},
		{
			name:       "correct_pin_prefixed",
			reqPins:    []string{correctPinPrefixed},
			targetPath: server.URL,
		},
		{
			name:       "incorrect_pin_fails",
			useUTLS:    true,
			insecure:   true,
			reqPins:    []string{incorrectPin},
			targetPath: server.URL,
			expectErr:  true,
		},
		{
			name:       "utls_client_correct_pin_with_insecure_skip_verify",
			useUTLS:    true,
			reqPins:    []string{correctPinBase64},
			targetPath: server.URL,
			insecure:   true,
		},
		{
			name:       "utls_client_incorrect_pin_fails",
			useUTLS:    true,
			reqPins:    []string{incorrectPin},
			targetPath: server.URL,
			insecure:   true,
			expectErr:  true,
		},
		{
			name:        "wildcard_domain_match_correct_pin",
			hostRewrite: map[string]string{"api.example.com": "127.0.0.1:" + port},
			reqPins:     []string{correctPinBase64},
			targetPath:  "https://api.example.com/test",
		},
		{
			name:       "client_level_correct_pin",
			clientPins: []string{correctPinBase64},
			targetPath: server.URL,
		},
		{
			name:       "client_level_incorrect_pin_fails",
			useUTLS:    true,
			insecure:   true,
			clientPins: []string{incorrectPin},
			targetPath: server.URL,
			expectErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var opts []aoni.ClientOption
			if tt.useUTLS {
				opts = append(opts, option.WithTLSFingerprint(aoni.BrowserChrome))
			} else {
				opts = append(opts, option.WithEngine(server.Client()))
			}

			for _, pin := range tt.clientPins {
				opts = append(opts, option.WithCertificatePin("127.0.0.1", pin))
			}

			client := aoni.NewClient(nil, opts...)

			var mods []aoni.RequestModifier
			if tt.insecure {
				mods = append(mods, mod.WithInsecureSkipVerify())
			}

			if tt.hostRewrite != nil {
				mods = append(mods, mod.WithHostRewrite(tt.hostRewrite))
			}

			for _, pin := range tt.reqPins {
				pinHost := "127.0.0.1"
				if tt.hostRewrite != nil {
					pinHost = "*.example.com"
				}

				mods = append(mods, mod.WithCertificatePin(pinHost, pin))
			}

			resp, err := client.Request(t.Context(), http.MethodGet, tt.targetPath, mods...)
			if tt.expectErr {
				require.Error(t, err)
				assert.True(
					t, strings.Contains(err.Error(), "certificate pinning validation failed"),
				)

				return
			}

			require.NoError(t, err)

			defer aoni.CloseResponse(resp)

			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
}

func TestClient_ResponseValidator(t *testing.T) {
	t.Parallel()

	errClientVal := errors.New("client validator error")
	errReqVal := errors.New("request validator error")

	tests := []struct {
		name             string
		clientValidator  func(*http.Response) error
		requestValidator func(*http.Response) error
		expectErr        error
		expectStatus     int
	}{
		{
			name: "client_level_only_failure",
			clientValidator: func(_ *http.Response) error {
				return errClientVal
			},
			requestValidator: nil,
			expectErr:        errClientVal,
		},
		{
			name: "client_and_request_level_both_fail_request_level_wins",
			clientValidator: func(_ *http.Response) error {
				return errClientVal
			},
			requestValidator: func(_ *http.Response) error {
				return errReqVal
			},
			expectErr: errReqVal,
		},
		{
			name: "client_level_fails_request_level_succeeds_override_passes",
			clientValidator: func(_ *http.Response) error {
				return errClientVal
			},
			requestValidator: func(_ *http.Response) error {
				return nil
			},
			expectErr:    nil,
			expectStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server, _ := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			opts := []aoni.ClientOption{option.WithBaseURL(server.URL)}
			if tt.clientValidator != nil {
				opts = append(opts, option.WithResponseValidator(tt.clientValidator))
			}

			client := aoni.NewClient(nil, opts...)

			var mods []aoni.RequestModifier
			if tt.requestValidator != nil {
				mods = append(mods, mod.WithResponseValidator(tt.requestValidator))
			}

			resp, err := client.Request(t.Context(), http.MethodGet, "/", mods...)
			if tt.expectErr != nil {
				assert.ErrorIs(t, err, tt.expectErr)
				return
			}

			require.NoError(t, err)

			defer aoni.CloseResponse(resp)

			assert.Equal(t, tt.expectStatus, resp.StatusCode)
		})
	}
}

func TestClient_MultiReadBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		body        string
		threshold   int64
		disableDisk bool
		expectErr   error
		expectBody  string
	}{
		{
			name:        "in_memory_caching_under_threshold",
			body:        "short body",
			threshold:   100,
			disableDisk: false,
			expectErr:   nil,
			expectBody:  "short body",
		},
		{
			name:        "disable_disk_fallback_above_threshold",
			body:        "long body exceeding threshold",
			threshold:   10,
			disableDisk: true,
			expectErr:   aoni.ErrBufferLimitExceeded,
			expectBody:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			})

			client = client.With(
				option.WithMultiReadBodyThreshold(tt.threshold),
				option.WithMultiReadDisableDisk(tt.disableDisk),
			)

			resp, err := client.Request(t.Context(), http.MethodGet, "/")
			if tt.expectErr != nil {
				assert.ErrorIs(t, err, tt.expectErr)
				return
			}

			require.NoError(t, err)
			t.Cleanup(func() { aoni.CloseResponse(resp) })

			body1, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, tt.expectBody, string(body1))

			_ = resp.Body.Close()

			body2, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, tt.expectBody, string(body2))
		})
	}
}

func TestClient_Cache(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("cached content"))
	}))
	t.Cleanup(server.Close)

	store := cache.NewInMemoryStore(1 * time.Minute)
	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithPipeline(aoni.PipelineConfig{
			Cache: &aoni.CacheConfig{Store: store, DefaultTTL: 1 * time.Minute},
		}),
	)

	resp1, err := request.Get(t.Context(), client, "/")
	require.NoError(t, err)

	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	assert.Equal(t, "cached content", string(body1))

	resp2, err := request.Get(t.Context(), client, "/")
	require.NoError(t, err)

	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	assert.Equal(t, "cached content", string(body2))

	assert.Equal(t, int32(1), hits.Load())
}

func TestClient_ProxyFailover(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	proxies := []string{"http://127.0.0.1:9999", server.URL}
	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithPipeline(aoni.PipelineConfig{
			ProxyFailover: &aoni.ProxyFailoverConfig{Proxies: proxies, RetryLimit: 2},
		}),
	)

	resp, err := request.Get(t.Context(), client, "/")
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(body))
	assert.Equal(t, int32(1), attempts.Load())
}

func TestClient_ProgressCallbacks(t *testing.T) {
	t.Parallel()

	t.Run("download_progress", func(t *testing.T) {
		t.Parallel()

		_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
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

func TestClient_Hedging_Deterministic(t *testing.T) {
	t.Parallel()

	var requestCount atomic.Int32

	slowRequestStarted := make(chan struct{})
	blockSlowRequest := make(chan struct{})

	_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
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
		res, err := request.GetTo[testPayload](t.Context(), client, "/")
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

func TestClient_TraceJA4_WithTLSFingerprint(t *testing.T) {
	t.Parallel()

	cert, key, err := generateTestCert(t)
	require.NoError(t, err)

	tlsCert, err := tls.X509KeyPair(cert, key)
	require.NoError(t, err)

	tlsConfig := &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

	info := &telemetry.TraceInfo{}
	_, err = client.Request(t.Context(), http.MethodGet, server.URL, mod.WithTraceJA4(info))
	require.NoError(t, err)

	require.NotNil(t, info.JA4)
	assert.NotEmpty(t, info.JA4.JA4H)
	assert.NotEmpty(t, info.JA4.JA4)
	assert.Regexp(t, `^t[0-9]{2}[di][0-9]{2}[0-9]{2}[a-z0-9]{2}_[a-f0-9]{12}_[a-f0-9]{12}$`, info.JA4.JA4)

	require.NotNil(t, callbackReport)
	assert.Equal(t, info.JA4.JA4, callbackReport.JA4)
}

func TestClient_SensitiveHeaderScrubbing(t *testing.T) {
	t.Parallel()

	t.Run("cross_origin_redirect_scrubs_headers", func(t *testing.T) {
		t.Parallel()

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

		reqMod := mod.Custom(func(req aoni.Request) {
			req.SetHeader("Authorization", "Bearer token123")
			req.SetHeader("Cookie", "session=cookie123")
			req.SetHeader("X-Session-ID", "sess123")
			req.SetHeader("X-Access-Token", "tok123")
			req.SetHeader("X-Safe-Header", "keep-me")
		})

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

func TestClient_HappyEyeballs(t *testing.T) {
	t.Parallel()

	_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	client = client.With(option.WithHappyEyeballs(10 * time.Millisecond))
	resp, err := client.Request(t.Context(), http.MethodGet, "/")
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
}

func TestClient_SourceIPRotator(t *testing.T) {
	t.Parallel()

	ips := []string{"192.168.1.1", "192.168.1.2"}
	rotator, err := ip.NewSourceIPRotator(ips)
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

	modifier.ApplyStd(req)

	extracted := aoni.HostRewriteRules(req.Context())
	assert.Equal(t, "127.0.0.1:8080", extracted["myapi.local"])

	appendMod := mod.WithAppendHostRewrite(map[string]string{"another.local": "10.0.0.2"})
	appendMod.ApplyStd(req)

	finalRules := aoni.HostRewriteRules(req.Context())
	assert.Equal(t, "127.0.0.1:8080", finalRules["myapi.local"])
	assert.Equal(t, "10.0.0.2", finalRules["another.local"])
}

func TestClient_PacketPadding(t *testing.T) {
	t.Parallel()

	cfg := fingerprint.PaddingConfig{
		MinPaddingBytes: 10,
		MaxPaddingBytes: 20,
		PaddingHeader:   "X-Custom-Padding",
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
	require.NoError(t, err)

	mod.WithPadding(cfg).ApplyStd(req)

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
		OnRetry: func(attempt uint32, _ error, _ time.Duration) {
			atomic.StoreUint32(&attempts, attempt)
		},
	}

	var handlerCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := handlerCount.Add(1)
		if count < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message": "recovered"}`))
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(nil, option.WithBaseURL(server.URL))
	retryMid := middleware.Retry(opts, middleware.RetryOnGatewayErrors())
	doer := retryMid(client)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := doer.Do(aoni.NewStdRequest(req))
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Close() })

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, uint32(2), atomic.LoadUint32(&attempts))
}

func TestConfigure_GenericEngine(t *testing.T) {
	t.Parallel()

	t.Run("configure_aoni_client", func(t *testing.T) {
		t.Parallel()

		stdClient := aoni.NewClient(nil)
		configured := aoni.Configure(stdClient, option.WithUserAgent("AoniUserAgent/1.0"))
		c, ok := configured.(*aoni.Client)
		require.True(t, ok)
		assert.Equal(t, "AoniUserAgent/1.0", c.Config().Defaults.Headers.Get("User-Agent"))
	})

	t.Run("configure_fast_client", func(t *testing.T) {
		t.Parallel()

		fastClient := fast.NewClient()
		configured := aoni.Configure(fastClient, option.WithUserAgent("FastUserAgent/1.0"))
		fc, ok := configured.(*fast.Client)
		require.True(t, ok)
		assert.Equal(t, "FastUserAgent/1.0", fc.Config().Defaults.Headers.Get("User-Agent"))
	})

	t.Run("configure_nil_client", func(t *testing.T) {
		t.Parallel()

		configured := aoni.Configure(nil, option.WithUserAgent("DefaultUserAgent/1.0"))
		c, ok := configured.(*aoni.Client)
		require.True(t, ok)
		assert.Equal(t, "DefaultUserAgent/1.0", c.Config().Defaults.Headers.Get("User-Agent"))
	})
}

func TestClient_StreamParsing(t *testing.T) {
	t.Parallel()

	t.Run("ndjson", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	t.Parallel()

	var lastReferer string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastReferer = r.Header.Get("Referer")

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(nil, option.WithRefererAutomaton(true))

	resp1, err := client.Request(t.Context(), http.MethodGet, server.URL+"/page1")
	require.NoError(t, err)

	_ = resp1.Body.Close()

	assert.Empty(t, lastReferer)

	resp2, err := client.Request(t.Context(), http.MethodGet, server.URL+"/page2")
	require.NoError(t, err)

	_ = resp2.Body.Close()

	assert.Equal(t, server.URL+"/page1", lastReferer)
}

func TestClient_HTTP3Settings(t *testing.T) {
	t.Parallel()

	client := aoni.NewClient(nil, option.WithHTTP3Settings(h3.ChromeSettings))
	assert.NotNil(t, client)
}

func TestUserAgentAndHintsRotation(t *testing.T) {
	t.Parallel()

	profList := []aoni.BrowserProfile{
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
	t.Cleanup(server.Close)

	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithPipeline(aoni.PipelineConfig{RotateUA: true}),
		option.WithUARotationProfiles(profList),
	)

	resp1, err := request.Get(t.Context(), client, "/")
	require.NoError(t, err)

	defer resp1.Body.Close()

	assert.Equal(t, "BrowserA", resp1.Header.Get("X-UA"))
	assert.Equal(t, "BrandA", resp1.Header.Get("X-Hint"))

	resp2, err := request.Get(t.Context(), client, "/")
	require.NoError(t, err)

	defer resp2.Body.Close()

	assert.Equal(t, "BrowserB", resp2.Header.Get("X-UA"))
	assert.Equal(t, "BrandB", resp2.Header.Get("X-Hint"))
}

func TestDPIJitter(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithPipeline(aoni.PipelineConfig{
			DPIJitter: &aoni.DPIJitterConfig{MinDelay: 10 * time.Millisecond, MaxDelay: 20 * time.Millisecond},
		}),
	)

	start := time.Now()
	resp, err := request.Get(t.Context(), client, "/")
	require.NoError(t, err)

	defer resp.Body.Close()

	duration := time.Since(start)
	assert.GreaterOrEqual(t, duration, 10*time.Millisecond)
}

type mockInspector struct {
	capturedReq  *http.Request
	capturedResp *http.Response
}

func (m *mockInspector) Capture(req *http.Request, resp *http.Response, _ error, _ *telemetry.TraceInfo) {
	m.capturedReq = req
	m.capturedResp = resp
}

func TestSensitiveDataRedactor(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Set-Cookie", "secret-session")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

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

	reqConfig := aoni.GetRequestConfig(inspector.capturedReq.Context())
	require.NotNil(t, reqConfig)
	cfg := reqConfig.Redact
	require.NotNil(t, cfg)

	_, ok1 := cfg.Headers["authorization"]
	_, ok2 := cfg.Headers["set-cookie"]

	assert.True(t, ok1)
	assert.True(t, ok2)
}

func TestHARGenerator(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("har body"))
	}))
	t.Cleanup(server.Close)

	harGen := telemetry.NewHARGenerator()
	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithPipeline(aoni.PipelineConfig{
			HAR: &aoni.HARConfig{Tracker: harGen},
		}),
	)

	resp, err := request.Get(t.Context(), client, "/")
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
	t.Cleanup(server.Close)

	customEncoder := func(_ any) (url.Values, error) {
		vals := make(url.Values)
		vals.Set("custom_key", "custom_val")
		return vals, nil
	}

	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithQueryEncoder(customEncoder),
	)

	_, err := request.Get(t.Context(), client, "/", mod.WithQuery(struct{ Dummy string }{Dummy: "value"}))
	require.NoError(t, err)
	assert.Equal(t, "custom_key=custom_val", capturedQuery)
}

func TestWithClientBrowserProfile(t *testing.T) {
	t.Parallel()

	t.Run("chrome_profile", func(t *testing.T) {
		client := aoni.NewClient(nil, option.WithBrowserProfile(aoni.BrowserChrome, profiles.Windows))
		assert.Equal(t, aoni.BrowserChrome, client.BrowserID())
		assert.True(t, client.Fingerprint().TLSClientHelloID != nil || client.Fingerprint().TLSClientHelloSpecProvider != nil)
	})

	t.Run("firefox_profile", func(t *testing.T) {
		t.Parallel()

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
	})

	t.Run("request_header_ordering", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		client := aoni.NewClient(nil,
			option.WithBaseURL(server.URL),
			option.WithBrowserProfile(aoni.BrowserChrome, profiles.Windows),
		)

		reqGet, err := http.NewRequestWithContext(t.Context(), "GET", server.URL, nil)
		require.NoError(t, err)

		reqGet = client.InitRequestConfig(reqGet)
		for _, defaultMod := range client.Defaults().DefaultMods {
			defaultMod.ApplyStd(reqGet)
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
			defaultMod.ApplyStd(reqPost)
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
			defaultMod.ApplyStd(reqMultipart)
		}

		modMultipart := mod.WithMultipart(map[string]string{"foo": "bar"}, nil)
		modMultipart.ApplyStd(reqMultipart)

		contentType := reqMultipart.Header.Get("Content-Type")
		assert.Contains(t, contentType, "multipart/form-data")
		assert.Contains(t, contentType, "----WebKitFormBoundary")
	})
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
		BuildHeaders: func(_ profiles.OSKey) []profiles.HeaderEntry {
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

	headers := client.Defaults().Headers
	assert.Equal(t, "CustomUA/1.0", headers.Get("User-Agent"))
	assert.Equal(t, "custom-val", headers.Get("custom-header"))

	require.NotNil(t, client.Fingerprint().H2Settings)
	assert.Equal(t, uint32(99999), client.Fingerprint().H2Settings.InitialWindowSize)

	require.NotNil(t, client.Fingerprint().TLSClientHelloID)
	assert.Equal(t, "Firefox", client.Fingerprint().TLSClientHelloID.Client)
	assert.Equal(t, "123", client.Fingerprint().TLSClientHelloID.Version)

	req, err := http.NewRequestWithContext(t.Context(), "GET", "http://example.com", nil)
	require.NoError(t, err)

	req = client.InitRequestConfig(req)
	for _, defaultMod := range client.Defaults().DefaultMods {
		defaultMod.ApplyStd(req)
	}

	cfg := aoni.GetRequestConfig(req.Context())
	require.NotNil(t, cfg)
	assert.Equal(t, []string{"custom-header", "user-agent"}, cfg.OrderedHeaders)

	reqMultipart, err := http.NewRequestWithContext(t.Context(), "POST", "http://example.com", nil)
	require.NoError(t, err)

	reqMultipart = client.InitRequestConfig(reqMultipart)
	for _, defaultMod := range client.Defaults().DefaultMods {
		defaultMod.ApplyStd(reqMultipart)
	}

	modMultipart := mod.WithMultipart(map[string]string{"foo": "bar"}, nil)
	modMultipart.ApplyStd(reqMultipart)

	contentType := reqMultipart.Header.Get("Content-Type")
	assert.Contains(t, contentType, "multipart/form-data")
	assert.Contains(t, contentType, "----CustomBoundary123")
}

func TestWithClientTCPDelay(t *testing.T) {
	t.Parallel()

	client := aoni.NewClient(nil,
		option.WithTCPDelay(10*time.Millisecond, 20*time.Millisecond),
	)

	reqGet, err := http.NewRequestWithContext(t.Context(), "GET", "http://example.com", nil)
	require.NoError(t, err)

	reqGet = client.InitRequestConfig(reqGet)
	for _, defaultMod := range client.Defaults().DefaultMods {
		defaultMod.ApplyStd(reqGet)
	}

	cfgGet := aoni.GetRequestConfig(reqGet.Context())
	require.NotNil(t, cfgGet)
	assert.Equal(t, 10*time.Millisecond, cfgGet.TCPDelay.Min)
	assert.Equal(t, 20*time.Millisecond, cfgGet.TCPDelay.Max)

	reqOverride, err := http.NewRequestWithContext(t.Context(), "GET", "http://example.com", nil)
	require.NoError(t, err)

	reqOverride = client.InitRequestConfig(reqOverride)
	for _, defaultMod := range client.Defaults().DefaultMods {
		defaultMod.ApplyStd(reqOverride)
	}

	mod.WithTCPDelay(100*time.Millisecond, 200*time.Millisecond).ApplyStd(reqOverride)

	cfgOverride := aoni.GetRequestConfig(reqOverride.Context())
	require.NotNil(t, cfgOverride)
	assert.Equal(t, 100*time.Millisecond, cfgOverride.TCPDelay.Min)
	assert.Equal(t, 200*time.Millisecond, cfgOverride.TCPDelay.Max)
}

func TestClient_WithClientInsecureSkipVerify(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(nil, option.WithInsecureSkipVerify())
	resp, err := client.Request(t.Context(), http.MethodGet, server.URL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestAsCurl_WithBody(t *testing.T) {
	t.Parallel()

	server, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, "replayed_body_data", string(body))
		w.WriteHeader(http.StatusOK)
	})

	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		server.URL+"/curl",
		strings.NewReader("replayed_body_data"),
	)
	require.NoError(t, err)

	mod.WithCurlDump().ApplyStd(req)

	resp, err := client.Engine().Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { aoni.CloseResponse(resp) })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

type dummyUnwrapper struct {
	inner request.Requester
}

func (d *dummyUnwrapper) Request(
	ctx context.Context,
	method, path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	return d.inner.Request(ctx, method, path, mods...)
}

func (d *dummyUnwrapper) Unwrap() request.Requester {
	return d.inner
}

func TestClient_GettersAndUnwrap(t *testing.T) {
	t.Parallel()

	client := aoni.NewClient(nil,
		option.WithTLSFingerprint(aoni.BrowserChrome),
	)

	assert.NotNil(t, client.Network())
	assert.NotNil(t, client.Fingerprint())
	assert.Nil(t, client.Inspector())
	assert.Equal(t, aoni.BrowserChrome, client.BrowserID())

	client.CloseIdleConnections()

	// Test UnwrapClient
	wrapper := &dummyUnwrapper{inner: client}
	unwrapped := request.UnwrapClient(wrapper)
	assert.Same(t, client, unwrapped)

	// Test WithTLSClientHelloID & WithPersona & WithHTTP3
	c2 := client.With(option.WithTLSClientHelloID(utls.HelloChrome_120))
	assert.NotNil(t, c2)

	c3 := client.With(option.WithPersonaStruct(fingerprint.PersonaChrome120Windows))
	assert.NotNil(t, c3)

	c4 := client.With(option.WithHTTP3())
	assert.NotNil(t, c4)
}

type msgpackTestDecoder struct{}

func (msgpackTestDecoder) Decode(r io.Reader, target any) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	switch v := target.(type) {
	case *string:
		*v = "msgpack:" + string(b)
		return nil
	case **string:
		**v = "msgpack:" + string(b)
		return nil
	default:
		return fmt.Errorf("invalid target type: %T", target)
	}
}

func TestClient_CustomMIMEDecoders(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-msgpack; charset=utf-8")
		_, _ = w.Write([]byte("binary_payload"))
	}))
	t.Cleanup(ts.Close)

	t.Run("via option.WithDecoder", func(t *testing.T) {
		client := aoni.NewClient(ts.Client(),
			option.WithBaseURL(ts.URL),
			option.WithDecoder("application/x-msgpack", msgpackTestDecoder{}),
		)

		result, err := request.GetTo[string](context.Background(), client, "/")
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "msgpack:binary_payload", *result)
	})
}

func TestClient_BrowserProfile_HTTP2(t *testing.T) {
	t.Parallel()

	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Proto", r.Proto)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	t.Cleanup(ts.Close)

	client := aoni.NewClient(nil,
		option.WithBrowserProfile(aoni.BrowserChrome, profiles.Windows),
		option.WithInsecureSkipVerify(),
		option.WithSessionCache(proxy.NewProxyAwareSessionCache()),
	)

	req, err := http.NewRequestWithContext(context.Background(), "GET", ts.URL, nil)
	require.NoError(t, err)

	resp, err := client.HTTP().Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "HTTP/2.0", resp.Header.Get("X-Proto"))
}

func TestClient_SNI_CleanHostPort_Handshake(t *testing.T) {
	t.Parallel()

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(ts.Close)

	client := aoni.NewClient(nil,
		option.WithBrowserProfile(aoni.BrowserChrome, profiles.Windows),
		option.WithInsecureSkipVerify(),
		option.WithSessionCache(proxy.NewProxyAwareSessionCache()),
	)

	// Test passing URL with explicit port
	req, err := http.NewRequestWithContext(context.Background(), "GET", ts.URL+"/dev/apikey", nil)
	require.NoError(t, err)

	resp, err := client.HTTP().Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRFC6265_CookiePathMatching(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		reqPath    string
		cookiePath string
		wantMatch  bool
	}{
		{
			name:       "exact_path_match",
			reqPath:    "/api/v1",
			cookiePath: "/api/v1",
			wantMatch:  true,
		},
		{
			name:       "prefix_match_with_subpath_slash_boundary",
			reqPath:    "/api/v1/users",
			cookiePath: "/api/v1",
			wantMatch:  true,
		},
		{
			name:       "prefix_match_with_trailing_slash_in_cookie_path",
			reqPath:    "/api/v1/users",
			cookiePath: "/api/v1/",
			wantMatch:  true,
		},
		{
			name:       "must_not_match_subpath_without_slash_boundary",
			reqPath:    "/api-v2/users",
			cookiePath: "/api",
			wantMatch:  false, // RFC 6265 §5.1.4: "/api-v2" does NOT match "/api"
		},
		{
			name:       "root_path_matches_everything",
			reqPath:    "/any/resource/path",
			cookiePath: "/",
			wantMatch:  true,
		},
		{
			name:       "empty_cookie_path_defaults_to_root",
			reqPath:    "/test",
			cookiePath: "",
			wantMatch:  true,
		},
		{
			name:       "disjoint_paths",
			reqPath:    "/user/profile",
			cookiePath: "/admin",
			wantMatch:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := cookie.PathMatch(tt.reqPath, tt.cookiePath)
			assert.Equal(t, tt.wantMatch, got, "PathMatch(%q, %q)", tt.reqPath, tt.cookiePath)
		})
	}
}

func TestRFC9112_ConflictingContentLengthHeaders(t *testing.T) {
	t.Parallel()

	t.Run("identical_multiple_content_length_normalized", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header()["Content-Length"] = []string{"13", "13"}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("hello world!\n"))
		}))
		t.Cleanup(server.Close)

		client := aoni.NewClient(nil, option.WithBaseURL(server.URL))
		resp, err := client.Request(t.Context(), http.MethodGet, "/")
		require.NoError(t, err)

		defer aoni.CloseResponse(resp)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, []string{"13"}, resp.Header["Content-Length"])
	})

	t.Run("conflicting_multiple_content_length_rejected", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header()["Content-Length"] = []string{"10", "20"}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("0123456789"))
		}))
		t.Cleanup(server.Close)

		client := aoni.NewClient(nil, option.WithBaseURL(server.URL))
		_, err := client.Request(t.Context(), http.MethodGet, "/")
		require.Error(t, err)

		isConflictingCLErr := errors.Is(err, aoni.ErrConflictingContentLength) ||
			strings.Contains(err.Error(), "multiple Content-Length headers")
		assert.True(t, isConflictingCLErr, "expected conflicting Content-Length error, got: %v", err)
	})
}

func TestRFC6266_RFC8187_ContentDispositionFilenameExtraction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		header     string
		wantResult string
	}{
		{
			name:       "rfc8187_utf8_encoded_filename_cyrillic",
			header:     `attachment; filename*=UTF-8''foo%20bar%20%D0%BF%D1%80%D0%B8%D0%B2%D0%B5%D1%82.txt`,
			wantResult: "foo bar привет.txt",
		},
		{
			name:       "rfc8187_utf8_encoded_filename_umlaut",
			header:     `attachment; filename*=UTF-8''t%C3%B6st.txt`,
			wantResult: "töst.txt",
		},
		{
			name:       "fallback_to_standard_filename_when_extended_missing",
			header:     `attachment; filename="report_2026.pdf"`,
			wantResult: "report_2026.pdf",
		},
		{
			name:       "strip_path_traversal_sequences",
			header:     `attachment; filename="../../../../../etc/passwd"`,
			wantResult: "passwd",
		},
		{
			name:       "windows_reserved_device_name_con_fallback",
			header:     `attachment; filename="CON.txt"`,
			wantResult: "downloaded_file",
		},
		{
			name:       "windows_reserved_device_name_nul_fallback",
			header:     `attachment; filename="NUL"`,
			wantResult: "downloaded_file",
		},
		{
			name:       "empty_header_fallback",
			header:     "",
			wantResult: "downloaded_file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := netutil.ExtractSanitizedFilename(tt.header)
			assert.Equal(t, tt.wantResult, got)
		})
	}
}

func TestRFC6874_HostNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rawHost  string
		wantHost string
	}{
		{
			name:     "strip_ipv6_zone_id_bracketed",
			rawHost:  "[fe80::1%eth0]",
			wantHost: "[fe80::1]",
		},
		{
			name:     "strip_ipv6_zone_id_unbracketed",
			rawHost:  "fe80::1%eth0",
			wantHost: "fe80::1",
		},
		{
			name:     "convert_idn_punycode_cyrillic",
			rawHost:  "президент.рф",
			wantHost: "xn--d1abbgf6aiiy.xn--p1ai",
		},
		{
			name:     "standard_ascii_hostname_remains_unchanged",
			rawHost:  "api.example.com",
			wantHost: "api.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := netutil.CleanHost(tt.rawHost)
			assert.Equal(t, tt.wantHost, got)
		})
	}
}

func TestRFC7616_DigestAuthentication(t *testing.T) {
	t.Parallel()

	realm := "restricted-area"
	username := "admin"
	password := "secret123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="`+realm+`", nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093", qop="auth,auth-int", algorithm=MD5`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		require.True(t, strings.HasPrefix(authHeader, "Digest "))
		assert.Contains(t, authHeader, `username="admin"`)
		assert.Contains(t, authHeader, `realm="restricted-area"`)
		assert.Contains(t, authHeader, `response=`)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "authenticated"}`))
	}))
	t.Cleanup(server.Close)

	digestTr := &digest.Transport{
		Username: username,
		Password: password,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	}

	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithEngine(&http.Client{Transport: digestTr}),
	)

	type authResponse struct {
		Status string `json:"status"`
	}

	res, err := request.GetTo[authResponse](t.Context(), client, "/protected")
	require.NoError(t, err)
	assert.Equal(t, "authenticated", res.Status)
}

func TestValues_CustomUnmarshalersAndTags(t *testing.T) {
	t.Parallel()

	t.Run("bool_int_json_unmarshaling", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			input string
			want  bool
		}{
			{input: `"1"`, want: true},
			{input: `"true"`, want: true},
			{input: `1`, want: true},
			{input: `"0"`, want: false},
			{input: `"false"`, want: false},
			{input: `0`, want: false},
			{input: `null`, want: false},
		}

		for _, tt := range tests {
			var bi values.BoolInt
			err := json.Unmarshal([]byte(tt.input), &bi)
			require.NoError(t, err, "input: %s", tt.input)
			assert.Equal(t, tt.want, bool(bi))
		}
	})

	t.Run("struct_to_values_custom_delimiters", func(t *testing.T) {
		t.Parallel()

		type FilterParams struct {
			CommaTags values.CommaSlice[string] `url:"comma_tags"`
			PipeTags  []string                  `url:"pipe_tags,pipe"`
			SpaceTags []string                  `url:"space_tags,space"`
		}

		p := FilterParams{
			CommaTags: []string{"go", "rust", "zig"},
			PipeTags:  []string{"read", "write"},
			SpaceTags: []string{"foo", "bar"},
		}

		vals, err := values.StructToValues(p)
		require.NoError(t, err)

		assert.Equal(t, "go,rust,zig", vals.Get("comma_tags"))
		assert.Equal(t, "read|write", vals.Get("pipe_tags"))
		assert.Equal(t, "foo bar", vals.Get("space_tags"))
	})
}

func TestGRPCWeb_TrailerValidation(t *testing.T) {
	t.Parallel()

	t.Run("valid_grpc_status_zero_succeeds", func(t *testing.T) {
		t.Parallel()

		trailerPayload := []byte("grpc-status:0\r\ngrpc-message:OK\r\n")
		err := decode.VerifyGRPCTrailer(trailerPayload)
		assert.NoError(t, err)
	})

	t.Run("non_zero_grpc_status_returns_error", func(t *testing.T) {
		t.Parallel()

		trailerPayload := []byte("grpc-status:14\r\ngrpc-message:UNAVAILABLE\r\n")
		err := decode.VerifyGRPCTrailer(trailerPayload)
		require.Error(t, err)

		var grpcErr *decode.GRPCWebError
		require.ErrorAs(t, err, &grpcErr)
		assert.Equal(t, "14", grpcErr.StatusCode)
		assert.Equal(t, "UNAVAILABLE", grpcErr.StatusMsg)
		assert.ErrorIs(t, err, decode.ErrGRPCWebStatusError)
	})
}

func TestRealtimeStream_SSEMultiLineAndCommentParsing(t *testing.T) {
	t.Parallel()

	ssePayload := `: heartbeat comment line
event: user_created
id: evt_101
retry: 5000
data: {
data:   "id": 42,
data:   "name": "Alice"
data: }

`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(ssePayload))
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(nil, option.WithBaseURL(server.URL))
	streamResp, err := stream.Get(t.Context(), client, "/events")
	require.NoError(t, err)

	type UserEvent struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	ch, errs := stream.ParseSSE[UserEvent](t.Context(), streamResp)

	var received []UserEvent
	for val := range ch {
		received = append(received, val)
	}

	select {
	case err := <-errs:
		require.NoError(t, err)
	default:
	}

	require.Len(t, received, 1)
	assert.Equal(t, 42, received[0].ID)
	assert.Equal(t, "Alice", received[0].Name)
}

func TestNetUtil_PrivateIPClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ip        string
		isPrivate bool
	}{
		{ip: "127.0.0.1", isPrivate: true},
		{ip: "10.0.1.5", isPrivate: true},
		{ip: "172.16.0.1", isPrivate: true},
		{ip: "192.168.1.1", isPrivate: true},
		{ip: "100.64.0.1", isPrivate: true}, // CGNAT (RFC 6598)
		{ip: "8.8.8.8", isPrivate: false},   // Public IPv4
		{ip: "1.1.1.1", isPrivate: false},   // Public IPv4
		{ip: "::1", isPrivate: true},        // IPv6 Loopback
		{ip: "fc00::1", isPrivate: true},    // IPv6 Unique Local Address (ULA)
		{ip: "2001:db8::1", isPrivate: false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			t.Parallel()

			parsed := net.ParseIP(tt.ip)
			require.NotNil(t, parsed)

			got := ip.IsPrivateIP(parsed)
			assert.Equal(t, tt.isPrivate, got)
		})
	}
}

func TestFluentAPI_PathInterpolationAndDownload(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/users/usr_42/orders/ord_100", r.URL.Path)
		assert.Equal(t, "desc", r.URL.Query().Get("sort"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "success", "message": "found"}`))
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(nil, option.WithBaseURL(server.URL))

	type apiResult struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}

	var res apiResult
	resp, err := fluent.R(client).
		SetContext(t.Context()).
		SetPathParam("userId", "usr_42").
		SetPathParam("orderId", "ord_100").
		SetQueryParam("sort", "desc").
		SetResult(&res).
		Get("/v1/users/{userId}/orders/{orderId}")

	require.NoError(t, err)
	t.Cleanup(func() { aoni.CloseResponse(resp) })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "found", res.Message)
	assert.Equal(t, "success", res.Status)
}

func TestClient_CustomConnFilter(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	var filterCalled int32

	customFilter := func(ctx context.Context, conn net.Conn, targetHost string, cfg *aoni.DialConfig) (net.Conn, error) {
		atomic.StoreInt32(&filterCalled, 1)
		return conn, nil
	}

	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithConnFilter(customFilter),
	)

	resp, err := client.Request(t.Context(), http.MethodGet, "/")
	require.NoError(t, err)
	t.Cleanup(func() { aoni.CloseResponse(resp) })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(1), atomic.LoadInt32(&filterCalled))
}

func TestClient_AuditFeatures(t *testing.T) {
	t.Parallel()

	t.Run("with_locale", func(t *testing.T) {
		t.Parallel()

		var rLang string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rLang = r.Header.Get("Accept-Language")
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		client := aoni.NewClient(nil,
			option.WithBaseURL(server.URL),
			option.WithLocale("fr-FR,fr;q=0.9,en-US;q=0.8"),
		)

		resp, err := client.Request(t.Context(), http.MethodGet, "/")
		require.NoError(t, err)
		t.Cleanup(func() { aoni.CloseResponse(resp) })

		assert.Equal(t, "fr-FR,fr;q=0.9,en-US;q=0.8", rLang)
	})

	t.Run("dns_resolver_override_helper", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		require.NoError(t, err)

		mod.WithDNSResolver(nil).ApplyStd(req)
		assert.Nil(t, aoni.GetDNSResolverOverride(req.Context()))
	})

	t.Run("aoni_root_facade_and_modifiers", func(t *testing.T) {
		t.Parallel()

		var receivedAuth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}))
		t.Cleanup(server.Close)

		client := aoni.New(aoni.WithBaseURL(server.URL), aoni.WithClientTimeout(5*time.Second))
		resp, err := client.Get(t.Context(), "/", aoni.WithBearer("secret_token_123"))
		require.NoError(t, err)
		t.Cleanup(func() { aoni.DrainAndClose(resp) })

		assert.Equal(t, "Bearer secret_token_123", receivedAuth)
	})

	t.Run("api_error_human_formatting_and_helpers", func(t *testing.T) {
		t.Parallel()

		err404 := &aoni.APIError{
			StatusCode: http.StatusNotFound,
			Body:       []byte("resource not found"),
		}
		assert.True(t, err404.IsNotFound())
		assert.False(t, err404.IsUnauthorized())
		assert.True(t, err404.IsClientError())
		assert.False(t, err404.IsServerError())
		assert.Equal(t, "resource not found", err404.BodyString())
		assert.Contains(t, err404.Error(), "HTTP 404 Not Found")

		err401 := &aoni.APIError{
			StatusCode: http.StatusUnauthorized,
			Body:       []byte("invalid credentials"),
		}
		assert.True(t, err401.IsUnauthorized())
		assert.False(t, err401.IsNotFound())
		assert.Contains(t, err401.Error(), "HTTP 401 Unauthorized")
	})

	t.Run("aoni_root_generics_GetTo_PostTo_Fetch", func(t *testing.T) {
		t.Parallel()

		type sampleUser struct {
			Name  string `json:"name"`
			Role  string `json:"role"`
			Admin bool   `json:"admin"`
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPost {
				var u sampleUser
				_ = json.NewDecoder(r.Body).Decode(&u)
				u.Admin = true
				_ = json.NewEncoder(w).Encode(u)
				return
			}

			_ = json.NewEncoder(w).Encode(sampleUser{Name: "Steve", Role: "Visionary"})
		}))
		t.Cleanup(server.Close)

		// 1. aoni.GetTo
		user, err := aoni.GetTo[sampleUser](t.Context(), server.URL)
		require.NoError(t, err)
		require.NotNil(t, user)
		assert.Equal(t, "Steve", user.Name)

		// 2. aoni.PostTo with smart body
		created, err := aoni.PostTo[sampleUser](t.Context(), server.URL, sampleUser{Name: "Woz", Role: "Engineer"})
		require.NoError(t, err)
		require.NotNil(t, created)
		assert.Equal(t, "Woz", created.Name)
		assert.True(t, created.Admin)

		// 3. aoni.Fetch returning Result[T]
		res, resp := aoni.Fetch[sampleUser](t.Context(), server.URL)
		require.NotNil(t, resp)
		assert.True(t, res.IsSuccess())
		fetched, err := res.Unwrap()
		require.NoError(t, err)
		assert.Equal(t, "Steve", fetched.Name)
	})

	t.Run("slog_log_valuer_observability", func(t *testing.T) {
		t.Parallel()

		apiErr := &aoni.APIError{
			StatusCode: http.StatusForbidden,
			Body:       []byte("access denied"),
		}
		val := apiErr.LogValue()
		assert.NotEmpty(t, val.Group())

		client := aoni.New(aoni.WithBaseURL("https://api.apple.com"), aoni.WithChrome())
		cVal := client.LogValue()
		assert.NotEmpty(t, cVal.Group())
	})
}
