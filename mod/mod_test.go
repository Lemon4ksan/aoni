// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod_test

import (
	"context"
	"io"
	"net/http"
	"testing"

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
}

func newDummyRequest() *dummyRequest {
	req, _ := http.NewRequest("GET", "http://example.com", nil)

	return &dummyRequest{
		httpReq: req,
		ctx:     context.Background(),
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
func (r *dummyRequest) AddQueryParam(k, v string)      {}
func (r *dummyRequest) AddQueryParamBytes(k, v []byte) {}
func (r *dummyRequest) SetQueryParam(k, v string)      {}
func (r *dummyRequest) SetQueryParamBytes(k, v []byte) {}
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
func (r *dummyRequest) DelHeader(key string)                  { r.httpReq.Header.Del(key) }
func (r *dummyRequest) DelHeaderBytes(key []byte)             { r.httpReq.Header.Del(string(key)) }
func (r *dummyRequest) ResetHeaders()                         { r.httpReq.Header = make(http.Header) }
func (r *dummyRequest) SetBodyBytes(b []byte)                 { r.body = b }
func (r *dummyRequest) BodyBytes() []byte                     { return r.body }
func (r *dummyRequest) SetBodyStream(rdr io.Reader, cl int64) {}
func (r *dummyRequest) BodyStream() io.Reader                 { return nil }
func (r *dummyRequest) SetBody(body io.Reader) error {
	if body != nil {
		r.body, _ = io.ReadAll(body)
	}

	return nil
}
func (r *dummyRequest) EngineRequest() any         { return r.httpReq }
func (r *dummyRequest) HTTPRequest() *http.Request { return r.httpReq }

var _ aoni.Request = (*dummyRequest)(nil)

func TestModifierBuilders(t *testing.T) {
	reqAdapter := newDummyRequest()

	mods := []aoni.RequestModifier{
		mod.WithHeader("X-Request-ID", "req-12345"),
		mod.WithBearer("secret-token-xyz"),
		mod.WithQuery("filter=active"),
		mod.WithForceHTTP2(),
	}

	for _, m := range mods {
		if m != nil {
			m(reqAdapter)
		}
	}

	if reqAdapter.Header("X-Request-ID") != "req-12345" {
		t.Errorf("got X-Request-ID %q, want req-12345", reqAdapter.Header("X-Request-ID"))
	}

	if reqAdapter.Header("Authorization") != "Bearer secret-token-xyz" {
		t.Errorf("got Auth %q, want Bearer secret-token-xyz", reqAdapter.Header("Authorization"))
	}

	ctx := reqAdapter.Context()

	reqCfg := aoni.GetRequestConfig(ctx)
	if reqCfg == nil || len(reqCfg.ALPNOverride) == 0 || reqCfg.ALPNOverride[0] != aoni.AlpnH2 {
		t.Errorf("expected ALPN h2 override in request config")
	}
}

func TestWithJSONBody(t *testing.T) {
	reqAdapter := newDummyRequest()

	user := dummyUser{Name: "Alice", Email: "alice@example.com"}
	mod.WithJSONBody(user)(reqAdapter)

	if reqAdapter.Header("Content-Type") != "application/json" {
		t.Errorf("got Content-Type %q, want application/json", reqAdapter.Header("Content-Type"))
	}

	if len(reqAdapter.body) == 0 {
		t.Errorf("expected non-empty JSON body")
	}
}

func TestWithForceHTTP3(t *testing.T) {
	ctx := context.Background()
	ctxH3 := aoni.WithContextModifier(ctx, mod.WithForceHTTP3())

	reqCfg := aoni.GetRequestConfig(ctxH3)
	if reqCfg == nil {
		t.Fatalf("expected non-nil RequestConfig in context")
	}

	reqAdapter := newDummyRequest()
	reqAdapter.SetContext(ctxH3)

	for _, m := range reqCfg.Modifiers {
		if m != nil {
			m(reqAdapter)
		}
	}

	activeCfg := aoni.GetRequestConfig(reqAdapter.Context())
	if activeCfg == nil || len(activeCfg.ALPNOverride) == 0 || activeCfg.ALPNOverride[0] != aoni.AlpnH3 {
		t.Errorf("expected ALPN h3 override in request config")
	}
}

func TestWithURL(t *testing.T) {
	reqAdapter := newDummyRequest()
	mod.WithURL("https://api.custom-target.com/v1/data")(reqAdapter)

	if reqAdapter.URL() != "https://api.custom-target.com/v1/data" {
		t.Errorf("got URL %q, want https://api.custom-target.com/v1/data", reqAdapter.URL())
	}
}

func TestWithDynamicHeader(t *testing.T) {
	reqAdapter := newDummyRequest()
	token := "initial-token"

	headerMod := mod.WithDynamicHeader("X-Short-Lived-Token", func() string {
		return token
	})

	headerMod(reqAdapter)

	if reqAdapter.Header("X-Short-Lived-Token") != "initial-token" {
		t.Errorf("got header %q, want initial-token", reqAdapter.Header("X-Short-Lived-Token"))
	}

	token = "refreshed-token"

	headerMod(reqAdapter)

	if reqAdapter.Header("X-Short-Lived-Token") != "refreshed-token" {
		t.Errorf("got header %q, want refreshed-token", reqAdapter.Header("X-Short-Lived-Token"))
	}
}
