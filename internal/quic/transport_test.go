// Copyright (c) 2016 the quic-go authors. All rights reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
	"github.com/lemon4ksan/aoni/internal/quic/internal/qerr"
	"github.com/lemon4ksan/aoni/internal/quic/internal/utils"
	"github.com/lemon4ksan/aoni/internal/quic/internal/wire"
)

type mockPacketConn struct {
	localAddr net.Addr
	readErrs  chan error
}

func (c *mockPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	err, ok := <-c.readErrs
	if !ok {
		return 0, nil, net.ErrClosed
	}

	return 0, nil, err
}

func (c *mockPacketConn) WriteTo(p []byte, addr net.Addr) (n int, err error) { panic("implement me") }
func (c *mockPacketConn) LocalAddr() net.Addr                                { return c.localAddr }
func (c *mockPacketConn) Close() error                                       { close(c.readErrs); return nil }
func (c *mockPacketConn) SetDeadline(t time.Time) error                      { return nil }
func (c *mockPacketConn) SetReadDeadline(t time.Time) error                  { return nil }
func (c *mockPacketConn) SetWriteDeadline(t time.Time) error                 { return nil }

type mockPacketHandler struct {
	packets     chan<- receivedPacket
	destruction chan<- error
}

func (h *mockPacketHandler) handlePacket(p receivedPacket) {
	h.packets <- p
}

func (h *mockPacketHandler) destroy(err error) {
	if h.destruction != nil {
		h.destruction <- err
	}
}

func (h *mockPacketHandler) closeWithTransportError(code qerr.TransportErrorCode) {}

