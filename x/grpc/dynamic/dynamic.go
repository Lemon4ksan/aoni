// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package dynamic provides dynamic gRPC invocation using Protobuf reflection descriptors and JSON messages.
package dynamic

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
	"github.com/lemon4ksan/aoni/grpc"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/request"
)

// DynamicInvoker provides dynamic gRPC invocation using Protobuf descriptors
// without requiring pre-compiled Go struct types.
type DynamicInvoker struct {
	jsonMarshal   protojson.MarshalOptions
	jsonUnmarshal protojson.UnmarshalOptions
}

// New creates a new [DynamicInvoker] with default JSON options.
func New() *DynamicInvoker {
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
		return "", errors.New("aoni/grpc/dynamic: input and output MessageDescriptors must not be nil")
	}

	reqMsg := dynamicpb.NewMessage(inputDesc)
	if strings.TrimSpace(reqJSON) != "" {
		if err := d.jsonUnmarshal.Unmarshal([]byte(reqJSON), reqMsg); err != nil {
			return "", fmt.Errorf("aoni/grpc/dynamic: unmarshal JSON request failed: %w", err)
		}
	}

	frameBytes, err := grpc.MarshalFrame(reqMsg, false)
	if err != nil {
		return "", err
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
		grpcMods = append(grpcMods, mod.WithGRPCWebTimeout(time.Until(deadline)))
	}

	grpcMods = append(grpcMods, mods...)
	requester := request.AsRequester(doer)

	resp, err := requester.Request(ctx, http.MethodPost, path, grpcMods...)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respMsg := dynamicpb.NewMessage(outputDesc)
	if _, err := grpc.UnmarshalFrame(resp.Body, respMsg); err != nil {
		return "", err
	}

	jsonBytes, err := d.jsonMarshal.Marshal(respMsg)
	if err != nil {
		return "", fmt.Errorf("aoni/grpc/dynamic: marshal response to JSON failed: %w", err)
	}

	return string(jsonBytes), nil
}
