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
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/codec/json"
	"github.com/lemon4ksan/foundation/generic"
	fheader "github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/refkit"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/silicon/offheap"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/internal/compress"
	"github.com/lemon4ksan/aoni/mod"
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
	r aoni.HTTPRequester,
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
	r aoni.HTTPRequester,
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
	return s.resp.Header.Get(fheader.ContentType)
}

// StatusCode returns HTTP status code.
func (s *Stream) StatusCode() int {
	return s.resp.StatusCode
}

// Response returns the underlying raw [*http.Response].
func (s *Stream) Response() *http.Response {
	return s.resp
}

// Unwrap returns the underlying raw [*http.Response].
func (s *Stream) Unwrap() any {
	return s.resp
}

// IterSSE returns an iter.Seq2 range-over-func iterator over Server-Sent Events in s (Go 1.23+).
func IterSSE[T any](s *Stream) iter.Seq2[T, error] {
	return StreamSSE[T](s).All()
}

// IterNDJSON returns an iter.Seq2 range-over-func iterator over newline-delimited JSON items in s (Go 1.23+).
func IterNDJSON[T any](s *Stream) iter.Seq2[T, error] {
	return StreamNDJSON[T](s).All()
}

// IterGRPCWeb returns an iter.Seq2 range-over-func iterator over gRPC-Web messages in s (Go 1.23+).
func IterGRPCWeb[T any](s *Stream) iter.Seq2[T, error] {
	return StreamGRPCWeb[T](s).All()
}

// GetNDJSON reads a newline-delimited JSON stream from resp, pushing decoded values to channels.
func GetNDJSON[T any](ctx context.Context, resp *Stream) (<-chan T, <-chan error) {
	return StreamNDJSON[T](resp).Channel(ctx)
}

// ParseSSE parses an incoming Server-Sent Event stream from resp, pushing decoded events to channels.
func ParseSSE[T any](ctx context.Context, resp *Stream) (<-chan T, <-chan error) {
	return StreamSSE[T](resp).Channel(ctx)
}

// SSEEvent represents parsed fields of a Server-Sent Event frame.
type SSEEvent struct {
	Event string
	Data  string
	ID    string
	Retry int
}

func parseSSELineBytes(line []byte, ev *SSEEvent) {
	line = bytes.TrimRight(line, "\r\n")
	if len(line) == 0 || line[0] == ':' {
		return
	}

	key, value, ok := bytes.Cut(line, []byte{':'})
	if ok {
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
	} else {
		key = line
	}

	switch string(key) {
	case "event":
		ev.Event = string(value)
	case "data":
		if ev.Data != "" {
			ev.Data += "\n" + string(value)
			return
		}

		ev.Data = string(value)

	case "id":
		ev.ID = string(value)
	case "retry":
		if r, err := strconv.Atoi(string(bytes.TrimSpace(value))); err == nil {
			ev.Retry = r
		}
	}
}

func dispatchSSEEvent[T any](ctx context.Context, ev SSEEvent, out chan<- T) error {
	if ev.Data == "" && ev.Event == "" {
		return nil
	}

	// Gracefully handle LLM stream completion signals (e.g. OpenAI / Gemini data: [DONE])
	if strings.EqualFold(strings.TrimSpace(ev.Data), "[DONE]") {
		return nil
	}

	val, err := decodeSSEPayload[T](ev)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case out <- val:
		return nil
	}
}

func decodeSSEPayload[T any](ev SSEEvent) (T, error) {
	if sse, ok := any(ev).(T); ok {
		return sse, nil
	}

	if s, ok := any(ev.Data).(T); ok {
		return s, nil
	}

	var val T

	dataBytes := bytesconv.S2B(ev.Data)
	if err := json.UnmarshalNoCopy(dataBytes, &val); err != nil {
		return val, fmt.Errorf("aoni/stream: unmarshal sse failed: %w", err)
	}

	return val, nil
}

// SSEReader provides sequential decoded access to W3C Server-Sent Event streams.
type SSEReader[T any] struct {
	br     *bufio.Reader
	closer io.Closer
}

