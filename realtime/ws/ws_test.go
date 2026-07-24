// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ws

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/telemetry"
)

func tcpPipe(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer ln.Close()

	type acceptResult struct {
		conn net.Conn
		err  error
	}

	ch := make(chan acceptResult, 1)
	go func() {
		conn, err := ln.Accept()
		ch <- acceptResult{conn: conn, err: err}
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)

	res := <-ch
	require.NoError(t, res.err)

	return res.conn, client
}

func TestDialWebSocket_Basic(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		for {
			mt, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}

			if err := ws.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	tests := []struct {
		name       string
		useConfig  bool
		config     DialWebSocketConfig
		mods       []aoni.RequestModifier
		expectCode int
	}{
		{
			name:       "plain_upgrade",
			mods:       []aoni.RequestModifier{mod.WithHeader("Origin", "http://localhost")},
			expectCode: http.StatusSwitchingProtocols,
		},
		{
			name:      "with_custom_buffers_config",
			useConfig: true,
			config: DialWebSocketConfig{
				ReadBufferSize:  1024,
				WriteBufferSize: 1024,
			},
			expectCode: http.StatusSwitchingProtocols,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := aoni.NewClient(nil)

			var (
				conn net.Conn
				resp *http.Response
				err  error
			)

			if tt.useConfig {
				conn, resp, err = DialWebSocketWithConfig(t.Context(), client, wsURL, tt.config, tt.mods...)
			} else {
				conn, resp, err = DialWebSocket(t.Context(), client, wsURL, tt.mods...)
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.expectCode, resp.StatusCode)

			require.NotNil(t, conn)
			defer conn.Close()

			testMsg := []byte("hello ws")
			_, err = conn.Write(testMsg)
			require.NoError(t, err)

			buf := make([]byte, 100)
			n, err := conn.Read(buf)
			require.NoError(t, err)
			assert.Equal(t, string(testMsg), string(buf[:n]))
		})
	}
}

func TestDialWebSocket_CustomDialers(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		mt, msg, err := ws.ReadMessage()
		if err == nil {
			_ = ws.WriteMessage(mt, msg)
		}
	})

	tests := []struct {
		name           string
		isTLS          bool
		setupTransport func(t *testing.T, tr *http.Transport, called *bool)
	}{
		{
			name:  "plain_custom_dial_context",
			isTLS: false,
			setupTransport: func(t *testing.T, tr *http.Transport, called *bool) {
				tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
					*called = true
					return (&net.Dialer{}).DialContext(ctx, network, addr)
				}
			},
		},
		{
			name:  "tls_custom_dial_context_fallback",
			isTLS: false,
			setupTransport: func(t *testing.T, tr *http.Transport, called *bool) {
				tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
					*called = true
					return (&net.Dialer{}).DialContext(ctx, network, addr)
				}
			},
		},
		{
			name:  "tls_custom_dial_tls_context",
			isTLS: true,
			setupTransport: func(t *testing.T, tr *http.Transport, called *bool) {
				tr.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
					*called = true
					dialer := &tls.Dialer{
						Config: &tls.Config{InsecureSkipVerify: true},
					}

					return dialer.DialContext(ctx, network, addr)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var server *httptest.Server
			if tt.isTLS {
				server = httptest.NewTLSServer(handler)
			} else {
				server = httptest.NewServer(handler)
			}

			defer server.Close()

			scheme := "ws"
			if tt.isTLS {
				scheme = "wss"
			}

			wsURL := scheme + strings.TrimPrefix(server.URL, "http"+strings.TrimPrefix(scheme, "ws"))

			client := aoni.NewClient(nil)
			if tt.isTLS {
				client.Transport().TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			}

			dialCalled := false
			tt.setupTransport(t, client.Transport(), &dialCalled)

			conn, resp, err := DialWebSocket(t.Context(), client, wsURL)
			require.NoError(t, err)
			assert.NotNil(t, resp)
			assert.True(t, dialCalled)

			defer conn.Close()

			testMsg := []byte("custom dialer test")
			_, err = conn.Write(testMsg)
			require.NoError(t, err)

			buf := make([]byte, 100)
			n, err := conn.Read(buf)
			require.NoError(t, err)
			assert.Equal(t, string(testMsg), string(buf[:n]))
		})
	}
}

