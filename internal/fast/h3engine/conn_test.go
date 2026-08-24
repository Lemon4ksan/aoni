// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3engine

import (
	"bytes"
	"crypto/tls"
	"io"
	"testing"

	"github.com/lemon4ksan/foundation/borrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
	"github.com/lemon4ksan/aoni/internal/qpack"
	"github.com/lemon4ksan/aoni/internal/quic"
	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

func TestSendRequest_HeadersAndBody(t *testing.T) {
	t.Parallel()

	cc := &ClientConn{
		qpack: NewQPACKCodec(),
	}

	req := h1engine.AcquireRequest()
	defer h1engine.ReleaseRequest(req)

	req.Header.SetMethod("POST")
	req.SetRequestURI("https://api.example.com/upload")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("user-agent", "aoni-h3-test")
	req.SetBody([]byte(`{"test":true}`))

	var buf bytes.Buffer

	err := cc.sendRequestTo(&buf, req, nil)
	require.NoError(t, err)

	r := bytes.NewReader(buf.Bytes())

	// 1. HEADERS frame
	frameType, payloadLen, err := ReadFrameHeader(r)
	require.NoError(t, err)
	assert.Equal(t, FrameTypeHeaders, frameType)
	assert.Greater(t, payloadLen, uint64(0))

	headerBlock := make([]byte, payloadLen)
	_, err = io.ReadFull(r, headerBlock)
	require.NoError(t, err)

	dec := qpack.NewDecoder()
	decodeFn := dec.Decode(headerBlock)

	headers := make(map[string]string)
	for {
		hf, dErr := decodeFn()
		if dErr != nil {
			break
		}

		headers[hf.Name] = hf.Value
	}

	assert.Equal(t, "POST", headers[":method"])
	assert.Equal(t, "https", headers[":scheme"])
	assert.Equal(t, "api.example.com", headers[":authority"])
	assert.Equal(t, "/upload", headers[":path"])
	assert.Equal(t, "application/json", headers["content-type"])
	assert.Equal(t, "aoni-h3-test", headers["user-agent"])

	// 2. DATA frame
	frameType, payloadLen, err = ReadFrameHeader(r)
	require.NoError(t, err)
	assert.Equal(t, FrameTypeData, frameType)
	assert.Equal(t, uint64(len(`{"test":true}`)), payloadLen)

	bodyBytes := make([]byte, payloadLen)
	_, err = io.ReadFull(r, bodyBytes)
	require.NoError(t, err)
	assert.Equal(t, `{"test":true}`, string(bodyBytes))

	// No extra bytes remaining
	assert.Equal(t, 0, r.Len())
}

func TestSendRequest_HeadersOnly(t *testing.T) {
	t.Parallel()

	cc := &ClientConn{
		qpack: NewQPACKCodec(),
	}

	req := h1engine.AcquireRequest()
	defer h1engine.ReleaseRequest(req)

	req.Header.SetMethod("GET")
	req.SetRequestURI("https://example.com/status")

	var buf bytes.Buffer

	err := cc.sendRequestTo(&buf, req, nil)
	require.NoError(t, err)

	r := bytes.NewReader(buf.Bytes())

	frameType, payloadLen, err := ReadFrameHeader(r)
	require.NoError(t, err)
	assert.Equal(t, FrameTypeHeaders, frameType)

	headerBlock := make([]byte, payloadLen)
	_, err = io.ReadFull(r, headerBlock)
	require.NoError(t, err)

	// No DATA frame
	assert.Equal(t, 0, r.Len())
}

func TestReadResponse_Success(t *testing.T) {
	t.Parallel()

	cc := &ClientConn{
		qpack: NewQPACKCodec(),
	}

	// Prepare an H3 response stream: HEADERS frame (200 OK, Content-Type) + DATA frame ("response body")
	var streamBuf bytes.Buffer

	// Build QPACK headers block
	var qpackBuf bytes.Buffer

	enc := qpack.NewEncoder(&qpackBuf)
	_ = enc.WriteField(qpack.HeaderField{Name: ":status", Value: "200"})
	_ = enc.WriteField(qpack.HeaderField{Name: "content-type", Value: "application/json"})
	_ = enc.WriteField(qpack.HeaderField{Name: "server", Value: "aoni-h3-server"})

	hBlock := qpackBuf.Bytes()
	streamBuf.Write(appendHeadersHeader(nil, uint64(len(hBlock))))
	streamBuf.Write(hBlock)

	// Append DATA frame
	payload := []byte(`{"status":"ok","count":42}`)
	streamBuf.Write(appendDataHeader(nil, uint64(len(payload))))
	streamBuf.Write(payload)

	resp := h1engine.AcquireResponse()
	defer h1engine.ReleaseResponse(resp)

	trailers, err := cc.readResponseFrom(&streamBuf, resp)
	require.NoError(t, err)
	assert.Empty(t, trailers)

	assert.Equal(t, 200, resp.StatusCode())
	assert.Equal(t, "application/json", string(resp.Header.Peek("Content-Type")))
	assert.Equal(t, "aoni-h3-server", string(resp.Header.Peek("Server")))
	assert.Equal(t, `{"status":"ok","count":42}`, string(resp.Body()))
}

func TestReadResponse_MultiChunkData(t *testing.T) {
	t.Parallel()

	cc := &ClientConn{
		qpack: NewQPACKCodec(),
	}

	var streamBuf bytes.Buffer

	// HEADERS frame
	var qpackBuf bytes.Buffer

	enc := qpack.NewEncoder(&qpackBuf)
	_ = enc.WriteField(qpack.HeaderField{Name: ":status", Value: "206"})
	hBlock := qpackBuf.Bytes()
	streamBuf.Write(appendHeadersHeader(nil, uint64(len(hBlock))))
	streamBuf.Write(hBlock)

	// DATA frame 1
	chunk1 := []byte("first chunk; ")
	streamBuf.Write(appendDataHeader(nil, uint64(len(chunk1))))
	streamBuf.Write(chunk1)

	// DATA frame 2
	chunk2 := []byte("second chunk; ")
	streamBuf.Write(appendDataHeader(nil, uint64(len(chunk2))))
	streamBuf.Write(chunk2)

	// DATA frame 3
	chunk3 := []byte("final chunk.")
	streamBuf.Write(appendDataHeader(nil, uint64(len(chunk3))))
	streamBuf.Write(chunk3)

	resp := h1engine.AcquireResponse()
	defer h1engine.ReleaseResponse(resp)

	_, err := cc.readResponseFrom(&streamBuf, resp)
	require.NoError(t, err)

	assert.Equal(t, 206, resp.StatusCode())
	assert.Equal(t, "first chunk; second chunk; final chunk.", string(resp.Body()))
}

func TestReadResponse_WithTrailers(t *testing.T) {
	t.Parallel()

	cc := &ClientConn{
		qpack: NewQPACKCodec(),
	}

	var streamBuf bytes.Buffer

	// 1. HEADERS frame
	var qpackBuf bytes.Buffer

	enc := qpack.NewEncoder(&qpackBuf)
	_ = enc.WriteField(qpack.HeaderField{Name: ":status", Value: "200"})
	_ = enc.WriteField(qpack.HeaderField{Name: "trailer", Value: "grpc-status, grpc-message"})
	hBlock := qpackBuf.Bytes()
	streamBuf.Write(appendHeadersHeader(nil, uint64(len(hBlock))))
	streamBuf.Write(hBlock)

	// 2. DATA frame
	body := []byte("grpc streaming message")
	streamBuf.Write(appendDataHeader(nil, uint64(len(body))))
	streamBuf.Write(body)

	// 3. Trailing HEADERS frame
	var trailerBuf bytes.Buffer

	encTrailer := qpack.NewEncoder(&trailerBuf)
	_ = encTrailer.WriteField(qpack.HeaderField{Name: "grpc-status", Value: "0"})
	_ = encTrailer.WriteField(qpack.HeaderField{Name: "grpc-message", Value: "OK"})
	tBlock := trailerBuf.Bytes()
	streamBuf.Write(appendHeadersHeader(nil, uint64(len(tBlock))))
	streamBuf.Write(tBlock)

	resp := h1engine.AcquireResponse()
	defer h1engine.ReleaseResponse(resp)

	trailers, err := cc.readResponseFrom(&streamBuf, resp)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode())
	assert.Equal(t, "grpc streaming message", string(resp.Body()))
	require.NotNil(t, trailers)
	assert.Equal(t, []string{"0"}, trailers["grpc-status"])
	assert.Equal(t, []string{"OK"}, trailers["grpc-message"])
}

