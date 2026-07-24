// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStdClientBridge(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "expected POST", http.StatusMethodNotAllowed)
			return
		}

		body, _ := io.ReadAll(r.Body)

		w.WriteHeader(http.StatusOK)

		_, _ = fmt.Fprintf(w, "echo: %s", string(body))
	}))

	defer ts.Close()

	fastClient := NewClient()
	stdClient := NewStdClient(fastClient)

	resp, err := stdClient.Post(ts.URL, "text/plain", strings.NewReader("hello bridge"))
	if err != nil {
		t.Fatalf("stdClient.Post failed: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	if string(body) != "echo: hello bridge" {
		t.Fatalf("body mismatch: got %q, want %q", string(body), "echo: hello bridge")
	}
}

func TestStdClientNilURLError(t *testing.T) {
	fastClient := NewClient()
	stdClient := NewStdClient(fastClient)

	req, err := http.NewRequest("GET", "https://example.com", nil)
	if err != nil {
		t.Fatalf("http.NewRequest failed: %v", err)
	}

	req.URL = nil

	_, err = stdClient.Do(req)
	if err == nil {
		t.Fatalf("expected error when req.URL is nil")
	}
}
