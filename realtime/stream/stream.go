// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package stream provides utilities for consuming real-time HTTP response streams (SSE, NDJSON, Chunked, gRPC-Web).
package stream

import (
	"bufio"
	"bytes"
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

	"github.com/klauspost/compress/gzip"
	"github.com/lemon4ksan/miyako/generic"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/foundation/offheap"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/request"
)

// ErrTargetNotProtoMessage is returned when a target output variable does not implement [proto.Message].
var ErrTargetNotProtoMessage = errors.New("aoni/stream: target type does not implement proto.Message")

// Stream wraps an [*http.Response] and manages live connection response stream reads.
type Stream struct {
	resp *http.Response
}

// Get performs a GET request through r and yields the live response stream as a [Stream].
//
// Postconditions:
//   - Callers must close the returned [Stream] via [Stream.Close] when reading is complete.
func Get(
	ctx context.Context,
	r request.Requester,
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

// WithBody performs an HTTP request with body payload and yields the response stream as a [Stream].
func WithBody(
	ctx context.Context,
	r request.Requester,
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

// Read reads stream data into p.
func (s *Stream) Read(p []byte) (n int, err error) {
	return s.resp.Body.Read(p)
}

// Close closes the underlying network response body stream.
func (s *Stream) Close() error {
	return s.resp.Body.Close()
}

// ContentLength returns response body content length, or -1 if unknown.
func (s *Stream) ContentLength() int64 {
	return s.resp.ContentLength
}

// ContentType returns Content-Type response header value.
func (s *Stream) ContentType() string {
	return s.resp.Header.Get("Content-Type")
}

// StatusCode returns HTTP status code.
func (s *Stream) StatusCode() int {
	return s.resp.StatusCode
}

// Response returns the underlying raw [*http.Response].
func (s *Stream) Response() *http.Response {
	return s.resp
}

// GetNDJSON reads a newline-delimited JSON stream from resp, pushing decoded values to the returned channel.
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

// SSEEvent represents parsed fields of a Server-Sent Event frame.
type SSEEvent struct {
	Event string
	Data  string
	ID    string
	Retry int
}

func parseSSELine(line string, currentEvent *SSEEvent) {
	// Strip trailing CRLF
	line = strings.TrimRight(line, "\r\n")
	if line == "" || strings.HasPrefix(line, ":") {
		return
	}

	var key, value string
	if idx := strings.IndexByte(line, ':'); idx != -1 {
		key = line[:idx]
		value = strings.TrimPrefix(line[idx+1:], " ")
	} else {
		key = line
		value = ""
	}

	switch key {
	case "event":
		currentEvent.Event = value
	case "data":
		if currentEvent.Data != "" {
			currentEvent.Data += "\n" + value
			return
		}

		currentEvent.Data = value

	case "id":
		currentEvent.ID = value
	case "retry":
		if r, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			currentEvent.Retry = r
		}
	}
}

func dispatchSSEEvent[T any](ctx context.Context, currentEvent SSEEvent, out chan<- T) error {
	if currentEvent.Data == "" && currentEvent.Event == "" {
		return nil
	}

	// Gracefully handle LLM stream completion signals (e.g. OpenAI / Gemini data: [DONE])
	if strings.EqualFold(strings.TrimSpace(currentEvent.Data), "[DONE]") {
		return nil
	}

	var val T
	if sse, ok := any(currentEvent).(T); ok {
		val = sse
	} else if s, ok := any(currentEvent.Data).(T); ok {
		val = s
	} else {
		if err := json.Unmarshal([]byte(currentEvent.Data), &val); err != nil {
			return fmt.Errorf("aoni/stream: unmarshal sse failed: %w", err)
		}
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case out <- val:
		return nil
	}
}

// ParseSSE parses an incoming Server-Sent Event stream from resp.
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

// SSEReconnectOptions configures automatic stream reconnection parameters for Server-Sent Events.
type SSEReconnectOptions struct {
	MaxReconnects int
	DefaultRetry  time.Duration
}

// ResumableSSE opens an auto-reconnecting Server-Sent Event stream sending standard Last-Event-ID headers on recovery.
func ResumableSSE[T any](
	ctx context.Context,
	c request.Requester,
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

			var stackBuf [stackModCapacity]aoni.RequestModifier

			reqMods := buildSSERequestModifiers(mods, lastEventID, &stackBuf)

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

			if err := consumeSSEResponse(ctx, resp, out, &lastEventID, &reconnectDelay); err != nil {
				errs <- err
				return
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

const stackModCapacity = 16

func buildSSERequestModifiers(
	mods []aoni.RequestModifier,
	lastEventID string,
	stackBuf *[stackModCapacity]aoni.RequestModifier,
) []aoni.RequestModifier {
	baseCount := 3
	if lastEventID != "" {
		baseCount = 4
	}

	total := baseCount + len(mods)
	if total <= stackModCapacity {
		stackBuf[0] = mod.WithHeader("Accept", "text/event-stream")
		stackBuf[1] = mod.WithHeader("Cache-Control", "no-cache")

		stackBuf[2] = mod.WithHeader("Connection", "keep-alive")
		if lastEventID != "" {
			stackBuf[3] = mod.WithHeader("Last-Event-ID", lastEventID)
		}

		copy(stackBuf[baseCount:], mods)

		return stackBuf[:total]
	}

	res := make([]aoni.RequestModifier, total)
	res[0] = mod.WithHeader("Accept", "text/event-stream")
	res[1] = mod.WithHeader("Cache-Control", "no-cache")

	res[2] = mod.WithHeader("Connection", "keep-alive")
	if lastEventID != "" {
		res[3] = mod.WithHeader("Last-Event-ID", lastEventID)
	}

	copy(res[baseCount:], mods)

	return res
}

func consumeSSEResponse[T any](
	ctx context.Context,
	resp *Stream,
	out chan<- T,
	lastEventID *string,
	reconnectDelay *time.Duration,
) error {
	defer resp.Close()

	reader := bufio.NewReader(resp)

	var currentEvent SSEEvent

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			return nil
		}

		if strings.TrimSpace(line) == "" {
			if currentEvent.ID != "" {
				*lastEventID = currentEvent.ID
			}

			if currentEvent.Retry > 0 {
				*reconnectDelay = time.Duration(currentEvent.Retry) * time.Millisecond
			}

			if err := dispatchSSEEvent(ctx, currentEvent, out); err != nil {
				return err
			}

			currentEvent = SSEEvent{}

			continue
		}

		parseSSELine(line, &currentEvent)
	}
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

// SSE opens a Server-Sent Event stream on path and returns event channels.
func SSE[T any](
	ctx context.Context,
	c request.Requester,
	path string,
	mods ...aoni.RequestModifier,
) (<-chan T, <-chan error, error) {
	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := buildSSERequestModifiers(mods, "", &stackBuf)

	resp, err := Get(ctx, c, path, allMods...)
	if err != nil {
		return nil, nil, err
	}

	out, errs := ParseSSE[T](ctx, resp)

	return out, errs, nil
}

// Chunks reads raw data from resp in 32KB chunks and pushes strings to a channel.
func Chunks(ctx context.Context, resp *Stream) (<-chan string, <-chan error) {
	out := make(chan string, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errs)
		defer resp.Close()

		reader := bufio.NewReaderSize(resp, 1024*1024)
		buf := make([]byte, 32*1024)

		for {
			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			default:
				n, err := reader.Read(buf)
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

// ParseGRPCWebStream reads a gRPC-Web response stream and pushes decoded [proto.Message] instances to a channel.
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

		for {
			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			default:
				val, done, err := readNextGRPCWebFrame[T](reader)
				if err != nil {
					errs <- err
					return
				}

				if done {
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

func readNextGRPCWebFrame[T any](reader io.Reader) (val T, done bool, err error) {
	var (
		zero   T
		header [5]byte
	)

	if _, err := io.ReadFull(reader, header[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return zero, true, nil
		}

		return zero, false, err
	}

	flags := header[0]
	length := binary.BigEndian.Uint32(header[1:5])

	var payload []byte
	if length >= 16*1024 {
		offBuf, err := offheap.NewBuffer(int(length))
		if err == nil {
			defer offBuf.Release()

			payload = offBuf.Bytes()[:length]
			if _, rErr := io.ReadFull(reader, payload); rErr != nil {
				return zero, false, rErr
			}
		} else {
			payload = make([]byte, length)
			if _, rErr := io.ReadFull(reader, payload); rErr != nil {
				return zero, false, rErr
			}
		}
	} else {
		payload = make([]byte, length)
		if _, rErr := io.ReadFull(reader, payload); rErr != nil {
			return zero, false, rErr
		}
	}

	if flags&0x80 != 0 {
		if err := decode.VerifyGRPCTrailer(payload); err != nil {
			return zero, false, err
		}

		return zero, true, nil
	}

	val, err = decodeProtoPayload[T](payload, flags)
	if err != nil {
		return zero, false, err
	}

	return val, false, nil
}

func decodeProtoPayload[T any](payload []byte, flags byte) (T, error) {
	var zero T

	if flags&0x01 != 0 {
		gzReader, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return zero, fmt.Errorf("aoni/stream: decompress gRPC-Web frame failed: %w", err)
		}

		decompressed, err := io.ReadAll(gzReader)
		_ = gzReader.Close()

		if err != nil {
			return zero, fmt.Errorf("aoni/stream: read decompressed gRPC-Web payload failed: %w", err)
		}

		payload = decompressed
	}

	var target T

	msg, err := resolveProtoTargetInstance(&target)
	if err != nil {
		return zero, fmt.Errorf("aoni/stream: %w", err)
	}

	if err := proto.Unmarshal(payload, msg); err != nil {
		return zero, fmt.Errorf("aoni/stream: unmarshal gRPC-Web payload failed: %w", err)
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

	return nil, ErrTargetNotProtoMessage
}
