// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/borrow"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/fluent"
	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
)

func TestFast_Pipeline(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", id)
		_, _ = fmt.Fprintf(w, `{"id":%s,"status":"ok"}`, id)
	}))
	defer ts.Close()

	client := fast.NewClient()
	defer client.Close()

	const batchSize = 20
	reqs := make([]*fast.Request, batchSize)
	resps := make([]*fast.Response, batchSize)

	for i := range batchSize {
		req := fast.NewRequest(nil)
		req.SetMethod(http.MethodGet)
		req.SetURL(fmt.Sprintf("%s/item?id=%d", ts.URL, i))
		reqs[i] = req
		resps[i] = fast.NewResponse(nil)
	}

	defer func() {
		for i := range batchSize {
			reqs[i].Release()
			resps[i].Release()
		}
	}()

	err := client.DoPipeline(context.Background(), reqs, resps)
	require.NoError(t, err)

	for i := range batchSize {
		require.Equal(t, http.StatusOK, resps[i].StatusCode())
		require.Equal(t, fmt.Sprintf("%d", i), resps[i].Header("X-Request-ID"))
		require.Contains(t, string(resps[i].BodyBytes()), fmt.Sprintf(`"id":%d`, i))
	}
}

func TestFast_ScopedBorrow_DoScoped(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"hello scoped"}`))
	}))
	defer ts.Close()

	client := fast.NewClient()
	defer client.Close()

	req := fast.NewRequest(nil)
	req.SetMethod(http.MethodGet)
	req.SetURL(ts.URL)
	defer req.Release()

	called := false
	err := client.DoScoped(context.Background(), req, func(scope *borrow.Scope, resp aoni.Response) error {
		called = true
		require.Equal(t, http.StatusOK, resp.StatusCode())
		body := resp.BodyBytes()
		borrowed := borrow.NewBytes(body, nil)
		require.Equal(t, `{"message":"hello scoped"}`, string(borrowed.AsSlice()))
		return nil
	})

	require.NoError(t, err)
	require.True(t, called)
}

func TestFluent_FetchScoped(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":42,"name":"scoped_user"}`))
	}))
	defer ts.Close()

	type User struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	called := false
	err := fluent.GetScoped[User](context.Background(), nil, ts.URL, func(scope *borrow.Scope, val User, resp *http.Response) error {
		called = true
		require.Equal(t, 42, val.ID)
		require.Equal(t, "scoped_user", val.Name)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		return nil
	})

	require.NoError(t, err)
	require.True(t, called)
}

func BenchmarkHTTP1_Pipelining_Batch50(b *testing.B) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("listen error: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					// Count how many requests in buffer
					reqCount := 0
					data := buf[:n]
					for len(data) > 0 {
						idx := bytes.IndexByte(data, '\n')
						if idx == -1 {
							break
						}
						line := data[:idx]
						data = data[idx+1:]
						if len(line) > 4 && string(line[:4]) == "GET " {
							reqCount++
						}
					}
					if reqCount == 0 {
						reqCount = 1
					}
					// Send back N responses
					respPayload := []byte("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 2\r\n\r\nOK")
					for range reqCount {
						if _, err := c.Write(respPayload); err != nil {
							return
						}
					}
				}
			}(conn)
		}
	}()

	addr := ln.Addr().String()
	hc := &h1engine.HostClient{
		Addr: addr,
	}

	const batchSize = 50
	reqs := make([]*h1engine.Request, batchSize)
	resps := make([]*h1engine.Response, batchSize)

	for i := range batchSize {
		req := h1engine.AcquireRequest()
		req.Header.SetMethod("GET")
		req.SetRequestURI(fmt.Sprintf("http://%s/test", addr))
		reqs[i] = req
		resps[i] = h1engine.AcquireResponse()
	}

	defer func() {
		for i := range batchSize {
			h1engine.ReleaseRequest(reqs[i])
			h1engine.ReleaseResponse(resps[i])
		}
	}()

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		if err := hc.DoPipeline(reqs, resps); err != nil {
			b.Fatalf("pipeline error: %v", err)
		}
	}
}

func BenchmarkHTTP1_Serial_Batch50(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("OK"))
	}))
	defer ts.Close()

	hc := &h1engine.HostClient{
		Addr: ts.Listener.Addr().String(),
	}

	req := h1engine.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI(ts.URL)
	resp := h1engine.AcquireResponse()
	defer h1engine.ReleaseRequest(req)
	defer h1engine.ReleaseResponse(resp)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		for range 50 {
			if err := hc.Do(req, resp); err != nil {
				b.Fatalf("serial error: %v", err)
			}
		}
	}
}
