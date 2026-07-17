// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Fingerprint Integrity using the Persona module.
//
// Demonstrates using pre-configured, immutable Personas (like PersonaChrome120Windows)
// to set TLS, HTTP/2 settings, User-Agent, header ordering, and p0f TCP signatures
// consistently in a single call.
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/lemon4ksan/aoni"
)

type Response struct {
	Headers map[string]string `json:"headers"`
	Origin  string            `json:"origin"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start a local test server to demonstrate without external network dependency
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"headers": {"User-Agent": %q}}`, r.UserAgent())
	}))
	defer server.Close()

	fmt.Println("=== Impersonating Chrome 120 on Windows ===")
	
	// Create client with Chrome 120 Windows persona
	chromeClient := aoni.NewClient(nil,
		aoni.WithClientBaseURL(server.URL),
	).WithPersona(aoni.PersonaChrome120Windows)

	res, err := aoni.GetTo[Response](ctx, chromeClient, "/headers")
	if err != nil {
		fmt.Printf("Request failed: %v\n", err)
	} else {
		fmt.Printf("User-Agent sent: %s\n", res.Headers["User-Agent"])
		fmt.Printf("Connection established successfully!\n")
	}

	fmt.Println("\n=== Impersonating Firefox 120 on Windows ===")

	// Create client with Firefox 120 Windows persona
	firefoxClient := aoni.NewClient(nil,
		aoni.WithClientBaseURL(server.URL),
	).WithPersona(aoni.PersonaFirefox120Windows)

	res, err = aoni.GetTo[Response](ctx, firefoxClient, "/headers")
	if err != nil {
		fmt.Printf("Request failed: %v\n", err)
	} else {
		fmt.Printf("User-Agent sent: %s\n", res.Headers["User-Agent"])
		fmt.Printf("Connection established successfully!\n")
	}

	fmt.Println("\nPersona example completed successfully")
}
