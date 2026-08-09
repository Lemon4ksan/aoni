// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3engine

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/quic-go/qpack"
	"github.com/quic-go/quic-go/quicvarint"
	"github.com/valyala/fasthttp"
)

func TestSendRequestH3Framing(t *testing.T) {
	codec := NewQPACKCodec()

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetMethod("POST")
	req.SetRequestURI("https://example.com/submit")
	req.SetBodyString("payload-123")

	var streamBuf bytes.Buffer

	var headerBuf bytes.Buffer

	if err := codec.EncodeRequestHeaders(&headerBuf, req, nil); err != nil {
		t.Fatalf("EncodeRequestHeaders failed: %v", err)
	}

	headerBlock := headerBuf.Bytes()
	streamBuf.Write(appendHeadersHeader(nil, uint64(len(headerBlock))))
	streamBuf.Write(headerBlock)

	body := req.Body()
	streamBuf.Write(appendDataHeader(nil, uint64(len(body))))
	streamBuf.Write(body)

	r := quicvarint.NewReader(&streamBuf)

	// 1. Проверяем H3 HEADERS фрейм
	fType1, pLen1, err := ReadFrameHeader(r)
	if err != nil || fType1 != FrameTypeHeaders {
		t.Fatalf("expected HEADERS frame, got fType=%d, err=%v", fType1, err)
	}

	hdrPayload := make([]byte, pLen1)
	if _, err := io.ReadFull(r, hdrPayload); err != nil {
		t.Fatalf("failed to read headers payload: %v", err)
	}

	dec := qpack.NewDecoder()
	decodeFn := dec.Decode(hdrPayload)
	hasMethod := false

	for {
		hf, err := decodeFn()
		if errors.Is(err, io.EOF) {
			break
		}

		if hf.Name == ":method" && hf.Value == "POST" {
			hasMethod = true
		}
	}

	if !hasMethod {
		t.Fatalf("expected :method POST in QPACK headers payload")
	}

	// 2. Проверяем H3 DATA фрейм
	fType2, pLen2, err := ReadFrameHeader(r)
	if err != nil || fType2 != FrameTypeData {
		t.Fatalf("expected DATA frame, got fType=%d, err=%v", fType2, err)
	}

	bodyPayload := make([]byte, pLen2)
	if _, err := io.ReadFull(r, bodyPayload); err != nil {
		t.Fatalf("failed to read data payload: %v", err)
	}

	if string(bodyPayload) != "payload-123" {
		t.Fatalf("body mismatch: got %q, want payload-123", string(bodyPayload))
	}
}

func TestReadResponseH3Framing(t *testing.T) {
	codec := NewQPACKCodec()

	var streamBuf bytes.Buffer

	// Формируем эмуляцию ответа сервера: HEADERS фрейм (:status 200) + DATA фрейм ("aoni h3engine success")
	var hdrBuf bytes.Buffer

	enc := qpack.NewEncoder(&hdrBuf)

	_ = enc.WriteField(qpack.HeaderField{Name: ":status", Value: "200"})
	_ = enc.WriteField(qpack.HeaderField{Name: "content-type", Value: "text/plain"})

	hdrBlock := hdrBuf.Bytes()
	streamBuf.Write(appendHeadersHeader(nil, uint64(len(hdrBlock))))
	streamBuf.Write(hdrBlock)

	responseData := []byte("aoni h3engine success")
	streamBuf.Write(appendDataHeader(nil, uint64(len(responseData))))
	streamBuf.Write(responseData)

	r := quicvarint.NewReader(&streamBuf)

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	headersParsed := false

	for {
		fType, pLen, err := ReadFrameHeader(r)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("ReadFrameHeader failed: %v", err)
		}

		switch fType {
		case FrameTypeHeaders:
			if headersParsed {
				t.Fatalf("unexpected duplicate HEADERS frame")
			}

			block := make([]byte, pLen)
			_, _ = io.ReadFull(r, block)

			if _, err := codec.DecodeResponseHeaders(block, &resp.Header); err != nil {
				t.Fatalf("DecodeResponseHeaders failed: %v", err)
			}

			headersParsed = true

		case FrameTypeData:
			if !headersParsed {
				t.Fatalf("DATA frame received before HEADERS frame")
			}

			data := make([]byte, pLen)
			_, _ = io.ReadFull(r, data)
			resp.AppendBody(data)
		}
	}

	if resp.StatusCode() != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode())
	}

	if string(resp.Body()) != "aoni h3engine success" {
		t.Fatalf("response body mismatch: got %q, want %q", resp.Body(), "aoni h3engine success")
	}
}
