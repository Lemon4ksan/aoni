// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod_test

import (
	"context"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
)

type dummyUser struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type dummyRequest struct {
	httpReq *http.Request
	ctx     context.Context
	url     string
	body    []byte
	bodyRdr io.Reader
}

func newDummyRequest() *dummyRequest {
	req, _ := http.NewRequest("GET", "http://example.com/v1/item/{id}", nil)

	return &dummyRequest{
		httpReq: req,
		ctx:     context.Background(),
		url:     "http://example.com/v1/item/{id}",
	}
}

func (r *dummyRequest) Context() context.Context       { return r.ctx }
func (r *dummyRequest) SetContext(ctx context.Context) { r.ctx = ctx }
func (r *dummyRequest) Method() string                 { return r.httpReq.Method }
func (r *dummyRequest) SetMethod(m string)             { r.httpReq.Method = m }
func (r *dummyRequest) SetMethodBytes(m []byte)        { r.httpReq.Method = string(m) }
func (r *dummyRequest) URL() string                    { return r.url }
func (r *dummyRequest) SetURL(u string)                { r.url = u }
func (r *dummyRequest) SetURIBytes(u []byte)           { r.url = string(u) }
func (r *dummyRequest) Path() string                   { return r.httpReq.URL.Path }
func (r *dummyRequest) SetPath(p string)               { r.httpReq.URL.Path = p }
func (r *dummyRequest) RawQuery() string               { return r.httpReq.URL.RawQuery }
func (r *dummyRequest) SetRawQuery(q string)           { r.httpReq.URL.RawQuery = q }
func (r *dummyRequest) SetRawQueryBytes(q []byte)      { r.httpReq.URL.RawQuery = string(q) }
func (r *dummyRequest) AddQueryParam(k, v string) {
	q := r.httpReq.URL.Query()
	q.Add(k, v)
	r.httpReq.URL.RawQuery = q.Encode()
}
func (r *dummyRequest) AddQueryParamBytes(k, v []byte) { r.AddQueryParam(string(k), string(v)) }
func (r *dummyRequest) SetQueryParam(k, v string) {
	q := r.httpReq.URL.Query()
	q.Set(k, v)
	r.httpReq.URL.RawQuery = q.Encode()
}
func (r *dummyRequest) SetQueryParamBytes(k, v []byte) { r.SetQueryParam(string(k), string(v)) }
func (r *dummyRequest) QueryParam(key string) string   { return r.httpReq.URL.Query().Get(key) }
func (r *dummyRequest) Header(key string) string       { return r.httpReq.Header.Get(key) }
func (r *dummyRequest) HeaderBytes(key []byte) []byte {
	return []byte(r.httpReq.Header.Get(string(key)))
}
func (r *dummyRequest) SetHeader(key, val string) { r.httpReq.Header.Set(key, val) }
func (r *dummyRequest) SetHeaderBytes(key, val []byte) {
	r.httpReq.Header.Set(string(key), string(val))
}
func (r *dummyRequest) AddHeader(key, val string) { r.httpReq.Header.Add(key, val) }
func (r *dummyRequest) AddHeaderBytes(key, val []byte) {
	r.httpReq.Header.Add(string(key), string(val))
}
func (r *dummyRequest) DelHeader(key string)      { r.httpReq.Header.Del(key) }
func (r *dummyRequest) DelHeaderBytes(key []byte) { r.httpReq.Header.Del(string(key)) }
func (r *dummyRequest) ResetHeaders()             { r.httpReq.Header = make(http.Header) }
func (r *dummyRequest) Headers() iter.Seq2[[]byte, []byte] {
	return func(yield func([]byte, []byte) bool) {
		for k, vv := range r.httpReq.Header {
			for _, v := range vv {
				if !yield([]byte(k), []byte(v)) {
					return
				}
			}
		}
	}
}
func (r *dummyRequest) SetBodyBytes(b []byte)                { r.body = b }
func (r *dummyRequest) BodyBytes() []byte                    { return r.body }
func (r *dummyRequest) SetBodyStream(rdr io.Reader, _ int64) { r.bodyRdr = rdr }
func (r *dummyRequest) BodyStream() io.Reader                { return r.bodyRdr }
func (r *dummyRequest) EngineRequest() any                   { return r.httpReq }
func (r *dummyRequest) HTTPRequest() *http.Request           { return r.httpReq }

