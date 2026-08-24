// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package p0f_test

import (
	"net"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/fingerprint/p0f"
)

func TestParse(t *testing.T) {
	t.Parallel()

	sig, err := p0f.Parse("*:64:0:*:mss*20,10:mss,sok,ts,nop,ws:df,id+:0")
	require.NoError(t, err)
	assert.Equal(t, "*", sig.IPVersion)
	assert.Equal(t, 64, sig.TTL)
	assert.Equal(t, 0, sig.IPOptLen)
	assert.Equal(t, -1, sig.MSS)
	assert.Equal(t, 20, sig.WindowSize)
	assert.Equal(t, p0f.WindowMSS, sig.WindowType)
	assert.Equal(t, 10, sig.WindowScale)
	assert.Equal(t, []string{"mss", "sok", "ts", "nop", "ws"}, sig.Options)
	assert.Equal(t, []string{"df", "id+"}, sig.Quirks)
	assert.Equal(t, "0", sig.Payload)
}

func TestParseWindows(t *testing.T) {
	t.Parallel()

	sig, err := p0f.Parse("*:128:0:*:8192,8:mss,nop,ws,nop,nop,sok:df,id+:0")
	require.NoError(t, err)
	assert.Equal(t, 128, sig.TTL)
	assert.Equal(t, 8192, sig.WindowSize)
	assert.Equal(t, p0f.WindowNormal, sig.WindowType)
	assert.Equal(t, 8, sig.WindowScale)
}

func TestParseMTUWindow(t *testing.T) {
	t.Parallel()

	sig, err := p0f.Parse("4:64:0:1460:mtu*4,0:mss,sok:df:0")
	require.NoError(t, err)
	assert.Equal(t, "4", sig.IPVersion)
	assert.Equal(t, 1460, sig.MSS)
	assert.Equal(t, p0f.WindowMTU, sig.WindowType)
	assert.Equal(t, 4, sig.WindowSize)
	assert.Equal(t, 0, sig.WindowScale)
	assert.Equal(t, "4:64:0:1460:mtu*4,0:mss,sok:df:0", sig.String())
}

func TestParseWildcard(t *testing.T) {
	t.Parallel()

	sig, err := p0f.Parse("*:64:0:*:*,-1:mss,sok,ts,nop,ws:df,id+:0")
	require.NoError(t, err)
	assert.Equal(t, p0f.WindowAny, sig.WindowType)
}

func TestParseTTLMinus(t *testing.T) {
	t.Parallel()

	sig, err := p0f.Parse("*:64-:0:265:512,0:mss,sok,ts:ack+:0")
	require.NoError(t, err)
	assert.Equal(t, 64, sig.TTL)
	assert.True(t, sig.HasTTLMinus)
}

func TestParseErrors(t *testing.T) {
	t.Parallel()

	t.Run("too_few_parts", func(t *testing.T) {
		t.Parallel()

		_, err := p0f.Parse("too:few:parts")
		assert.ErrorIs(t, err, p0f.ErrInvalidP0fSignature)
	})

	t.Run("invalid_ip_version", func(t *testing.T) {
		t.Parallel()

		_, err := p0f.Parse("3:64:0:*:mss*20,10:mss,sok,ts,nop,ws:df,id+:0")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid IP version")
	})

	t.Run("invalid_ttl", func(t *testing.T) {
		t.Parallel()

		_, err := p0f.Parse("*:invalid:0:*:mss*20,10:mss:df:0")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid TTL")
	})

	t.Run("invalid_ip_opt_len", func(t *testing.T) {
		t.Parallel()

		_, err := p0f.Parse("*:64:invalid:*:mss*20,10:mss:df:0")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid IP option length")
	})

	t.Run("invalid_mss", func(t *testing.T) {
		t.Parallel()

		_, err := p0f.Parse("*:64:0:invalid:mss*20,10:mss:df:0")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid MSS")
	})

	t.Run("invalid_window_scale", func(t *testing.T) {
		t.Parallel()

		_, err := p0f.Parse("*:64:0:*:8192,invalid:mss:df:0")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid window scale")
	})
}

func TestSignatureRoundTrip(t *testing.T) {
	t.Parallel()

	original := "*:64:0:*:mss*20,10:mss,sok,ts,nop,ws:df,id+:0"
	sig := p0f.MustParse(original)
	assert.Equal(t, original, sig.String())
}

func TestSignatureRoundTripTTLMinus(t *testing.T) {
	t.Parallel()

	original := "*:64-:0:265:512,0:mss,sok,ts:ack+:0"
	sig := p0f.MustParse(original)
	assert.Equal(t, original, sig.String())
}

func TestMustParse_PanicOnInvalid(t *testing.T) {
	t.Parallel()

	assert.Panics(t, func() {
		p0f.MustParse("invalid:signature")
	})
}

func TestSpoofer_Apply(t *testing.T) {
	t.Parallel()

	sig := p0f.Linux311
	spoofer := p0f.NewSpoofer(sig)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			_ = conn.Close()
		}
	}()

	clientConn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientConn.Close() })

	err = spoofer.Apply(clientConn)
	assert.NoError(t, err)

	tcpConn, ok := clientConn.(*net.TCPConn)
	require.True(t, ok)

	rawConn, err := tcpConn.SyscallConn()
	require.NoError(t, err)

	err = spoofer.ApplyToRawConn(rawConn)
	assert.NoError(t, err)
}

func TestBuiltinSignatures(t *testing.T) {
	t.Parallel()

	assert.NotNil(t, p0f.Linux311)
	assert.NotNil(t, p0f.Linux3x)
	assert.NotNil(t, p0f.Linux26)
	assert.NotNil(t, p0f.Linux24)
	assert.NotNil(t, p0f.WindowsXP)
	assert.NotNil(t, p0f.Windows7)
	assert.NotNil(t, p0f.Windows10)
	assert.NotNil(t, p0f.MacOS)
	assert.NotNil(t, p0f.Android)
	assert.NotNil(t, p0f.IOS)
	assert.NotNil(t, p0f.Nmap)

	assert.Equal(t, 64, p0f.Linux311.TTL)
	assert.Equal(t, 128, p0f.Windows7.TTL)
	assert.Equal(t, 128, p0f.Windows10.TTL)
	assert.Equal(t, 64, p0f.MacOS.TTL)
	assert.Equal(t, 64, p0f.Android.TTL)
	assert.Equal(t, 64, p0f.IOS.TTL)
}
