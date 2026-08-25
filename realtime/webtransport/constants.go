// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webtransport

// WebTransport HTTP Upgrade Token (draft-ietf-webtrans-http3-16 §3.2 & §9.1).
const (
	// ConnectProtocolWebTransport is the HTTP Extended CONNECT :protocol token for WebTransport over HTTP/3.
	ConnectProtocolWebTransport = "webtransport-h3"
)

// HTTP Header Fields for Application Protocol Negotiation (draft-ietf-webtrans-http3-16 §3.3 & §9.7).
const (
	// HeaderWTAvailableProtocols lists the client's supported application subprotocols (Structured Fields List).
	HeaderWTAvailableProtocols = "wt-available-protocols"

	// HeaderWTProtocol indicates the server's selected application subprotocol (Structured Fields Item).
	HeaderWTProtocol = "wt-protocol"
)

// HTTP/3 Settings Parameter Identifiers (draft-ietf-webtrans-http3-16 §3.1, §5.5, §9.2, RFC 9114, RFC 9220, RFC 9297).
const (
	// SettingEnableConnectProtocol enables Extended CONNECT for WebSockets and WebTransport (RFC 9220 §5: 0x08).
	SettingEnableConnectProtocol uint64 = 0x08

	// SettingH3Datagram enables HTTP/3 Datagram support over QUIC (RFC 9297 §5.1: 0x33).
	SettingH3Datagram uint64 = 0x33

	// SettingWebTransportEnabled indicates server/client support for WebTransport over HTTP/3 (draft-16 §9.2: 0x2c7cf000).
	SettingWebTransportEnabled uint64 = 0x2c7cf000

	// SettingWTInitialMaxData sets initial session data flow control limit in bytes (draft-16 §9.2: 0x2b61).
	SettingWTInitialMaxData uint64 = 0x2b61

	// SettingWTInitialMaxStreamsUni sets initial unidirectional stream limit for sessions (draft-16 §9.2: 0x2b64).
	SettingWTInitialMaxStreamsUni uint64 = 0x2b64

	// SettingWTInitialMaxStreamsBidi sets initial bidirectional stream limit for sessions (draft-16 §9.2: 0x2b65).
	SettingWTInitialMaxStreamsBidi uint64 = 0x2b65
)

// Stream Types and Frame Types for Multiplexed QUIC Streams (draft-ietf-webtrans-http3-16 §4.2, §4.3, §9.3, §9.4).
const (
	// StreamTypeWebTransportUni specifies the HTTP/3 unidirectional stream type for WebTransport (draft-16 §4.2: 0x54).
	StreamTypeWebTransportUni uint64 = 0x54

	// FrameTypeWebTransportBidi specifies the WT_STREAM frame signal value for bidirectional streams (draft-16 §4.3: 0x41).
	FrameTypeWebTransportBidi uint64 = 0x41
)

// Capsule Types for WebTransport Control Stream (draft-ietf-webtrans-http3-16 §4.7, §5.6, §6, §9.6).
const (
	// CapsuleCloseWebTransportSession communicates application-level closure with error code (draft-16 §6: 0x2843).
	CapsuleCloseWebTransportSession uint64 = 0x2843

	// CapsuleDrainWebTransportSession signals graceful drainage without opening new streams (draft-16 §4.7: 0x78ae).
	CapsuleDrainWebTransportSession uint64 = 0x78ae

	// CapsuleWTMaxData informs the peer of the maximum total stream data allowed on the session (draft-16 §5.6.4: 0x190b4d3d).
	CapsuleWTMaxData uint64 = 0x190b4d3d

	// CapsuleWTMaxStreamsBidi informs the peer of the cumulative bidirectional streams allowed (draft-16 §5.6.2: 0x190b4d3f).
	CapsuleWTMaxStreamsBidi uint64 = 0x190b4d3f

	// CapsuleWTMaxStreamsUni informs the peer of the cumulative unidirectional streams allowed (draft-16 §5.6.2: 0x190b4d40).
	CapsuleWTMaxStreamsUni uint64 = 0x190b4d40

	// CapsuleWTDataBlocked indicates the sender was blocked by session-level data limit (draft-16 §5.6.5: 0x190b4d41).
	CapsuleWTDataBlocked uint64 = 0x190b4d41

	// CapsuleWTStreamsBlockedBidi indicates the sender was blocked by bidirectional stream limit (draft-16 §5.6.3: 0x190b4d43).
	CapsuleWTStreamsBlockedBidi uint64 = 0x190b4d43

	// CapsuleWTStreamsBlockedUni indicates the sender was blocked by unidirectional stream limit (draft-16 §5.6.3: 0x190b4d44).
	CapsuleWTStreamsBlockedUni uint64 = 0x190b4d44
)

// HTTP/3 and WebTransport Error Codes (draft-ietf-webtrans-http3-16 §4.4, §9.5, RFC 9114 §8.1).
const (
	// ErrorCodeNoError represents successful termination with no error (RFC 9114 §8.1: 0x0100).
	ErrorCodeNoError uint64 = 0x0100

	// WTErrorCodeBufferedStreamRejected indicates stream rejected due to lack of associated session (draft-16 §9.5: 0x3994bd84).
	WTErrorCodeBufferedStreamRejected uint64 = 0x3994bd84

	// WTErrorCodeSessionGone indicates stream aborted because session closed or connect stream stopped (draft-16 §9.5: 0x170d7b68).
	WTErrorCodeSessionGone uint64 = 0x170d7b68

	// WTErrorCodeFlowControlError indicates session aborted due to flow control violation (draft-16 §9.5: 0x045d4487).
	WTErrorCodeFlowControlError uint64 = 0x045d4487

	// WTErrorCodeAlpnError indicates session aborted because ALPN negotiation failed (draft-16 §9.5: 0x0817b3dd).
	WTErrorCodeAlpnError uint64 = 0x0817b3dd

	// WTErrorCodeRequirementsNotMet indicates connection closed because WebTransport requirements not met (draft-16 §9.5: 0x212c0d48).
	WTErrorCodeRequirementsNotMet uint64 = 0x212c0d48

	// WTApplicationErrorFirst defines the lower bound of HTTP/3 error range reserved for WT application errors (draft-16 §4.4: 0x52e4a40fa8db).
	WTApplicationErrorFirst uint64 = 0x52e4a40fa8db

	// WTApplicationErrorLast defines the upper bound of HTTP/3 error range reserved for WT application errors (draft-16 §4.4: 0x52e5ac983162).
	WTApplicationErrorLast uint64 = 0x52e5ac983162
)

// TLS Keying Material Exporter Label (draft-ietf-webtrans-http3-16 §4.8).
const (
	// WTExporterLabel is the TLS 1.3 exporter label used for WebTransport session keying material derivation.
	WTExporterLabel = "EXPORTER-WebTransport"
)
