// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package socket provides a zero-allocation, protocol-agnostic, multi-core socket engine for persistent TCP and WebSocket protocols.
//
// # Architectural Pillars
//
//   - [github.com/lemon4ksan/aoni/realtime/socket/connector]: Resilient dialing, cipher integration, and exponential backoff auto-reconnect cycles.
//   - [github.com/lemon4ksan/aoni/realtime/socket/processor]: Parallel worker pools for parsing raw framed byte streams across CPU cores.
//   - [github.com/lemon4ksan/aoni/realtime/socket/dispatcher]: Dual-indexed routing (dense opcode array + sparse method map) and synchronous RPC correlation via jobs.Manager.
//   - [github.com/lemon4ksan/aoni/realtime/socket/session]: Lock-free atomic session state snapshots.
package socket
