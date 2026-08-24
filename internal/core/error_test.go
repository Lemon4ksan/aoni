// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core_test

import (
	"context"
	"crypto/tls"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/core"
)

func TestPhase_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Unknown", core.PhaseUnknown.String())
	assert.Equal(t, "DNS", core.PhaseDNS.String())
	assert.Equal(t, "ProxyConnect", core.PhaseProxyConnect.String())
	assert.Equal(t, "TCPConnect", core.PhaseTCPConnect.String())
	assert.Equal(t, "TLSHandshake", core.PhaseTLSHandshake.String())
	assert.Equal(t, "SendHeaders", core.PhaseSendHeaders.String())
	assert.Equal(t, "SendBody", core.PhaseSendBody.String())
	assert.Equal(t, "WaitResponse", core.PhaseWaitResponse.String())
	assert.Equal(t, "ReadBody", core.PhaseReadBody.String())
	assert.Equal(t, "Unknown", core.Phase(99).String())
}

func TestH2CodeName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "NO_ERROR", core.H2CodeName(0x0))
	assert.Equal(t, "PROTOCOL_ERROR", core.H2CodeName(0x1))
	assert.Equal(t, "INTERNAL_ERROR", core.H2CodeName(0x2))
	assert.Equal(t, "FLOW_CONTROL_ERROR", core.H2CodeName(0x3))
	assert.Equal(t, "SETTINGS_TIMEOUT", core.H2CodeName(0x4))
	assert.Equal(t, "STREAM_CLOSED", core.H2CodeName(0x5))
	assert.Equal(t, "FRAME_SIZE_ERROR", core.H2CodeName(0x6))
	assert.Equal(t, "REFUSED_STREAM", core.H2CodeName(0x7))
	assert.Equal(t, "CANCEL", core.H2CodeName(0x8))
	assert.Equal(t, "COMPRESSION_ERROR", core.H2CodeName(0x9))
	assert.Equal(t, "CONNECT_ERROR", core.H2CodeName(0xa))
	assert.Equal(t, "ENHANCE_YOUR_CALM", core.H2CodeName(0xb))
	assert.Equal(t, "INADEQUATE_SECURITY", core.H2CodeName(0xc))
	assert.Equal(t, "HTTP_1_1_REQUIRED", core.H2CodeName(0xd))
	assert.Equal(t, "0xfe", core.H2CodeName(0xfe))
}

func TestError_FormattingAndUnwrap(t *testing.T) {
	t.Parallel()

	rootErr := context.DeadlineExceeded

	err := &core.Error{
		Op:         "POST",
		URL:        "https://api.example.com/v1/charge",
		Phase:      core.PhaseWaitResponse,
		Protocol:   "HTTP/2",
		StreamID:   5,
		RemoteAddr: "104.21.4.19:443",
		ProxyURL:   "socks5://127.0.0.1:9050",
		IsReused:   true,
		BytesSent:  512,
		BytesRecv:  0,
		Duration:   250 * time.Millisecond,
		H2Code:     0xb,
		H2CodeSet:  true,
		Err:        rootErr,
	}

	msg := err.Error()
	assert.Contains(t, msg, "POST")
	assert.Contains(t, msg, "https://api.example.com/v1/charge")
	assert.Contains(t, msg, "[WaitResponse]")
	assert.Contains(t, msg, "HTTP/2 stream=5")
	assert.Contains(t, msg, "h2_code=ENHANCE_YOUR_CALM")
	assert.Contains(t, msg, "remote=104.21.4.19:443")
	assert.Contains(t, msg, "via proxy=socks5://127.0.0.1:9050")
	assert.Contains(t, msg, "reused=true")
	assert.Contains(t, msg, "sent=512B")
	assert.Contains(t, msg, "context deadline exceeded")

	// Test Unwrap and errors.Is
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	var target *core.Error
	require.True(t, errors.As(err, &target))
	assert.Equal(t, uint32(5), target.StreamID)
}

func TestError_NilSafety(t *testing.T) {
	t.Parallel()

	var err *core.Error
	assert.Equal(t, "<nil>", err.Error())
	assert.Nil(t, err.Unwrap())
	assert.False(t, err.IsTimeout())
	assert.False(t, err.IsTemporary())
	assert.False(t, err.IsTLS())
	assert.False(t, err.IsProxy())
	assert.False(t, err.IsRateLimited())
	assert.False(t, err.IsConnectionRefused())
}

func TestError_Predicates(t *testing.T) {
	t.Parallel()

	// Timeout
	tErr := &core.Error{Err: context.DeadlineExceeded}
	assert.True(t, tErr.IsTimeout())

	// TLS
	tlsErr := &core.Error{
		Phase: core.PhaseTLSHandshake,
		Err:   errors.New("tls: handshake failure"),
	}
	assert.True(t, tlsErr.IsTLS())

	certErr := &core.Error{
		Phase: core.PhaseUnknown,
		Err:   &tls.CertificateVerificationError{},
	}
	assert.True(t, certErr.IsTLS())

	// Proxy
	proxyErr := &core.Error{
		ProxyURL: "http://proxy.local:8080",
		Err:      errors.New("proxy error"),
	}
	assert.True(t, proxyErr.IsProxy())

	// Rate limited
	rlErr := &core.Error{
		H2CodeSet: true,
		H2Code:    0xb,
	}
	assert.True(t, rlErr.IsRateLimited())

	// Refused Stream (temporary)
	refusedErr := &core.Error{
		H2CodeSet: true,
		H2Code:    0x7,
	}
	assert.True(t, refusedErr.IsTemporary())

	// Connection refused
	connRefusedErr := &core.Error{
		Err: syscall.ECONNREFUSED,
	}
	assert.True(t, connRefusedErr.IsConnectionRefused())

	// Connection reset
	connResetErr := &core.Error{
		Err: syscall.ECONNRESET,
	}
	assert.True(t, connResetErr.IsConnectionRefused())
}
