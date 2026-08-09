// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package h3 provides QUIC transport parameters and flow control configurations for HTTP/3 browser impersonation.
//
// Provides pre-configured flow control presets ([ChromeSettings], [FirefoxSettings]) matching initial receive
// window sizes, stream limits, and datagram capabilities of modern browsers over QUIC.
package h3

// Settings holds QUIC flow control and stream parameters for browser-grade HTTP/3 fingerprint impersonation.
//
// Specification Adherence:
// Conforms to IETF RFC 9114 (HTTP/3) and RFC 9000 (QUIC: A UDP-Based Multiplexed and Secure Transport).
//
// Thread Safety:
// Struct values are immutable configuration DTOs; concurrent reads across goroutines are 100% safe.
type Settings struct {
	InitialStreamReceiveWindow     uint64
	MaxStreamReceiveWindow         uint64
	InitialConnectionReceiveWindow uint64
	MaxConnectionReceiveWindow     uint64
	MaxIncomingStreams             int64
	MaxIncomingUniStreams          int64
	EnableDatagrams                bool
}

// ChromeSettings provides production-grade QUIC transport presets matching Google Chrome.
var ChromeSettings = Settings{
	InitialStreamReceiveWindow:     6291456, // 6 MB
	MaxStreamReceiveWindow:         6291456,
	InitialConnectionReceiveWindow: 15728640, // 15 MB
	MaxConnectionReceiveWindow:     15728640,
	MaxIncomingStreams:             100,
	MaxIncomingUniStreams:          100,
	EnableDatagrams:                true,
}

// FirefoxSettings provides production-grade QUIC transport presets matching Mozilla Firefox.
var FirefoxSettings = Settings{
	InitialStreamReceiveWindow:     6291456, // 6 MB
	MaxStreamReceiveWindow:         6291456,
	InitialConnectionReceiveWindow: 25165824, // 24 MB
	MaxConnectionReceiveWindow:     25165824,
	MaxIncomingStreams:             100,
	MaxIncomingUniStreams:          100,
	EnableDatagrams:                false,
}
