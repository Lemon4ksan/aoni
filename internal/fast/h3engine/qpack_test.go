// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3engine

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
	"github.com/lemon4ksan/aoni/internal/qpack"
)

func TestQPACKEncodeRequestHeaders(t *testing.T) {
	t.Parallel()

	codec := NewQPACKCodec()

	req := h1engine.AcquireRequest()
	defer h1engine.ReleaseRequest(req)

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
		if errors.Is(err, io.EOF) {
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
	t.Parallel()

	codec := NewQPACKCodec()

	req := h1engine.AcquireRequest()
	defer h1engine.ReleaseRequest(req)

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
		if errors.Is(err, io.EOF) {
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
	t.Parallel()

	codec := NewQPACKCodec()

	var buf bytes.Buffer

	enc := qpack.NewEncoder(&buf)

	_ = enc.WriteField(qpack.HeaderField{Name: ":status", Value: "201"})
	_ = enc.WriteField(qpack.HeaderField{Name: "content-type", Value: "application/json"})
	_ = enc.WriteField(qpack.HeaderField{Name: "content-length", Value: "128"})

	var respHeader h1engine.ResponseHeader

	if _, err := codec.DecodeResponseHeaders(buf.Bytes(), &respHeader); err != nil {
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
	t.Parallel()

	codec := NewQPACKCodec()

	var buf bytes.Buffer

	enc := qpack.NewEncoder(&buf)

	_ = enc.WriteField(qpack.HeaderField{Name: "content-type", Value: "text/plain"})

	var respHeader h1engine.ResponseHeader

	_, err := codec.DecodeResponseHeaders(buf.Bytes(), &respHeader)
	if !errors.Is(err, ErrMissingStatusHeader) {
		t.Fatalf("expected ErrMissingStatusHeader, got %v", err)
	}
}

func TestQPACKEncodeExtendedCONNECTProtocolHeader(t *testing.T) {
	t.Parallel()

	codec := NewQPACKCodec()

	req := h1engine.AcquireRequest()
	defer h1engine.ReleaseRequest(req)

	req.Header.SetMethod("CONNECT")
	req.SetRequestURI("https://example.com/ws")
	req.Header.Set(":protocol", "websocket")
	req.Header.Set("Sec-WebSocket-Protocol", "chat.v1")

	var buf bytes.Buffer

	err := codec.EncodeRequestHeaders(&buf, req, nil)
	require.NoError(t, err)

	dec := qpack.NewDecoder()
	decodeFn := dec.Decode(buf.Bytes())

	decodedMap := make(map[string]string)
	for {
		hf, err := decodeFn()
		if errors.Is(err, io.EOF) {
			break
		}

		require.NoError(t, err)

		decodedMap[hf.Name] = hf.Value
	}

	assert.Equal(t, "CONNECT", decodedMap[":method"])
	assert.Equal(t, "websocket", decodedMap[":protocol"])
	assert.Equal(t, "chat.v1", decodedMap["sec-websocket-protocol"])
}

func TestIsForbiddenH3Header(t *testing.T) {
	t.Parallel()

	forbidden := []string{
		"connection",
		"keep-alive",
		"proxy-connection",
		"transfer-encoding",
		"upgrade",
		"sec-websocket-key",
		"sec-websocket-accept",
		"CONNECTION",
		"Sec-WebSocket-Key",
	}

	for _, h := range forbidden {
		assert.True(
			t,
			isForbiddenH3Header([]byte(h), []byte("val")),
			"isForbiddenH3Header byte slice should return true for %s",
			h,
		)
		assert.True(
			t,
			isForbiddenH3HeaderStr(h, []byte("val")),
			"isForbiddenH3HeaderStr string should return true for %s",
			h,
		)
	}

	allowed := []string{
		"authorization",
		"user-agent",
		"sec-websocket-protocol",
		"sec-websocket-version",
		"x-custom-header",
	}

	for _, h := range allowed {
		assert.False(
			t,
			isForbiddenH3Header([]byte(h), []byte("val")),
			"isForbiddenH3Header byte slice should return false for %s",
			h,
		)
		assert.False(
			t,
			isForbiddenH3HeaderStr(h, []byte("val")),
			"isForbiddenH3HeaderStr string should return false for %s",
			h,
		)
	}
}

func TestQPACKForbiddenHeadersFilteringInEncode(t *testing.T) {
	t.Parallel()

	codec := NewQPACKCodec()

	req := h1engine.AcquireRequest()
	defer h1engine.ReleaseRequest(req)

	req.Header.SetMethod("GET")
	req.SetRequestURI("https://example.com/ws")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Sec-WebSocket-Accept", "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=")
	req.Header.Set("User-Agent", "aoni-test")

	var buf bytes.Buffer

	err := codec.EncodeRequestHeaders(&buf, req, nil)
	require.NoError(t, err)

	dec := qpack.NewDecoder()
	decodeFn := dec.Decode(buf.Bytes())

	decodedMap := make(map[string]string)
	for {
		hf, err := decodeFn()
		if errors.Is(err, io.EOF) {
			break
		}

		require.NoError(t, err)

		decodedMap[hf.Name] = hf.Value
	}

	assert.Equal(t, "aoni-test", decodedMap["user-agent"])
	_, hasUpgrade := decodedMap["upgrade"]
	assert.False(t, hasUpgrade, "upgrade header should be filtered out")

	_, hasConn := decodedMap["connection"]
	assert.False(t, hasConn, "connection header should be filtered out")

	_, hasKey := decodedMap["sec-websocket-key"]
	assert.False(t, hasKey, "sec-websocket-key header should be filtered out")

	_, hasAccept := decodedMap["sec-websocket-accept"]
	assert.False(t, hasAccept, "sec-websocket-accept header should be filtered out")
}

func TestRFC9204AppendixBExamples(t *testing.T) {
	t.Parallel()

	// RFC 9204 Appendix B.1: Literal Field Line with Static Name Reference
	// Static table index 0 is ":authority".
	// Encoded field section prefix: Required Insert Count = 0, Sign = 0, Delta Base = 0 -> 0x00, 0x00
	// Literal with static name reference (RFC 9204 §4.5.4): '01' | 'N'=0 | 'T'=1 | Index=0 -> 0x50
	// String literal for value "www.example.com" without Huffman: length 15 (0x0f), "www.example.com"
	rawBlock := []byte{
		0x00, 0x00, // Prefix: RIC=0, Base=0
		0x50, // 0101 0000: Literal with static name ref (index 0 = :authority)
		0x0f, // Length 15
		'w', 'w', 'w', '.', 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm',
		0xd1, // 1101 0001: Indexed static field line (RFC 9204 §4.5.2): T=1, index 17 = ":method: GET"
		0xd7, // 1101 0111: Indexed static field line (RFC 9204 §4.5.2): T=1, index 23 = ":scheme: https"
	}

	dec := qpack.NewDecoder()
	decodeFn := dec.Decode(rawBlock)

	fields := make(map[string]string)
	for {
		hf, err := decodeFn()
		if errors.Is(err, io.EOF) {
			break
		}

		require.NoError(t, err)

		fields[hf.Name] = hf.Value
	}

	assert.Equal(t, "www.example.com", fields[":authority"])
	assert.Equal(t, "GET", fields[":method"])
	assert.Equal(t, "https", fields[":scheme"])
}

func BenchmarkQPACKEncodeRequestHeaders(b *testing.B) {
	codec := NewQPACKCodec()

	req := h1engine.AcquireRequest()
	defer h1engine.ReleaseRequest(req)

	req.Header.SetMethod("CONNECT")
	req.SetRequestURI("https://example.com/ws")
	req.Header.Set(":protocol", "websocket")
	req.Header.Set("User-Agent", "aoni-h3-bench")
	req.Header.Set("Sec-WebSocket-Protocol", "chat.v1")

	var buf bytes.Buffer

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		buf.Reset()
		_ = codec.EncodeRequestHeaders(&buf, req, nil)
	}
}

func BenchmarkQPACKDecodeResponseHeaders(b *testing.B) {
	codec := NewQPACKCodec()

	var buf bytes.Buffer

	enc := qpack.NewEncoder(&buf)
	_ = enc.WriteField(qpack.HeaderField{Name: ":status", Value: "200"})
	_ = enc.WriteField(qpack.HeaderField{Name: "sec-websocket-version", Value: "13"})
	_ = enc.WriteField(qpack.HeaderField{Name: "sec-websocket-protocol", Value: "chat.v1"})
	encoded := buf.Bytes()

	var respHeader h1engine.ResponseHeader

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		respHeader.Reset()
		_, _ = codec.DecodeResponseHeaders(encoded, &respHeader)
	}
}
