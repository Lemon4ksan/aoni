// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package grpc provides a native lightweight gRPC client implementation over aoni's stealth HTTP/2 transport.
package grpc

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	asyncctx "github.com/lemon4ksan/foundation/async/context"
	fheader "github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/pathkit"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/mod"
)

// Metadata represents Custom-Metadata key-value headers per PROTOCOL-HTTP2.md.
type Metadata map[string]string

// Invoke executes a native gRPC call over aoni's stealth HTTP/2 transport.
//
// Zero-Dependency gRPC:
// Operates directly on raw HTTP/2 frames without dragging in google.golang.org/grpc.
// Inherits aoni's uTLS Chrome fingerprints, p0f OS spoofing, and HPACK header ordering.
//
// Preconditions:
//   - ctx and reqMsg must be non-nil.
//   - Resp must be a pointer or struct type implementing [proto.Message].
func Invoke[Resp any](
	ctx context.Context,
	doer core.HTTPRequester,
	fullMethod string,
	reqMsg proto.Message,
	mods ...core.RequestModifier,
) (*Resp, error) {
	ctx = asyncctx.Wrap(ctx)

	frameBytes, err := MarshalFrame(reqMsg, false)
	if err != nil {
		return nil, err
	}

	path := normalizeMethodPath(fullMethod)
	grpcMods := prepareGRPCModifiers(ctx, frameBytes, false, mods)

	resp, err := doer.Request(ctx, http.MethodPost, path, grpcMods...)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := validateInitialHeaders(resp); err != nil {
		return nil, err
	}

	result := new(Resp)

	msg, ok := any(result).(proto.Message)
	if !ok {
		return nil, fmt.Errorf("aoni/grpc: response type %T does not implement proto.Message", result)
	}

	if _, err := UnmarshalFrame(resp.Body, msg); err != nil {
		return nil, err
	}

	if err := validateResponseTrailers(resp); err != nil {
		return nil, err
	}

	return result, nil
}

// InvokeInto executes a native unary gRPC call and unmarshals the response frame directly into target message without allocation.
func InvokeInto(
	ctx context.Context,
	doer core.HTTPRequester,
	fullMethod string,
	reqMsg proto.Message,
	target proto.Message,
	mods ...core.RequestModifier,
) error {
	ctx = asyncctx.Wrap(ctx)

	frameBytes, err := MarshalFrame(reqMsg, false)
	if err != nil {
		return err
	}

	path := normalizeMethodPath(fullMethod)
	grpcMods := prepareGRPCModifiers(ctx, frameBytes, false, mods)

	resp, err := doer.Request(ctx, http.MethodPost, path, grpcMods...)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if err := validateInitialHeaders(resp); err != nil {
		return err
	}

	if _, err := UnmarshalFrame(resp.Body, target); err != nil {
		return err
	}

	return validateResponseTrailers(resp)
}

// normalizeMethodPath guarantees that the gRPC method path starts with a leading slash.
func normalizeMethodPath(fullMethod string) string {
	if !strings.HasPrefix(fullMethod, "/") && !pathkit.New(fullMethod).IsURL() {
		return "/" + fullMethod
	}

	return fullMethod
}

// prepareGRPCModifiers builds standard gRPC request headers (Content-Type, TE, User-Agent, Timeout, and Metadata).
func prepareGRPCModifiers(
	ctx context.Context,
	frameBytes []byte,
	isStreaming bool,
	mods []core.RequestModifier,
) []core.RequestModifier {
	grpcMods := make([]core.RequestModifier, 0, len(mods)+5)
	grpcMods = append(grpcMods,
		mod.WithContentType(fheader.MIMEApplicationGRPC),
		mod.WithHeader(fheader.TE, fheader.ValueTrailers),
		mod.WithHeader(fheader.UserAgent, "grpc-aoni/1.0"),
		mod.WithBody(bytes.NewReader(frameBytes)),
	)

	if isStreaming {
		grpcMods = append(
			grpcMods,
			mod.WithHeader(fheader.GRPCAcceptEncoding, fheader.ValueGzip+", "+fheader.ValueIdentity),
		)
	}

	if deadline, ok := ctx.Deadline(); ok {
		grpcMods = append(grpcMods, mod.WithHeader(fheader.GRPCTimeout, formatTimeout(time.Until(deadline))))
	}

	if md, ok := FromContext(ctx); ok && len(md) > 0 {
		grpcMods = append(grpcMods, WithMetadata(md))
	}

	grpcMods = append(grpcMods, mods...)

	return grpcMods
}