func TestDialWebSocket_WithTraceJA4(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		for {
			mt, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}

			_ = ws.WriteMessage(mt, msg)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := aoni.NewClient(nil)
	info := &telemetry.TraceInfo{}

	wsConn, _, err := DialWebSocket(t.Context(), client, wsURL, mod.WithTraceJA4(info))
	require.NoError(t, err)

	defer wsConn.Close()

	require.NotNil(t, info.JA4)
	assert.NotEmpty(t, info.JA4.JA4H)
}

func TestDialWebSocket_InvalidURL(t *testing.T) {
	t.Parallel()

	client := aoni.NewClient(nil)
	_, _, err := DialWebSocket(t.Context(), client, "http://invalid-scheme.com")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedWSScheme)
}

func TestDialWebSocket_WithFragmentation(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		mt, msg, err := ws.ReadMessage()
		if err == nil {
			_ = ws.WriteMessage(mt, msg)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := aoni.NewClient(nil)

	fragCfg := fragment.Config{
		ChunkSize: 2,
		MaxDelay:  1 * time.Millisecond,
	}

	wsConn, resp, err := DialWebSocket(t.Context(), client, wsURL, mod.WithFragmentation(fragCfg))
	require.NoError(t, err)
	assert.NotNil(t, resp)

	defer wsConn.Close()

	_, err = wsConn.Write([]byte("fragmentation"))
	require.NoError(t, err)

	buf := make([]byte, 100)
	n, err := wsConn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "fragmentation", string(buf[:n]))
}

func TestDialWebSocket_TLSFingerprint(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
	}))
	defer server.Close()

	client := aoni.NewClient(nil)
	client.Transport().TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	client = client.With(option.WithTLSFingerprint(aoni.BrowserChrome))

	wsURL := "wss" + strings.TrimPrefix(server.URL, "https")

	info := &telemetry.TraceInfo{}
	conn, resp, err := DialWebSocket(t.Context(), client, wsURL, mod.WithTraceJA4(info))
	require.NoError(t, err)
	assert.NotNil(t, conn)
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	defer conn.Close()

	assert.NotEmpty(t, info.JA4.JA4H)
}

func TestDialWebSocket_TLSH2HandshakeFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.EnableHTTP2 = true

	server.StartTLS()
	defer server.Close()

	wssURL := "wss" + strings.TrimPrefix(server.URL, "https")

	client := aoni.NewClient(nil, option.WithTLSFingerprint(aoni.BrowserChrome))
	client.Transport().TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	_, _, err := DialWebSocket(t.Context(), client, wssURL)
	assert.Error(t, err)
}

func TestWSGorillaConn_Full(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			mt, p, err := conn.ReadMessage()
			if err != nil {
				return
			}

			_ = conn.WriteMessage(mt, p)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	dialer := websocket.Dialer{}
	ws, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)

	gConn := wrapGorillaConn(ws)
	defer gConn.Close()

	msg := []byte("hello gorilla")
	n, err := gConn.Write(msg)
	require.NoError(t, err)
	assert.Equal(t, len(msg), n)

	buf := make([]byte, 100)
	n, err = gConn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello gorilla", string(buf[:n]))

	assert.NotNil(t, gConn.RawConn())
	assert.NotNil(t, gConn.LocalAddr())
	assert.NotNil(t, gConn.RemoteAddr())
	assert.NoError(t, gConn.SetDeadline(time.Now().Add(10*time.Second)))
	assert.NoError(t, gConn.SetReadDeadline(time.Now().Add(10*time.Second)))
	assert.NoError(t, gConn.SetWriteDeadline(time.Now().Add(10*time.Second)))
	assert.NotNil(t, gConn.CloseChan())

	require.NoError(t, gConn.Close())
	_, err = gConn.Read(buf)
	assert.Error(t, err)
}

