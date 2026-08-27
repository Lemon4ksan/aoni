// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Full-duplex gRPC streaming over aoni's HTTP/2 stealth transport.
//
// Demonstrates:
// 1. Server-Streaming (1 request -> stream of responses)
// 2. Client-Streaming (stream of requests -> 1 response)
// 3. Bidirectional-Streaming (stream of requests <-> stream of responses)
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/lemon4ksan/foundation/net/http/header"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/grpc"
)

func main() {
	// 1. Start an in-process gRPC test server with full duplex support
	server := startGRPCTestServer()
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := aoni.NewClient(nil)

	fmt.Println("=== 1. Server Streaming Demo ===")
	demoServerStreaming(ctx, client, server.URL)

	fmt.Println("\n=== 2. Client Streaming Demo ===")
	demoClientStreaming(ctx, client, server.URL)

	fmt.Println("\n=== 3. Bidirectional Streaming Demo ===")
	demoBidiStreaming(ctx, client, server.URL)
}

// demoServerStreaming requests a stream of numbers and prints them as they arrive.
func demoServerStreaming(ctx context.Context, client *aoni.Client, serverURL string) {
	stream, err := grpc.ServerStream[wrapperspb.Int32Value](
		ctx,
		client,
		serverURL+"/MathService/CountDown",
		wrapperspb.Int32(5),
	)
	if err != nil {
		log.Fatalf("ServerStream failed: %v", err)
	}
	defer stream.Close()

	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			fmt.Println("[Client] Server stream finished (EOF).")
			break
		}
		if err != nil {
			log.Fatalf("Recv failed: %v", err)
		}
		fmt.Printf("[Client] Received number: %d\n", msg.GetValue())
	}
}

// demoClientStreaming sends multiple numbers and receives the aggregated sum.
func demoClientStreaming(ctx context.Context, client *aoni.Client, serverURL string) {
	stream, err := grpc.ClientStream[*wrapperspb.Int32Value, wrapperspb.Int32Value](
		ctx,
		client,
		serverURL+"/MathService/SumAll",
	)
	if err != nil {
		log.Fatalf("ClientStream failed: %v", err)
	}
	defer stream.Close()

	numbers := []int32{10, 20, 30, 40}
	for _, n := range numbers {
		fmt.Printf("[Client] Sending: %d\n", n)
		if err := stream.Send(wrapperspb.Int32(n)); err != nil {
			log.Fatalf("Send failed: %v", err)
		}
	}

	total, err := stream.CloseAndRecv()
	if err != nil {
		log.Fatalf("CloseAndRecv failed: %v", err)
	}
	fmt.Printf("[Client] Final Sum from Server: %d\n", total.GetValue())
}

// demoBidiStreaming streams messages and concurrently receives processed results.
func demoBidiStreaming(ctx context.Context, client *aoni.Client, serverURL string) {
	stream, err := grpc.BidiStream[*wrapperspb.StringValue, wrapperspb.StringValue](
		ctx,
		client,
		serverURL+"/ChatService/EchoUpper",
	)
	if err != nil {
		log.Fatalf("BidiStream failed: %v", err)
	}
	defer stream.Close()

	messages := []string{"hello", "aoni", "grpc", "bidirectional"}

	go func() {
		for _, text := range messages {
			fmt.Printf("[Client -> Server] %s\n", text)
			_ = stream.Send(wrapperspb.String(text))
			time.Sleep(20 * time.Millisecond)
		}
		_ = stream.CloseSend()
	}()

	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			fmt.Println("[Client] Bidi stream concluded.")
			break
		}
		if err != nil {
			log.Fatalf("Recv failed: %v", err)
		}
		fmt.Printf("[Server -> Client] %s\n", resp.GetValue())
	}
}

// startGRPCTestServer builds a mock gRPC HTTP/2 handler.
func startGRPCTestServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		_ = rc.EnableFullDuplex()

		w.Header().Set(header.ContentType, header.MIMEApplicationGRPC)
		w.Header().Set(header.Trailer, header.GRPCStatus+", "+header.GRPCMessage)
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)

		switch r.URL.Path {
		case "/MathService/CountDown":
			var req wrapperspb.Int32Value
			_, _ = grpc.UnmarshalFrame(r.Body, &req)

			for i := req.GetValue(); i >= 1; i-- {
				frame, _ := grpc.MarshalFrame(wrapperspb.Int32(i), false)
				_, _ = w.Write(frame)
				if flusher != nil {
					flusher.Flush()
				}
			}

		case "/MathService/SumAll":
			var sum int32
			for {
				var item wrapperspb.Int32Value
				_, err := grpc.UnmarshalFrame(r.Body, &item)
				if err != nil {
					break
				}
				sum += item.GetValue()
			}
			frame, _ := grpc.MarshalFrame(wrapperspb.Int32(sum), false)
			_, _ = w.Write(frame)

		case "/ChatService/EchoUpper":
			for {
				var item wrapperspb.StringValue
				_, err := grpc.UnmarshalFrame(r.Body, &item)
				if err != nil {
					break
				}
				upper := wrapperspb.String(fmt.Sprintf("ECHO: %s", item.GetValue()))
				frame, _ := grpc.MarshalFrame(upper, false)
				_, _ = w.Write(frame)
				if flusher != nil {
					flusher.Flush()
				}
			}
		}

		w.Header().Set(header.GRPCStatus, "0")
		w.Header().Set(header.GRPCMessage, "OK")
	}))
}
