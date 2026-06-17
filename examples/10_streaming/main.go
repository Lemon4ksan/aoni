// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Streaming responses with Stream, StreamSSE, and StreamNDJSON.
//
// Demonstrates Stream for raw response reading, StreamSSE for server-sent events,
// and StreamNDJSON for newline-delimited JSON streams.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/lemon4ksan/aoni"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := aoni.NewClient(nil).
		WithBaseURL("https://httpbin.org")

	demoStream(ctx, client)
	demoSSE(ctx, client)
	demoNDJSON()
}

// demoStream reads a raw streaming response in chunks.
func demoStream(ctx context.Context, client *aoni.Client) {
	stream, err := aoni.Stream(ctx, client, "/stream/3")
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	fmt.Printf("Streaming response: status=%d content-type=%s length=%d\n",
		stream.StatusCode(), stream.ContentType(), stream.ContentLength())

	buf := make([]byte, 1024)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			fmt.Printf("Received %d bytes\n", n)
		}

		if err != nil {
			break
		}
	}
}

// demoSSE processes server-sent events from a streaming endpoint.
func demoSSE(ctx context.Context, client *aoni.Client) {
	sseStream, err := aoni.Stream(ctx, client, "/events/2")
	if err != nil {
		log.Fatal(err)
	}
	defer sseStream.Close()

	events, errs := aoni.StreamSSE(ctx, sseStream)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}

			fmt.Printf("SSE event: id=%s type=%s data=%s\n", event.ID, event.Event, event.Data)
		case err, ok := <-errs:
			if !ok || err != nil {
				if err != nil {
					log.Printf("SSE error: %v", err)
				}

				return
			}

		case <-ctx.Done():
			return
		}
	}
}

// demoNDJSON shows StreamNDJSON usage (requires a JSON streaming endpoint).
func demoNDJSON() {
	fmt.Println("\nStreamNDJSON usage:")
	fmt.Println("  ndjsonStream, _ := aoni.Stream(ctx, client, \"/chat/stream\")")
	fmt.Println("  ch, errs := aoni.StreamNDJSON[ChatMessage](ctx, ndjsonStream)")
	fmt.Println("  for msg := range ch { fmt.Println(msg.Content) }")
}
