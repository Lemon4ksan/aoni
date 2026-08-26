---
name: aoni
description: >-
  Authoritative architectural guide, code patterns, and operational rules for building
  high-performance, resilient HTTP/WebSocket clients and services using the Aoni networking engine.
  Use when writing network requests, service constructors, API wrappers, or TLS evasion code.
# Aoni High-Performance Networking Engine Guide

**`aoni`** is a unified, ultra-high-performance Internet Protocol engine for Go. It consolidates modern IETF RFC standards, Chromium-grade resilience, JA4/TLS stealth fingerprinting, and zero-allocation data paths into a single profile-driven architecture.

## 1. Core Engineering Manifesto & Dual Engines

Aoni provides two execution engines under a single unified interface (`aoni.RequestDoer`):

Engine | Package | Characteristics | When to Use
:--- | :--- | :--- | :---
**Fast Engine** | `github.com/lemon4ksan/aoni/fast` | Built on `fasthttp` + H2/H3. **1.87M+ RPS**, 0 heap allocations on hot paths. | High-throughput microservices, scraping, bot engines, high RPS pipelines.
**Standard Engine** | `github.com/lemon4ksan/aoni` (`aoni.Client`) | 100% `net/http` compatible, middleware chains, standard ecosystem drop-in. | Complex middleware pipelines, compatibility with third-party libraries.

### Unified Interface: `aoni.RequestDoer`
All services, helpers, and codecs accept `aoni.RequestDoer`:
```go
type RequestDoer interface {
    Do(req *http.Request) (*http.Response, error)
}
```

## 2. Canonical Service Constructor Pattern (Golden Standard)

When implementing an API client or service wrapper in Go:

```go
package myservice

import (
	"context"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/option"
)

// Service encapsulates API operations.
type Service struct {
	client *aoni.Client
}

// NewService constructs a new API client with canonical fallback and default options.
func NewService(doer any, opts ...option.Option) *Service {
	if doer == nil {
		// Default to zero-alloc high-performance engine
		doer = fast.NewClient()
	}

	defaultOpts := []option.Option{
		option.WithBaseURL("https://api.example.com/v1"),
		option.WithTimeout(15 * time.Second),
		option.WithChrome(), // Optional: browser persona stealth evasion
	}

	return &Service{
		client: aoni.NewClient(doer, append(defaultOpts, opts...)...),
	}
}
```

## 3. Strict Rules for AI Assistants

1. **NO DIRECT `net/http` CLIENTS**:
   - ❌ **NEVER** write `&http.Client{}` or `http.DefaultClient`.
   - ✅ **ALWAYS** use `aoni.NewClient()`, `fast.NewClient()`, or accept `any` (`aoni.RequestDoer` / `aoni.HTTPRequester`).
2. **USE TYPE-SAFE GENERICS**:
   - ❌ **NEVER** unmarshal JSON responses manually with `json.Unmarshal(body, &v)`.
   - ✅ **ALWAYS** use `client.Get[T]()`, `client.Post[T]()`, or `client.R().FetchTo[T]()`.
3. **USE IDIOMATIC ERROR CHECKING**:
   - ❌ **NEVER** inspect status codes manually with `if resp.StatusCode == 404`.
   - ✅ **ALWAYS** use `aoni.IsNotFound(err)`, `aoni.IsUnauthorized(err)`, `aoni.IsRateLimited(err)`.
4. **ALWAYS CLOSE RESPONSE BODIES**:
   - When handling raw `*http.Response`, always write `defer aoni.CloseResponse(resp)`.

## 4. Request Modifiers & Payloads Cheatsheet

Import: `"github.com/lemon4ksan/aoni/mod"`

