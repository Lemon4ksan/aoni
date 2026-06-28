// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
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
)

// tcpPipe creates two connected TCP sockets, bypassing limitations and deadlocks
// of synchronous unbuffered net.Pipe() during large writes.
func tcpPipe(t *testing.T) (net.Conn, net.Conn) {
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

func TestWSGorillaConn_Full(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

			err = conn.WriteMessage(mt, p)
			if err != nil {
				return
			}
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	dialer := websocket.Dialer{}
	ws, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)

	gConn := wrapGorillaConn(ws)
	defer gConn.Close()

	// Test basic Write & Read
	msg := []byte("hello gorilla")
	n, err := gConn.Write(msg)
	require.NoError(t, err)
	assert.Equal(t, len(msg), n)

	buf := make([]byte, 100)
	n, err = gConn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello gorilla", string(buf[:n]))

	// Test interface methods
	assert.NotNil(t, gConn.RawConn())
	assert.NotNil(t, gConn.LocalAddr())
	assert.NotNil(t, gConn.RemoteAddr())
	assert.NoError(t, gConn.SetDeadline(time.Now().Add(10*time.Second)))
	assert.NoError(t, gConn.SetReadDeadline(time.Now().Add(10*time.Second)))
	assert.NoError(t, gConn.SetWriteDeadline(time.Now().Add(10*time.Second)))
	assert.NotNil(t, gConn.CloseChan())

	// Test Close
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

	// 1. Length 126 (e.g., 500 bytes)
	{
		s, c := tcpPipe(t)
		clientRaw := wrapRawConn(c, true)
		serverRaw := wrapRawConn(s, false)

		data := make([]byte, 500)
		for i := range data {
			data[i] = byte(i % 256)
		}

		errCh := make(chan error, 1)
		go func() {
			buf := make([]byte, 1000)

			n, err := serverRaw.Read(buf)
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

		buf := make([]byte, 1000)
		n, err := clientRaw.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, len(data), n)
		assert.Equal(t, data, buf[:n])
		require.NoError(t, <-errCh)

		clientRaw.Close()
		serverRaw.Close()
	}

	// 2. Length 127 (e.g., 70000 bytes)
	{
		s, c := tcpPipe(t)
		clientRaw := wrapRawConn(c, true)
		serverRaw := wrapRawConn(s, false)

		data := make([]byte, 70000)
		for i := range data {
			data[i] = byte(i % 256)
		}

		errCh := make(chan error, 1)
		go func() {
			buf := make([]byte, 80000)

			n, err := io.ReadFull(serverRaw, buf[:70000])
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

		buf := make([]byte, 80000)
		n, err := io.ReadFull(clientRaw, buf[:70000])
		require.NoError(t, err)
		assert.Equal(t, len(data), n)
		assert.Equal(t, data, buf[:n])
		require.NoError(t, <-errCh)

		clientRaw.Close()
		serverRaw.Close()
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

	// 1. Close frame -> EOF
	{
		s, c := tcpPipe(t)
		raw := wrapRawConn(c, true)

		go func() {
			_, _ = s.Write([]byte{0x88, 0})
		}()

		_, err := raw.Read(make([]byte, 10))
		assert.Equal(t, io.EOF, err)
		raw.Close()
		s.Close()
	}

	// 2. Ping frame -> Auto Pong response
	{
		s, c := tcpPipe(t)
		raw := wrapRawConn(c, true)

		errCh := make(chan error, 1)
		go func() {
			_, err := s.Write([]byte{0x89, 4, 'p', 'i', 'n', 'g'})
			if err != nil {
				errCh <- err
				return
			}

			header := make([]byte, 2)

			_, err = io.ReadFull(s, header)
			if err != nil {
				errCh <- err
				return
			}

			opcode := header[0] & 0x0f
			if opcode != 10 {
				errCh <- fmt.Errorf("expected pong (10), got %d", opcode)
				return
			}

			masked := header[1]&0x80 != 0

			length := header[1] & 0x7f
			if length != 4 {
				errCh <- fmt.Errorf("expected length 4, got %d", length)
				return
			}

			var mask [4]byte
			if masked {
				_, _ = io.ReadFull(s, mask[:])
			}

			payload := make([]byte, 4)

			_, _ = io.ReadFull(s, payload)
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
		raw.Close()
		s.Close()
	}

	// 3. Pong frame (ignored) + Continuation frame
	{
		s, c := tcpPipe(t)
		raw := wrapRawConn(c, true)

		go func() {
			_, _ = s.Write([]byte{0x8a, 0})
			_, _ = s.Write([]byte{0x80, 2, 'c', 'o'})
		}()

		buf := make([]byte, 10)
		n, err := raw.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, "co", string(buf[:n]))
		raw.Close()
		s.Close()
	}

	// 4. Max consecutive empty reads -> EOF
	{
		s, c := tcpPipe(t)
		raw := wrapRawConn(c, true)

		go func() {
			for range 101 {
				_, _ = s.Write([]byte{0x8a, 0})
			}
		}()

		_, err := raw.Read(make([]byte, 10))
		assert.Equal(t, io.EOF, err)
		raw.Close()
		s.Close()
	}
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

			opcode := header[0] & 0x0f
			errCh <- opcode

			masked := header[1]&0x80 != 0
			length := header[1] & 0x7f

			if masked {
				mask := make([]byte, 4)
				_, _ = io.ReadFull(s, mask)
			}

			payload := make([]byte, length)
			_, _ = io.ReadFull(s, payload)
		}
	}()

	_, err := raw.Write([]byte("hello utf8"))
	require.NoError(t, err)

	_, err = raw.Write([]byte{0xff, 0xfe, 0xfd})
	require.NoError(t, err)

	op1 := <-errCh
	op2 := <-errCh

	assert.Equal(t, byte(1), op1) // wsFrameText
	assert.Equal(t, byte(2), op2) // wsFrameBinary
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

	require.NoError(t, raw.Close())
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

	assert.NotNil(t, h2Conn.LocalAddr())
	assert.NotNil(t, h2Conn.RemoteAddr())
	assert.NoError(t, h2Conn.SetDeadline(time.Now().Add(time.Second)))
	assert.NoError(t, h2Conn.SetReadDeadline(time.Now().Add(time.Second)))
	assert.NoError(t, h2Conn.SetWriteDeadline(time.Now().Add(time.Second)))

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

		f1, err := readExpectedFrame(framerServer)
		if err != nil {
			return
		}

		df1, ok := f1.(*http2.DataFrame)
		if !ok {
			return
		}

		assert.Equal(t, 16384, len(df1.Data()))

		f2, err := readExpectedFrame(framerServer)
		if err != nil {
			return
		}

		df2, ok := f2.(*http2.DataFrame)
		if !ok {
			return
		}

		assert.Equal(t, 3616, len(df2.Data()))
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

		f, err := readExpectedFrame(framerServer)
		if err != nil {
			errCh <- err
			return
		}

		sf, ok := f.(*http2.SettingsFrame)
		if !ok || !sf.IsAck() {
			errCh <- errors.New("expected settings ack")
			return
		}

		err = framerServer.WritePing(false, [8]byte{1, 2, 3, 4, 5, 6, 7, 8})
		if err != nil {
			errCh <- err
			return
		}

		f, err = readExpectedFrame(framerServer)
		if err != nil {
			errCh <- err
			return
		}

		pf, ok := f.(*http2.PingFrame)
		if !ok || !pf.IsAck() {
			errCh <- errors.New("expected ping ack")
			return
		}

		err = framerServer.WriteRSTStream(1, http2.ErrCodeCancel)
		if err != nil {
			errCh <- err
			return
		}

		errCh <- nil
	}()

	_, err = h2Conn.Read(buf)
	assert.Equal(t, io.EOF, err)
	require.NoError(t, <-errCh)

	err = h2Conn.Close()
	require.NoError(t, err)

	server2, client2 := tcpPipe(t)
	defer server2.Close()
	defer client2.Close()

	framerServer2 := http2.NewFramer(server2, server2)
	framerClient2 := http2.NewFramer(client2, client2)

	h2Conn2 := &wsH2Conn{
		base:     client2,
		framer:   framerClient2,
		streamID: 2,
		closed:   make(chan struct{}),
	}
	defer h2Conn2.Close()

	err = framerServer2.WriteGoAway(2, http2.ErrCodeNo, nil)
	require.NoError(t, err)

	_, err = h2Conn2.Read(buf)
	assert.Equal(t, io.EOF, err)
}