func TestReadResponse_Informational100Continue(t *testing.T) {
	t.Parallel()

	cc := &ClientConn{
		qpack: NewQPACKCodec(),
	}

	var streamBuf bytes.Buffer

	// 1. Informational 100 Continue HEADERS frame
	var qpackBuf100 bytes.Buffer

	enc100 := qpack.NewEncoder(&qpackBuf100)
	_ = enc100.WriteField(qpack.HeaderField{Name: ":status", Value: "100"})
	hBlock100 := qpackBuf100.Bytes()
	streamBuf.Write(appendHeadersHeader(nil, uint64(len(hBlock100))))
	streamBuf.Write(hBlock100)

	// 2. Final 200 OK HEADERS frame
	var qpackBuf200 bytes.Buffer

	enc200 := qpack.NewEncoder(&qpackBuf200)
	_ = enc200.WriteField(qpack.HeaderField{Name: ":status", Value: "200"})
	_ = enc200.WriteField(qpack.HeaderField{Name: "x-final", Value: "true"})
	hBlock200 := qpackBuf200.Bytes()
	streamBuf.Write(appendHeadersHeader(nil, uint64(len(hBlock200))))
	streamBuf.Write(hBlock200)

	// 3. DATA frame
	payload := []byte("after 100 continue")
	streamBuf.Write(appendDataHeader(nil, uint64(len(payload))))
	streamBuf.Write(payload)

	resp := h1engine.AcquireResponse()
	defer h1engine.ReleaseResponse(resp)

	_, err := cc.readResponseFrom(&streamBuf, resp)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode())
	assert.Equal(t, "true", string(resp.Header.Peek("X-Final")))
	assert.Equal(t, "after 100 continue", string(resp.Body()))
}

