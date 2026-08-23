// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/request"
)

// DynamicInvoker provides dynamic gRPC invocation using Protobuf descriptors
// without requiring pre-compiled Go struct types.
type DynamicInvoker struct {
	jsonMarshal   protojson.MarshalOptions
	jsonUnmarshal protojson.UnmarshalOptions
}

// NewDynamicInvoker creates a new [DynamicInvoker] with default JSON options.
func NewDynamicInvoker() *DynamicInvoker {
	return &DynamicInvoker{
		jsonMarshal: protojson.MarshalOptions{
			EmitUnpopulated: false,
			UseProtoNames:   true,
		},
		jsonUnmarshal: protojson.UnmarshalOptions{
			DiscardUnknown: true,
		},
	}
}

// InvokeJSON executes a dynamic gRPC call using raw JSON input and output strings,
// guided by input and output Protobuf MessageDescriptors.
func (d *DynamicInvoker) InvokeJSON(
	ctx context.Context,
	doer aoni.RequestDoer,
	fullMethod string,
	reqJSON string,
	inputDesc protoreflect.MessageDescriptor,
	outputDesc protoreflect.MessageDescriptor,
	mods ...aoni.RequestModifier,
) (string, error) {
	if inputDesc == nil || outputDesc == nil {
		return "", errors.New("aoni/grpc: input and output MessageDescriptors must not be nil")
	}

	reqMsg := dynamicpb.NewMessage(inputDesc)
	if strings.TrimSpace(reqJSON) != "" {
		if err := d.jsonUnmarshal.Unmarshal([]byte(reqJSON), reqMsg); err != nil {
			return "", fmt.Errorf("aoni/grpc: unmarshal JSON request failed: %w", err)
		}
	}

	frameBytes, err := MarshalFrame(reqMsg, false)
	if err != nil {
		return "", err
	}

	path := normalizeMethodPath(fullMethod)

	grpcMods := make([]aoni.RequestModifier, 0, len(mods)+4)
	grpcMods = append(grpcMods,
		mod.WithContentType("application/grpc"),
		mod.WithHeader("te", "trailers"),
		mod.WithHeader("user-agent", "grpc-aoni/1.0"),
		mod.WithBody(bytes.NewReader(frameBytes)),
	)

	if deadline, ok := ctx.Deadline(); ok {
		grpcMods = append(grpcMods, mod.WithHeader("grpc-timeout", formatTimeout(time.Until(deadline))))
	}

	grpcMods = append(grpcMods, mods...)
	requester := request.AsRequester(doer)

	resp, err := requester.Request(ctx, http.MethodPost, path, grpcMods...)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if err := validateInitialHeaders(resp); err != nil {
		return "", err
	}

	respMsg := dynamicpb.NewMessage(outputDesc)
	if _, err := UnmarshalFrame(resp.Body, respMsg); err != nil {
		return "", err
	}

	if err := validateResponseTrailers(resp); err != nil {
		return "", err
	}

	jsonBytes, err := d.jsonMarshal.Marshal(respMsg)
	if err != nil {
		return "", fmt.Errorf("aoni grpc: marshal response to JSON failed: %w", err)
	}

	return string(jsonBytes), nil
}