func TestDialH2ExtendedConnect_Success(t *testing.T) {
	t.Parallel()

	server, client := tcpPipe(t)
	defer server.Close()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		preface := make([]byte, len(http2.ClientPreface))

		_, err := io.ReadFull(server, preface)
		if err != nil {
			errCh <- err
			return
		}

		if string(preface) != http2.ClientPreface {
			errCh <- errors.New("bad preface")
			return
		}

		framer := http2.NewFramer(server, server)

		frame, err := framer.ReadFrame()
		if err != nil {
			errCh <- err
			return
		}

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

		err = framer.WriteSettings(
			http2.Setting{ID: http2.SettingEnableConnectProtocol, Val: 1},
		)
		if err != nil {
			errCh <- err
			return
		}

		frame, err = framer.ReadFrame()
		if err != nil {
			errCh <- err
			return
		}

		sfAck, ok := frame.(*http2.SettingsFrame)
		if !ok || !sfAck.IsAck() {
			errCh <- errors.New("expected settings ack")
			return
		}

		err = framer.WriteSettingsAck()
		if err != nil {
			errCh <- err
			return
		}

		frame, err = framer.ReadFrame()
		if err != nil {
			errCh <- err
			return
		}

		hf, ok := frame.(*http2.HeadersFrame)
		if !ok {
			errCh <- errors.New("expected headers")
			return
		}

		decoder := hpack.NewDecoder(4096, nil)

		fields, err := decoder.DecodeFull(hf.HeaderBlockFragment())
		if err != nil {
			errCh <- err
			return
		}

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

		err = framer.WriteHeaders(http2.HeadersFrameParam{
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

	// 1. Server settings missing Connect Protocol support
	{
		server, client := tcpPipe(t)

		errCh := make(chan error, 1)
		go func() {
			preface := make([]byte, len(http2.ClientPreface))
			_, _ = io.ReadFull(server, preface)
			framer := http2.NewFramer(server, server)
			_, _ = framer.ReadFrame()
			_ = framer.WriteSettings()

			errCh <- nil
		}()

		_, err := dialH2ExtendedConnect(context.Background(), client, "wss://example.com/ws", "example.com")
		assert.Equal(t, errH2ConnectNotSupported, err)
		client.Close()
		server.Close()
		<-errCh
	}

	// 2. Server responds with 403 Forbidden status
	{
		server, client := tcpPipe(t)

		errCh := make(chan error, 1)
		go func() {
			preface := make([]byte, len(http2.ClientPreface))
			_, _ = io.ReadFull(server, preface)
			framer := http2.NewFramer(server, server)
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

			errCh <- nil
		}()

		_, err := dialH2ExtendedConnect(context.Background(), client, "wss://example.com/ws", "example.com")
		assert.Equal(t, errH2ConnectFailed, err)
		client.Close()
		server.Close()
		<-errCh
	}

	// 3. Server responds with an unexpected frame type instead of settings during preface
	{
		server, client := tcpPipe(t)

		errCh := make(chan error, 1)
		go func() {
			preface := make([]byte, len(http2.ClientPreface))
			_, _ = io.ReadFull(server, preface)
			framer := http2.NewFramer(server, server)
			_, _ = framer.ReadFrame()
			_ = framer.WriteHeaders(http2.HeadersFrameParam{
				StreamID:      1,
				BlockFragment: []byte{},
				EndHeaders:    true,
			})

			errCh <- nil
		}()

		_, err := dialH2ExtendedConnect(context.Background(), client, "wss://example.com/ws", "example.com")
		assert.Equal(t, errH2UnexpectedFrame, err)
		client.Close()
		server.Close()
		<-errCh
	}

	// 4. Server RSTs the stream
	{
		server, client := tcpPipe(t)

		errCh := make(chan error, 1)
		go func() {
			preface := make([]byte, len(http2.ClientPreface))
			_, _ = io.ReadFull(server, preface)
			framer := http2.NewFramer(server, server)
			_, _ = framer.ReadFrame()
			_ = framer.WriteSettings(http2.Setting{ID: http2.SettingEnableConnectProtocol, Val: 1})
			_, _ = framer.ReadFrame()
			_ = framer.WriteSettingsAck()
			hf, _ := framer.ReadFrame()
			_ = framer.WriteRSTStream(hf.(*http2.HeadersFrame).StreamID, http2.ErrCodeCancel)

			errCh <- nil
		}()

		_, err := dialH2ExtendedConnect(context.Background(), client, "wss://example.com/ws", "example.com")
		assert.Equal(t, errH2StreamClosed, err)
		client.Close()
		server.Close()
		<-errCh
	}

	// 5. Server responds with GoAway frame
	{
		server, client := tcpPipe(t)

		errCh := make(chan error, 1)
		go func() {
			preface := make([]byte, len(http2.ClientPreface))
			_, _ = io.ReadFull(server, preface)
			framer := http2.NewFramer(server, server)
			_, _ = framer.ReadFrame()
			_ = framer.WriteSettings(http2.Setting{ID: http2.SettingEnableConnectProtocol, Val: 1})
			_, _ = framer.ReadFrame()
			_ = framer.WriteSettingsAck()
			_, _ = framer.ReadFrame()
			_ = framer.WriteGoAway(1, http2.ErrCodeNo, nil)

			errCh <- nil
		}()

		_, err := dialH2ExtendedConnect(context.Background(), client, "wss://example.com/ws", "example.com")
		assert.Equal(t, errH2GoAway, err)
		client.Close()
		server.Close()
		<-errCh
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
		_ net.Conn = (*wsGorillaConn)(nil)
		_ net.Conn = (*wsRawConn)(nil)
		_ net.Conn = (*wsH2Conn)(nil)
	)
}

func TestH2Preface_ContextDeadline(t *testing.T) {
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
					_, err := conn.Read(buf)
					if err != nil {
						return
					}
				}
			}()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	require.NoError(t, err)

	defer conn.Close()

	start := time.Now()
	_, err = dialH2ExtendedConnect(ctx, conn, "ws://example.com/ws", "example.com")
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Less(t, elapsed, 2*time.Second)
}

func TestH2Preface_ContextCancel(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	require.NoError(t, err)

	defer conn.Close()

	start := time.Now()
	_, err = dialH2ExtendedConnect(ctx, conn, "ws://example.com/ws", "example.com")
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Less(t, elapsed, 2*time.Second)
}
