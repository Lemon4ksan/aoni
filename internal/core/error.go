// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

// Phase represents a specific discrete phase of the network request lifecycle.
type Phase uint8

const (
	// PhaseUnknown indicates the failure occurred outside tracked request phases.
	PhaseUnknown Phase = iota

	// PhaseDNS indicates failure during domain name resolution.
	PhaseDNS

	// PhaseProxyConnect indicates failure during proxy tunnel establishment (SOCKS5 / HTTP CONNECT).
	PhaseProxyConnect

	// PhaseTCPConnect indicates failure during raw TCP / QUIC socket dial.
	PhaseTCPConnect

	// PhaseTLSHandshake indicates failure during TLS negotiation, certificate validation, or ECH exchange.
	PhaseTLSHandshake

	// PhaseSendHeaders indicates failure while framing and writing request headers.
	PhaseSendHeaders

	// PhaseSendBody indicates failure while streaming the request body.
	PhaseSendBody

	// PhaseWaitResponse indicates failure while waiting for initial response headers / TTFB (e.g. server timeout).
	PhaseWaitResponse

	// PhaseReadBody indicates failure while reading the incoming response stream payload.
	PhaseReadBody
)

// String returns the canonical human-readable label for the execution phase.
func (p Phase) String() string {
	switch p {
	case PhaseDNS:
		return "DNS"
	case PhaseProxyConnect:
		return "ProxyConnect"
	case PhaseTCPConnect:
		return "TCPConnect"
	case PhaseTLSHandshake:
		return "TLSHandshake"
	case PhaseSendHeaders:
		return "SendHeaders"
	case PhaseSendBody:
		return "SendBody"
	case PhaseWaitResponse:
		return "WaitResponse"
	case PhaseReadBody:
		return "ReadBody"
	default:
		return "Unknown"
	}
}

// RFC 9113 HTTP/2 error code names.
var h2CodeNames = [...]string{
	0x0: "NO_ERROR",
	0x1: "PROTOCOL_ERROR",
	0x2: "INTERNAL_ERROR",
	0x3: "FLOW_CONTROL_ERROR",
	0x4: "SETTINGS_TIMEOUT",
	0x5: "STREAM_CLOSED",
	0x6: "FRAME_SIZE_ERROR",
	0x7: "REFUSED_STREAM",
	0x8: "CANCEL",
	0x9: "COMPRESSION_ERROR",
	0xa: "CONNECT_ERROR",
	0xb: "ENHANCE_YOUR_CALM",
	0xc: "INADEQUATE_SECURITY",
	0xd: "HTTP_1_1_REQUIRED",
}

// H2CodeName returns the RFC 9113 mnemonic name for an HTTP/2 error code.
func H2CodeName(code uint32) string {
	if int(code) < len(h2CodeNames) && h2CodeNames[code] != "" {
		return h2CodeNames[code]
	}

	return fmt.Sprintf("0x%x", code)
}

// Error describes a structured operational failure across the networking pipeline.
type Error struct {
	// Err holds the underlying cause error.
	Err error
	// Op identifies the high-level operation during which the failure occurred.
	Op string
	// Path specifies the request path or URI associated with the error.
	Path string
	// Code holds the HTTP status code or discrete error code if applicable.
	Code int
	// Target identifies the remote target host or address.
	Target string
	// URL specifies the complete target URL if available.
	URL string
	// Phase identifies the lifecycle phase where the failure triggered.
	Phase Phase
	// Protocol identifies the negotiated protocol ("HTTP/1.1", "HTTP/2", "HTTP/3").
	Protocol string
	// StreamID indicates the multiplexed stream ID if applicable.
	StreamID uint32
	// RemoteAddr specifies the remote server IP:Port.
	RemoteAddr string
	// ProxyURL specifies the proxy address if a proxy was used.
	ProxyURL string
	// IsReused indicates whether a pooled keep-alive socket was reused.
	IsReused bool
	// BytesSent records the count of bytes transmitted before failure.
	BytesSent int64
	// BytesRecv records the count of bytes received before failure.
	BytesRecv int64
	// Duration records elapsed time before failure.
	Duration time.Duration
	// H2Code holds the RFC 9113 HTTP/2 or HTTP/3 error code.
	H2Code uint32
	// H2CodeSet reports whether H2Code is populated.
	H2CodeSet bool
}

