// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3engine

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"sync"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/quic-go/qpack"
	"github.com/valyala/fasthttp"
)

var bufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// QPACKCodec manages zero-allocation QPACK header serialization and deserialization.
type QPACKCodec struct {
	decoder *qpack.Decoder
}

// NewQPACKCodec instantiates a new QPACKCodec.
func NewQPACKCodec() *QPACKCodec {
	return &QPACKCodec{
		decoder: qpack.NewDecoder(),
	}
}

// WriteDecoderTable processes instructions received over the QPACK Encoder Stream.
//
// Note: Currently quic-go/qpack operates on static tables and does not expose
// dynamic table instructions, so incoming bytes are safely consumed.
func (q *QPACKCodec) WriteDecoderTable(_ []byte) error {
	return nil
}

// EncodeRequestHeaders encodes a fasthttp request header into a QPACK block,
// strictly maintaining the specified orderedKeys sequence for Anti-DPI fingerprinting and RFC 9220 Extended CONNECT.
func (q *QPACKCodec) EncodeRequestHeaders(w io.Writer, req *fasthttp.Request, orderedKeys []string) error {
	buf := bufferPool.Get().(*bytes.Buffer)

	buf.Reset()
	defer bufferPool.Put(buf)

	enc := qpack.NewEncoder(buf)

	method := bytesconv.B2S(req.Header.Method())
	_ = enc.WriteField(qpack.HeaderField{Name: ":method", Value: method})
	_ = enc.WriteField(qpack.HeaderField{Name: ":scheme", Value: bytesconv.B2S(req.URI().Scheme())})
	_ = enc.WriteField(qpack.HeaderField{Name: ":authority", Value: bytesconv.B2S(req.URI().Host())})
	_ = enc.WriteField(qpack.HeaderField{Name: ":path", Value: bytesconv.B2S(req.URI().RequestURI())})

	if protoVal := req.Header.Peek(":protocol"); len(protoVal) > 0 {
		_ = enc.WriteField(qpack.HeaderField{Name: ":protocol", Value: bytesconv.B2S(protoVal)})
	}

	if len(orderedKeys) > 0 {
		q.encodeOrderedHeaders(enc, req, orderedKeys)
	} else {
		var stackKeyBuf [128]byte
		req.Header.All()(func(k, v []byte) bool {
			if isForbiddenH3Header(k, v) {
				return true
			}

			var keyStr string
			if len(k) <= len(stackKeyBuf) {
				keyBuf := stackKeyBuf[:len(k)]
				for i := range k {
					keyBuf[i] = bytesconv.LowercaseByte(k[i])
				}

				keyStr = bytesconv.B2S(keyBuf)
			} else {
				keyStr = bytesconv.B2S(bytesconv.AppendToLower(nil, k))
			}

			_ = enc.WriteField(qpack.HeaderField{Name: keyStr, Value: bytesconv.B2S(v)})

			return true
		})
	}

	_, err := w.Write(buf.Bytes())

	return err
}

func isForbiddenH3Header(key, val []byte) bool {
	if len(key) == 0 || key[0] == ':' {
		return true
	}

	keyStr := bytesconv.B2S(key)
	if bytesconv.EqualFoldASCII(keyStr, "connection") ||
		bytesconv.EqualFoldASCII(keyStr, "keep-alive") ||
		bytesconv.EqualFoldASCII(keyStr, "proxy-connection") ||
		bytesconv.EqualFoldASCII(keyStr, "transfer-encoding") ||
		bytesconv.EqualFoldASCII(keyStr, "upgrade") ||
		bytesconv.EqualFoldASCII(keyStr, "sec-websocket-key") ||
		bytesconv.EqualFoldASCII(keyStr, "sec-websocket-accept") {
		return true
	}

	if bytesconv.EqualFoldASCII(keyStr, "te") {
		return !bytesconv.EqualFoldASCII(bytesconv.B2S(val), "trailers")
	}

	return false
}