func TestWSRawConn_RoundTrip(t *testing.T) {
	t.Parallel()

	server, client := tcpPipe(t)
	defer server.Close()

	raw := wrapRawConn(client, true)
	defer raw.Close()

	go func() {
		header := make([]byte, 2)
		_, _ = io.ReadFull(server, header)
		masked := header[1]&0x80 != 0
		length := uint64(header[1] & 0x7f)

		var mask [4]byte
		if masked {
			_, _ = io.ReadFull(server, mask[:])
		}

		payload := make([]byte, length)
		_, _ = io.ReadFull(server, payload)

		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}

		echoHeader := []byte{0x82, byte(length)}
		_, _ = server.Write(echoHeader)
		_, _ = server.Write(payload)
	}()

	_, err := raw.Write([]byte("hello"))
	require.NoError(t, err)

	buf := make([]byte, 1024)
	n, err := raw.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(buf[:n]))
}

func TestWSRawConn_FrameLengthsAndMasking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		size int
	}{
		{name: "frame_length_126", size: 500},
		{name: "frame_length_127", size: 70000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, c := tcpPipe(t)
			clientRaw := wrapRawConn(c, true)
			serverRaw := wrapRawConn(s, false)

			defer clientRaw.Close()
			defer serverRaw.Close()

			data := make([]byte, tt.size)
			for i := range data {
				data[i] = byte(i % 256)
			}

			errCh := make(chan error, 1)
			go func() {
				buf := make([]byte, tt.size+10000)

				n, err := io.ReadFull(serverRaw, buf[:tt.size])
				if err != nil {
					errCh <- err
					return
				}

				if !bytes.Equal(data, buf[:n]) {
					errCh <- errors.New("data mismatch")
					return
				}

				_, err = serverRaw.Write(data)
				errCh <- err
			}()

			_, err := clientRaw.Write(data)
			require.NoError(t, err)

			buf := make([]byte, tt.size+10000)
			n, err := io.ReadFull(clientRaw, buf[:tt.size])
			require.NoError(t, err)
			assert.Equal(t, len(data), n)
			assert.Equal(t, data, buf[:n])
			require.NoError(t, <-errCh)
		})
	}
}

func TestWSRawConn_FrameTooLarge(t *testing.T) {
	t.Parallel()

	s, c := tcpPipe(t)
	defer s.Close()
	defer c.Close()

	raw := wrapRawConn(c, true)
	defer raw.Close()

	go func() {
		header := []byte{0x82, 127}
		extended := make([]byte, 8)
		binary.BigEndian.PutUint64(extended, 20*1024*1024)
		_, _ = s.Write(append(header, extended...))
	}()

	buf := make([]byte, 100)
	_, err := raw.Read(buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "payload too large")
}