func TestTransportPacketHandling(t *testing.T) {
	tr := &Transport{Conn: newUDPConnLocalhost(t)}

	tr.init(true)
	defer tr.Close()

	connID1 := protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	connID2 := protocol.ParseConnectionID([]byte{8, 7, 6, 5, 4, 3, 2, 1})

	connChan1 := make(chan receivedPacket, 1)
	conn1 := &mockPacketHandler{packets: connChan1}
	(*packetHandlerMap)(tr).Add(connID1, conn1)

	connChan2 := make(chan receivedPacket, 1)
	conn2 := &mockPacketHandler{packets: connChan2}
	(*packetHandlerMap)(tr).Add(connID2, conn2)

	conn := newUDPConnLocalhost(t)
	_, err := conn.WriteTo(getPacket(t, connID1), tr.Conn.LocalAddr())
	require.NoError(t, err)
	_, err = conn.WriteTo(getPacket(t, connID2), tr.Conn.LocalAddr())
	require.NoError(t, err)

	select {
	case p := <-connChan1:
		require.Equal(t, conn.LocalAddr(), p.remoteAddr)
		connID, err := wire.ParseConnectionID(p.data, 0)
		require.NoError(t, err)
		require.Equal(t, connID1, connID)
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	select {
	case p := <-connChan2:
		require.Equal(t, conn.LocalAddr(), p.remoteAddr)
		connID, err := wire.ParseConnectionID(p.data, 0)
		require.NoError(t, err)
		require.Equal(t, connID2, connID)
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestTransportAndDialConcurrentClose(t *testing.T) {
	server := newUDPConnLocalhost(t)

	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	// close transport and dial concurrently
	errChan := make(chan error, 1)
	go func() { errChan <- tr.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := tr.Dial(ctx, server.LocalAddr(), &tls.Config{}, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTransportClosed)
	require.NotErrorIs(t, err, context.DeadlineExceeded)

	select {
	case <-errChan:
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestTransportErrFromConn(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING", "true")

	synctest.Test(t, func(t *testing.T) {
		readErrChan := make(chan error, 2)

		tr := Transport{
			Conn: &mockPacketConn{
				readErrs:  readErrChan,
				localAddr: &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 1234},
			},
		}
		defer tr.Close()

		tr.init(true)

		errChan := make(chan error, 1)
		ph := &mockPacketHandler{destruction: errChan}
		(*packetHandlerMap)(&tr).Add(protocol.ParseConnectionID([]byte{1, 2, 3, 4}), ph)

		// temporary errors don't lead to a shutdown...
		var tempErr deadlineError
		require.True(t, tempErr.Temporary())

		readErrChan <- tempErr
		// don't expect any calls to phm.Close
		synctest.Wait()

		// ...but non-temporary errors do
		readErrChan <- errors.New("read failed")

		synctest.Wait()

		select {
		case err := <-errChan:
			require.ErrorIs(t, err, ErrTransportClosed)
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		_, err := tr.Dial(ctx, &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 1234}, &tls.Config{}, nil)
		require.ErrorIs(t, err, ErrTransportClosed)
	})
}

func TestTransportStatelessResetReceiving(t *testing.T) {
	tr := &Transport{
		Conn:               newUDPConnLocalhost(t),
		ConnectionIDLength: 4,
	}

	tr.init(true)
	defer tr.Close()

	connID := protocol.ParseConnectionID([]byte{9, 10, 11, 12})
	// now send a packet with a connection ID that doesn't exist
	token := protocol.StatelessResetToken{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	b, err := wire.AppendShortHeader(nil, connID, 1337, 2, protocol.KeyPhaseOne)
	require.NoError(t, err)

	b = append(b, token[:]...)

	destroyChan := make(chan error, 1)
	conn1 := &mockPacketHandler{destruction: destroyChan}
	(*packetHandlerMap)(tr).AddResetToken(token, conn1)

	conn := newUDPConnLocalhost(t)
	_, err = conn.WriteTo(b, tr.Conn.LocalAddr())
	require.NoError(t, err)

	select {
	case err := <-destroyChan:
		require.ErrorIs(t, err, &qerr.StatelessResetError{})
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

type faultySyscallConn struct{ net.PacketConn }

func (c *faultySyscallConn) SyscallConn() (syscall.RawConn, error) { return nil, assert.AnError }

func TestTransportFaultySyscallConn(t *testing.T) {
	syscallconn := &faultySyscallConn{PacketConn: newUDPConnLocalhost(t)}

	tr := &Transport{Conn: syscallconn}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tr.Dial(ctx, &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 1234}, &tls.Config{}, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, assert.AnError)
}

func TestTransportSetTLSConfigServerName(t *testing.T) {
	for _, tt := range []struct {
		name     string
		expected string
		conf     *tls.Config
		host     string
	}{
		{
			name:     "uses the value from the config",
			expected: "foo.bar",
			conf:     &tls.Config{ServerName: "foo.bar"},
			host:     "baz.foo",
		},
		{
			name:     "uses the hostname",
			expected: "golang.org",
			conf:     &tls.Config{},
			host:     "golang.org",
		},
		{
			name:     "removes the port from the hostname",
			expected: "golang.org",
			conf:     &tls.Config{},
			host:     "golang.org:1234",
		},
		{
			name:     "uses the IP",
			expected: "1.3.5.7",
			conf:     &tls.Config{},
			host:     "",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setTLSConfigServerName(tt.conf, &net.UDPAddr{IP: net.IPv4(1, 3, 5, 7), Port: 1234}, tt.host)
			require.Equal(t, tt.expected, tt.conf.ServerName)
		})
	}
}

func TestTransportDialingVersionNegotiation(t *testing.T) {
	originalClientConnConstructor := newClientConnection
	t.Cleanup(func() { newClientConnection = originalClientConnConstructor })

	conn := &connTestHooks{
		handshakeComplete: func() <-chan struct{} { return make(chan struct{}) },
		run:               func() error { return &errCloseForRecreating{nextPacketNumber: 109, nextVersion: 789} },
	}
	conn2 := &connTestHooks{
		handshakeComplete: func() <-chan struct{} { return make(chan struct{}) },
		run:               func() error { return assert.AnError },
	}

	type connParams struct {
		pn                   protocol.PacketNumber
		hasNegotiatedVersion bool
		version              protocol.Version
	}

	connChan := make(chan connParams, 2)

	var counter int

	newClientConnection = func(
		_ context.Context,
		_ sendConn,
		_ connRunner,
		_ protocol.ConnectionID,
		_ protocol.ConnectionID,
		_ ConnectionIDGenerator,
		_ *Config,
		_ *tls.Config,
		pn protocol.PacketNumber,
		_ bool,
		hasNegotiatedVersion bool,
		_ utils.Logger,
		v protocol.Version,
	) *wrappedConn {
		connChan <- connParams{pn: pn, hasNegotiatedVersion: hasNegotiatedVersion, version: v}

		if counter == 0 {
			counter++
			return &wrappedConn{testHooks: conn}
		}

		return &wrappedConn{testHooks: conn2}
	}

	tr := &Transport{Conn: newUDPConnLocalhost(t)}

	tr.init(true)
	defer tr.Close()

	_, err := tr.Dial(context.Background(), nil, &tls.Config{}, nil)
	require.ErrorIs(t, err, assert.AnError)

	select {
	case params := <-connChan:
		require.Zero(t, params.pn)
		require.False(t, params.hasNegotiatedVersion)
		require.Equal(t, protocol.Version1, params.version)
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	select {
	case params := <-connChan:
		require.Equal(t, protocol.PacketNumber(109), params.pn)
		require.True(t, params.hasNegotiatedVersion)
		require.Equal(t, protocol.Version(789), params.version)
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}