// NewSSEReader wraps r into an [SSEReader] for sequential SSE event streaming.
func NewSSEReader[T any](r io.ReadCloser) *SSEReader[T] {
	return &SSEReader[T]{
		br:     bufio.NewReader(r),
		closer: r,
	}
}

// StreamSSE creates an [SSEReader] over stream s.
func StreamSSE[T any](s *Stream) *SSEReader[T] {
	if s == nil || s.resp == nil {
		return nil
	}

	return NewSSEReader[T](s.resp.Body)
}

// Next reads and decodes the next Server-Sent Event payload into T as a [generic.Result].
// Returns a Failure wrapping [io.EOF] when the stream terminates normally.
func (r *SSEReader[T]) Next() generic.Result[T] {
	if r == nil || r.br == nil {
		return generic.Failure[T](io.EOF)
	}

	event, err := r.NextEvent()
	if err != nil {
		return generic.Failure[T](err)
	}

	val, err := decodeSSEPayload[T](event)
	if err != nil {
		return generic.Failure[T](err)
	}

	return generic.Success(val)
}

// NextEvent reads the next raw [SSEEvent] according to the W3C Server-Sent Events specification.
func (r *SSEReader[T]) NextEvent() (SSEEvent, error) {
	if r == nil || r.br == nil {
		return SSEEvent{}, io.EOF
	}

	var currentEvent SSEEvent

	for {
		lineBytes, err := r.br.ReadSlice('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(lineBytes) > 0 {
					parseSSELineBytes(lineBytes, &currentEvent)

					if currentEvent.Data != "" || currentEvent.Event != "" {
						if strings.EqualFold(strings.TrimSpace(currentEvent.Data), "[DONE]") {
							return SSEEvent{}, io.EOF
						}

						return currentEvent, nil
					}
				}

				return SSEEvent{}, io.EOF
			}

			return SSEEvent{}, err
		}

		if len(bytes.TrimRight(lineBytes, "\r\n")) == 0 {
			if currentEvent.Data != "" || currentEvent.Event != "" {
				if strings.EqualFold(strings.TrimSpace(currentEvent.Data), "[DONE]") {
					return SSEEvent{}, io.EOF
				}

				return currentEvent, nil
			}

			continue
		}

		parseSSELineBytes(lineBytes, &currentEvent)
	}
}

// All returns a Go 1.23+ range-over-func iterator over all decoded items in the SSE stream.
func (r *SSEReader[T]) All() iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for {
			val, err := r.Next().Unwrap()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					yield(generic.Zero[T](), err)
				}

				return
			}

			if !yield(val, nil) {
				return
			}
		}
	}
}

// SeqToChan converts any iter.Seq2[T, error] range-over-func iterator into asynchronous channels with clean lifecycle management.
func SeqToChan[T any](ctx context.Context, seq iter.Seq2[T, error], closer io.Closer) (<-chan T, <-chan error) {
	out := make(chan T)
	errs := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errs)

		if closer != nil {
			defer closer.Close()
		}

		for val, err := range seq {
			if ctx.Err() != nil {
				errs <- ctx.Err()
				return
			}

			if err != nil {
				if !errors.Is(err, io.EOF) {
					if ctx.Err() != nil {
						errs <- ctx.Err()
					} else {
						errs <- err
					}
				}

				return
			}

			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			case out <- val:
			}
		}

		if ctx.Err() != nil {
			errs <- ctx.Err()
		}
	}()

	return out, errs
}

// Channel yields items over channels for asynchronous consumption.
func (r *SSEReader[T]) Channel(ctx context.Context) (<-chan T, <-chan error) {
	return SeqToChan(ctx, r.All(), r)
}

// Close closes the underlying connection reader.
func (r *SSEReader[T]) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}

	return r.closer.Close()
}

// SSEReconnectOptions configures automatic stream reconnection parameters for Server-Sent Events.
type SSEReconnectOptions struct {
	MaxReconnects int
	DefaultRetry  time.Duration
}

