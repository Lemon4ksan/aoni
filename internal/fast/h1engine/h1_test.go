// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h1engine

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/borrow"
)

func TestH1Engine_ClientDo(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "aoni-h1" {
			http.Error(w, "missing header", http.StatusBadRequest)
			return
		}
		w.Header().Set("X-Resp", "ok")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello from h1 server"))
	}))
	defer ts.Close()

	c := &Client{}
	req := AcquireRequest()
	resp := AcquireResponse()
	defer ReleaseRequest(req)
	defer ReleaseResponse(resp)

	req.SetRequestURI(ts.URL)
	req.Header.SetMethod("GET")
	req.Header.Set("X-Custom", "aoni-h1")

	if err := c.Do(req, resp); err != nil {
		t.Fatalf("c.Do failed: %v", err)
	}

	if resp.StatusCode() != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode())
	}
	if string(resp.Header.Peek("X-Resp")) != "ok" {
		t.Fatalf("expected header ok, got %s", string(resp.Header.Peek("X-Resp")))
	}
	if !bytes.Equal(resp.Body(), []byte("hello from h1 server")) {
		t.Fatalf("unexpected body: %s", string(resp.Body()))
	}
}

func TestH1Engine_URIAndArgs(t *testing.T) {
	u := AcquireURI()
	defer ReleaseURI(u)

	u.Parse(nil, []byte("https://example.com:8080/path/test?foo=bar&baz=123"))

	if string(u.Scheme()) != "https" {
		t.Fatalf("expected scheme https, got %s", u.Scheme())
	}
	if string(u.Host()) != "example.com:8080" {
		t.Fatalf("expected host example.com:8080, got %s", u.Host())
	}
	if string(u.Path()) != "/path/test" {
		t.Fatalf("expected path /path/test, got %s", u.Path())
	}
	if string(u.QueryArgs().Peek("foo")) != "bar" {
		t.Fatalf("expected foo=bar, got %s", u.QueryArgs().Peek("foo"))
	}
	if u.QueryArgs().GetUintOrZero("baz") != 123 {
		t.Fatalf("expected baz=123, got %d", u.QueryArgs().GetUintOrZero("baz"))
	}
}

func TestH1Engine_InmemoryListener(t *testing.T) {
	ln := NewInmemoryListener()
	defer ln.Close()

	ch := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			ch <- c
		}
	}()

	clientConn, err := ln.Dial()
	if err != nil {
		t.Fatalf("ln.Dial failed: %v", err)
	}
	defer clientConn.Close()

	select {
	case serverConn := <-ch:
		defer serverConn.Close()
		go func() {
			_, _ = serverConn.Write([]byte("ping"))
		}()
		buf := make([]byte, 4)
		_, err := clientConn.Read(buf)
		if err != nil || string(buf) != "ping" {
			t.Fatalf("inmemory connection read failed: %v, got %s", err, string(buf))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("inmemory listener timeout")
	}
}

var legacySyncPool = sync.Pool{
	New: func() any {
		return &Request{}
	},
}

func BenchmarkPool_LegacySyncPool_Parallel(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := legacySyncPool.Get().(*Request)
			req.Reset()
			legacySyncPool.Put(req)
		}
	})
}

func BenchmarkPool_PerPStorage_Parallel(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := AcquireRequest()
			ReleaseRequest(req)
		}
	})
}

func BenchmarkBorrow_Scoped(b *testing.B) {
	resp := AcquireResponse()
	defer ReleaseResponse(resp)
	resp.SetBodyString(`{"status":"ok","code":200,"message":"high-performance"}`)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = resp.ReadBodyScoped(func(body []byte) error {
			if len(body) == 0 {
				b.Fatal("empty body")
			}
			return nil
		})
	}
}

func BenchmarkBorrow_LegacyCloneCopy(b *testing.B) {
	resp := AcquireResponse()
	defer ReleaseResponse(resp)
	resp.SetBodyString(`{"status":"ok","code":200,"message":"high-performance"}`)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		raw := resp.Body()
		copied := append([]byte(nil), raw...)
		if len(copied) == 0 {
			b.Fatal("empty body")
		}
	}
}

var testHeaderRaw1KB = []byte("HTTP/1.1 200 OK\r\n" +
	"Date: Mon, 24 Aug 2026 12:00:00 GMT\r\n" +
	"Content-Type: application/json; charset=utf-8\r\n" +
	"Content-Length: 1024\r\n" +
	"Server: aoni-silicon/1.0\r\n" +
	"Connection: keep-alive\r\n" +
	"X-Frame-Options: SAMEORIGIN\r\n" +
	"X-Content-Type-Options: nosniff\r\n" +
	"X-XSS-Protection: 1; mode=block\r\n" +
	"Strict-Transport-Security: max-age=31536000; includeSubDomains; preload\r\n" +
	"Accept-Ranges: bytes\r\n" +
	"Cache-Control: public, max-age=3600, stale-while-revalidate=60\r\n" +
	"ETag: W/\"65a123-bc90f1\"\r\n" +
	"Vary: Accept-Encoding, User-Agent\r\n" +
	"Access-Control-Allow-Origin: *\r\n" +
	"Access-Control-Allow-Methods: GET, POST, OPTIONS, PUT, DELETE\r\n" +
	"Access-Control-Allow-Headers: Content-Type, Authorization, X-Requested-With\r\n\r\n")

