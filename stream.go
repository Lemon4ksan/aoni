// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// StreamResponse wraps an http.Response and provides streaming methods.
// The caller is responsible for closing the response after reading is complete.
type StreamResponse struct {
	resp *http.Response
}

// Stream performs a GET request and returns the raw response body as a StreamResponse.
// The caller must close the StreamResponse when done.
func Stream(
	ctx context.Context,
	c Requester,
	path string,
	mods ...RequestModifier,
) (*StreamResponse, error) {
	resp, err := c.Request(ctx, http.MethodGet, path, mods...)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, &APIError{StatusCode: resp.StatusCode, Body: nil}
	}

	return &StreamResponse{resp: resp}, nil
}

// StreamWithBody performs a request with a body and returns the raw response body.
func StreamWithBody(
	ctx context.Context,
	c Requester,
	method, path string,
	body io.Reader,
	mods ...RequestModifier,
) (*StreamResponse, error) {
	resp, err := c.Request(ctx, method, path, append(mods, WithBody(body))...)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, &APIError{StatusCode: resp.StatusCode, Body: nil}
	}

	return &StreamResponse{resp: resp}, nil
}

// Read reads from the stream into p.
func (s *StreamResponse) Read(p []byte) (n int, err error) {
	return s.resp.Body.Read(p)
}

// Close closes the response body.
func (s *StreamResponse) Close() error {
	return s.resp.Body.Close()
}

// ContentLength returns the content length (-1 if unknown).
func (s *StreamResponse) ContentLength() int64 {
	return s.resp.ContentLength
}

// ContentType returns the Content-Type header.
func (s *StreamResponse) ContentType() string {
	return s.resp.Header.Get("Content-Type")
}

// StatusCode returns the HTTP status code.
func (s *StreamResponse) StatusCode() int {
	return s.resp.StatusCode
}

// Response returns the underlying http.Response.
// Use this for advanced access to headers, cookies, etc.
func (s *StreamResponse) Response() *http.Response {
	return s.resp
}

// io.Reader interface implementation
var _ io.Reader = (*StreamResponse)(nil)

// StreamNDJSON reads a newline-delimited JSON stream from the response body
// and sends parsed values to the returned channel.
// It runs a background goroutine and closes the channel when the stream is exhausted.
func StreamNDJSON[T any](ctx context.Context, resp *StreamResponse) (<-chan T, <-chan error) {
	out := make(chan T)
	errs := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errs)
		defer resp.Close()

		dec := json.NewDecoder(resp)
		for {
			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			default:
				var val T
				if err := dec.Decode(&val); err != nil {
					if errors.Is(err, io.EOF) {
						return
					}

					errs <- err

					return
				}

				select {
				case <-ctx.Done():
					errs <- ctx.Err()
					return
				case out <- val:
				}
			}
		}
	}()

	return out, errs
}

// SSEEvent represents a single Server-Sent Event.
type SSEEvent struct {
	Event string
	Data  string
	ID    string
	Retry int
}

// StreamSSE reads Server-Sent Events from the response body.
func StreamSSE(ctx context.Context, resp *StreamResponse) (<-chan SSEEvent, <-chan error) {
	out := make(chan SSEEvent)
	errs := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errs)
		defer resp.Close()

		reader := bufio.NewReader(resp)

		var currentEvent SSEEvent

		for {
			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			default:
				line, err := reader.ReadString('\n')
				if err != nil {
					if errors.Is(err, io.EOF) {
						return
					}

					errs <- err

					return
				}

				line = strings.TrimSpace(line)
				if line == "" {
					if currentEvent.Data != "" || currentEvent.Event != "" {
						select {
						case <-ctx.Done():
							errs <- ctx.Err()
							return
						case out <- currentEvent:
						}

						currentEvent = SSEEvent{}
					}

					continue
				}

				if strings.HasPrefix(line, ":") {
					continue
				}

				parts := strings.SplitN(line, ":", 2)
				key := parts[0]

				var value string
				if len(parts) > 1 {
					value = strings.TrimSpace(parts[1])
				}

				switch key {
				case "event":
					currentEvent.Event = value
				case "data":
					if currentEvent.Data != "" {
						currentEvent.Data += "\n" + value
					} else {
						currentEvent.Data = value
					}

				case "id":
					currentEvent.ID = value
				case "retry":
					if r, err := strconv.Atoi(value); err == nil {
						currentEvent.Retry = r
					}
				}
			}
		}
	}()

	return out, errs
}