// ResumableSSE opens an auto-reconnecting Server-Sent Event stream sending standard Last-Event-ID headers on recovery.
func ResumableSSE[T any](
	ctx context.Context,
	c aoni.HTTPRequester,
	path string,
	opts SSEReconnectOptions,
	mods ...aoni.RequestModifier,
) (<-chan T, <-chan error, error) {
	out := make(chan T, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errs)

		var lastEventID string

		retryDelay := generic.Coalesce(opts.DefaultRetry, 3*time.Second)
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

				if !sleepOrCancel(ctx, retryDelay, errs) {
					return
				}

				continue
			}

			attempts = 0

			if err := consumeSSEResponse(ctx, resp, out, &lastEventID, &retryDelay); err != nil {
				errs <- err
				return
			}

			if resp.StatusCode() == http.StatusNoContent {
				return
			}

			if !sleepOrCancel(ctx, retryDelay, errs) {
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
	baseCount := generic.Ternary(lastEventID != "", 4, 3)

	total := baseCount + len(mods)
	if total <= stackModCapacity {
		stackBuf[0] = mod.WithHeader(fheader.Accept, fheader.MIMETextEventStream)
		stackBuf[1] = mod.WithHeader(fheader.CacheControl, fheader.ValueNoCache)

		stackBuf[2] = mod.WithHeader(fheader.Connection, fheader.ValueKeepAlive)
		if lastEventID != "" {
			stackBuf[3] = mod.WithHeader(fheader.LastEventID, lastEventID)
		}

		copy(stackBuf[baseCount:], mods)

		return stackBuf[:total]
	}

	res := make([]aoni.RequestModifier, total)
	res[0] = mod.WithHeader(fheader.Accept, fheader.MIMETextEventStream)
	res[1] = mod.WithHeader(fheader.CacheControl, fheader.ValueNoCache)

	res[2] = mod.WithHeader(fheader.Connection, fheader.ValueKeepAlive)
	if lastEventID != "" {
		res[3] = mod.WithHeader(fheader.LastEventID, lastEventID)
	}

	copy(res[baseCount:], mods)

	return res
}

func consumeSSEResponse[T any](
	ctx context.Context,
	resp *Stream,
	out chan<- T,
	lastEventID *string,
	retryDelay *time.Duration,
) error {
	reader := StreamSSE[T](resp)
	defer reader.Close()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		event, err := reader.NextEvent()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			return err
		}

		if event.ID != "" {
			*lastEventID = event.ID
		}

		if event.Retry > 0 {
			*retryDelay = time.Duration(event.Retry) * time.Millisecond
		}

		if err := dispatchSSEEvent(ctx, event, out); err != nil {
			return err
		}
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
	c aoni.HTTPRequester,
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

// ChunkReader provides sequential chunked read access to an underlying stream.
type ChunkReader struct {
	reader    io.Reader
	closer    io.Closer
	chunkSize int
	buf       []byte
	done      bool
}

// NewChunkReader wraps r with a [ChunkReader] reading blocks of chunkSize bytes.
// If chunkSize <= 0, a default 32KB buffer size is used.
func NewChunkReader(r io.ReadCloser, chunkSize int) *ChunkReader {
	if chunkSize <= 0 {
		chunkSize = 32 * 1024
	}

	return &ChunkReader{
		reader:    r,
		closer:    r,
		chunkSize: chunkSize,
		buf:       make([]byte, chunkSize),
	}
}

// StreamChunks instantiates a [ChunkReader] reading from the provided [Stream].
func StreamChunks(resp *Stream, chunkSize ...int) *ChunkReader {
	size := 32 * 1024
	if len(chunkSize) > 0 && chunkSize[0] > 0 {
		size = chunkSize[0]
	}

	return NewChunkReader(resp, size)
}