// Error formats the structured network fault into a concise, informative single-line error message.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	var sb strings.Builder

	sb.WriteString("aoni: ")

	if e.Op != "" {
		sb.WriteString(e.Op)
		sb.WriteString(": ")
	}

	if e.Target != "" {
		sb.WriteString(e.Target)
		sb.WriteString(": ")
	}

	if e.Path != "" {
		sb.WriteString(e.Path)
		sb.WriteString(": ")
	} else if e.URL != "" && e.Target == "" {
		sb.WriteString(e.URL)
		sb.WriteString(": ")
	}

	if e.Phase != PhaseUnknown {
		sb.WriteString("[")
		sb.WriteString(e.Phase.String())
		sb.WriteString("] ")
	}

	var meta []string

	if e.Protocol != "" {
		if e.StreamID > 0 {
			meta = append(meta, fmt.Sprintf("%s stream=%d", e.Protocol, e.StreamID))
		} else {
			meta = append(meta, e.Protocol)
		}
	}

	if e.H2CodeSet {
		meta = append(meta, "h2_code="+H2CodeName(e.H2Code))
	}

	if e.RemoteAddr != "" {
		meta = append(meta, "remote="+e.RemoteAddr)
	}

	if e.ProxyURL != "" {
		meta = append(meta, "via proxy="+e.ProxyURL)
	}

	if e.IsReused {
		meta = append(meta, "reused=true")
	}

	if e.BytesSent > 0 {
		meta = append(meta, fmt.Sprintf("sent=%dB", e.BytesSent))
	}

	if e.BytesRecv > 0 {
		meta = append(meta, fmt.Sprintf("recv=%dB", e.BytesRecv))
	}

	if len(meta) > 0 {
		sb.WriteString("(")
		sb.WriteString(strings.Join(meta, ", "))
		sb.WriteString("): ")
	}

	if e.Err != nil {
		sb.WriteString(e.Err.Error())
	}

	return sb.String()
}

// Unwrap returns the underlying root cause error to support errors.Is and errors.As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

// LogValue implements [slog.LogValuer] for structured, zero-allocation logging.
func (e *Error) LogValue() slog.Value {
	if e == nil {
		return slog.GroupValue()
	}

	attrs := make([]slog.Attr, 0, 8)
	if e.Op != "" {
		attrs = append(attrs, slog.String("op", e.Op))
	}

	if e.Target != "" {
		attrs = append(attrs, slog.String("target", e.Target))
	}

	if e.Path != "" {
		attrs = append(attrs, slog.String("path", e.Path))
	}

	if e.Phase != PhaseUnknown {
		attrs = append(attrs, slog.String("phase", e.Phase.String()))
	}

	if e.Protocol != "" {
		attrs = append(attrs, slog.String("protocol", e.Protocol))
	}

	if e.RemoteAddr != "" {
		attrs = append(attrs, slog.String("remote", e.RemoteAddr))
	}

	if e.Err != nil {
		attrs = append(attrs, slog.String("cause", e.Err.Error()))
	}

	return slog.GroupValue(attrs...)
}

// IsTimeout reports whether the error was caused by a context deadline, connect timeout, or I/O deadline.
func (e *Error) IsTimeout() bool {
	if e == nil || e.Err == nil {
		return false
	}

	var netErr net.Error
	if errors.As(e.Err, &netErr) && netErr.Timeout() {
		return true
	}

	return errors.Is(e.Err, os.ErrDeadlineExceeded)
}

// IsTemporary reports whether the failure is considered transient and potentially retryable.
func (e *Error) IsTemporary() bool {
	if e == nil {
		return false
	}

	if e.H2CodeSet && e.H2Code == 0x7 { // REFUSED_STREAM
		return true
	}

	var netErr net.Error
	if errors.As(e.Err, &netErr) && netErr.Timeout() {
		return true
	}

	return false
}

// IsTLS reports whether the failure occurred during TLS handshake or certificate verification.
func (e *Error) IsTLS() bool {
	if e == nil || e.Err == nil {
		return false
	}

	if e.Phase == PhaseTLSHandshake {
		return true
	}

	var certErr *tls.CertificateVerificationError

	return errors.As(e.Err, &certErr)
}

// IsProxy reports whether the failure occurred on the proxy hop.
func (e *Error) IsProxy() bool {
	if e == nil {
		return false
	}

	return e.Phase == PhaseProxyConnect || e.ProxyURL != ""
}

// IsRateLimited reports whether the failure was caused by server-side rate-limiting (e.g. H2 ENHANCE_YOUR_CALM).
func (e *Error) IsRateLimited() bool {
	if e == nil {
		return false
	}

	return e.H2CodeSet && e.H2Code == 0xb // ENHANCE_YOUR_CALM
}

// IsConnectionRefused reports whether the network connection was refused or reset by the remote peer.
func (e *Error) IsConnectionRefused() bool {
	if e == nil || e.Err == nil {
		return false
	}

	return errors.Is(e.Err, syscall.ECONNREFUSED) || errors.Is(e.Err, syscall.ECONNRESET)
}
