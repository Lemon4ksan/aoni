// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3engine

import (
	"bytes"
	"io"
	"testing"

	"github.com/quic-go/qpack"
	"github.com/valyala/fasthttp"
)

func TestQPACKEncodeRequestHeaders(t *testing.T) {
	codec := NewQPACKCodec()

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetMethod("POST")
	req.SetRequestURI("https://api.example.com/v1/data")
	req.Header.Set("User-Agent", "aoni-h3-test")
	req.Header.Set("Content-Type", "application/json")

	var buf bytes.Buffer

	if err := codec.EncodeRequestHeaders(&buf, req, nil); err != nil {
		t.Fatalf("EncodeRequestHeaders failed: %v", err)
	}

	dec := qpack.NewDecoder()
	decodeFn := dec.Decode(buf.Bytes())

	decodedMap := make(map[string]string)

	for {
		hf, err := decodeFn()
		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatalf("qpack decode failed: %v", err)
		}

		decodedMap[hf.Name] = hf.Value
	}

	if decodedMap[":method"] != "POST" {
		t.Errorf("got :method %q, want POST", decodedMap[":method"])
	}

	if decodedMap[":scheme"] != "https" {
		t.Errorf("got :scheme %q, want https", decodedMap[":scheme"])
	}

	if decodedMap[":authority"] != "api.example.com" {
		t.Errorf("got :authority %q, want api.example.com", decodedMap[":authority"])
	}

	if decodedMap[":path"] != "/v1/data" {
		t.Errorf("got :path %q, want /v1/data", decodedMap[":path"])
	}

	if decodedMap["user-agent"] != "aoni-h3-test" {
		t.Errorf("got user-agent %q, want aoni-h3-test", decodedMap["user-agent"])
	}
}

func TestQPACKOrderedHeadersSequence(t *testing.T) {
	codec := NewQPACKCodec()

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetMethod("GET")
	req.SetRequestURI("https://example.com/test")

	req.Header.Set("x-header-c", "val-c")
	req.Header.Set("x-header-a", "val-a")
	req.Header.Set("x-header-b", "val-b")

	orderedKeys := []string{"x-header-a", "x-header-b", "x-header-c"}

	var buf bytes.Buffer

	if err := codec.EncodeRequestHeaders(&buf, req, orderedKeys); err != nil {
		t.Fatalf("EncodeRequestHeaders failed: %v", err)
	}

	dec := qpack.NewDecoder()
	decodeFn := dec.Decode(buf.Bytes())

	var capturedKeys []string

	for {
		hf, err := decodeFn()
		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatalf("qpack decode failed: %v", err)
		}

		if !hf.IsPseudo() {
			capturedKeys = append(capturedKeys, hf.Name)
		}
	}

	if len(capturedKeys) < 3 {
		t.Fatalf("expected at least 3 regular headers, got %d", len(capturedKeys))
	}

	if capturedKeys[0] != "x-header-a" || capturedKeys[1] != "x-header-b" || capturedKeys[2] != "x-header-c" {
		t.Fatalf("QPACK header order sequence violated: got %v, want %v", capturedKeys, orderedKeys)
	}
}

func TestQPACKDecodeResponseHeaders(t *testing.T) {
	codec := NewQPACKCodec()

	var buf bytes.Buffer

	enc := qpack.NewEncoder(&buf)

	_ = enc.WriteField(qpack.HeaderField{Name: ":status", Value: "201"})
	_ = enc.WriteField(qpack.HeaderField{Name: "content-type", Value: "application/json"})
	_ = enc.WriteField(qpack.HeaderField{Name: "content-length", Value: "128"})

	var respHeader fasthttp.ResponseHeader

	if err := codec.DecodeResponseHeaders(buf.Bytes(), &respHeader); err != nil {
		t.Fatalf("DecodeResponseHeaders failed: %v", err)
	}

	if respHeader.StatusCode() != 201 {
		t.Errorf("got status code %d, want 201", respHeader.StatusCode())
	}

	if string(respHeader.Peek("Content-Type")) != "application/json" {
		t.Errorf("got content-type %q, want application/json", respHeader.Peek("Content-Type"))
	}

	if respHeader.ContentLength() != 128 {
		t.Errorf("got content-length %d, want 128", respHeader.ContentLength())
	}
}

func TestQPACKDecodeResponseMissingStatus(t *testing.T) {
	codec := NewQPACKCodec()

	var buf bytes.Buffer

	enc := qpack.NewEncoder(&buf)

	_ = enc.WriteField(qpack.HeaderField{Name: "content-type", Value: "text/plain"})

	var respHeader fasthttp.ResponseHeader

	err := codec.DecodeResponseHeaders(buf.Bytes(), &respHeader)
	if err != ErrMissingStatusHeader {
		t.Fatalf("expected ErrMissingStatusHeader, got %v", err)
	}
}