// unmarshalFastGRPCFrame decodes Protobuf payload bytes from a fast client response.
func unmarshalFastGRPCFrame(resp core.Response, msg proto.Message) error {
	if stream := resp.BodyStream(); stream != nil && resp.HTTPResponse() == nil { //nolint:bodyclose
		_, err := UnmarshalFrame(stream, msg)
		return err
	}

	_, err := UnmarshalFrame(bytes.NewReader(resp.UnsafeBodyBytes()), msg)

	return err
}

// validateInitialHeaders verifies that HTTP status is 200 and Content-Type starts with application/grpc.
func validateInitialHeaders(resp *http.Response) error {
	if resp.StatusCode != http.StatusOK {
		return &StatusError{
			Code:    StatusUnknown,
			Message: "non-200 HTTP status: " + resp.Status,
		}
	}

	ct := resp.Header.Get(fheader.ContentType)
	if ct != "" && !strings.HasPrefix(ct, fheader.MIMEApplicationGRPC) {
		return fmt.Errorf("%w: %s", ErrInvalidContentType, ct)
	}

	// Check "Trailers-Only" response case (when error status is delivered directly in initial response headers)
	if statusCode := resp.Header.Get(fheader.GRPCStatus); statusCode != "" && statusCode != "0" {
		return parseGRPCStatus(resp.Header)
	}

	return nil
}

// validateResponseTrailers inspects HTTP/2 response trailers for grpc-status codes.
func validateResponseTrailers(resp *http.Response) error {
	trailers := resp.Trailer
	if len(trailers) == 0 {
		trailers = resp.Header
	}

	statusCode := trailers.Get(fheader.GRPCStatus)
	if statusCode == "" {
		return ErrMissingGRPCStatus
	}

	if statusCode != "0" {
		return parseGRPCStatus(trailers)
	}

	return nil
}

// EncodeBinaryHeader base64-encodes binary metadata for headers ending in "-bin".
func EncodeBinaryHeader(val []byte) string {
	return base64.RawStdEncoding.EncodeToString(val)
}

// DecodeBinaryHeader base64-decodes binary metadata for headers ending in "-bin".
func DecodeBinaryHeader(val string) ([]byte, error) {
	val = strings.TrimSpace(val)
	if b, err := base64.RawStdEncoding.DecodeString(val); err == nil {
		return b, nil
	}

	return base64.StdEncoding.DecodeString(val)
}

// prepareGRPCStreamModifiers builds standard gRPC request headers with a streaming body reader.
func prepareGRPCStreamModifiers(
	ctx context.Context,
	bodyReader io.Reader,
	mods []core.RequestModifier,
) []core.RequestModifier {
	grpcMods := make([]core.RequestModifier, 0, len(mods)+5)
	grpcMods = append(grpcMods,
		mod.WithContentType(fheader.MIMEApplicationGRPC),
		mod.WithHeader(fheader.TE, fheader.ValueTrailers),
		mod.WithHeader(fheader.UserAgent, "grpc-aoni/1.0"),
		mod.WithHeader(fheader.GRPCAcceptEncoding, fheader.ValueGzip+", "+fheader.ValueIdentity),
		mod.WithBody(bodyReader),
	)

	if deadline, ok := ctx.Deadline(); ok {
		grpcMods = append(grpcMods, mod.WithHeader(fheader.GRPCTimeout, formatTimeout(time.Until(deadline))))
	}

	if md, ok := FromContext(ctx); ok && len(md) > 0 {
		grpcMods = append(grpcMods, WithMetadata(md))
	}

	grpcMods = append(grpcMods, mods...)

	return grpcMods
}

// StreamResponse represents an active Server-Streaming gRPC session.
type StreamResponse[Resp any] struct {
	stream io.ReadCloser
	resp   *http.Response
}

// Header returns the initial response headers received from the gRPC server.
func (s *StreamResponse[Resp]) Header() http.Header {
	if s.resp != nil {
		return s.resp.Header
	}

	return nil
}