func TestWSRawConn_ControlFramesAndOpcodes(t *testing.T) {
	t.Parallel()

	t.Run("close_frame_returns_eof", func(t *testing.T) {
		t.Parallel()
		s, c := tcpPipe(t)

		raw := wrapRawConn(c, true)
		defer raw.Close()
		defer s.Close()

		go func() { _, _ = s.Write([]byte{0x88, 0}) }()

		_, err := raw.Read(make([]byte, 10))
		assert.Equal(t, io.EOF, err)
	})

	t.Run("ping_frame_replies_pong", func(t *testing.T) {
		t.Parallel()
		s, c := tcpPipe(t)

		raw := wrapRawConn(c, true)
		defer raw.Close()
		defer s.Close()

		errCh := make(chan error, 1)
		go func() {
			if _, err := s.Write([]byte{0x89, 4, 'p', 'i', 'n', 'g'}); err != nil {
				errCh <- err
				return
			}

			header := make([]byte, 2)
			if _, err := io.ReadFull(s, header); err != nil {
				errCh <- err
				return
			}

			if (header[0] & 0x0f) != 10 {
				errCh <- fmt.Errorf("expected pong, got %d", header[0]&0x0f)
				return
			}

			masked := (header[1] & 0x80) != 0

			var mask [4]byte
			if masked {
				if _, err := io.ReadFull(s, mask[:]); err != nil {
					errCh <- err
					return
				}
			}

			payload := make([]byte, 4)
			if _, err := io.ReadFull(s, payload); err != nil {
				errCh <- err
				return
			}

			if masked {
				for i := range payload {
					payload[i] ^= mask[i%4]
				}
			}

			if string(payload) != "ping" {
				errCh <- fmt.Errorf("expected 'ping', got %q", string(payload))
				return
			}

			_, _ = s.Write([]byte{0x81, 2, 'o', 'k'})

			errCh <- nil
		}()

		buf := make([]byte, 10)
		n, err := raw.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, "ok", string(buf[:n]))
		require.NoError(t, <-errCh)
	})

	t.Run("max_empty_reads_returns_eof", func(t *testing.T) {
		t.Parallel()
		s, c := tcpPipe(t)

		raw := wrapRawConn(c, true)
		defer raw.Close()
		defer s.Close()

		go func() {
			for range 101 {
				_, _ = s.Write([]byte{0x8a, 0})
			}
		}()

		_, err := raw.Read(make([]byte, 10))
		assert.Equal(t, io.EOF, err)
	})
}

func TestWSRawConn_WriteTextVsBinary(t *testing.T) {
	t.Parallel()

	s, c := tcpPipe(t)
	defer s.Close()
	defer c.Close()

	raw := wrapRawConn(c, true)
	defer raw.Close()

	errCh := make(chan byte, 2)
	go func() {
		for range 2 {
			header := make([]byte, 2)

			_, _ = io.ReadFull(s, header)
			errCh <- header[0] & 0x0f

			masked := header[1]&0x80 != 0
			length := header[1] & 0x7f

			if masked {
				_, _ = io.ReadFull(s, make([]byte, 4))
			}

			_, _ = io.ReadFull(s, make([]byte, length))
		}
	}()

	_, err := raw.Write([]byte("hello utf8"))
	require.NoError(t, err)

	_, err = raw.Write([]byte{0xff, 0xfe, 0xfd})
	require.NoError(t, err)

	assert.Equal(t, byte(1), <-errCh) // wsFrameText
	assert.Equal(t, byte(2), <-errCh) // wsFrameBinary
}

func TestWSRawConn_Close(t *testing.T) {
	t.Parallel()

	server, client := tcpPipe(t)
	defer server.Close()

	raw := wrapRawConn(client, true)

	closed := raw.CloseChan()
	select {
	case <-closed:
		t.Fatal("should not be closed yet")
	default:
	}

	require.NoError(t, raw.Close())

	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("CloseChan should be closed after Close()")
	}
}

func TestWSRawConn_Timeout(t *testing.T) {
	t.Parallel()

	server, client := tcpPipe(t)
	defer server.Close()
	defer client.Close()

	raw := wrapRawConn(client, true)
	defer raw.Close()

	err := raw.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	require.NoError(t, err)

	_, err = raw.Read(make([]byte, 1024))
	assert.Error(t, err)
}

func TestWSRawConn_NetConnMethods(t *testing.T) {
	t.Parallel()

	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	raw := wrapRawConn(c, true)
	defer raw.Close()

	assert.NotNil(t, raw.LocalAddr())
	assert.NotNil(t, raw.RemoteAddr())
	assert.NoError(t, raw.SetDeadline(time.Now().Add(time.Second)))
	assert.NoError(t, raw.SetReadDeadline(time.Now().Add(time.Second)))
	assert.NoError(t, raw.SetWriteDeadline(time.Now().Add(time.Second)))
}

