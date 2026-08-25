// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unsafe"
)

func TestBridgeLifecycle(t *testing.T) {
	if Version != "1.0.0-silicon" {
		t.Fatalf("expected version '1.0.0-silicon', got %s", Version)
	}

	cfg := Config{
		MaxConnsPerHost: 100,
		Concurrency:     1000,
		TimeoutMS:       5000,
		BrowserProfile:  AONIBrowserChrome,
	}

	client := NewClientFromConfig(&cfg)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestBridgeSingleRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Header") != "aoni-val" {
			http.Error(w, "missing header", http.StatusBadRequest)
			return
		}

		body, _ := io.ReadAll(r.Body)
		if string(body) != "hello from C" {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Resp", "awesome-fast-header")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","echo":"hello from C"}`))
	}))
	defer server.Close()

	client := NewClientFromConfig(nil)
	if client == nil {
		t.Fatal("failed to create client")
	}

	urlStr := server.URL
	methodStr := "POST"
	headersStr := "X-Test-Header: aoni-val\r\nContent-Type: text/plain\r\n"
	bodyStr := "hello from C"

	respBuf := make([]byte, 1024)
	respHdrs := make([]byte, 1024)

	task := Task{
		TaskID:         1,
		Method:         unsafe.StringData(methodStr),
		MethodLen:      uintptr(len(methodStr)),
		URL:            unsafe.StringData(urlStr),
		URLLen:         uintptr(len(urlStr)),
		HeadersRaw:     unsafe.StringData(headersStr),
		HeadersLen:     uintptr(len(headersStr)),
		BodyPtr:        unsafe.StringData(bodyStr),
		BodyLen:        uintptr(len(bodyStr)),
		RespBufPtr:     &respBuf[0],
		RespBufCap:     uintptr(len(respBuf)),
		RespHeadersPtr: &respHdrs[0],
		RespHeadersCap: uintptr(len(respHdrs)),
	}

	status := DoTask(client, &task)
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d (err_code=%d)", status, task.ErrorCode)
	}

	if task.ErrorCode != AONIOk {
		t.Fatalf("expected AONIOk, got %d", task.ErrorCode)
	}

	gotBody := string(respBuf[:task.RespBufLen])
	expectedBody := `{"status":"ok","echo":"hello from C"}`
	if gotBody != expectedBody {
		t.Fatalf("expected body %q, got %q", expectedBody, gotBody)
	}

	gotHdrs := string(respHdrs[:task.RespHeadersLen])
	if !strings.Contains(gotHdrs, "X-Custom-Resp: awesome-fast-header") {
		t.Fatalf("expected response headers to contain X-Custom-Resp, got %q", gotHdrs)
	}
}

func TestBridgeBatchRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "resp-%s", id)
	}))
	defer server.Close()

	client := NewClientFromConfig(nil)
	if client == nil {
		t.Fatal("failed to create client")
	}

	const numTasks = 100
	tasks := make([]Task, numTasks)
	buffers := make([][]byte, numTasks)
	urls := make([]string, numTasks)

	for i := 0; i < numTasks; i++ {
		buffers[i] = make([]byte, 256)
		urls[i] = fmt.Sprintf("%s/?id=%d", server.URL, i)

		tasks[i] = Task{
			TaskID:     uint64(i),
			URL:        unsafe.StringData(urls[i]),
			URLLen:     uintptr(len(urls[i])),
			RespBufPtr: &buffers[i][0],
			RespBufCap: uintptr(len(buffers[i])),
		}
	}

	DoBatchTasks(client, tasks)

	for i := 0; i < numTasks; i++ {
		if tasks[i].StatusCode != http.StatusOK {
			t.Fatalf("task %d: expected status 200, got %d (err: %d)", i, tasks[i].StatusCode, tasks[i].ErrorCode)
		}
		got := string(buffers[i][:tasks[i].RespBufLen])
		expected := fmt.Sprintf("resp-%d", i)
		if got != expected {
			t.Fatalf("task %d: expected body %q, got %q", i, expected, got)
		}
	}
}

func TestBridgeBufferOverflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("this is a very long response that will overflow small buffer"))
	}))
	defer server.Close()

	client := NewClientFromConfig(nil)
	if client == nil {
		t.Fatal("failed to create client")
	}

	smallBuf := make([]byte, 10)
	urlStr := server.URL

	task := Task{
		TaskID:     1,
		URL:        unsafe.StringData(urlStr),
		URLLen:     uintptr(len(urlStr)),
		RespBufPtr: &smallBuf[0],
		RespBufCap: uintptr(len(smallBuf)),
	}

	status := DoTask(client, &task)
	if status != AONIErrBufferOverflow {
		t.Fatalf("expected error code %d, got %d", AONIErrBufferOverflow, status)
	}

	if task.ErrorCode != AONIErrBufferOverflow {
		t.Fatalf("expected task ErrorCode %d, got %d", AONIErrBufferOverflow, task.ErrorCode)
	}

	if task.RespBufLen != 10 {
		t.Fatalf("expected 10 bytes copied, got %d", task.RespBufLen)
	}
}

func TestBytePtrToStringSafety(t *testing.T) {
	// 1. Normal string
	s := "http://127.0.0.1:8080\x00"
	res := bytePtrToString(unsafe.StringData(s))
	if res != "http://127.0.0.1:8080" {
		t.Fatalf("expected %q, got %q", "http://127.0.0.1:8080", res)
	}

	// 2. Nil pointer
	if bytePtrToString(nil) != "" {
		t.Fatal("expected empty string for nil pointer")
	}

	// 3. Unbounded string (exceeds maxSafeCStringLen)
	large := make([]byte, 5000)
	for i := range large {
		large[i] = 'a'
	}
	bounded := bytePtrToString(&large[0])
	if len(bounded) != maxSafeCStringLen {
		t.Fatalf("expected bounded length %d, got %d", maxSafeCStringLen, len(bounded))
	}
}
