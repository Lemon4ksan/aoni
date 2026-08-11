// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package grpc implements a lightweight, native gRPC client over HTTP/2 without heavy SDK dependencies.
//
// It provides full support for unary RPCs, server streaming, dynamic Protobuf invokers,
// metadata propagation, status code mapping, and 5-byte length-prefixed framing.
//
// # Core API
//
//   - [Invoke]: Perform type-safe unary RPC calls returning typed Protobuf responses.
//   - [ServerStream]: Perform server-streaming RPCs yielding a stream reader ([StreamResponse]).
//   - [DynamicInvoker]: Execute dynamic JSON-to-gRPC calls using Protobuf descriptors ([InvokeJSON]).
//   - [Metadata]: Key-value context metadata map supporting binary headers (`-bin`).
//
// # Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"log"
//
//		"github.com/lemon4ksan/aoni"
//		"github.com/lemon4ksan/aoni/grpc"
//		"google.golang.org/protobuf/types/known/wrapperspb"
//	)
//
//	func main() {
//		client := aoni.NewClient(nil)
//
//		resp, err := grpc.Invoke[wrapperspb.StringValue](
//			context.Background(),
//			client,
//			"https://grpc.example.com/Greeter/SayHello",
//			wrapperspb.String("World"),
//		)
//		if err != nil {
//			log.Fatal(err)
//		}
//
//		fmt.Println("gRPC Response:", resp.GetValue())
//	}
package grpc
