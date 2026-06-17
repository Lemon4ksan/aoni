// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: p0f fingerprinting
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/p0f"
)

type IPResponse struct {
	Origin string `json:"origin"`
}

func main() {
	ctx := context.Background()

	client := aoni.NewClient(nil).
		WithBaseURL("https://httpbin.org").
		WithTimeout(10 * time.Second).
		WithP0fSignature(p0f.Linux311)

	resp, err := aoni.GetJSON[IPResponse](ctx, client, "/ip")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Your IP: %s\n", resp.Origin)
	fmt.Println("Connection appears as Linux 3.11+ to passive fingerprinters")
	fmt.Printf("TTL=%d, Options=%v, Window=mss*%d\n",
		p0f.Linux311.TTL, p0f.Linux311.Options, p0f.Linux311.WindowSize)
}
