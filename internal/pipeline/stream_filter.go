// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"io"
	"net/http"
	"strings"

	"github.com/lemon4ksan/foundation/codec/compress"
	fio "github.com/lemon4ksan/foundation/iokit"

	"github.com/lemon4ksan/aoni/netutil/dict"
)

// StreamFilter defines a response body stream transformation filter.
// It receives the HTTP response metadata and current body reader stream,
// returning a transformed body reader stream or error.
type StreamFilter func(resp *http.Response, body io.ReadCloser) (io.ReadCloser, error)

// StreamPipeline encapsulates a sequential stream filter execution chain.
type StreamPipeline struct {
	filters []StreamFilter
}

// NewStreamPipeline constructs a new [StreamPipeline] with the provided filters.
func NewStreamPipeline(filters ...StreamFilter) *StreamPipeline {
	sp := &StreamPipeline{}
	sp.Add(filters...)

	return sp
}

// Add appends non-nil stream filters to the pipeline chain.
func (sp *StreamPipeline) Add(filters ...StreamFilter) *StreamPipeline {
	for _, f := range filters {
		if f != nil {
			sp.filters = append(sp.filters, f)
		}
	}

	return sp
}

// Execute applies the stream pipeline sequentially over resp.Body.
func (sp *StreamPipeline) Execute(resp *http.Response) error {
	if resp == nil || resp.Body == nil || len(sp.filters) == 0 {
		return nil
	}

	var err error

	currBody := resp.Body

	for _, filter := range sp.filters {
		currBody, err = filter(resp, currBody)
		if err != nil {
			_ = resp.Body.Close()
			return err
		}
	}

	resp.Body = currBody

	return nil
}

// ExecuteStreamPipeline applies a slice of StreamFilters sequentially over resp.Body.
func ExecuteStreamPipeline(resp *http.Response, filters []StreamFilter) error {
	return NewStreamPipeline(filters...).Execute(resp)
}

// DecompressStreamFilter returns a StreamFilter that decompresses Brotli, Zstd, or Gzip payloads.
func DecompressStreamFilter(req *http.Request) StreamFilter {
	return func(r *http.Response, body io.ReadCloser) (io.ReadCloser, error) {
		if hasExplicitAcceptEncoding(req) {
			return body, nil
		}

		decompressedBody, decompressed := applyStreamDecompression(req, r, body)
		if decompressed {
			r.Uncompressed = true
		}

		return decompressedBody, nil
	}
}

func applyStreamDecompression(req *http.Request, resp *http.Response, body io.ReadCloser) (io.ReadCloser, bool) {
	encoding := resp.Header.Get("Content-Encoding")
	if encoding == "" || strings.EqualFold(encoding, "identity") {
		return body, false
	}

	normEnc := strings.ToLower(strings.TrimSpace(encoding))
	if normEnc == dict.ContentEncodingDCZ || normEnc == dict.ContentEncodingDCB {
		var dictData []byte
		if req != nil && req.Context() != nil {
			cfg := GetRequestConfig(req.Context())
			if cfg != nil && cfg.AvailableDictionary != nil {
				dictData = cfg.AvailableDictionary.Data
			}
		}

		if len(dictData) > 0 {
			reader, err := compress.NewDictionaryReader(normEnc, body, dictData)
			if err == nil {
				resetDecompressedHeader(resp)

				return reader, true
			}
		}
	}

	reader, err := compress.NewReader(encoding, body)
	if err != nil {
		return body, false
	}

	resetDecompressedHeader(resp)

	return reader, true
}

// TranscodeStreamFilter returns a StreamFilter that transcodes non-UTF-8 character sets to UTF-8.
func TranscodeStreamFilter() StreamFilter {
	return func(r *http.Response, body io.ReadCloser) (io.ReadCloser, error) {
		return applyCharsetTranscoding(r, body), nil
	}
}

// ProgressStreamFilter returns a StreamFilter that invokes onProgress as response bytes are read.
func ProgressStreamFilter(progress fio.ProgressFunc) StreamFilter {
	if progress == nil {
		return nil
	}

	return func(r *http.Response, body io.ReadCloser) (io.ReadCloser, error) {
		return &fio.ProgressReader{
			Reader:     body,
			Total:      r.ContentLength,
			OnProgress: progress,
		}, nil
	}
}
