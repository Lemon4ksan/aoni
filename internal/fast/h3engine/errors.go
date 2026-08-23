// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3engine

import "errors"

// ErrorCode represents an HTTP/3 application error code transmitted over QUIC (RFC 9114 §8.1).
type ErrorCode uint64

const (
	// ErrCodeH3NoError signals graceful closure without an error (RFC 9114 §8.1: 0x0100).
	ErrCodeH3NoError ErrorCode = 0x0100

	// ErrCodeH3GeneralProtocolError signals an unspecified protocol violation (RFC 9114 §8.1: 0x0101).
	ErrCodeH3GeneralProtocolError ErrorCode = 0x0101

	// ErrCodeH3InternalError signals an internal software defect in HTTP/3 stack (RFC 9114 §8.1: 0x0102).
	ErrCodeH3InternalError ErrorCode = 0x0102

	// ErrCodeH3StreamCreationError signals an unaccepted or duplicate stream creation (RFC 9114 §8.1 & §6.1: 0x0103).
	ErrCodeH3StreamCreationError ErrorCode = 0x0103

	// ErrCodeH3ClosedCriticalStream signals closure of control or QPACK stream (RFC 9114 §8.1 & §6.2.1: 0x0104).
	ErrCodeH3ClosedCriticalStream ErrorCode = 0x0104

	// ErrCodeH3FrameUnexpected signals receipt of an invalid frame in current state (RFC 9114 §8.1 & §4.1: 0x0105).
	ErrCodeH3FrameUnexpected ErrorCode = 0x0105

	// ErrCodeH3FrameError signals frame layout or payload length violations (RFC 9114 §8.1 & §7.1: 0x0106).
	ErrCodeH3FrameError ErrorCode = 0x0106

	// ErrCodeH3ExcessiveLoad signals excessive load or potential DoS activity (RFC 9114 §8.1 & §10.5: 0x0107).
	ErrCodeH3ExcessiveLoad ErrorCode = 0x0107

	// ErrCodeH3IDError signals incorrect stream ID or push ID usage (RFC 9114 §8.1 & §4.6: 0x0108).
	ErrCodeH3IDError ErrorCode = 0x0108

	// ErrCodeH3SettingsError signals invalid SETTINGS parameters or reserved H2 setting IDs (RFC 9114 §8.1 & §7.2.4: 0x0109).
	ErrCodeH3SettingsError ErrorCode = 0x0109

	// ErrCodeH3MissingSettings signals missing initial SETTINGS frame on control stream (RFC 9114 §8.1 & §6.2.1: 0x010a).
	ErrCodeH3MissingSettings ErrorCode = 0x010a

	// ErrCodeH3RequestRejected signals server rejection without application processing (RFC 9114 §8.1 & §4.1.1: 0x010b).
	ErrCodeH3RequestRejected ErrorCode = 0x010b

	// ErrCodeH3RequestCancelled signals request or push cancellation (RFC 9114 §8.1 & §4.1.1: 0x010c).
	ErrCodeH3RequestCancelled ErrorCode = 0x010c

	// ErrCodeH3RequestIncomplete signals incomplete request stream termination (RFC 9114 §8.1 & §4.1: 0x010d).
	ErrCodeH3RequestIncomplete ErrorCode = 0x010d

	// ErrCodeH3MessageError signals malformed HTTP message syntax or pseudo-headers (RFC 9114 §8.1 & §4.1.2: 0x010e).
	ErrCodeH3MessageError ErrorCode = 0x010e

	// ErrCodeH3ConnectError signals TCP tunnel reset on CONNECT method (RFC 9114 §8.1 & §4.4: 0x010f).
	ErrCodeH3ConnectError ErrorCode = 0x010f

	// ErrCodeH3VersionFallback signals retry requirement over HTTP/1.1 (RFC 9114 §8.1: 0x0110).
	ErrCodeH3VersionFallback ErrorCode = 0x0110

	// ErrCodeQpackDecompressionFailed indicates decoding of a field section failed (RFC 9204 §6 & §8.3: 0x0200).
	ErrCodeQpackDecompressionFailed ErrorCode = 0x0200

	// ErrCodeQpackEncoderStreamError indicates an error on the QPACK encoder stream (RFC 9204 §6 & §8.3: 0x0201).
	ErrCodeQpackEncoderStreamError ErrorCode = 0x0201

	// ErrCodeQpackDecoderStreamError indicates an error on the QPACK decoder stream (RFC 9204 §6 & §8.3: 0x0202).
	ErrCodeQpackDecoderStreamError ErrorCode = 0x0202
)

var (
	// ErrServerClosed indicates graceful or abrupt server connection termination (RFC 9114 §5.2).
	ErrServerClosed = errors.New("aoni/h3engine: server closed HTTP/3 connection (RFC 9114 §5.2)")

	// ErrFrameUnexpected indicates an unexpected frame type was received in current stream state (RFC 9114 §4.1 & §7.2).
	ErrFrameUnexpected = errors.New("aoni/h3engine: received unexpected frame type (RFC 9114 §4.1)")

	// ErrHeaderTooLarge indicates header block exceeded SETTINGS_MAX_FIELD_SECTION_SIZE (RFC 9114 §7.2.4.1 & §10.5.1).
	ErrHeaderTooLarge = errors.New("aoni/h3engine: response headers exceed size limit (RFC 9114 §10.5.1)")

	// ErrStreamClosed indicates that an HTTP/3 stream was closed prematurely (RFC 9114 §4.1).
	ErrStreamClosed = errors.New("aoni/h3engine: stream closed prematurely (RFC 9114 §4.1)")

	// ErrInvalidStreamType indicates an unrecognized or unsupported unidirectional stream header (RFC 9114 §6.2).
	ErrInvalidStreamType = errors.New("aoni/h3engine: invalid unidirectional stream type (RFC 9114 §6.2)")

	// ErrMissingSettings indicates the peer failed to send SETTINGS as the first frame on control stream (RFC 9114 §6.2.1).
	ErrMissingSettings = errors.New("aoni/h3engine: server did not send initial SETTINGS frame (RFC 9114 §6.2.1)")

	// ErrDuplicateControlStream indicates receipt of multiple control streams from the same peer (RFC 9114 §6.2.1).
	ErrDuplicateControlStream = errors.New("aoni/h3engine: duplicate control stream received (RFC 9114 §6.2.1)")

	// ErrClosedCriticalStream indicates a critical control or QPACK stream was closed (RFC 9114 §6.2.1).
	ErrClosedCriticalStream = errors.New("aoni/h3engine: critical unidirectional stream was closed (RFC 9114 §6.2.1)")

	// ErrQPACKDecompressFailed indicates failure in QPACK header block decompression (RFC 9114 §4.2 & RFC 9204).
	ErrQPACKDecompressFailed = errors.New("aoni/h3engine: QPACK header decompression failed (RFC 9114 §4.2)")

	// ErrMissingStatusHeader indicates response headers missing mandatory :status pseudo-header (RFC 9114 §4.3).
	ErrMissingStatusHeader = errors.New("aoni/h3engine: response missing mandatory :status header (RFC 9114 §4.3)")

	// ErrInvalidHostHeader indicates malformed Host or :authority header formatting (RFC 9114 §4.3).
	ErrInvalidHostHeader = errors.New("aoni/h3engine: invalid Host or :authority header (RFC 9114 §4.3)")
)
