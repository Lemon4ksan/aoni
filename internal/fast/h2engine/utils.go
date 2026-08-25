// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strconv"

	"github.com/lemon4ksan/foundation/silicon/rand"

	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
)

var (
	// http2Preface defines the 24-octet client connection preface string (RFC 9113 §3.4: "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n").
	http2Preface = []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	prefaceLen   = len(http2Preface)

	// HTTP/2 Request & Response Pseudo-Headers (RFC 9113 §8.3, §8.3.1 & §8.3.2)
	StringPath          = []byte(":path")
	StringStatus        = []byte(":status")
	StringAuthority     = []byte(":authority")
	StringScheme        = []byte(":scheme")
	StringMethod        = []byte(":method")
	StringServer        = []byte("server")
	StringContentLength = []byte("content-length")
	StringContentType   = []byte("content-type")
	StringUserAgent     = []byte("user-agent")
	StringHTTP2         = []byte("HTTP/2")
)

// ReadPreface verifies the 24-octet connection initialization preface from r (RFC 9113 §3.4).
func ReadPreface(r io.Reader) bool {
	b := make([]byte, prefaceLen)

	n, err := r.Read(b[:prefaceLen])

	return err == nil && n == prefaceLen && bytes.Equal(b, http2Preface)
}

// WritePreface writes the 24-octet HTTP/2 client connection preface to w (RFC 9113 §3.4).
func WritePreface(w io.Writer) error {
	_, err := w.Write(http2Preface)
	return err
}

// PerformHandshake sends the client connection preface, SETTINGS frame, and initial connection WINDOW_UPDATE (RFC 9113 §3.4, §6.5 & §6.9).
func PerformHandshake(preface bool, bw *bufio.Writer, st *Settings, maxWin int32) error {
	if preface {
		if err := WritePreface(bw); err != nil {
			return err
		}
	}

	fr := AcquireFrameHeader()
	defer ReleaseFrameHeader(fr)

	st2 := &Settings{}
	st.CopyTo(st2)
	fr.SetBody(st2)

	if _, err := fr.WriteTo(bw); err != nil {
		return err
	}

	frWin := AcquireFrameHeader()
	defer ReleaseFrameHeader(frWin)

	wu := AcquireFrame(FrameWindowUpdate).(*WindowUpdate)
	wu.SetIncrement(int(maxWin))
	frWin.SetBody(wu)

	if _, err := frWin.WriteTo(bw); err != nil {
		return err
	}

	return bw.Flush()
}

func uint24ToBytes(b []byte, n uint32) {
	_ = b[2]
	b[0] = byte(n >> 16)
	b[1] = byte(n >> 8)
	b[2] = byte(n)
}

func bytesToUint24(b []byte) uint32 {
	_ = b[2]
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
}

func appendUint32Bytes(dst []byte, n uint32) []byte {
	return append(dst, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}

func uint32ToBytes(b []byte, n uint32) {
	_ = b[3]
	b[0] = byte(n >> 24)
	b[1] = byte(n >> 16)
	b[2] = byte(n >> 8)
	b[3] = byte(n)
}

func bytesToUint32(b []byte) uint32 {
	_ = b[3]
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func resizeSlice(b []byte, neededLen int) []byte {
	b = b[:cap(b)]
	if n := neededLen - len(b); n > 0 {
		b = append(b, make([]byte, n)...)
	}

	return b[:neededLen]
}

func cutPadding(payload []byte, length int) ([]byte, error) {
	if len(payload) == 0 {
		return nil, ErrMissingBytes
	}

	pad := int(payload[0])
	if len(payload) < length-pad-1 || length-pad < 1 {
		return nil, errors.New("h2engine: padding out of range")
	}

	return payload[1 : length-pad], nil
}

func addPadding(b []byte) []byte {
	n := int(rand.Uint32n(247)) + 9
	nn := len(b)

	b = resizeSlice(b, nn+n)
	b = append(b[:1], b...)
	b[0] = uint8(n) //nolint:gosec

	return b
}

// toLowerCopy converts ASCII header name bytes to lowercase using a stack-allocated buffer to prevent heap allocations.
func toLowerCopy(b []byte) []byte {
	var (
		stackBuf [64]byte
		out      []byte
	)

	if len(b) <= len(stackBuf) {
		out = stackBuf[:len(b)]
	} else {
		out = make([]byte, len(b))
	}

	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			out[i] = b[i] + 32
		} else {
			out[i] = b[i]
		}
	}

	return out
}

// fasthttpResponseHeaders serializes response status and headers from a h1engine.Response into HPACK-encoded header fields.
func fasthttpResponseHeaders(dst *Headers, hp *HPACK, res *h1engine.Response) {
	hf := AcquireHeaderField()
	defer ReleaseHeaderField(hf)

	hf.SetKeyBytes(StringStatus)
	hf.SetValue(strconv.Itoa(res.Header.StatusCode()))

	dst.AppendHeaderField(hp, hf, true)

	if !res.IsBodyStream() {
		res.Header.SetContentLength(len(res.Body()))
	}

	// RFC 9113 §8.2.2: Connection-specific headers (Connection, Transfer-Encoding) MUST NOT be sent in HTTP/2.
	res.Header.Del("Connection")
	res.Header.Del("Transfer-Encoding")

	for k, v := range res.Header.All() {
		hf.SetBytes(toLowerCopy(k), v)
		dst.AppendHeaderField(hp, hf, false)
	}
}