// Next reads the next chunk of raw bytes from the stream.
// Returns an error with io.EOF on end of stream.
// The returned byte slice is valid until the next call to Next.
func (r *ChunkReader) Next() generic.Result[[]byte] {
	if r.done {
		return generic.Failure[[]byte](io.EOF)
	}

	n, err := r.reader.Read(r.buf)
	if n > 0 {
		if err != nil && errors.Is(err, io.EOF) {
			r.done = true
		}

		return generic.Success(r.buf[:n])
	}

	if err != nil {
		r.done = true
		return generic.Failure[[]byte](err)
	}

	r.done = true

	return generic.Failure[[]byte](io.EOF)
}

// Close closes the underlying stream source.
func (r *ChunkReader) Close() error {
	if r.closer != nil {
		return r.closer.Close()
	}

	return nil
}

// All yields an [iter.Seq2] sequence compatible with Go 1.23+ range-over-func loops.
func (r *ChunkReader) All() iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		defer r.Close()

		for {
			chunk, err := r.Next().Unwrap()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}

				yield(nil, err)

				return
			}

			if !yield(chunk, nil) {
				return
			}
		}
	}
}

// IterChunks returns a Go 1.23+ range-over-func iterator yielding byte chunks directly from resp.
func IterChunks(resp *Stream, chunkSize ...int) iter.Seq2[[]byte, error] {
	return StreamChunks(resp, chunkSize...).All()
}

// Channel adapts the ChunkReader to push chunks to asynchronous Go channels.
func (r *ChunkReader) Channel(ctx context.Context) (<-chan string, <-chan error) {
	stringSeq := func(yield func(string, error) bool) {
		for chunk, err := range r.All() {
			if err != nil {
				yield("", err)
				return
			}

			if !yield(string(chunk), nil) {
				return
			}
		}
	}

	return SeqToChan(ctx, stringSeq, r)
}

// Chunks reads raw data from resp in 32KB chunks and pushes strings to a channel.
func Chunks(ctx context.Context, resp *Stream) (<-chan string, <-chan error) {
	return StreamChunks(resp).Channel(ctx)
}

// ParseGRPCWebStream reads a gRPC-Web response stream and pushes decoded [proto.Message] instances to channels.
func ParseGRPCWebStream[T any](ctx context.Context, resp *Stream) (<-chan T, <-chan error) {
	return StreamGRPCWeb[T](resp).Channel(ctx)
}

// GRPCWebReader provides sequential decoded access to gRPC-Web 5-byte framed Protobuf streams.
type GRPCWebReader[T any] struct {
	reader io.Reader
	closer io.Closer
}

// NewGRPCWebReader wraps r with a typed [GRPCWebReader].
func NewGRPCWebReader[T any](r io.ReadCloser) *GRPCWebReader[T] {
	br := bufio.NewReader(r)

	var reader io.Reader = br
	if peek, err := br.Peek(5); err == nil && decode.IsBase64Header(peek) {
		reader = base64.NewDecoder(base64.StdEncoding, br)
	}

	return &GRPCWebReader[T]{
		reader: reader,
		closer: r,
	}
}

// StreamGRPCWeb creates a [GRPCWebReader] over stream s.
func StreamGRPCWeb[T any](s *Stream) *GRPCWebReader[T] {
	if s == nil || s.resp == nil {
		return nil
	}

	return NewGRPCWebReader[T](s.resp.Body)
}

// Next decodes the next gRPC-Web Protobuf message in the stream as a [generic.Result].
// Returns a Failure wrapping [io.EOF] when the stream finishes or reaches trailers.
func (r *GRPCWebReader[T]) Next() generic.Result[T] {
	if r == nil || r.reader == nil {
		return generic.Failure[T](io.EOF)
	}

	val, done, err := readNextGRPCWebFrame[T](r.reader)
	if err != nil {
		return generic.Failure[T](err)
	}

	if done {
		return generic.Failure[T](io.EOF)
	}

	return generic.Success(val)
}

// All returns a Go 1.23+ range-over-func iterator over all decoded Protobuf messages in the stream.
func (r *GRPCWebReader[T]) All() iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for {
			val, err := r.Next().Unwrap()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					yield(generic.Zero[T](), err)
				}

				return
			}

			if !yield(val, nil) {
				return
			}
		}
	}
}

