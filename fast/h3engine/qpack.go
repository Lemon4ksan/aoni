// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3engine

import (
	"bytes"
	"io"
	"strconv"
	"sync"

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
// strictly maintaining the specified orderedKeys sequence for Anti-DPI fingerprinting.
func (q *QPACKCodec) EncodeRequestHeaders(w io.Writer, req *fasthttp.Request, orderedKeys []string) error {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufferPool.Put(buf)

	enc := qpack.NewEncoder(buf)

	enc.WriteField(qpack.HeaderField{Name: ":method", Value: string(req.Header.Method())})
	enc.WriteField(qpack.HeaderField{Name: ":scheme", Value: string(req.URI().Scheme())})
	enc.WriteField(qpack.HeaderField{Name: ":authority", Value: string(req.URI().Host())})
	enc.WriteField(qpack.HeaderField{Name: ":path", Value: string(req.URI().RequestURI())})

	if len(orderedKeys) > 0 {
		q.encodeOrderedHeaders(enc, req, orderedKeys)
	} else {
		req.Header.All()(func(k, v []byte) bool {
			enc.WriteField(qpack.HeaderField{Name: toLowerCopy(k), Value: string(v)})
			return true
		})
	}

	_, err := w.Write(buf.Bytes())

	return err
}

func (q *QPACKCodec) encodeOrderedHeaders(enc *qpack.Encoder, req *fasthttp.Request, orderedKeys []string) {
	visited := make(map[string]bool, len(orderedKeys))

	for _, key := range orderedKeys {
		if key == "" || key[0] == ':' {
			continue
		}

		val := req.Header.Peek(key)
		if len(val) > 0 {
			enc.WriteField(qpack.HeaderField{Name: toLowerCopy([]byte(key)), Value: string(val)})
			visited[key] = true
		}
	}

	req.Header.All()(func(k, v []byte) bool {
		if !visited[string(k)] {
			enc.WriteField(qpack.HeaderField{Name: toLowerCopy(k), Value: string(v)})
		}
		return true
	})
}

// DecodeResponseHeaders parses a QPACK header block directly into fasthttp ResponseHeader.
func (q *QPACKCodec) DecodeResponseHeaders(headerBlock []byte, res *fasthttp.ResponseHeader) error {
	decodeFn := q.decoder.Decode(headerBlock)
	hasStatus := false

	for {
		hf, err := decodeFn()
		if err == io.EOF {
			break
		}

		if err != nil {
			return ErrQPACKDecompressFailed
		}

		if hf.Name == ":status" {
			statusCode, parseErr := strconv.Atoi(hf.Value)
			if parseErr != nil {
				return parseErr
			}

			res.SetStatusCode(statusCode)
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
		return ErrMissingStatusHeader
	}

	return nil
}

func toLowerCopy(b []byte) string {
	out := make([]byte, len(b))
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			out[i] = b[i] + 32
		} else {
			out[i] = b[i]
		}
	}

	return string(out)
}