func TestReadResponse_UnexpectedDataBeforeHeaders(t *testing.T) {
	t.Parallel()

	cc := &ClientConn{
		qpack: NewQPACKCodec(),
	}

	var streamBuf bytes.Buffer

	// DATA frame without preceding HEADERS frame -> MUST fail with ErrFrameUnexpected
	streamBuf.Write(appendDataHeader(nil, 10))
	streamBuf.Write([]byte("0123456789"))

	resp := h1engine.AcquireResponse()
	defer h1engine.ReleaseResponse(resp)

	_, err := cc.readResponseFrom(&streamBuf, resp)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFrameUnexpected)
}

func TestReadResponse_UnknownFrameDiscarded(t *testing.T) {
	t.Parallel()

	cc := &ClientConn{
		qpack: NewQPACKCodec(),
	}

	var streamBuf bytes.Buffer

	// 1. Unknown frame type 0x33 with 4 bytes payload (RFC 9114 §7.2.8: MUST ignore unknown frame types)
	var unknownHeader []byte

	unknownHeader = quicvarint.Append(unknownHeader, 0x33)
	unknownHeader = quicvarint.Append(unknownHeader, 4)
	streamBuf.Write(unknownHeader)
	streamBuf.Write([]byte("abcd"))

	// 2. HEADERS frame
	var qpackBuf bytes.Buffer

	enc := qpack.NewEncoder(&qpackBuf)
	_ = enc.WriteField(qpack.HeaderField{Name: ":status", Value: "204"})
	hBlock := qpackBuf.Bytes()
	streamBuf.Write(appendHeadersHeader(nil, uint64(len(hBlock))))
	streamBuf.Write(hBlock)

	resp := h1engine.AcquireResponse()
	defer h1engine.ReleaseResponse(resp)

	_, err := cc.readResponseFrom(&streamBuf, resp)
	require.NoError(t, err)
	assert.Equal(t, 204, resp.StatusCode())
}