var _ aoni.Request = (*dummyRequest)(nil)

func TestMod_URIAndPathModifiers(t *testing.T) {
	t.Parallel()

	t.Run("with_var_single_substitution", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()
		mod.WithVar("id", 42).Apply(req)

		assert.Equal(t, "/v1/item/42", req.Path())
	})

	t.Run("with_vars_multiple_pairs", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()
		req.SetPath("/v1/users/{userId}/orders/{orderId}")

		mod.WithVars("userId", "usr_123", "orderId", "ord_999").Apply(req)

		assert.Equal(t, "/v1/users/usr_123/orders/ord_999", req.Path())
	})

	t.Run("with_vars_odd_pair_count_fails", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()
		mod.WithVars("key_without_value").Apply(req)

		cfg := aoni.GetRequestConfig(req.Context())
		require.NotNil(t, cfg)
		assert.ErrorIs(t, cfg.BodyError, mod.ErrInvalidPairCount)
	})

	t.Run("with_url_override", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()
		mod.WithBaseURL("https://api.custom-target.com/v1/data").Apply(req)

		assert.Equal(t, "https://api.custom-target.com/v1/data", req.URL())
	})

	t.Run("without_base_url", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()
		req.SetPath("/local/path")

		mod.WithoutBaseURL().Apply(req)

		assert.Equal(t, "/local/path", req.URL())
	})

	t.Run("with_query_struct_and_map", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()

		queryMap := map[string]string{"sort": "desc", "page": "1"}
		mod.WithQuery(queryMap).Apply(req)

		assert.Contains(t, req.RawQuery(), "sort=desc")
		assert.Contains(t, req.RawQuery(), "page=1")
	})
}

func TestMod_HeadersAndAuthModifiers(t *testing.T) {
	t.Parallel()

	t.Run("header_mutations", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()

		mod.WithHeader("X-Request-ID", "req-12345").Apply(req)
		mod.WithHeaderBytes([]byte("X-Engine"), []byte("aoni-fast")).Apply(req)
		mod.WithHeaders(map[string]string{"X-App": "demo", "X-Env": "prod"}).Apply(req)

		assert.Equal(t, "req-12345", req.Header("X-Request-ID"))
		assert.Equal(t, "aoni-fast", string(req.HeaderBytes([]byte("X-Engine"))))
		assert.Equal(t, "demo", req.Header("X-App"))
		assert.Equal(t, "prod", req.Header("X-Env"))

		mod.ResetHeaders().Apply(req)
		assert.Empty(t, req.Header("X-App"))
	})

	t.Run("bearer_and_basic_auth", func(t *testing.T) {
		t.Parallel()

		req1 := newDummyRequest()
		mod.WithBearer("secret-token-xyz").Apply(req1)
		assert.Equal(t, "Bearer secret-token-xyz", req1.Header("Authorization"))

		req2 := newDummyRequest()
		mod.WithBasicAuth("admin", "secret123").Apply(req2)
		assert.Equal(t, "Basic YWRtaW46c2VjcmV0MTIz", req2.Header("Authorization"))
	})

	t.Run("pkce_modifiers", func(t *testing.T) {
		t.Parallel()

		// Test Vector from RFC 7636 Appendix B
		const (
			verifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
			challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
		)

		reqAuth := newDummyRequest()
		mod.WithPKCE(verifier).Apply(reqAuth)
		assert.Equal(t, challenge, reqAuth.QueryParam("code_challenge"))
		assert.Equal(t, "S256", reqAuth.QueryParam("code_challenge_method"))

		reqToken := newDummyRequest()
		mod.WithPKCEVerifier(verifier).Apply(reqToken)
		assert.Equal(t, verifier, reqToken.QueryParam("code_verifier"))
	})

	t.Run("dynamic_header", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()
		token := "initial-token"

		headerMod := mod.WithDynamicHeader("X-Short-Lived-Token", func() string {
			return token
		})

		headerMod.Apply(req)
		assert.Equal(t, "initial-token", req.Header("X-Short-Lived-Token"))

		token = "refreshed-token"

		headerMod.Apply(req)
		assert.Equal(t, "refreshed-token", req.Header("X-Short-Lived-Token"))
	})

	t.Run("cookies_modifiers", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()

		mod.WithCookie(&http.Cookie{Name: "session", Value: "sess123"}).Apply(req)
		mod.WithCookies(map[string]string{"theme": "dark", "lang": "en"}).Apply(req)

		cookieValues := req.HTTPRequest().Header.Values("Cookie")
		combined := strings.Join(cookieValues, "; ")

		assert.Contains(t, combined, "session=sess123")
		assert.Contains(t, combined, "theme=dark")
		assert.Contains(t, combined, "lang=en")
	})

	t.Run("conditional_headers", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()

		mod.WithIfMatch(`"etag123"`).Apply(req)
		mod.WithIfNoneMatch(`"etag456"`).Apply(req)

		now := time.Now().UTC().Truncate(time.Second)
		mod.WithIfModifiedSince(now).Apply(req)

		assert.Equal(t, `"etag123"`, req.Header("If-Match"))
		assert.Equal(t, `"etag456"`, req.Header("If-None-Match"))
		assert.Equal(t, now.Format(http.TimeFormat), req.Header("If-Modified-Since"))
	})
}

