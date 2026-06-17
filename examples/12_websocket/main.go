// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: WebSocket connection and binary message round-trip.
//
// Demonstrates DialWebSocket for establishing WebSocket connections
// with the aoni client, including reading and writing binary messages.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/lemon4ksan/aoni"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := aoni.NewClient(nil).
		WithBaseURL("https://echo.websocket.org")

	// Establish a WebSocket connection
	conn, resp, err := aoni.DialWebSocket(ctx, client, "wss://echo.websocket.org",
		aoni.WithHeader("Origin", "https://example.com"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	fmt.Printf("WebSocket connected: status=%d\n", resp.StatusCode)

	// Send a binary message
	msg := []byte("Hello from aoni WebSocket!")

	_, err = conn.Write(msg)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Sent: %s\n", msg)

	// Read the echo response
	buf := make([]byte, 4096)

	n, err := conn.Read(buf)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Received: %s\n", string(buf[:n]))

	// Send and receive multiple messages
	for i := 0; i < 3; i++ {
		text := fmt.Sprintf("Message %d", i)

		_, err = conn.Write([]byte(text))
		if err != nil {
			log.Fatal(err)
		}

		n, err = conn.Read(buf)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("Round-trip %d: sent=%q received=%q\n", i, text, string(buf[:n]))
	}

	fmt.Println("WebSocket example completed")
}
