// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cookie provides Chromium-grade, proxy-isolated cookie jars, persistence backends,
// Netscape HTTP cookie file exports, and RFC 6265bis CHIPS partitioned state management.
//
// # Specification Adherence
//
// Conforms rigorously to IETF standards:
//   - RFC 6265 (HTTP State Management Mechanism)
//   - RFC 6265bis §5.5 (400-day max age limits) & §5.7 (Cookie Prefixes: __Secure-, __Host-)
//   - RFC 6265bis CHIPS (Cookies Having Independent Partitioned State)
//   - Netscape HTTP Cookie File specification
//
// # Architectural Overview: Proxy-Isolated Sessions
//
// When running concurrent web automation or scraping through rotating proxies, standard [net/http/cookiejar]
// pools all cookies under a global host key. This causes severe session contamination: requests through Proxy A
// receive authentication cookies set by Proxy B, leading to account bans and security leaks.
//
// [ProxyIsolatedJar] solves this by strictly isolating cookie stores per proxy endpoint ([WithProxyAddress]):
//
//	jar := cookie.NewProxyIsolatedJar().WithStorageBackend(cookie.NewJSONFileStorage("cookies.json"))
//	client := aoni.NewClient(option.WithCookieJar(jar))
//
//	// Requests via Proxy 1 and Proxy 2 maintain completely independent sessions:
//	ctx1 := cookie.WithProxyAddress(context.Background(), "http://proxy1.lan:8080")
//	ctx2 := cookie.WithProxyAddress(context.Background(), "http://proxy2.lan:8080")
//
// # Persistence Storage Engines
//
//   - [JSONFileStorage]: Thread-safe, atomic disk storage utilizing temporary file creation and OS rename swaps
//     to guarantee zero file corruption on crashes.
//   - [SQLStorage]: High-concurrency relational backend supporting SQLite and PostgreSQL databases.
//
// # Cookie Inspection & Swift-Inspired Generics
//
// [ProxyIsolatedJar] exposes type-safe query helpers backed by [github.com/lemon4ksan/foundation/generic]:
//
//	if val, ok := jar.GetCookieValue(targetURL, "session_id"); ok {
//	    fmt.Println("Session:", val)
//	}
//
//	if cookieOpt := jar.FindCookieOptional(targetURL, "csrf_token"); cookieOpt.IsPresent() {
//	    ...
//	}
//
// # Export & Mirror Utilities
//
//   - [ExportNetscape]: Exports cookie jars as standard Netscape cookies.txt files for cURL/Wget/Puppeteer.
//   - [Export] / [Import]: Serializes and deserializes structured [Cookie] slices.
//   - [Mirror]: Replicates authentication cookies across related domains (e.g. login.example.com -> api.example.com).
package cookie
