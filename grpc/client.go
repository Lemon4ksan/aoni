// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package grpc provides a native lightweight gRPC client implementation over aoni's stealth HTTP/2 transport.
package grpc

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
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
func Invoke[Resp any](
	ctx context.Context,
	doer aoni.RequestDoer,
	fullMethod string,
	reqMsg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	frameBytes, err := MarshalFrame(reqMsg, false)
	if err != nil {
		return nil, err
	}

	path := fullMethod
	if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		path = "/" + path
	}

	grpcMods := make([]aoni.RequestModifier, 0, len(mods)+4)
	grpcMods = append(grpcMods,
		mod.WithContentType("application/grpc"),
		mod.WithHeader("te", "trailers"),
		mod.WithHeader("user-agent", "grpc-aoni/1.0"),
		mod.WithBody(bytes.NewReader(frameBytes)),
	)

	if deadline, ok := ctx.Deadline(); ok {
		grpcMods = append(grpcMods, mod.WithHeader("grpc-timeout", FormatTimeout(time.Until(deadline))))
	}

	grpcMods = append(grpcMods, mods...)

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
		return nil, fmt.Errorf("aoni grpc: response type %T does not implement proto.Message", result)
	}

	if _, err := UnmarshalFrame(resp.Body, msg); err != nil {
		return nil, err
	}

	if err := validateResponseTrailers(resp); err != nil {
		return nil, err
	}

	return result, nil
}

// InvokeFast is a fast gRPC client that does not create a *http.Response.
func InvokeFast[Resp any](
	ctx context.Context,
	fastClient *fast.Client,
	fullMethod string,
	reqMsg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	frameBytes, err := MarshalFrame(reqMsg, false)
	if err != nil {
		return nil, err
	}

	path := fullMethod
	if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		path = "/" + path
	}

	req := fast.NewRequest(nil)
	defer req.Release()

	req.SetContext(ctx)
	req.SetMethod(http.MethodPost)
	req.SetURL(path)
	req.SetHeader("Content-Type", "application/grpc")
	req.SetHeader("TE", "trailers")
	req.SetBodyBytes(frameBytes)

	for _, m := range mods {
		if m != nil {
			m(req)
		}
	}

	resp, err := fastClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Close()

	result := new(Resp)

	msg, ok := any(result).(proto.Message)
	if !ok {
		return nil, fmt.Errorf("aoni grpc: response type %T does not implement proto.Message", result)
	}

	if err := unmarshalFastGRPCFrame(resp, msg); err != nil {
		return nil, err
	}

	return result, nil
}

func unmarshalFastGRPCFrame(resp aoni.Response, msg proto.Message) error {
	if stream := resp.BodyStream(); stream != nil && resp.HTTPResponse() == nil { //nolint:bodyclose
		_, err := UnmarshalFrame(stream, msg)
		return err
	}

	_, err := UnmarshalFrame(bytes.NewReader(resp.UnsafeBodyBytes()), msg)

	return err
}

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
		return nil, fmt.Errorf("aoni grpc: response type %T does not implement proto.Message", result)
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

// Close terminates the streaming RPC session.
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
	frameBytes, err := MarshalFrame(reqMsg, false)
	if err != nil {
		return nil, err
	}

	path := fullMethod
	if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		path = "/" + path
	}

	grpcMods := make([]aoni.RequestModifier, 0, len(mods)+5)
	grpcMods = append(grpcMods,
		mod.WithContentType("application/grpc"),
		mod.WithHeader("te", "trailers"),
		mod.WithHeader("grpc-accept-encoding", "gzip, identity"),
		mod.WithHeader("user-agent", "grpc-aoni/1.0"),
		mod.WithBody(bytes.NewReader(frameBytes)),
	)

	if deadline, ok := ctx.Deadline(); ok {
		grpcMods = append(grpcMods, mod.WithHeader("grpc-timeout", FormatTimeout(time.Until(deadline))))
	}

	grpcMods = append(grpcMods, mods...)
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

// MarshalFrameCompressed encodes a Protobuf message with optional Gzip compression.
func MarshalFrameCompressed(msg proto.Message, compress bool) ([]byte, error) {
	if msg == nil {
		return []byte{0, 0, 0, 0, 0}, nil
	}

	payload, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("aoni grpc: marshal proto failed: %w", err)
	}

	var compressedFlag byte = 0x00
	if compress {
		var buf bytes.Buffer

		gz := gzip.NewWriter(&buf)
		if _, err := gz.Write(payload); err != nil {
			return nil, fmt.Errorf("aoni grpc: compress payload failed: %w", err)
		}

		_ = gz.Close()

		payload = buf.Bytes()
		compressedFlag = 0x01
	}

	payloadLen := len(payload)
	frame := make([]byte, 5+payloadLen)
	frame[0] = compressedFlag

	binary.BigEndian.PutUint32(frame[1:5], uint32(payloadLen)) //nolint:gosec
	copy(frame[5:], payload)

	return frame, nil
}
