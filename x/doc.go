// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package x provides supplementary protocols, vendor database connectors, and ecosystem extensions for the aoni networking stack.
//
// Unlike core aoni, which strictly adheres to IETF RFCs and W3C/Chromium networking standards to guarantee immutable API stability (Evergreen v1),
// the aoni/x sub-module houses higher-level application protocols, evolving specifications, and third-party vendor integrations:
//
//   - [github.com/lemon4ksan/aoni/x/socketio]: Full client implementation for Socket.IO v5 / Engine.IO v4 over aoni WebSockets & HTTP Long-Polling.
//   - [github.com/lemon4ksan/aoni/x/geoip]: Fast MaxMind MMDB GeoIP2 / GeoLite2 geolocation database lookup.
//   - [github.com/lemon4ksan/aoni/x/otel]: Zero-dependency pure-Go OpenTelemetry (W3C TraceContext & OTLP/HTTP) distributed tracing engine.
//   - [github.com/lemon4ksan/aoni/x/webtransport]: W3C and IETF WebTransport over HTTP/3 protocol engine (RFC 9114, RFC 9297, RFC 9220, RFC 9221).
//   - [github.com/lemon4ksan/aoni/x/grpc/dynamic]: Dynamic gRPC invocation using Protobuf reflection descriptors and JSON messages.
//   - [github.com/lemon4ksan/aoni/x/sqlcookie]: SQL database-backed storage implementation of cookie.Storage for persistent proxy-isolated cookie jars.
//   - [github.com/lemon4ksan/aoni/x/tunnel/tun]: Low-level OS Layer 3 TUN/TAP device drivers for Windows (Wintun), Linux (/dev/net/tun), and macOS (utun).
package x
