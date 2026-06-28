// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// StreamResponse wraps an [http.Response] and manages connection reading streams.
// Callers are responsible for calling [StreamResponse.Close] after read operations complete.
type StreamResponse struct {
	resp *http.Response
}

// Stream executes a GET request and returns the resulting connection body as [StreamResponse].
// Callers must ensure the returned stream is closed when done.
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

// StreamWithBody executes an HTTP request with the provided body and returns a raw [StreamResponse].
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

// Read reads connection body data into p.
func (s *StreamResponse) Read(p []byte) (n int, err error) {
	return s.resp.Body.Read(p)
}

// Close closes the underlying network response body stream.
func (s *StreamResponse) Close() error {
	return s.resp.Body.Close()
}

// ContentLength returns the response body content length, or -1 if unknown.
func (s *StreamResponse) ContentLength() int64 {
	return s.resp.ContentLength
}

// ContentType returns the Content-Type header field value.
func (s *StreamResponse) ContentType() string {
	return s.resp.Header.Get("Content-Type")
}

// StatusCode returns the HTTP status code of the response.
func (s *StreamResponse) StatusCode() int {
	return s.resp.StatusCode
}

// Response returns the underlying raw [http.Response] structure.
func (s *StreamResponse) Response() *http.Response {
	return s.resp
}

var _ io.Reader = (*StreamResponse)(nil)

// StreamNDJSON reads a newline-delimited JSON stream from the [StreamResponse] body.
// It parses values concurrently in a background goroutine and pushes them to the returned channel.
// It automatically closes channels and connection streams when done or on context cancellation.
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

// SSEEvent holds the parsed fields of a Server-Sent Event.
type SSEEvent struct {
	// Event is the event identifier string.
	Event string
	// Data is the data payload buffer string.
	Data string
	// ID is the unique event tracking ID.
	ID string
	// Retry is the reconnection timeout value in milliseconds.
	Retry int
}

// ParseSSE parses a Server-Sent Event stream and returns a channel of parsed events and an error channel.
func ParseSSE[T any](ctx context.Context, resp *StreamResponse) (<-chan T, <-chan error) {
	out := make(chan T, 100)
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
						var val T

						if sse, ok := any(currentEvent).(T); ok {
							val = sse
						} else if s, ok := any(currentEvent.Data).(T); ok {
							val = s
						} else {
							if err := json.Unmarshal([]byte(currentEvent.Data), &val); err != nil {
								errs <- fmt.Errorf("aoni sse: unmarshal failed: %w", err)
								return
							}
						}

						select {
						case <-ctx.Done():
							errs <- ctx.Err()
							return
						case out <- val:
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

// StreamSSE parses incoming Server-Sent Events from the [StreamResponse] body.
// It executes a background parsing loop and closes returned channels when done.
func StreamSSE[T any](
	ctx context.Context,
	c Requester,
	path string,
	mods ...RequestModifier,
) (<-chan T, <-chan error, error) {
	sseMods := []RequestModifier{ //nolint:prealloc
		WithHeader("Accept", "text/event-stream"),
		WithHeader("Cache-Control", "no-cache"),
		WithHeader("Connection", "keep-alive"),
	}
	mods = append(sseMods, mods...)

	resp, err := Stream(ctx, c, path, mods...)
	if err != nil {
		return nil, nil, err
	}

	out, errs := ParseSSE[T](ctx, resp)

	return out, errs, nil
}

// StreamChunks reads raw data from the stream chunk-by-chunk and yields them as strings.
// This is a high-level helper suitable for or real-time streaming.
func StreamChunks(ctx context.Context, resp *StreamResponse) (<-chan string, <-chan error) {
	out := make(chan string, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errs)
		defer resp.Close()

		buf := make([]byte, 4096)
		for {
			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			default:
				n, err := resp.Read(buf)
				if n > 0 {
					select {
					case <-ctx.Done():
						errs <- ctx.Err()
						return
					case out <- string(buf[:n]):
					}
				}

				if err != nil {
					if errors.Is(err, io.EOF) {
						return
					}

					errs <- err

					return
				}
			}
		}
	}()

	return out, errs
}