func TestSettings_ReservedH2SettingsError(t *testing.T) {
	t.Parallel()

	// Reserved HTTP/2 settings (RFC 9114 §7.2.4.1): 0x02, 0x03, 0x04, 0x05
	reservedIDs := []uint64{0x02, 0x03, 0x04, 0x05}

	for _, id := range reservedIDs {
		var buf []byte

		buf = quicvarint.Append(buf, id)
		buf = quicvarint.Append(buf, 100)

		r := bytes.NewReader(buf)
		_, err := DecodeSettings(r, uint64(len(buf)))
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrH3SettingsError)
	}
}

func TestClient_ConnectionPoolAndRemoval(t *testing.T) {
	t.Parallel()

	client := NewClient(&tls.Config{InsecureSkipVerify: true}, &quic.Config{})
	require.NotNil(t, client)

	// Simulate adding and removing connection
	mockConn := &ClientConn{
		qpack:  NewQPACKCodec(),
		closed: make(chan struct{}),
	}

	client.conns["example.com:443"] = mockConn
	assert.Len(t, client.conns, 1)

	client.removeConn("example.com:443")
	assert.Empty(t, client.conns)

	err := client.Close()
	assert.NoError(t, err)
}

func TestSendRequest_LargePayload_Pooled(t *testing.T) {
	t.Parallel()

	cc := &ClientConn{
		qpack: NewQPACKCodec(),
	}

	req := h1engine.AcquireRequest()
	defer h1engine.ReleaseRequest(req)

	req.Header.SetMethod("POST")
	req.SetRequestURI("https://example.com/upload")
	// 32 KB body to exceed stack 8 KB buffer and trigger pooled storage
	largeBody := make([]byte, 32768)
	for i := range largeBody {
		largeBody[i] = byte(i % 256)
	}

	req.SetBody(largeBody)

	var buf bytes.Buffer

	err := cc.sendRequestTo(&buf, req, nil)
	require.NoError(t, err)
	assert.Greater(t, buf.Len(), 32768)
}

func TestReadResponse_LargeHeaders_Pooled(t *testing.T) {
	t.Parallel()

	cc := &ClientConn{
		qpack: NewQPACKCodec(),
	}

	var streamBuf bytes.Buffer

	// Build large headers block (> 4 KB) to trigger pooled storage
	var qpackBuf bytes.Buffer

	enc := qpack.NewEncoder(&qpackBuf)

	_ = enc.WriteField(qpack.HeaderField{Name: ":status", Value: "200"})

	for i := 0; i < 100; i++ {
		_ = enc.WriteField(qpack.HeaderField{
			Name:  "x-custom-large-header-" + string(rune('a'+(i%26))),
			Value: "some-repeated-value-that-fills-space-0123456789-abcdefghijklmnopqrstuvwxyz",
		})
	}

	hBlock := qpackBuf.Bytes()
	require.Greater(t, len(hBlock), 4096)

	streamBuf.Write(appendHeadersHeader(nil, uint64(len(hBlock))))
	streamBuf.Write(hBlock)

	body := []byte("large headers body")
	streamBuf.Write(appendDataHeader(nil, uint64(len(body))))
	streamBuf.Write(body)

	resp := h1engine.AcquireResponse()
	defer h1engine.ReleaseResponse(resp)

	_, err := cc.readResponseFrom(&streamBuf, resp)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode())
	assert.Equal(t, "large headers body", string(resp.Body()))
}

func TestDoScoped_Execution(t *testing.T) {
	t.Parallel()

	scope := borrow.AcquireScope()
	defer scope.Release()

	cc := &ClientConn{
		qpack: NewQPACKCodec(),
	}

	var (
		streamBuf bytes.Buffer
		qpackBuf  bytes.Buffer
	)

	enc := qpack.NewEncoder(&qpackBuf)

	_ = enc.WriteField(qpack.HeaderField{Name: ":status", Value: "200"})
	hBlock := qpackBuf.Bytes()

	streamBuf.Write(appendHeadersHeader(nil, uint64(len(hBlock))))
	streamBuf.Write(hBlock)

	body := []byte("scoped body content")
	streamBuf.Write(appendDataHeader(nil, uint64(len(body))))
	streamBuf.Write(body)

	resp := h1engine.AcquireResponse()
	defer h1engine.ReleaseResponse(resp)

	_, err := cc.readResponseScoped(&streamBuf, resp, scope)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode())
	assert.Equal(t, "scoped body content", string(resp.Body()))
}
