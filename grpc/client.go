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
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/request"
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
	doer aoni.RequestDoer,
	fullMethod string,
	reqMsg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	frameBytes, err := marshalFrame(reqMsg, false)
	if err != nil {
		return nil, err
	}

	path := normalizeMethodPath(fullMethod)
	grpcMods := prepareGRPCModifiers(ctx, frameBytes, false, mods)

	requester := request.AsRequester(doer)

	resp, err := requester.Request(ctx, http.MethodPost, path, grpcMods...)
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

	if _, err := unmarshalFrame(resp.Body, msg); err != nil {
		return nil, err
	}

	if err := validateResponseTrailers(resp); err != nil {
		return nil, err
	}

	return result, nil
}

// InvokeFast executes a high-performance gRPC unary call using the fast client engine without allocating *http.Response.
//
// Preconditions:
//   - ctx, fastClient, and reqMsg must be non-nil.
func InvokeFast[Resp any](
	ctx context.Context,
	fastClient *fast.Client,
	fullMethod string,
	reqMsg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	frameBytes, err := marshalFrame(reqMsg, false)
	if err != nil {
		return nil, err
	}

	path := normalizeMethodPath(fullMethod)

	req := fast.NewRequest(nil)
	defer req.Release()

	req.SetContext(ctx)
	req.SetMethod(http.MethodPost)
	req.SetURL(path)
	req.SetHeader("Content-Type", "application/grpc")
	req.SetHeader("TE", "trailers")
	req.SetBodyBytes(frameBytes)

	for _, m := range mods {
		m.Apply(req)
	}

	resp, err := fastClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Close()

	result := new(Resp)

	msg, ok := any(result).(proto.Message)
	if !ok {
		return nil, fmt.Errorf("aoni/grpc: response type %T does not implement proto.Message", result)
	}

	if err := unmarshalFastGRPCFrame(resp, msg); err != nil {
		return nil, err
	}

	return result, nil
}

// normalizeMethodPath guarantees that the gRPC method path starts with a leading slash.
func normalizeMethodPath(fullMethod string) string {
	if !strings.HasPrefix(fullMethod, "/") && !strings.HasPrefix(fullMethod, "http://") &&
		!strings.HasPrefix(fullMethod, "https://") {
		return "/" + fullMethod
	}

	return fullMethod
}

// prepareGRPCModifiers builds standard gRPC request headers (Content-Type, TE, User-Agent, Timeout, and Metadata).
func prepareGRPCModifiers(
	ctx context.Context,
	frameBytes []byte,
	isStreaming bool,
	mods []aoni.RequestModifier,
) []aoni.RequestModifier {
	grpcMods := make([]aoni.RequestModifier, 0, len(mods)+5)
	grpcMods = append(grpcMods,
		mod.WithContentType("application/grpc"),
		mod.WithHeader("te", "trailers"),
		mod.WithHeader("user-agent", "grpc-aoni/1.0"),
		mod.WithBody(bytes.NewReader(frameBytes)),
	)

	if isStreaming {
		grpcMods = append(grpcMods, mod.WithHeader("grpc-accept-encoding", "gzip, identity"))
	}

	if deadline, ok := ctx.Deadline(); ok {
		grpcMods = append(grpcMods, mod.WithHeader("grpc-timeout", formatTimeout(time.Until(deadline))))
	}

	if md, ok := FromContext(ctx); ok && len(md) > 0 {
		grpcMods = append(grpcMods, WithMetadata(md))
	}

	grpcMods = append(grpcMods, mods...)

	return grpcMods
}

// unmarshalFastGRPCFrame decodes Protobuf payload bytes from a fast client response.
func unmarshalFastGRPCFrame(resp aoni.Response, msg proto.Message) error {
	if stream := resp.BodyStream(); stream != nil && resp.HTTPResponse() == nil { //nolint:bodyclose
		_, err := unmarshalFrame(stream, msg)
		return err
	}

	_, err := unmarshalFrame(bytes.NewReader(resp.UnsafeBodyBytes()), msg)

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

	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "application/grpc") {
		return fmt.Errorf("%w: %s", ErrInvalidContentType, ct)
	}

	// Check "Trailers-Only" response case (when error status is delivered directly in initial response headers)
	if statusCode := resp.Header.Get("grpc-status"); statusCode != "" && statusCode != "0" {
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

	statusCode := trailers.Get("grpc-status")
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

// StreamResponse represents an active Server-Streaming gRPC session.
type StreamResponse[Resp any] struct {
	stream io.ReadCloser
	resp   *http.Response
}

// Recv reads and decodes the next Protobuf message frame from the streaming response.
// Returns io.EOF when the server finishes sending messages.
func (s *StreamResponse[Resp]) Recv() (*Resp, error) {
	result := new(Resp)

	msg, ok := any(result).(proto.Message)
	if !ok {
		return nil, fmt.Errorf("aoni/grpc: response type %T does not implement proto.Message", result)
	}

	_, err := unmarshalFrame(s.stream, msg)
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
	doer aoni.RequestDoer,
	fullMethod string,
	reqMsg proto.Message,
	mods ...aoni.RequestModifier,
) (*StreamResponse[Resp], error) {
	frameBytes, err := marshalFrame(reqMsg, false)
	if err != nil {
		return nil, err
	}

	path := normalizeMethodPath(fullMethod)
	grpcMods := prepareGRPCModifiers(ctx, frameBytes, true, mods)

	requester := request.AsRequester(doer)

	resp, err := requester.Request(ctx, http.MethodPost, path, grpcMods...)
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