Operation | Idiomatic Code
:--- | :---
**GET JSON into DTO** | `user, err := s.client.Get[UserDTO](ctx, "users/"+id, mods...)`
**POST JSON payload** | `created, err := s.client.Post[UserDTO](ctx, "users", payload, mods...)`
**POST Form URL-Encoded** | `login, err := s.client.Post[LoginDTO](ctx, "auth/login", nil, mod.WithFormValues(url.Values{"user": {"alice"}, "pass": {"secret"}}))`
**Query Parameters** | `items, err := s.client.Get[ListDTO](ctx, "items", mod.WithQuery("limit", "50"), mod.WithQuery("page", "2"))`
**Headers & Bearer Token** | `profile, err := s.client.Get[ProfileDTO](ctx, "profile", mod.WithHeader("X-Tenant", id), mod.WithBearer(token))`
**Per-Request Timeout** | `health, err := s.client.Get[HealthDTO](ctx, "health", mod.WithTimeout(2*time.Second))`
**Multipart File Upload** | `upload, err := s.client.Post[UploadDTO](ctx, "upload", nil, mod.WithMultipartFile("avatar", "pic.png", reader))`

## 5. Idiomatic Error Classification

Import: `"github.com/lemon4ksan/aoni"`

Function | Target Status Codes / Conditions | Typical Usage
:--- | :--- | :---
`aoni.IsNotFound(err)` | HTTP 404 (Not Found) | Cache miss or entity does not exist.
`aoni.IsUnauthorized(err)` | HTTP 401 (Unauthorized) | Trigger token refresh or prompt re-login.
`aoni.IsForbidden(err)` | HTTP 403 (Forbidden) | Insufficient permissions / WAF block.
`aoni.IsRateLimited(err)` | HTTP 429 (Too Many Requests) | Apply exponential backoff & retry.
`aoni.IsTimeout(err)` | HTTP 408 / Context Deadline | Circuit breaker trip / fallback.
`aoni.IsServerUnavailable(err)` | HTTP 500, 502, 503, 504 | Upstream degradation / retry on replica.

### Example Error Handling:
```go
user, err := s.GetUser(ctx, userID)
if err != nil {
	switch {
	case aoni.IsNotFound(err):
		return nil, ErrUserNotFound
	case aoni.IsRateLimited(err):
		time.Sleep(1 * time.Second)
		return s.GetUser(ctx, userID)
	case aoni.IsUnauthorized(err):
		if rErr := s.refreshToken(ctx); rErr == nil {
			return s.GetUser(ctx, userID)
		}
	}
	return nil, fmt.Errorf("fetching user: %w", err)
}
```

## 6. Real-Time Protocols (WebSockets & SSE)

Import: `"github.com/lemon4ksan/aoni/realtime"`

### WebSockets:
```go
conn, resp, err := realtime.DialWebSocket(ctx, "wss://api.example.com/ws",
	realtime.WithSubprotocols("graphql-ws"),
	realtime.WithHeader("Authorization", "Bearer "+token),
)
if err != nil {
	return err
}
defer conn.Close()

// Read / Write JSON messages
var msg IncomingEvent
if err := conn.ReadJSON(&msg); err != nil { ... }
```

### Server-Sent Events (SSE):
```go
stream, err := realtime.StreamSSE(ctx, doer, "https://api.example.com/events")
if err != nil {
	return err
}
defer stream.Close()

for event := range stream.Events() {
	fmt.Printf("Event %s: %s\n", event.Type, event.Data)
}
```

## 7. TLS Fingerprinting & Browser Evasion

Import: `"github.com/lemon4ksan/aoni/option"`

To bypass Cloudflare, DataDome, Akamai, and AWS WAF:

```go
client := aoni.NewClient(
	option.WithChrome(),                  // Emulate modern Chrome TLS JA4 & HTTP/2 framing
	option.WithRandomizedHeaderOrder(),   // Randomize header ordering (RFC 7540)
	option.WithECH(),                     // TLS 1.3 Encrypted Client Hello
	option.WithProxyPool(proxyRotator),   // Rotate residential proxies
	option.WithIsolatedCookieJar(),       // Separate cookie jar per proxy IP
)
```