func TestWSH2Conn_AllFrames(t *testing.T) {
	t.Parallel()

	server, client := tcpPipe(t)
	defer server.Close()
	defer client.Close()

	framerServer := http2.NewFramer(server, server)
	framerClient := http2.NewFramer(client, client)

	h2Conn := &wsH2Conn{
		base:     client,
		framer:   framerClient,
		streamID: 1,
		closed:   make(chan struct{}),
	}
	defer h2Conn.Close()

	readExpectedFrame := func(framer *http2.Framer) (http2.Frame, error) {
		for {
			f, err := framer.ReadFrame()
			if err != nil {
				return nil, err
			}

			if _, ok := f.(*http2.WindowUpdateFrame); ok {
				continue
			}

			return f, nil
		}
	}

	done1 := make(chan struct{})
	go func() {
		defer close(done1)

		frame, err := readExpectedFrame(framerServer)
		if err != nil {
			return
		}

		df, ok := frame.(*http2.DataFrame)
		if !ok {
			return
		}

		_ = framerServer.WriteData(df.StreamID, false, []byte("response"))
	}()

	n, err := h2Conn.Write([]byte("request"))
	require.NoError(t, err)
	assert.Equal(t, 7, n)

	buf := make([]byte, 100)
	n, err = h2Conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "response", string(buf[:n]))
	<-done1

	largeData := make([]byte, 20000)

	done2 := make(chan struct{})
	go func() {
		defer close(done2)

		f1, _ := readExpectedFrame(framerServer)
		assert.Equal(t, 16384, len(f1.(*http2.DataFrame).Data()))

		f2, _ := readExpectedFrame(framerServer)
		assert.Equal(t, 3616, len(f2.(*http2.DataFrame).Data()))
	}()

	n, err = h2Conn.Write(largeData)
	require.NoError(t, err)
	assert.Equal(t, 20000, n)
	<-done2

	errCh := make(chan error, 1)
	go func() {
		err := framerServer.WriteSettings(http2.Setting{ID: http2.SettingInitialWindowSize, Val: 1000})
		if err != nil {
			errCh <- err
			return
		}

		f, _ := readExpectedFrame(framerServer)

		sf, ok := f.(*http2.SettingsFrame)
		if !ok || !sf.IsAck() {
			errCh <- errors.New("expected settings ack")
			return
		}

		_ = framerServer.WritePing(false, [8]byte{1, 2, 3, 4, 5, 6, 7, 8})
		f, _ = readExpectedFrame(framerServer)

		pf, ok := f.(*http2.PingFrame)
		if !ok || !pf.IsAck() {
			errCh <- errors.New("expected ping ack")
			return
		}

		_ = framerServer.WriteRSTStream(1, http2.ErrCodeCancel)

		errCh <- nil
	}()

	_, err = h2Conn.Read(buf)
	assert.Equal(t, io.EOF, err)
	require.NoError(t, <-errCh)
}

