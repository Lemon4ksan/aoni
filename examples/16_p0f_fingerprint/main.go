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

	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/request"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/p0f"
)

type IPResponse struct {
	Origin string `json:"origin"`
}

func main() {
	ctx := context.Background()

	client := aoni.NewClient(nil,
		option.WithBaseURL("https://httpbin.org"),
		option.WithTimeout(10*time.Second),
		option.WithP0fSignature(p0f.Linux311),
	)

	resp, err := request.GetTo[IPResponse](ctx, client, "/ip")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Your IP: %s\n", resp.Origin)
	fmt.Println("Connection appears as Linux 3.11+ to passive fingerprinters")
	fmt.Printf("TTL=%d, Options=%v, Window=mss*%d\n",
		p0f.Linux311.TTL, p0f.Linux311.Options, p0f.Linux311.WindowSize)
}