func BenchmarkHeaderScanner_SIMD(b *testing.B) {
	b.SetBytes(int64(len(testHeaderRaw1KB)))
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var s headerScanner
		s.b = testHeaderRaw1KB
		for s.next() {
			if len(s.key) == 0 || len(s.value) == 0 {
				b.Fatal("empty kv")
			}
		}
	}
}

func BenchmarkHeaderParse_ResponseHeader_SIMD(b *testing.B) {
	b.SetBytes(int64(len(testHeaderRaw1KB)))
	b.ReportAllocs()
	b.ResetTimer()

	var h ResponseHeader
	for b.Loop() {
		h.Reset()
		if _, err := h.parse(testHeaderRaw1KB); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCookie_Scoped(b *testing.B) {
	scope := borrow.AcquireScope()
	defer scope.Release()

	var c Cookie
	c.SetKey("session_id")
	c.SetValue("xyz_987654321_secure_token")
	c.SetDomain("api.aoni.dev")
	c.SetPath("/v1/auth")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		k := c.KeyScoped(scope)
		v := c.ValueScoped(scope)
		d := c.DomainScoped(scope)
		p := c.PathScoped(scope)
		if len(k.AsSlice()) == 0 || len(v.AsSlice()) == 0 || len(d.AsSlice()) == 0 || len(p.AsSlice()) == 0 {
			b.Fatal("empty cookie field")
		}
	}
}

func BenchmarkCookie_LegacyAlloc(b *testing.B) {
	var c Cookie
	c.SetKey("session_id")
	c.SetValue("xyz_987654321_secure_token")
	c.SetDomain("api.aoni.dev")
	c.SetPath("/v1/auth")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		k := string(c.Key())
		v := string(c.Value())
		d := string(c.Domain())
		p := string(c.Path())
		if len(k) == 0 || len(v) == 0 || len(d) == 0 || len(p) == 0 {
			b.Fatal("empty cookie field")
		}
	}
}

func BenchmarkURI_Scoped(b *testing.B) {
	scope := borrow.AcquireScope()
	defer scope.Release()

	u := AcquireURI()
	defer ReleaseURI(u)
	u.Parse(nil, []byte("https://user:pass@api.aoni.dev:8443/v1/users/42/transactions?limit=50&offset=100#details"))

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		host := u.HostScoped(scope)
		scheme := u.SchemeScoped(scope)
		path := u.PathScoped(scope)
		query := u.QueryScoped(scope)
		full := u.FullURIScoped(scope)
		if len(host.AsSlice()) == 0 || len(scheme.AsSlice()) == 0 || len(path.AsSlice()) == 0 || len(query.AsSlice()) == 0 || len(full.AsSlice()) == 0 {
			b.Fatal("empty uri field")
		}
	}
}

func BenchmarkURI_LegacyAlloc(b *testing.B) {
	u := AcquireURI()
	defer ReleaseURI(u)
	u.Parse(nil, []byte("https://user:pass@api.aoni.dev:8443/v1/users/42/transactions?limit=50&offset=100#details"))

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		host := string(u.Host())
		scheme := string(u.Scheme())
		path := string(u.Path())
		query := string(u.QueryString())
		full := string(u.FullURI())
		if len(host) == 0 || len(scheme) == 0 || len(path) == 0 || len(query) == 0 || len(full) == 0 {
			b.Fatal("empty uri field")
		}
	}
}

func BenchmarkFullPipeline_ScopedBorrow(b *testing.B) {
	scope := borrow.AcquireScope()
	defer scope.Release()

	resp := AcquireResponse()
	defer ReleaseResponse(resp)
	if _, err := resp.Header.parse(testHeaderRaw1KB); err != nil {
		b.Fatal(err)
	}
	resp.SetBodyString(`{"id":42,"username":"admin","status":"authenticated","role":"superadmin"}`)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		contentType := resp.Header.PeekScoped(scope, "Content-Type")
		server := resp.Header.PeekScoped(scope, "Server")
		etag := resp.Header.PeekScoped(scope, "ETag")
		body := resp.BodyScoped(scope)

		if len(contentType.AsSlice()) == 0 || len(server.AsSlice()) == 0 || len(etag.AsSlice()) == 0 || len(body.AsSlice()) == 0 {
			b.Fatal("empty pipeline field")
		}
	}
}

func BenchmarkFullPipeline_LegacyCopy(b *testing.B) {
	resp := AcquireResponse()
	defer ReleaseResponse(resp)
	if _, err := resp.Header.parse(testHeaderRaw1KB); err != nil {
		b.Fatal(err)
	}
	resp.SetBodyString(`{"id":42,"username":"admin","status":"authenticated","role":"superadmin"}`)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		contentType := string(resp.Header.Peek("Content-Type"))
		server := string(resp.Header.Peek("Server"))
		etag := string(resp.Header.Peek("ETag"))
		body := append([]byte(nil), resp.Body()...)

		if len(contentType) == 0 || len(server) == 0 || len(etag) == 0 || len(body) == 0 {
			b.Fatal("empty pipeline field")
		}
	}
}