func TestMod_BodyAndPayloadModifiers(t *testing.T) {
	t.Parallel()

	t.Run("json_body", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()
		user := dummyUser{Name: "Alice", Email: "alice@example.com"}

		mod.WithJSONBody(user).Apply(req)

		assert.Equal(t, "application/json", req.Header("Content-Type"))
		assert.JSONEq(t, `{"name":"Alice","email":"alice@example.com"}`, string(req.BodyBytes()))
	})

	t.Run("form_values_and_form_body", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()
		vals := url.Values{}
		vals.Set("foo", "bar")
		vals.Add("foo", "baz")

		mod.WithFormValues(vals).Apply(req)

		assert.Equal(t, "application/x-www-form-urlencoded", req.Header("Content-Type"))
		assert.Equal(t, "foo=bar&foo=baz", string(req.BodyBytes()))
	})

	t.Run("proto_and_grpc_web_body", func(t *testing.T) {
		t.Parallel()

		msg := wrapperspb.String("protobuf_test_payload")

		// Proto body
		req1 := newDummyRequest()
		mod.WithProtoBody(msg).Apply(req1)

		assert.Equal(t, "application/x-protobuf", req1.Header("Content-Type"))
		assert.NotEmpty(t, req1.BodyBytes())

		// gRPC-Web body
		req2 := newDummyRequest()
		mod.WithGRPCWebBody(msg).Apply(req2)

		assert.Equal(t, "application/grpc-web+proto", req2.Header("Content-Type"))
		assert.Equal(t, "1", req2.Header("X-Grpc-Web"))
		assert.True(t, len(req2.BodyBytes()) >= 5)
		assert.Equal(t, byte(0x00), req2.BodyBytes()[0]) // 5-byte header prefix
	})

	t.Run("multipart_fields", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()
		fields := []mod.MultipartField{
			{Name: "username", Value: "administrator"},
			{Name: "role", Value: "admin"},
		}

		mod.WithMultipartFields(fields).Apply(req)

		contentType := req.Header("Content-Type")
		assert.Contains(t, contentType, "multipart/form-data")
		assert.NotEmpty(t, req.BodyBytes())
	})
}