// Trailer returns the response trailers received from the gRPC server once the stream finishes.
func (s *StreamResponse[Resp]) Trailer() http.Header {
	if s.resp != nil {
		if len(s.resp.Trailer) > 0 {
			return s.resp.Trailer
		}

		return s.resp.Header
	}

	return nil
}

// Recv reads and decodes the next Protobuf message frame from the streaming response.
// Returns io.EOF when the server finishes sending messages.
func (s *StreamResponse[Resp]) Recv() (*Resp, error) {
	result := new(Resp)

	msg, ok := any(result).(proto.Message)
	if !ok {
		return nil, fmt.Errorf("aoni/grpc: response type %T does not implement proto.Message", result)
	}

	_, err := UnmarshalFrame(s.stream, msg)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			if validateErr := validateResponseTrailers(s.resp); validateErr != nil {
				return nil, validateErr
			}

			return nil, io.EOF
		}

		return nil, err
	}

	return result, nil
}

// Close terminates the streaming RPC session and closes the underlying response body stream.
func (s *StreamResponse[Resp]) Close() error {
	if s.stream != nil {
		return s.stream.Close()
	}

	return nil
}

// ServerStream executes a Server-Streaming gRPC call (/Service/Method),
// allowing the client to receive multiple Protobuf responses over time.
func ServerStream[Resp any](
	ctx context.Context,
	doer core.HTTPRequester,
	fullMethod string,
	reqMsg proto.Message,
	mods ...core.RequestModifier,
) (*StreamResponse[Resp], error) {
	ctx = asyncctx.Wrap(ctx)

	frameBytes, err := MarshalFrame(reqMsg, false)
	if err != nil {
		return nil, err
	}

	path := normalizeMethodPath(fullMethod)
	grpcMods := prepareGRPCModifiers(ctx, frameBytes, true, mods)

	resp, err := doer.Request(ctx, http.MethodPost, path, grpcMods...)
	if err != nil {
		return nil, err
	}

	if err := validateInitialHeaders(resp); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}

	return &StreamResponse[Resp]{
		stream: resp.Body,
		resp:   resp,
	}, nil
}

type connectResult struct {
	resp *http.Response
	err  error
}

// BidiStreamClient represents an active full-duplex bidirectional gRPC streaming session.
type BidiStreamClient[Req proto.Message, Resp any] struct {
	pipeWriter *io.PipeWriter
	stream     io.ReadCloser
	resp       *http.Response

	ctx    context.Context
	cancel context.CancelFunc

	connectedOnce sync.Once
	connectResCh  chan connectResult
	connectErr    error

	sendMu sync.Mutex
	recvMu sync.Mutex

	closedSend atomic.Bool
	closed     atomic.Bool
}

func (s *BidiStreamClient[Req, Resp]) ensureConnected() error {
	s.connectedOnce.Do(func() {
		res := <-s.connectResCh
		if res.err != nil {
			s.connectErr = res.err
			return
		}

		if err := validateInitialHeaders(res.resp); err != nil {
			_ = res.resp.Body.Close()
			s.connectErr = err
			return
		}

		s.resp = res.resp
		s.stream = res.resp.Body
	})

	return s.connectErr
}

// Send marshals and transmits a Protobuf message frame to the remote gRPC server.
func (s *BidiStreamClient[Req, Resp]) Send(msg Req) error {
	if s.closed.Load() {
		return errors.New("aoni/grpc: stream is closed")
	}

	if s.closedSend.Load() {
		return errors.New("aoni/grpc: send stream is closed")
	}

	frameBytes, err := MarshalFrame(msg, false)
	if err != nil {
		return err
	}

	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	_, err = s.pipeWriter.Write(frameBytes)
	if err != nil {
		if errors.Is(err, io.ErrClosedPipe) {
			if connErr := s.ensureConnected(); connErr != nil {
				return connErr
			}
		}

		return err
	}

	return nil
}

// CloseSend signals to the server that the client has finished transmitting messages.
func (s *BidiStreamClient[Req, Resp]) CloseSend() error {
	if s.closedSend.CompareAndSwap(false, true) {
		return s.pipeWriter.Close()
	}

	return nil
}

