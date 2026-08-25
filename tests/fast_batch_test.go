// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lemon4ksan/foundation/borrow"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/fluent"
	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
)

func TestFast_DoBatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Batch-ID", id)
		_, _ = fmt.Fprintf(w, `{"id":%s,"status":"batch_ok"}`, id)
	}))
	defer ts.Close()

	client := fast.NewClient()
	defer client.Close()

	const batchSize = 25
	reqs := make([]*fast.Request, batchSize)
	resps := make([]*fast.Response, batchSize)

	for i := range batchSize {
		req := fast.NewRequest(nil)
		req.SetMethod(http.MethodGet)
		req.SetURL(fmt.Sprintf("%s/items?id=%d", ts.URL, i))
		reqs[i] = req
		resps[i] = fast.NewResponse(nil)
	}

	defer func() {
		for i := range batchSize {
			reqs[i].Release()
			resps[i].Release()
		}
	}()

	err := client.DoBatch(context.Background(), reqs, resps)
	require.NoError(t, err)

	for i := range batchSize {
		require.Equal(t, http.StatusOK, resps[i].StatusCode())
		require.Equal(t, fmt.Sprintf("%d", i), resps[i].Header("X-Batch-ID"))
		require.Contains(t, string(resps[i].BodyBytes()), fmt.Sprintf(`"id":%d`, i))
	}
}

func TestFast_DoBatchScoped(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":%s,"scoped":true}`, id)
	}))
	defer ts.Close()

	client := fast.NewClient()
	defer client.Close()

	const batchSize = 10
	reqs := make([]*fast.Request, batchSize)
	for i := range batchSize {
		req := fast.NewRequest(nil)
		req.SetMethod(http.MethodGet)
		req.SetURL(fmt.Sprintf("%s/scoped?id=%d", ts.URL, i))
		reqs[i] = req
	}

	defer func() {
		for i := range batchSize {
			reqs[i].Release()
		}
	}()

	called := false
	err := client.DoBatchScoped(context.Background(), reqs, func(scope *borrow.Scope, resps []*fast.Response) error {
		called = true
		require.Len(t, resps, batchSize)
		for i := range batchSize {
			require.Equal(t, http.StatusOK, resps[i].StatusCode())
			require.Contains(t, string(resps[i].BodyBytes()), fmt.Sprintf(`"id":%d`, i))
		}
		return nil
	})

	require.NoError(t, err)
	require.True(t, called)
}

func TestFluent_BatchGetTo(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":%s,"active":true}`, id)
	}))
	defer ts.Close()

	type Item struct {
		ID     int  `json:"id"`
		Active bool `json:"active"`
	}

	paths := make([]string, 10)
	for i := range 10 {
		paths[i] = fmt.Sprintf("%s/item?id=%d", ts.URL, i)
	}

	items, err := fluent.BatchGetTo[Item](context.Background(), nil, paths)
	require.NoError(t, err)
	require.Len(t, items, 10)
	for i := range 10 {
		require.Equal(t, i, items[i].ID)
		require.True(t, items[i].Active)
	}
}

func BenchmarkFast_Batch50(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("BATCH_OK"))
	}))
	defer ts.Close()

	client := fast.NewClient()
	defer client.Close()

	const batchSize = 50
	reqs := make([]*fast.Request, batchSize)
	resps := make([]*fast.Response, batchSize)

	for i := range batchSize {
		req := fast.NewRequest(nil)
		req.SetMethod(http.MethodGet)
		req.SetURL(fmt.Sprintf("%s/test?i=%d", ts.URL, i))
		reqs[i] = req
		resps[i] = fast.NewResponse(nil)
	}

	defer func() {
		for i := range batchSize {
			reqs[i].Release()
			resps[i].Release()
		}
	}()

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		if err := client.DoBatch(context.Background(), reqs, resps); err != nil {
			b.Fatalf("batch error: %v", err)
		}
	}
}

type nullConn struct {
	net.Conn
}

func (n *nullConn) Write(b []byte) (int, error) {
	return len(b), nil
}

func BenchmarkRequest_WriteBuffered(b *testing.B) {
	req := h1engine.AcquireRequest()
	req.Header.SetMethod("POST")
	req.SetRequestURI("http://localhost:8080/api/v1/update")
	req.Header.SetContentType("application/json")
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9")
	req.SetBody([]byte(`{"user_id":12345,"status":"active","payload":"simd_zero_copy_payload"}`))
	defer h1engine.ReleaseRequest(req)

	nc := &nullConn{}
	bw := bufio.NewWriter(nc)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		if err := req.Write(bw); err != nil {
			b.Fatal(err)
		}
		if err := bw.Flush(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRequest_WriteVectored(b *testing.B) {
	req := h1engine.AcquireRequest()
	req.Header.SetMethod("POST")
	req.SetRequestURI("http://localhost:8080/api/v1/update")
	req.Header.SetContentType("application/json")
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9")
	req.SetBody([]byte(`{"user_id":12345,"status":"active","payload":"simd_zero_copy_payload"}`))
	defer h1engine.ReleaseRequest(req)

	nc := &nullConn{}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		if err := req.WriteVectored(nc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFluent_BatchGetTo50(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":100,"active":true}`))
	}))
	defer ts.Close()

	type Item struct {
		ID     int  `json:"id"`
		Active bool `json:"active"`
	}

	paths := make([]string, 50)
	for i := range 50 {
		paths[i] = fmt.Sprintf("%s/item?id=%d", ts.URL, i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		items, err := fluent.BatchGetTo[Item](context.Background(), nil, paths)
		if err != nil || len(items) != 50 {
			b.Fatalf("batch error: %v", err)
		}
	}
}

func BenchmarkFluent_SerialGetTo50(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":100,"active":true}`))
	}))
	defer ts.Close()

	type Item struct {
		ID     int  `json:"id"`
		Active bool `json:"active"`
	}

	url := fmt.Sprintf("%s/item", ts.URL)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		for range 50 {
			_, resp, err := fluent.GetTo[Item](context.Background(), nil, url)
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			if err != nil {
				b.Fatalf("serial error: %v", err)
			}
		}
	}
}
