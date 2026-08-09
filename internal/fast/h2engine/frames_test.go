// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

import (
	"bufio"
	"bytes"
	"reflect"
	"testing"
)

func TestFrameHeaderFlags(t *testing.T) {
	flags := FrameFlags(0)

	flags = flags.Add(FlagEndHeaders).Add(FlagEndStream)
	if !flags.Has(FlagEndHeaders) || !flags.Has(FlagEndStream) {
		t.Fatalf("expected flags to contain FlagEndHeaders and FlagEndStream")
	}

	flags = flags.Del(FlagEndHeaders)
	if flags.Has(FlagEndHeaders) {
		t.Fatalf("expected FlagEndHeaders to be removed")
	}

	if !flags.Has(FlagEndStream) {
		t.Fatalf("expected FlagEndStream to remain")
	}
}

func TestFrameHeaderBoundsCheck(t *testing.T) {
	fh := AcquireFrameHeader()
	defer ReleaseFrameHeader(fh)

	fh.length = 20000
	fh.maxLen = 16384

	if err := fh.checkLen(); err == nil {
		t.Fatalf("expected ErrPayloadExceeds when frame length exceeds maxLen")
	}
}

func TestFrameHeaderParseHeaderSymmetry(t *testing.T) {
	fh := AcquireFrameHeader()
	defer ReleaseFrameHeader(fh)

	fh.length = 1024
	fh.kind = FrameHeaders
	fh.flags = FlagEndHeaders | FlagEndStream
	fh.stream = 13

	var buf [DefaultFrameSize]byte
	fh.parseHeader(buf[:])

	parsedFH := AcquireFrameHeader()
	defer ReleaseFrameHeader(parsedFH)

	parsedFH.parseValues(buf[:])

	if parsedFH.length != fh.length {
		t.Errorf("length mismatch: got %d, want %d", parsedFH.length, fh.length)
	}

	if parsedFH.kind != fh.kind {
		t.Errorf("type mismatch: got %v, want %v", parsedFH.kind, fh.kind)
	}

	if parsedFH.flags != fh.flags {
		t.Errorf("flags mismatch: got %v, want %v", parsedFH.flags, fh.flags)
	}

	if parsedFH.stream != fh.stream {
		t.Errorf("stream mismatch: got %d, want %d", parsedFH.stream, fh.stream)
	}
}

func TestFramesSerializationRoundtrip(t *testing.T) {
	testCases := []struct {
		name  string
		frame Frame
	}{
		{
			name: "Data Frame",
			frame: &Data{
				endStream:  true,
				hasPadding: false,
				b:          []byte("hello h2engine"),
			},
		},
		{
			name: "Headers Frame",
			frame: &Headers{
				endStream:  true,
				endHeaders: true,
				rawHeaders: []byte{0x82, 0x86, 0x84},
			},
		},
		{
			name: "Settings Frame",
			frame: &Settings{
				tableSize:  8192,
				enablePush: false,
				maxStreams: 250,
				windowSize: 1048576,
			},
		},
		{
			name: "Ping Frame",
			frame: func() *Ping {
				p := &Ping{ack: true}
				p.SetData([]byte("12345678"))

				return p
			}(),
		},
		{
			name: "GoAway Frame",
			frame: &GoAway{
				stream: 15,
				code:   ProtocolError,
				data:   []byte("fatal error"),
			},
		},
		{
			name:  "WindowUpdate Frame",
			frame: &WindowUpdate{increment: 65535},
		},
		{
			name:  "RstStream Frame",
			frame: &RstStream{code: StreamCanceled},
		},
		{
			name:  "Priority Frame",
			frame: &Priority{stream: 3, weight: 255},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fhOut := AcquireFrameHeader()
			fhOut.SetStream(1)
			fhOut.SetBody(tc.frame)

			var buf bytes.Buffer

			bw := bufio.NewWriter(&buf)

			if _, err := fhOut.WriteTo(bw); err != nil {
				t.Fatalf("failed to serialize frame: %v", err)
			}

			_ = bw.Flush()

			br := bufio.NewReader(&buf)

			fhIn, err := ReadFrameFrom(br)
			if err != nil {
				t.Fatalf("failed to deserialize frame: %v", err)
			}

			defer ReleaseFrameHeader(fhIn)

			if fhIn.Type() != tc.frame.Type() {
				t.Fatalf("frame type mismatch: got %v, want %v", fhIn.Type(), tc.frame.Type())
			}

			if reflect.TypeOf(fhIn.Body()) != reflect.TypeOf(tc.frame) {
				t.Fatalf("body type mismatch: got %T, want %T", fhIn.Body(), tc.frame)
			}
		})
	}
}

func TestPaddingHelpers(t *testing.T) {
	data := []byte("secret payload")

	padded := addPadding(data)
	if len(padded) <= len(data) {
		t.Fatalf("expected padded length to be larger than original")
	}

	unpadded, err := cutPadding(padded, len(padded))
	if err != nil {
		t.Fatalf("failed to cut padding: %v", err)
	}

	if !bytes.Equal(unpadded, data) {
		t.Fatalf("unpadded data mismatch: got %q, want %q", unpadded, data)
	}
}
