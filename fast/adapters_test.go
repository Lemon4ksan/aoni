// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"testing"
)

func TestFastRequestAdapter(t *testing.T) {
	req := NewRequest(nil)

	req.SetMethod("POST")
	if req.Method() != "POST" {
		t.Errorf("got method %q, want POST", req.Method())
	}

	req.SetURL("https://example.com/api/test?foo=bar")

	if req.URL() != "https://example.com/api/test?foo=bar" {
		t.Errorf("got URL %q, want https://example.com/api/test?foo=bar", req.URL())
	}

	if req.Path() != "/api/test" {
		t.Errorf("got path %q, want /api/test", req.Path())
	}

	if req.RawQuery() != "foo=bar" {
		t.Errorf("got raw query %q, want foo=bar", req.RawQuery())
	}

	req.AddQueryParam("baz", "123")

	if req.RawQuery() != "foo=bar&baz=123" {
		t.Errorf("got updated raw query %q, want foo=bar&baz=123", req.RawQuery())
	}

	req.SetHeader("X-Custom", "val")

	if req.Header("X-Custom") != "val" {
		t.Errorf("got header %q, want val", req.Header("X-Custom"))
	}

	payload := []byte("hello world payload")

	req.SetBodyBytes(payload)

	if string(req.BodyBytes()) != string(payload) {
		t.Errorf("got body bytes %q, want %q", req.BodyBytes(), payload)
	}
}

func TestFastResponseAdapter(t *testing.T) {
	resp := NewResponse(nil)

	fastResp := resp.FastHTTPResponse()
	fastResp.SetStatusCode(201)
	fastResp.Header.Set("Content-Type", "application/json")
	fastResp.SetBodyString(`{"status":"created"}`)

	if resp.StatusCode() != 201 {
		t.Errorf("got status %d, want 201", resp.StatusCode())
	}

	if resp.Header("Content-Type") != "application/json" {
		t.Errorf("got content-type %q, want application/json", resp.Header("Content-Type"))
	}

	if string(resp.BodyBytes()) != `{"status":"created"}` {
		t.Errorf("got body %q, want {\"status\":\"created\"}", resp.BodyBytes())
	}
}
