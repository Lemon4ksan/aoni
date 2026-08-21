// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package asyncapi provides zero-dependency parsing, version normalization, trait resolution,
// and declarative Go contract generation for AsyncAPI 2.x (2.0 - 2.6.0) and AsyncAPI 3.x (3.0 - 3.1.0) specifications.
//
// # Architecture Overview
//
// The asyncapi package enables asynchronous, real-time message streaming contracts (WebSockets, SSE, Socket.IO, MQTT, Kafka)
// to be represented as declarative Go interfaces compatible with the aoni client framework:
//
//  1. Parsing & Dual-Version Normalization (parser.go):
//     Parses AsyncAPI 2.x and 3.x specifications from YAML/JSON. Migrates legacy 2.x channel-centric models
//     (publish/subscribe) into the unified 3.x operations model (send/receive) in-memory.
//
//  2. Trait Merging & Dynamic Address Resolution:
//     Applies operation and message traits inheritance, resolving shared headers and parameters.
//     Extracts dynamic topic/channel parameters ({channel_id}) from channel addresses.
//
//  3. Multi-Specification Set Algebra (merge.go):
//     Enables combining multiple streaming service definitions through Union, Intersection, and Difference merge modes.
//
//  4. Declarative Contract Emitter (importer.go):
//     Generates Go interfaces with aoni real-time directives (@service, @protocol, @topic, @send, @receive),
//     client subscription methods, and payload DTO structs.
//
// # Supported Standards
//
//   - AsyncAPI Specification v3.1.0: https://www.asyncapi.com/docs/reference/specification/v3.1.0
//   - AsyncAPI Specification v2.6.0: https://v2.asyncapi.com/docs/reference/specification/v2.6.0
//   - RFC 6455 (The WebSocket Protocol): https://datatracker.ietf.org/doc/html/rfc6455
//   - RFC 8441 (Bootstrapping WebSockets with HTTP/2): https://datatracker.ietf.org/doc/html/rfc8441
//   - W3C Server-Sent Events: https://www.w3.org/TR/eventsource/
//   - JSON Schema Draft 2020-12: https://json-schema.org/draft/2020-12/json-schema-core.html
//
// # Example
//
//	res, err := asyncapi.Import(asyncapi.ImportConfig{
//	    SpecFile:    "chat_stream.yaml",
//	    PackageName: "chat",
//	    ServiceName: "ChatStream",
//	})
//	if err != nil {
//	    log.Fatalf("failed importing asyncapi spec: %v", err)
//	}
//	_ = os.WriteFile("chat_contract.go", res.ContractCode, 0644)
package asyncapi
