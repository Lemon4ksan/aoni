// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stream

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/mod"
)

// Stream wraps an [http.Response] and manages connection reading streams.
// Callers are responsible for calling [Stream.Close] after read operations complete.
type Stream struct {
	resp *http.Response
}

// Get executes a GET request and returns the resulting connection body as [Stream].
// Callers must ensure the returned stream is closed when done.
func Get(
	ctx context.Context,
	r aoni.Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*Stream, error) {
	resp, err := r.Request(ctx, http.MethodGet, path, mods...)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, &aoni.APIError{StatusCode: resp.StatusCode, Body: nil}
	}

	return &Stream{resp: resp}, nil
}

// WithBody executes an HTTP request with the provided body and returns a raw [Stream].
func WithBody(
	ctx context.Context,
	r aoni.Requester,
	method, path string,
	body io.Reader,
	mods ...aoni.RequestModifier,
) (*Stream, error) {
	resp, err := r.Request(ctx, method, path, append(mods, mod.WithBody(body))...)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, &aoni.APIError{StatusCode: resp.StatusCode, Body: nil}
	}

	return &Stream{resp: resp}, nil
}

// Read reads connection body data into p.
func (s *Stream) Read(p []byte) (n int, err error) {
	return s.resp.Body.Read(p)
}

// Close closes the underlying network response body stream.
func (s *Stream) Close() error {
	return s.resp.Body.Close()
}

// ContentLength returns the response body content length, or -1 if unknown.
func (s *Stream) ContentLength() int64 {
	return s.resp.ContentLength
}

// ContentType returns the Content-Type header field value.
func (s *Stream) ContentType() string {
	return s.resp.Header.Get("Content-Type")
}

// StatusCode returns the HTTP status code of the response.
func (s *Stream) StatusCode() int {
	return s.resp.StatusCode
}

// Response returns the underlying raw [http.Response] structure.
func (s *Stream) Response() *http.Response {
	return s.resp
}

// GetNDJSON reads a newline-delimited JSON stream from the [Stream] body.
// It parses values concurrently in a background goroutine and pushes them to the returned channel.
// It automatically closes channels and connection streams when done or on context cancellation.
func GetNDJSON[T any](ctx context.Context, resp *Stream) (<-chan T, <-chan error) {
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

func parseSSELine(line string, currentEvent *SSEEvent) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, ":") {
		return
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

func dispatchSSEEvent[T any](ctx context.Context, currentEvent SSEEvent, out chan<- T) error {
	if currentEvent.Data == "" && currentEvent.Event == "" {
		return nil
	}

	var val T
	if sse, ok := any(currentEvent).(T); ok {
		val = sse
	} else if s, ok := any(currentEvent.Data).(T); ok {
		val = s
	} else {
		if err := json.Unmarshal([]byte(currentEvent.Data), &val); err != nil {
			return fmt.Errorf("aoni sse: unmarshal failed: %w", err)
		}
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case out <- val:
		return nil
	}
}

// ParseSSE parses a Server-Sent Event stream and returns a channel of parsed events and an error channel.
func ParseSSE[T any](ctx context.Context, resp *Stream) (<-chan T, <-chan error) {
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

				if strings.TrimSpace(line) == "" {
					if err := dispatchSSEEvent(ctx, currentEvent, out); err != nil {
						errs <- err
						return
					}

					currentEvent = SSEEvent{}

					continue
				}

				parseSSELine(line, &currentEvent)
			}
		}
	}()

	return out, errs
}

// SSEReconnectOptions configures automatic stream reconnection behavior for SSE streams.
type SSEReconnectOptions struct {
	// MaxReconnects specifies the maximum number of reconnection attempts after network drops (0 = infinite reconnects).
	MaxReconnects int
	// DefaultRetry specifies the initial delay before attempting to reconnect (default 3 seconds).
	DefaultRetry time.Duration
}

// ResumableSSE opens a Server-Sent Event stream that automatically reconnects on network drops.
// On reconnection, it sends the WHATWG standard Last-Event-ID header containing the last seen event ID
// and respects any server-sent 'retry: <ms>' directives.
func ResumableSSE[T any](
	ctx context.Context,
	c aoni.Requester,
	path string,
	opts SSEReconnectOptions,
	mods ...aoni.RequestModifier,
) (<-chan T, <-chan error, error) {
	out := make(chan T, 100)
	errs := make(chan error, 1)

	defaultRetry := generic.Coalesce(opts.DefaultRetry, 3*time.Second)

	go func() {
		defer close(out)
		defer close(errs)

		var lastEventID string

		reconnectDelay := defaultRetry
		attempts := 0

		for {
			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			default:
			}

			reqMods := make([]aoni.RequestModifier, 0, len(mods)+4)

			reqMods = append(reqMods,
				mod.WithHeader("Accept", "text/event-stream"),
				mod.WithHeader("Cache-Control", "no-cache"),
				mod.WithHeader("Connection", "keep-alive"),
			)
			if lastEventID != "" {
				reqMods = append(reqMods, mod.WithHeader("Last-Event-ID", lastEventID))
			}

			reqMods = append(reqMods, mods...)

			resp, err := Get(ctx, c, path, reqMods...)
			if err != nil {
				attempts++
				if opts.MaxReconnects > 0 && attempts > opts.MaxReconnects {
					errs <- fmt.Errorf("aoni sse: max reconnect attempts reached (%d): %w", opts.MaxReconnects, err)
					return
				}

				if !sleepOrCancel(ctx, reconnectDelay, errs) {
					return
				}

				continue
			}

			attempts = 0
			reader := bufio.NewReader(resp)

			var currentEvent SSEEvent

			for {
				select {
				case <-ctx.Done():
					_ = resp.Close()

					errs <- ctx.Err()

					return

				default:
				}

				line, err := reader.ReadString('\n')
				if err != nil {
					_ = resp.Close()
					break
				}

				if strings.TrimSpace(line) == "" {
					if currentEvent.ID != "" {
						lastEventID = currentEvent.ID
					}

					if currentEvent.Retry > 0 {
						reconnectDelay = time.Duration(currentEvent.Retry) * time.Millisecond
					}

					if err := dispatchSSEEvent(ctx, currentEvent, out); err != nil {
						errs <- err
						return
					}

					currentEvent = SSEEvent{}

					continue
				}

				parseSSELine(line, &currentEvent)
			}

			if resp.StatusCode() == http.StatusNoContent {
				return
			}

			if !sleepOrCancel(ctx, reconnectDelay, errs) {
				return
			}
		}
	}()

	return out, errs, nil
}