func TestDialH2ExtendedConnect_Success(t *testing.T) {
	t.Parallel()

	server, client := tcpPipe(t)
	defer server.Close()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		preface := make([]byte, len(http2.ClientPreface))
		_, _ = io.ReadFull(server, preface)

		framer := http2.NewFramer(server, server)
		frame, _ := framer.ReadFrame()

		sf, ok := frame.(*http2.SettingsFrame)
		if !ok {
			errCh <- errors.New("expected settings")
			return
		}

		enableConnect := false
		_ = sf.ForeachSetting(func(s http2.Setting) error {
			if s.ID == http2.SettingEnableConnectProtocol && s.Val == 1 {
				enableConnect = true
			}

			return nil
		})

		if !enableConnect {
			errCh <- errors.New("client didn't enable connect")
			return
		}

		_ = framer.WriteSettings(http2.Setting{ID: http2.SettingEnableConnectProtocol, Val: 1})
		frame, _ = framer.ReadFrame()

		sfAck, ok := frame.(*http2.SettingsFrame)
		if !ok || !sfAck.IsAck() {
			errCh <- errors.New("expected settings ack")
			return
		}

		_ = framer.WriteSettingsAck()
		frame, _ = framer.ReadFrame()
		hf, _ := frame.(*http2.HeadersFrame)

		decoder := hpack.NewDecoder(4096, nil)
		fields, _ := decoder.DecodeFull(hf.HeaderBlockFragment())

		hasProtocolWS := false
		for _, f := range fields {
			if f.Name == ":protocol" && f.Value == "websocket" {
				hasProtocolWS = true
			}
		}

		if !hasProtocolWS {
			errCh <- errors.New("missing :protocol header")
			return
		}

		var buf bytes.Buffer

		encoder := hpack.NewEncoder(&buf)
		_ = encoder.WriteField(hpack.HeaderField{Name: ":status", Value: "200"})

		err := framer.WriteHeaders(http2.HeadersFrameParam{
			StreamID:      hf.StreamID,
			BlockFragment: buf.Bytes(),
			EndHeaders:    true,
		})
		errCh <- err
	}()

	ctx := context.Background()
	conn, err := dialH2ExtendedConnect(ctx, client, "wss://example.com/ws", "example.com")
	require.NoError(t, err)
	assert.NotNil(t, conn)
	require.NoError(t, <-errCh)
	conn.Close()
}

func TestDialH2ExtendedConnect_Failures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setupMock func(server net.Conn, framer *http2.Framer)
		expectErr error
	}{
		{
			name: "missing_connect_protocol_support",
			setupMock: func(server net.Conn, framer *http2.Framer) {
				_, _ = framer.ReadFrame()
				_ = framer.WriteSettings()
			},
			expectErr: ErrH2ConnectNotSupported,
		},
		{
			name: "forbidden_status_403",
			setupMock: func(server net.Conn, framer *http2.Framer) {
				_, _ = framer.ReadFrame()
				_ = framer.WriteSettings(http2.Setting{ID: http2.SettingEnableConnectProtocol, Val: 1})
				_, _ = framer.ReadFrame()
				_ = framer.WriteSettingsAck()
				hf, _ := framer.ReadFrame()

				var buf bytes.Buffer

				encoder := hpack.NewEncoder(&buf)
				_ = encoder.WriteField(hpack.HeaderField{Name: ":status", Value: "403"})
				_ = framer.WriteHeaders(http2.HeadersFrameParam{
					StreamID:      hf.(*http2.HeadersFrame).StreamID,
					BlockFragment: buf.Bytes(),
					EndHeaders:    true,
				})
			},
			expectErr: ErrH2ConnectFailed,
		},
		{
			name: "unexpected_frame_during_preface",
			setupMock: func(server net.Conn, framer *http2.Framer) {
				_, _ = framer.ReadFrame()
				_ = framer.WriteHeaders(http2.HeadersFrameParam{
					StreamID:      1,
					BlockFragment: []byte{},
					EndHeaders:    true,
				})
			},
			expectErr: ErrH2UnexpectedFrame,
		},
		{
			name: "stream_reset_by_server",
			setupMock: func(server net.Conn, framer *http2.Framer) {
				_, _ = framer.ReadFrame()
				_ = framer.WriteSettings(http2.Setting{ID: http2.SettingEnableConnectProtocol, Val: 1})
				_, _ = framer.ReadFrame()
				_ = framer.WriteSettingsAck()
				hf, _ := framer.ReadFrame()
				_ = framer.WriteRSTStream(hf.(*http2.HeadersFrame).StreamID, http2.ErrCodeCancel)
			},
			expectErr: ErrH2StreamClosed,
		},
		{
			name: "goaway_frame_received",
			setupMock: func(server net.Conn, framer *http2.Framer) {
				_, _ = framer.ReadFrame()
				_ = framer.WriteSettings(http2.Setting{ID: http2.SettingEnableConnectProtocol, Val: 1})
				_, _ = framer.ReadFrame()
				_ = framer.WriteSettingsAck()
				_, _ = framer.ReadFrame()
				_ = framer.WriteGoAway(1, http2.ErrCodeNo, nil)
			},
			expectErr: ErrH2GoAway,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, client := tcpPipe(t)
			defer server.Close()
			defer client.Close()

			errCh := make(chan error, 1)
			go func() {
				preface := make([]byte, len(http2.ClientPreface))
				if _, err := io.ReadFull(server, preface); err != nil {
					errCh <- err
					return
				}

				framer := http2.NewFramer(server, server)
				tt.setupMock(server, framer)

				errCh <- nil
			}()

			_, err := dialH2ExtendedConnect(t.Context(), client, "wss://example.com/ws", "example.com")
			assert.ErrorIs(t, err, tt.expectErr)
			require.NoError(t, <-errCh)
		})
	}
}

func TestParseWSURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url    string
		scheme string
		host   string
		port   string
		path   string
		err    bool
	}{
		{"wss://example.com/ws", "wss", "example.com", "443", "/ws", false},
		{"ws://localhost:8080/chat", "ws", "localhost", "8080", "/chat", false},
		{"wss://api.example.com/", "wss", "api.example.com", "443", "/", false},
		{"wss://example.com", "wss", "example.com", "443", "/", false},
		{"http://example.com/ws", "", "", "", "", true},
		{"ftp://example.com", "", "", "", "", true},
	}

	for _, tt := range tests {
		u, err := parseWSURL(tt.url)
		if tt.err {
			assert.Error(t, err, tt.url)
			continue
		}

		require.NoError(t, err, tt.url)
		assert.Equal(t, tt.scheme, u.scheme, tt.url)
		assert.Equal(t, tt.host, u.host, tt.url)
		assert.Equal(t, tt.port, u.port, tt.url)
		assert.Equal(t, tt.path, u.Path, tt.url)
	}
}

func TestWSConn_ImplementsNetConn(t *testing.T) {
	t.Parallel()

	var (
		_ Conn     = (*wsGorillaConn)(nil)
		_ Conn     = (*wsRawConn)(nil)
		_ net.Conn = (*wsH2Conn)(nil)
	)
}

func TestH2Preface_ContextCancellation(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			go func() {
				buf := make([]byte, 4096)
				for {
					if _, err := conn.Read(buf); err != nil {
						return
					}
				}
			}()
		}
	}()

	tests := []struct {
		name     string
		setupCtx func(parent context.Context) (context.Context, context.CancelFunc)
	}{
		{
			name: "context_deadline_timeout",
			setupCtx: func(parent context.Context) (context.Context, context.CancelFunc) {
				return context.WithTimeout(parent, 200*time.Millisecond)
			},
		},
		{
			name: "context_manual_cancellation",
			setupCtx: func(parent context.Context) (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithTimeout(parent, 200*time.Millisecond)
				go func() {
					time.Sleep(50 * time.Millisecond)
					cancel()
				}()

				return ctx, cancel
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := tt.setupCtx(t.Context())
			defer cancel()

			conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
			require.NoError(t, err)

			defer conn.Close()

			start := time.Now()
			_, err = dialH2ExtendedConnect(ctx, conn, "ws://example.com/ws", "example.com")
			elapsed := time.Since(start)

			assert.Error(t, err)
			assert.Less(t, elapsed, 2*time.Second)
		})
	}
}

func TestMaxWebSocketFrameSize(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 16*1024*1024, maxWebSocketFrameSize)
}
