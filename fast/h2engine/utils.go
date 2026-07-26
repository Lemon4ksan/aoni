// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"

	"github.com/valyala/fasthttp"
	"github.com/valyala/fastrand"
)

var (
	http2Preface = []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	prefaceLen   = len(http2Preface)

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

// ReadPreface verifies the connection initialisation preface.
func ReadPreface(r io.Reader) bool {
	b := make([]byte, prefaceLen)

	n, err := r.Read(b[:prefaceLen])

	return err == nil && n == prefaceLen && bytes.Equal(b, http2Preface)
}

// WritePreface writes the HTTP/2 connection preface to w.
func WritePreface(w io.Writer) error {
	_, err := w.Write(http2Preface)
	return err
}

// PerformHandshake sends the preface, settings, and initial window update frames.
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
		return nil, fmt.Errorf("h2engine: padding out of range")
	}

	return payload[1 : length-pad], nil
}

func addPadding(b []byte) []byte {
	n := int(fastrand.Uint32n(247)) + 9
	nn := len(b)

	b = resizeSlice(b, nn+n)
	b = append(b[:1], b...)
	b[0] = uint8(n)

	return b
}

func toLowerCopy(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			out[i] = b[i] + 32
		} else {
			out[i] = b[i]
		}
	}

	return out
}

// fasthttpResponseHeaders serializes response status and headers from a fasthttp.Response into HPACK-encoded header fields.
func fasthttpResponseHeaders(dst *Headers, hp *HPACK, res *fasthttp.Response) {
	hf := AcquireHeaderField()
	defer ReleaseHeaderField(hf)

	hf.SetKeyBytes(StringStatus)
	hf.SetValue(strconv.Itoa(res.Header.StatusCode()))

	dst.AppendHeaderField(hp, hf, true)

	if !res.IsBodyStream() {
		res.Header.SetContentLength(len(res.Body()))
	}

	res.Header.Del("Connection")
	res.Header.Del("Transfer-Encoding")

	res.Header.All()(func(k, v []byte) bool {
		hf.SetBytes(toLowerCopy(k), v)
		dst.AppendHeaderField(hp, hf, false)
		return true
	})
}