func sleepOrCancel(ctx context.Context, delay time.Duration, errs chan<- error) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		errs <- ctx.Err()
		return false
	case <-timer.C:
		return true
	}
}

// SSE parses incoming Server-Sent Events from the [Stream] body.
// It executes a background parsing loop and closes returned channels when done.
func SSE[T any](
	ctx context.Context,
	c aoni.Requester,
	path string,
	mods ...aoni.RequestModifier,
) (<-chan T, <-chan error, error) {
	sseMods := []aoni.RequestModifier{ //nolint:prealloc
		mod.WithHeader("Accept", "text/event-stream"),
		mod.WithHeader("Cache-Control", "no-cache"),
		mod.WithHeader("Connection", "keep-alive"),
	}
	mods = append(sseMods, mods...)

	resp, err := Get(ctx, c, path, mods...)
	if err != nil {
		return nil, nil, err
	}

	out, errs := ParseSSE[T](ctx, resp)

	return out, errs, nil
}

// Chunks reads raw data from the stream chunk-by-chunk and yields them as strings.
// This is a high-level helper suitable for or real-time streaming.
func Chunks(ctx context.Context, resp *Stream) (<-chan string, <-chan error) {
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

// ParseGRPCWebStream reads a gRPC-Web response stream and yields decoded Protobuf messages to a channel.
// ParseGRPCWebStream reads a gRPC-Web response stream and yields decoded Protobuf messages to a channel.
func ParseGRPCWebStream[T any](ctx context.Context, resp *Stream) (<-chan T, <-chan error) {
	out := make(chan T, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errs)
		defer resp.Close()

		br := bufio.NewReader(resp.resp.Body)

		var reader io.Reader = br

		if peek, err := br.Peek(5); err == nil && decode.IsBase64Header(peek) {
			reader = base64.NewDecoder(base64.StdEncoding, br)
		}

		var header [5]byte

		for {
			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			default:
			}

			if _, err := io.ReadFull(reader, header[:]); err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
					return
				}

				errs <- err

				return
			}

			flags := header[0]
			length := binary.BigEndian.Uint32(header[1:5])

			payload := make([]byte, length)
			if _, err := io.ReadFull(reader, payload); err != nil {
				errs <- err
				return
			}

			if flags&0x80 != 0 {
				if err := decode.VerifyGRPCTrailer(payload); err != nil {
					errs <- err
				}

				return
			}

			val, err := decodeProtoPayload[T](payload, flags)
			if err != nil {
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
	}()

	return out, errs
}

func decodeProtoPayload[T any](payload []byte, flags byte) (T, error) {
	var zero T

	if flags&0x01 != 0 {
		gzReader, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return zero, fmt.Errorf("aoni stream: decompress gRPC-Web frame failed: %w", err)
		}

		decompressed, err := io.ReadAll(gzReader)
		_ = gzReader.Close()

		if err != nil {
			return zero, fmt.Errorf("aoni stream: read decompressed gRPC-Web payload failed: %w", err)
		}

		payload = decompressed
	}

	var target T

	msg, err := resolveProtoTargetInstance(&target)
	if err != nil {
		return zero, fmt.Errorf("aoni stream: %w", err)
	}

	if err := proto.Unmarshal(payload, msg); err != nil {
		return zero, fmt.Errorf("aoni stream: unmarshal gRPC-Web payload failed: %w", err)
	}

	return target, nil
}

func resolveProtoTargetInstance(targetPtr any) (proto.Message, error) {
	if msg, ok := targetPtr.(proto.Message); ok {
		return msg, nil
	}

	val := reflect.ValueOf(targetPtr)
	if val.Kind() == reflect.Pointer && !val.IsNil() {
		elem := val.Elem()
		if msg, ok := elem.Interface().(proto.Message); ok {
			return msg, nil
		}

		if elem.Kind() == reflect.Pointer && elem.IsNil() && elem.CanSet() {
			elem.Set(reflect.New(elem.Type().Elem()))

			if msg, ok := elem.Interface().(proto.Message); ok {
				return msg, nil
			}
		}
	}

	return nil, fmt.Errorf("target type %T does not implement proto.Message", targetPtr)
}