func TestMod_ProtocolAndNetworkModifiers(t *testing.T) {
	t.Parallel()

	t.Run("force_http_versions_and_alpn", func(t *testing.T) {
		t.Parallel()

		req1 := newDummyRequest()
		mod.WithForceHTTP2().Apply(req1)

		cfg1 := aoni.GetRequestConfig(req1.Context())
		require.NotNil(t, cfg1)
		require.NotEmpty(t, cfg1.ALPNOverride)
		assert.Equal(t, aoni.AlpnH2, cfg1.ALPNOverride[0])

		req2 := newDummyRequest()
		mod.WithForceHTTP3().Apply(req2)

		cfg2 := aoni.GetRequestConfig(req2.Context())
		require.NotNil(t, cfg2)
		require.NotEmpty(t, cfg2.ALPNOverride)
		assert.Equal(t, aoni.AlpnH3, cfg2.ALPNOverride[0])
	})

	t.Run("ordered_headers", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()
		order := []string{":method", ":authority", ":scheme", ":path", "user-agent"}

		mod.WithOrderedHeaders(order).Apply(req)

		cfg := aoni.GetRequestConfig(req.Context())
		require.NotNil(t, cfg)
		assert.Equal(t, order, cfg.OrderedHeaders)
	})

	t.Run("network_and_security_flags", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()

		mod.WithProxyOverride("http://proxy.internal:8080").Apply(req)
		mod.WithSSRFGuard().Apply(req)
		mod.WithInsecureSkipVerify().Apply(req)
		mod.WithProxyDNS().Apply(req)

		cfg := aoni.GetRequestConfig(req.Context())
		require.NotNil(t, cfg)

		require.NotNil(t, cfg.ProxyAddr)
		assert.Equal(t, "proxy.internal:8080", cfg.ProxyAddr.Host)
		assert.True(t, cfg.SSRFGuard)
		assert.True(t, cfg.InsecureSkipVerify)
		assert.True(t, cfg.ProxyDNS)
	})

	t.Run("with_network", func(t *testing.T) {
		t.Parallel()

		req1 := newDummyRequest()
		mod.WithNetwork(aoni.NetworkUnix).Apply(req1)

		cfg1 := aoni.GetRequestConfig(req1.Context())
		require.NotNil(t, cfg1)
		assert.Equal(t, "unix", cfg1.Network)

		req2 := newDummyRequest()
		mod.WithNetworkString("tcp4").Apply(req2)

		cfg2 := aoni.GetRequestConfig(req2.Context())
		require.NotNil(t, cfg2)
		assert.Equal(t, "tcp4", cfg2.Network)
	})
}

func TestMod_TelemetryAndTracingModifiers(t *testing.T) {
	t.Parallel()

	t.Run("correlation_id_and_label", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()

		mod.WithCorrelationID("corr_abc123").Apply(req)
		mod.WithLabel("user-login-route").Apply(req)

		assert.Equal(t, "corr_abc123", req.Header("X-Correlation-ID"))

		cfg := aoni.GetRequestConfig(req.Context())
		require.NotNil(t, cfg)
		assert.Equal(t, "user-login-route", cfg.Label)
	})

	t.Run("debug_flag_and_trace_context", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()

		mod.WithDebug().Apply(req)
		mod.WithTraceContext().Apply(req)

		cfg := aoni.GetRequestConfig(req.Context())
		require.NotNil(t, cfg)
		assert.True(t, cfg.Debug)
		assert.NotNil(t, cfg.TraceInfo)
	})
}

