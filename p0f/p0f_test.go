// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package p0f_test

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/p0f"
)

func TestParse(t *testing.T) {
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
	sig, err := p0f.Parse("*:128:0:*:8192,8:mss,nop,ws,nop,nop,sok:df,id+:0")
	require.NoError(t, err)
	assert.Equal(t, 128, sig.TTL)
	assert.Equal(t, 8192, sig.WindowSize)
	assert.Equal(t, p0f.WindowNormal, sig.WindowType)
	assert.Equal(t, 8, sig.WindowScale)
}

func TestParseWildcard(t *testing.T) {
	sig, err := p0f.Parse("*:64:0:*:*,-1:mss,sok,ts,nop,ws:df,id+:0")
	require.NoError(t, err)
	assert.Equal(t, p0f.WindowAny, sig.WindowType)
}

func TestParseTTLMinus(t *testing.T) {
	sig, err := p0f.Parse("*:64-:0:265:512,0:mss,sok,ts:ack+:0")
	require.NoError(t, err)
	assert.Equal(t, 64, sig.TTL)
	assert.True(t, sig.HasTTLMinus)
}

func TestParseInvalidFormat(t *testing.T) {
	_, err := p0f.Parse("too:few:parts")
	assert.Error(t, err)
}

func TestParseInvalidIPVersion(t *testing.T) {
	_, err := p0f.Parse("3:64:0:*:mss*20,10:mss,sok,ts,nop,ws:df,id+:0")
	assert.Error(t, err)
}

func TestSignatureRoundTrip(t *testing.T) {
	original := "*:64:0:*:mss*20,10:mss,sok,ts,nop,ws:df,id+:0"
	sig := p0f.MustParse(original)
	assert.Equal(t, original, sig.String())
}

func TestSignatureRoundTripTTLMinus(t *testing.T) {
	original := "*:64-:0:265:512,0:mss,sok,ts:ack+:0"
	sig := p0f.MustParse(original)
	assert.Equal(t, original, sig.String())
}

func TestSpoofer_Apply(t *testing.T) {
	sig := p0f.Linux311
	spoofer := p0f.NewSpoofer(sig)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer ln.Close()

	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	clientConn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)

	defer clientConn.Close()

	err = spoofer.Apply(clientConn)
	assert.NoError(t, err)
}

func TestBuiltinSignatures(t *testing.T) {
	assert.NotNil(t, p0f.Linux311)
	assert.NotNil(t, p0f.Windows7)
	assert.NotNil(t, p0f.MacOS)
	assert.Equal(t, 64, p0f.Linux311.TTL)
	assert.Equal(t, 128, p0f.Windows7.TTL)
}
