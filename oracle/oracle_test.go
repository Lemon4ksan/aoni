// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package oracle_test

import (
	"context"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/oracle"
)

type dummyReq struct {
	headers map[string]string
	ctx     context.Context
}

func newDummyReq() *dummyReq {
	return &dummyReq{
		headers: make(map[string]string),
		ctx:     context.Background(),
	}
}

func (r *dummyReq) Context() context.Context       { return r.ctx }
func (r *dummyReq) SetContext(ctx context.Context) { r.ctx = ctx }
func (r *dummyReq) Method() string                 { return "GET" }
func (r *dummyReq) SetMethod(m string)             {}
func (r *dummyReq) SetMethodBytes(m []byte)        {}
func (r *dummyReq) URL() string                    { return "http://example.com" }
func (r *dummyReq) SetURL(u string)                {}
func (r *dummyReq) SetURIBytes(u []byte)           {}
func (r *dummyReq) Path() string                   { return "/" }
func (r *dummyReq) SetPath(p string)               {}
func (r *dummyReq) RawQuery() string               { return "" }
func (r *dummyReq) SetRawQuery(q string)           {}
func (r *dummyReq) SetRawQueryBytes(q []byte)      {}
func (r *dummyReq) AddQueryParam(k, v string)      {}
func (r *dummyReq) AddQueryParamBytes(k, v []byte) {}
func (r *dummyReq) SetQueryParam(k, v string)      {}
func (r *dummyReq) SetQueryParamBytes(k, v []byte) {}
func (r *dummyReq) QueryParam(key string) string   { return "" }
func (r *dummyReq) Header(key string) string       { return r.headers[key] }
func (r *dummyReq) HeaderBytes(key []byte) []byte  { return []byte(r.headers[string(key)]) }
func (r *dummyReq) SetHeader(key, val string)      { r.headers[key] = val }
func (r *dummyReq) SetHeaderBytes(key, val []byte) { r.headers[string(key)] = string(val) }
func (r *dummyReq) AddHeader(key, val string)      { r.headers[key] = val }
func (r *dummyReq) AddHeaderBytes(key, val []byte) { r.headers[string(key)] = string(val) }
func (r *dummyReq) DelHeader(key string)           { delete(r.headers, key) }
func (r *dummyReq) DelHeaderBytes(key []byte)      { delete(r.headers, string(key)) }
func (r *dummyReq) ResetHeaders()                  { r.headers = make(map[string]string) }
func (r *dummyReq) Headers() iter.Seq2[[]byte, []byte] {
	return func(yield func([]byte, []byte) bool) {
		for k, v := range r.headers {
			if !yield([]byte(k), []byte(v)) {
				return
			}
		}
	}
}
func (r *dummyReq) SetBodyBytes(b []byte)                {}
func (r *dummyReq) BodyBytes() []byte                    { return nil }
func (r *dummyReq) SetBodyStream(rdr io.Reader, _ int64) {}
func (r *dummyReq) BodyStream() io.Reader                { return nil }
func (r *dummyReq) EngineRequest() any                   { return nil }
func (r *dummyReq) HTTPRequest() *http.Request           { return nil }

func TestOracleClient_MockServer(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			_ = json.NewEncoder(w).Encode(oracle.StatusResponse{
				Status: "ok",
				Ready:  true,
			})

		case "/init":
			_ = json.NewEncoder(w).Encode(oracle.InitResponse{
				Status: "initialized",
			})

		case "/token":
			_ = json.NewEncoder(w).Encode(oracle.TokenResponse{
				Status:  "ok",
				Token:   "attestation-jwt-12345",
				Cookies: "session=xyz",
				Headers: map[string]string{
					"X-Attestation-Signature": "sig-abc",
					"Host":                    "should-be-ignored.com",
					"Content-Length":          "9999",
				},
			})

		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := oracle.NewClient(ts.URL)
	assert.Equal(t, ts.URL, c.BaseURL())

	ctx := context.Background()

	// 1. Status
	st, err := c.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, "ok", st.Status)
	assert.True(t, st.Ready)

	// 2. Init
	err = c.Init(ctx, "cookie=123")
	require.NoError(t, err)

	// 3. GetToken
	tok, err := c.GetToken(ctx, "test prompt")
	require.NoError(t, err)
	assert.Equal(t, "attestation-jwt-12345", tok.Token)
	assert.Equal(t, "session=xyz", tok.Cookies)

	// 4. WithOracle Modifier
	mod := oracle.WithOracle(c, "prompt", "X-Custom-Token")
	req := newDummyReq()
	mod.Apply(req)

	assert.Equal(t, "attestation-jwt-12345", req.Header("X-Custom-Token"))
	assert.Equal(t, "sig-abc", req.Header("X-Attestation-Signature"))
	assert.Equal(t, "session=xyz", req.Header("Cookie"))
	assert.Empty(t, req.Header("Host"))
	assert.Empty(t, req.Header("Content-Length"))

	// Nil client modifier
	nilMod := oracle.WithOracle(nil, "", "")
	nilMod.Apply(req)
}

func TestOracleSupervisor(t *testing.T) {
	t.Parallel()

	c := oracle.NewClient("http://127.0.0.1:64055")
	s := oracle.NewSupervisor(c, "sidecar/server.js")
	require.NotNil(t, s)

	// Stop when not started is safe
	s.Stop()
}