// Recv reads and decodes the next Protobuf message frame from the gRPC response stream.
// Returns io.EOF when the server finishes transmitting messages.
func (s *BidiStreamClient[Req, Resp]) Recv() (*Resp, error) {
	if s.closed.Load() {
		return nil, errors.New("aoni/grpc: stream is closed")
	}

	if err := s.ensureConnected(); err != nil {
		return nil, err
	}

	s.recvMu.Lock()
	defer s.recvMu.Unlock()

	result := new(Resp)

	msg, ok := any(result).(proto.Message)
	if !ok {
		return nil, fmt.Errorf("aoni/grpc: response type %T does not implement proto.Message", result)
	}

	_, err := UnmarshalFrame(s.stream, msg)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			if validateErr := validateResponseTrailers(s.resp); validateErr != nil {
				return nil, validateErr
			}

			return nil, io.EOF
		}

		return nil, err
	}

	return result, nil
}

// Header returns the response headers from the gRPC server.
func (s *BidiStreamClient[Req, Resp]) Header() (http.Header, error) {
	if err := s.ensureConnected(); err != nil {
		return nil, err
	}

	return s.resp.Header, nil
}

// Trailer returns the response trailers once the stream concludes.
func (s *BidiStreamClient[Req, Resp]) Trailer() http.Header {
	if s.resp != nil {
		if len(s.resp.Trailer) > 0 {
			return s.resp.Trailer
		}

		return s.resp.Header
	}

	return nil
}

// Close terminates both send and receive directions of the gRPC stream.
func (s *BidiStreamClient[Req, Resp]) Close() error {
	if s == nil {
		return nil
	}

	if s.closed.CompareAndSwap(false, true) {
		s.closedSend.Store(true)

		if s.pipeWriter != nil {
			_ = s.pipeWriter.CloseWithError(io.EOF)
		}

		if s.cancel != nil {
			s.cancel()
		}

		if s.stream != nil {
			return s.stream.Close()
		}
	}

	return nil
}

// BidiStream initiates a full-duplex bidirectional gRPC streaming session.
func BidiStream[Req proto.Message, Resp any](
	ctx context.Context,
	doer core.HTTPRequester,
	fullMethod string,
	mods ...core.RequestModifier,
) (*BidiStreamClient[Req, Resp], error) {
	ctx = asyncctx.Wrap(ctx)
	streamCtx, cancel := context.WithCancel(ctx) //nolint:gosec
	pipeReader, pipeWriter := io.Pipe()

	path := normalizeMethodPath(fullMethod)
	grpcMods := prepareGRPCStreamModifiers(streamCtx, pipeReader, mods)

	connectResCh := make(chan connectResult, 1)
	go func() {
		resp, err := doer.Request(streamCtx, http.MethodPost, path, grpcMods...) //nolint:bodyclose
		connectResCh <- connectResult{resp: resp, err: err}
	}()

	client := &BidiStreamClient[Req, Resp]{
		pipeWriter:   pipeWriter,
		ctx:          streamCtx,
		cancel:       cancel,
		connectResCh: connectResCh,
	}

	return client, nil
}

// ClientStreamClient represents a client-streaming gRPC session where the client transmits multiple messages
// and receives a single final response.
type ClientStreamClient[Req proto.Message, Resp any] struct {
	bidi *BidiStreamClient[Req, Resp]
}

// Send transmits a Protobuf message frame to the remote gRPC server.
func (s *ClientStreamClient[Req, Resp]) Send(msg Req) error {
	return s.bidi.Send(msg)
}

// CloseAndRecv concludes client transmission and reads the server's single response message.
func (s *ClientStreamClient[Req, Resp]) CloseAndRecv() (*Resp, error) {
	if err := s.bidi.CloseSend(); err != nil {
		return nil, err
	}

	defer s.bidi.Close()

	return s.bidi.Recv()
}

// Close terminates the client-streaming session.
func (s *ClientStreamClient[Req, Resp]) Close() error {
	if s == nil || s.bidi == nil {
		return nil
	}

	return s.bidi.Close()
}

// ClientStream initiates a client-streaming gRPC call.
func ClientStream[Req proto.Message, Resp any](
	ctx context.Context,
	doer core.HTTPRequester,
	fullMethod string,
	mods ...core.RequestModifier,
) (*ClientStreamClient[Req, Resp], error) {
	bidi, err := BidiStream[Req, Resp](ctx, doer, fullMethod, mods...)
	if err != nil {
		return nil, err
	}

	return &ClientStreamClient[Req, Resp]{bidi: bidi}, nil
}
