// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	stdio "io"
	"net/http"

	"github.com/lemon4ksan/aoni/internal/io"
)

// StreamFilter defines a response body stream transformation filter.
// It receives the HTTP response metadata and current body reader stream,
// returning a transformed body reader stream or error.
type StreamFilter func(resp *http.Response, body stdio.ReadCloser) (stdio.ReadCloser, error)

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
	return func(r *http.Response, body stdio.ReadCloser) (stdio.ReadCloser, error) {
		if hasExplicitAcceptEncoding(req) {
			return body, nil
		}

		decompressedBody, decompressed := applyContentDecompression(r, body)
		if decompressed {
			r.Uncompressed = true
		}

		return decompressedBody, nil
	}
}

// TranscodeStreamFilter returns a StreamFilter that transcodes non-UTF-8 character sets to UTF-8.
func TranscodeStreamFilter() StreamFilter {
	return func(r *http.Response, body stdio.ReadCloser) (stdio.ReadCloser, error) {
		return applyCharsetTranscoding(r, body), nil
	}
}

// ProgressStreamFilter returns a StreamFilter that invokes onProgress as response bytes are read.
func ProgressStreamFilter(progress io.ProgressFunc) StreamFilter {
	if progress == nil {
		return nil
	}

	return func(r *http.Response, body stdio.ReadCloser) (stdio.ReadCloser, error) {
		return &io.ProgressReader{
			Reader:     body,
			Total:      r.ContentLength,
			OnProgress: progress,
		}, nil
	}
}
