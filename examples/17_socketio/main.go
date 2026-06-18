// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Socket.IO client
// 
// Demonstrates a simple Socket.IO client that connects to a server and listens for price updates.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/lemon4ksan/aoni"
)

type PriceUpdate struct {
	SKU  string `json:"sku"`
	Name string `json:"name"`
	Buy  struct {
		Metal float64 `json:"metal"`
	} `json:"buy"`
}

func main() {
	ctx := context.Background()

	// Create a secure client with TLS fingerprint and user agent
	client := aoni.NewClient(nil).WithTLSFingerprint(aoni.BrowserChrome)

	// Specify the Socket.IO URL with Engine.IO v4 parameters
	socketURL := "ws://localhost:3000/socket.io/?EIO=4&transport=websocket"

	// Connect to Socket.IO
	sio, err := aoni.DialSocketIO(ctx, client, socketURL)
	if err != nil {
		log.Fatalf("Failed to connect to Socket.IO: %v", err)
	}
	defer sio.Close()

	// Register event handler
	sio.On("price", func(args []json.RawMessage) {
		if len(args) == 0 {
			return
		}
		var update PriceUpdate
		if err := json.Unmarshal(args[0], &update); err == nil {
			fmt.Printf("Received: %s - %f metal\n", update.Name, update.Buy.Metal)
		}
	})

	// Send our event
	err = sio.Emit("join_room", "pricedb")
	if err != nil {
		log.Printf("Failed to send event: %v", err)
	}

	time.Sleep(10 * time.Second)
}