// Channel yields items over channels for asynchronous consumption.
func (r *GRPCWebReader[T]) Channel(ctx context.Context) (<-chan T, <-chan error) {
	return SeqToChan(ctx, r.All(), r)
}

// Close closes the underlying stream reader.
func (r *GRPCWebReader[T]) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}

	return r.closer.Close()
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
		offBuf, bufErr := offheap.NewBuffer(int(length))
		if bufErr == nil {
			defer offBuf.Release()

			payload = offBuf.Bytes()[:length]
		}
	}

	if payload == nil {
		payload = make([]byte, length)
	}

	if _, rErr := io.ReadFull(reader, payload); rErr != nil {
		return zero, false, rErr
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
		decompressed, err := compress.Gunzip(payload, nil)
		if err != nil {
			return zero, fmt.Errorf("aoni/stream: decompress gRPC-Web frame failed: %w", err)
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
		elem, _ := refkit.EnsureAlloc(val.Elem())
		if elem.IsValid() && elem.CanAddr() {
			if msg, ok := elem.Addr().Interface().(proto.Message); ok {
				return msg, nil
			}
		}

		if elem.IsValid() && elem.CanInterface() {
			if msg, ok := elem.Interface().(proto.Message); ok {
				return msg, nil
			}
		}
	}

	return nil, ErrTargetNotProtoMessage
}

// GetResult performs a GET request through r and yields the live response stream as a [generic.Result].
func GetResult(
	ctx context.Context,
	r aoni.HTTPRequester,
	path string,
	mods ...aoni.RequestModifier,
) generic.Result[*Stream] {
	st, err := Get(ctx, r, path, mods...)
	if err != nil {
		return generic.Failure[*Stream](err)
	}

	return generic.Success(st)
}

// NDJSONReader provides sequential decoded access to newline-delimited JSON streams.
type NDJSONReader[T any] struct {
	br     *bufio.Reader
	closer io.Closer
}

// NewNDJSONReader wraps reader with a typed [NDJSONReader].
func NewNDJSONReader[T any](r io.ReadCloser) *NDJSONReader[T] {
	return &NDJSONReader[T]{
		br:     bufio.NewReader(r),
		closer: r,
	}
}

// StreamNDJSON creates an [NDJSONReader] over the stream.
func StreamNDJSON[T any](s *Stream) *NDJSONReader[T] {
	if s == nil || s.resp == nil {
		return nil
	}

	return NewNDJSONReader[T](s.resp.Body)
}

// Next decodes the next JSON record in the stream as a [generic.Result].
// When the stream finishes normally, it returns a Failure wrapping [io.EOF].
func (r *NDJSONReader[T]) Next() generic.Result[T] {
	if r == nil || r.br == nil {
		return generic.Failure[T](io.EOF)
	}

	for {
		line, err := r.br.ReadBytes('\n')
		if err != nil && len(line) == 0 {
			return generic.Failure[T](err)
		}

		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			if err != nil {
				return generic.Failure[T](err)
			}

			continue
		}

		var val T

		if unmarshalErr := safeUnmarshalJSON(trimmed, &val); unmarshalErr != nil {
			return generic.Failure[T](unmarshalErr)
		}

		return generic.Success(val)
	}
}

func safeUnmarshalJSON(data []byte, val any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("aoni/stream: malformed json payload: %v", r)
		}
	}()

	return json.UnmarshalNoCopy(data, val)
}

// All returns a Go 1.23+ range-over-func iterator over all decoded records in the NDJSON stream.
func (r *NDJSONReader[T]) All() iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for {
			val, err := r.Next().Unwrap()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					yield(generic.Zero[T](), err)
				}

				return
			}

			if !yield(val, nil) {
				return
			}
		}
	}
}

// Channel yields items over channels for asynchronous consumption.
func (r *NDJSONReader[T]) Channel(ctx context.Context) (<-chan T, <-chan error) {
	return SeqToChan(ctx, r.All(), r)
}

// Close closes the underlying stream reader.
func (r *NDJSONReader[T]) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}

	return r.closer.Close()
}
