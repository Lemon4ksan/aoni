// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

// HTTP3Settings holds QUIC flow control and stream parameters
// for browser-grade HTTP/3 fingerprint impersonation. Each field
// directly configures the underlying QUIC transport layer parameters.
type HTTP3Settings struct {
	InitialStreamReceiveWindow     uint64
	MaxStreamReceiveWindow         uint64
	InitialConnectionReceiveWindow uint64
	MaxConnectionReceiveWindow     uint64
	MaxIncomingStreams             int64
	MaxIncomingUniStreams          int64
	EnableDatagrams                bool
}

// ChromeHTTP3Settings provides production-grade QUIC transport presets matching Google Chrome.
var ChromeHTTP3Settings = HTTP3Settings{
	InitialStreamReceiveWindow:     6291456, // 6 MB
	MaxStreamReceiveWindow:         6291456,
	InitialConnectionReceiveWindow: 15728640, // 15 MB
	MaxConnectionReceiveWindow:     15728640,
	MaxIncomingStreams:             100,
	MaxIncomingUniStreams:          100,
	EnableDatagrams:                true,
}

// FirefoxHTTP3Settings provides production-grade QUIC transport presets matching Mozilla Firefox.
var FirefoxHTTP3Settings = HTTP3Settings{
	InitialStreamReceiveWindow:     6291456, // 6 MB
	MaxStreamReceiveWindow:         6291456,
	InitialConnectionReceiveWindow: 25165824, // 24 MB
	MaxConnectionReceiveWindow:     25165824,
	MaxIncomingStreams:             100,
	MaxIncomingUniStreams:          100,
	EnableDatagrams:                false,
}
