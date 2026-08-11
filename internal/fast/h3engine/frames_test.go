// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3engine

import (
	"bytes"
	"testing"

	"github.com/quic-go/quic-go/quicvarint"
)

func TestSettingsEncodeAndParse(t *testing.T) {
	st := &Settings{
		MaxFieldSectionSize: 65536,
		QpackMaxTableCap:    4096,
		QpackBlockedStreams: 100,
		EnableDatagrams:     true,
		EnableConnect:       true,
		Other: map[uint64]uint64{
			0x1234: 5678,
		},
	}

	encoded := st.Encode()
	r := bytes.NewReader(encoded)

	frameType, payloadLen, err := ReadFrameHeader(r)
	if err != nil {
		t.Fatalf("failed to read frame header: %v", err)
	}

	if frameType != FrameTypeSettings {
		t.Fatalf("expected FrameTypeSettings (%d), got %d", FrameTypeSettings, frameType)
	}

	if int(payloadLen) != r.Len() {
		t.Fatalf("payload length mismatch: header specifies %d, remaining buffer is %d", payloadLen, r.Len())
	}
}

func TestReadFrameHeader(t *testing.T) {
	var buf []byte

	buf = quicvarint.Append(buf, FrameTypeHeaders)
	buf = quicvarint.Append(buf, 1024)

	r := bytes.NewReader(buf)

	fType, pLen, err := ReadFrameHeader(r)
	if err != nil {
		t.Fatalf("ReadFrameHeader failed: %v", err)
	}

	if fType != FrameTypeHeaders {
		t.Errorf("got frame type %d, want %d", fType, FrameTypeHeaders)
	}

	if pLen != 1024 {
		t.Errorf("got payload length %d, want 1024", pLen)
	}
}

func TestAppendFrameHeaders(t *testing.T) {
	hHeader := appendHeadersHeader(nil, 512)
	rH := bytes.NewReader(hHeader)

	fType, pLen, err := ReadFrameHeader(rH)
	if err != nil || fType != FrameTypeHeaders || pLen != 512 {
		t.Fatalf("appendHeadersHeader failed: type=%d, len=%d, err=%v", fType, pLen, err)
	}

	dHeader := appendDataHeader(nil, 2048)
	rD := bytes.NewReader(dHeader)

	fType, pLen, err = ReadFrameHeader(rD)
	if err != nil || fType != FrameTypeData || pLen != 2048 {
		t.Fatalf("appendDataHeader failed: type=%d, len=%d, err=%v", fType, pLen, err)
	}
}