func isForbiddenH3HeaderStr(key string, val []byte) bool {
	if key == "" || key[0] == ':' {
		return true
	}

	if bytesconv.EqualFoldASCII(key, "connection") ||
		bytesconv.EqualFoldASCII(key, "keep-alive") ||
		bytesconv.EqualFoldASCII(key, "proxy-connection") ||
		bytesconv.EqualFoldASCII(key, "transfer-encoding") ||
		bytesconv.EqualFoldASCII(key, "upgrade") ||
		bytesconv.EqualFoldASCII(key, "sec-websocket-key") ||
		bytesconv.EqualFoldASCII(key, "sec-websocket-accept") {
		return true
	}

	if bytesconv.EqualFoldASCII(key, "te") {
		return !bytesconv.EqualFoldASCII(bytesconv.B2S(val), "trailers")
	}

	return false
}

func (q *QPACKCodec) encodeOrderedHeaders(enc *qpack.Encoder, req *fasthttp.Request, orderedKeys []string) {
	var visitedBits uint64

	numOrdered := min(len(orderedKeys), 64)

	for i := 0; i < numOrdered; i++ {
		key := orderedKeys[i]
		val := req.Header.Peek(key)

		if isForbiddenH3HeaderStr(key, val) {
			continue
		}

		if len(val) > 0 {
			_ = enc.WriteField(qpack.HeaderField{Name: key, Value: bytesconv.B2S(val)})

			visitedBits |= (1 << i)
		}
	}

	req.Header.All()(func(k, v []byte) bool {
		kStr := bytesconv.B2S(k)

		if isForbiddenH3Header(k, v) {
			return true
		}

		for i := 0; i < numOrdered; i++ {
			if (visitedBits&(1<<i)) != 0 && bytesconv.EqualFoldASCII(kStr, orderedKeys[i]) {
				return true
			}
		}

		var (
			stackKeyBuf [128]byte
			keyStr      string
		)

		if len(k) <= len(stackKeyBuf) {
			keyBuf := stackKeyBuf[:len(k)]
			for i := range k {
				keyBuf[i] = bytesconv.LowercaseByte(k[i])
			}

			keyStr = bytesconv.B2S(keyBuf)
		} else {
			keyStr = bytesconv.B2S(bytesconv.AppendToLower(nil, k))
		}

		_ = enc.WriteField(qpack.HeaderField{
			Name:  keyStr,
			Value: bytesconv.B2S(v),
		})

		return true
	})
}

// DecodeResponseHeaders parses a QPACK header block directly into fasthttp ResponseHeader,
// returning the parsed status code and ignoring 1xx informational frames.
func (q *QPACKCodec) DecodeResponseHeaders(headerBlock []byte, res *fasthttp.ResponseHeader) (int, error) {
	decodeFn := q.decoder.Decode(headerBlock)

	var (
		hasStatus  bool
		statusCode int
	)

	for {
		hf, err := decodeFn()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return 0, ErrQPACKDecompressFailed
		}

		if hf.Name == ":status" {
			code, parseErr := strconv.Atoi(hf.Value)
			if parseErr != nil {
				return 0, parseErr
			}

			statusCode = code
			if statusCode < 100 || statusCode >= 200 || statusCode == 101 {
				res.SetStatusCode(statusCode)
			}

			hasStatus = true

			continue
		}

		if hf.IsPseudo() {
			continue
		}

		if hf.Name == "content-length" {
			if clen, parseErr := strconv.Atoi(hf.Value); parseErr == nil {
				res.SetContentLength(clen)
			}

			continue
		}

		res.Add(hf.Name, hf.Value)
	}

	if !hasStatus {
		return 0, ErrMissingStatusHeader
	}

	return statusCode, nil
}

// DecodeResponseTrailers decodes a QPACK header block containing response trailers into a key-value map.
func (q *QPACKCodec) DecodeResponseTrailers(headerBlock []byte) (map[string][]string, error) {
	decodeFn := q.decoder.Decode(headerBlock)
	trailers := make(map[string][]string)

	for {
		hf, err := decodeFn()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, ErrQPACKDecompressFailed
		}

		if hf.IsPseudo() {
			continue
		}

		trailers[hf.Name] = append(trailers[hf.Name], hf.Value)
	}

	return trailers, nil
}