func TestMod_SmartBody_And_Retry(t *testing.T) {
	t.Parallel()

	t.Run("with_smart_body_types", func(t *testing.T) {
		t.Parallel()

		// 1. Struct -> JSON
		req1 := newDummyRequest()
		mod.WithSmartBody(dummyUser{Name: "Steve"}).Apply(req1)
		assert.Equal(t, "application/json", req1.Header("Content-Type"))
		assert.Contains(t, string(req1.body), `"name":"Steve"`)

		// 2. String -> text/plain
		req2 := newDummyRequest()
		mod.WithSmartBody("hello world").Apply(req2)
		assert.Equal(t, "text/plain; charset=utf-8", req2.Header("Content-Type"))
		assert.Equal(t, "hello world", string(req2.body))

		// 3. Bytes -> raw bytes
		req3 := newDummyRequest()
		mod.WithSmartBody([]byte{0x01, 0x02, 0x03}).Apply(req3)
		assert.Equal(t, []byte{0x01, 0x02, 0x03}, req3.body)

		// 4. url.Values -> form urlencoded
		req4 := newDummyRequest()
		vals := url.Values{"query": []string{"apple"}}
		mod.WithSmartBody(vals).Apply(req4)
		assert.Equal(t, "application/x-www-form-urlencoded", req4.Header("Content-Type"))
		assert.Equal(t, "query=apple", string(req4.body))
	})

	t.Run("with_retry_and_json_alias", func(t *testing.T) {
		t.Parallel()

		req := newDummyRequest()
		mod.WithRetry(3).Apply(req)
		mod.WithJSON(dummyUser{Name: "Woz"}).Apply(req)

		cfg := aoni.GetRequestConfig(req.Context())
		require.NotNil(t, cfg)
		require.NotNil(t, cfg.RetryPolicy)
		assert.Equal(t, 3, cfg.RetryPolicy.MaxAttempts)
		assert.Contains(t, string(req.body), `"name":"Woz"`)
	})
}

func TestMod_WebSocketModifiers(t *testing.T) {
	t.Parallel()

	// RFC 6455 §11.3.4 & RFC 8441 §5: Sec-WebSocket-Protocol
	req1 := newDummyRequest()
	mod.WithSecWebSocketProtocol("chat", "superchat").Apply(req1)
	assert.Equal(t, "chat, superchat", req1.Header("Sec-WebSocket-Protocol"))

	// RFC 6455 §11.3.2 & RFC 8441 §5: Sec-WebSocket-Extensions
	req2 := newDummyRequest()
	mod.WithSecWebSocketExtensions("permessage-deflate", "client_max_window_bits").Apply(req2)
	assert.Equal(t, "permessage-deflate, client_max_window_bits", req2.Header("Sec-WebSocket-Extensions"))

	// RFC 6455 §11.3.5 & RFC 8441 §5: Sec-WebSocket-Version
	req3 := newDummyRequest()
	mod.WithSecWebSocketVersion("13").Apply(req3)
	assert.Equal(t, "13", req3.Header("Sec-WebSocket-Version"))

	// RFC 7692 §7 & RFC 8441 §5: WithPermessageDeflate defaults
	req4 := newDummyRequest()
	mod.WithPermessageDeflate().Apply(req4)
	assert.Equal(t, "permessage-deflate; client_max_window_bits", req4.Header("Sec-WebSocket-Extensions"))

	// RFC 7692 §7 & RFC 8441 §5: WithPermessageDeflate with custom params
	req5 := newDummyRequest()
	mod.WithPermessageDeflate("server_no_context_takeover", "client_no_context_takeover").Apply(req5)
	assert.Equal(
		t,
		"permessage-deflate; server_no_context_takeover; client_no_context_takeover",
		req5.Header("Sec-WebSocket-Extensions"),
	)
}

func TestMod_HPKP(t *testing.T) {
	t.Parallel()

	req1 := newDummyRequest()
	hpkpHeader := `max-age=3000; pin-sha256="d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM="`
	mod.WithPublicKeyPins(hpkpHeader).Apply(req1)
	assert.Equal(t, hpkpHeader, req1.Header("Public-Key-Pins"))

	req2 := newDummyRequest()
	hpkpROHeader := `pin-sha256="d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM="; report-uri="https://example.com/pkp"`
	mod.WithPublicKeyPinsReportOnly(hpkpROHeader).Apply(req2)
	assert.Equal(t, hpkpROHeader, req2.Header("Public-Key-Pins-Report-Only"))

	req3 := newDummyRequest()
	mod.WithSPKIPin("example.com", "d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM=").Apply(req3)
	reqCfg := aoni.GetRequestConfig(req3.Context())
	require.NotNil(t, reqCfg)
	assert.Contains(t, reqCfg.CertificatePins["example.com"], "d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM=")
}
