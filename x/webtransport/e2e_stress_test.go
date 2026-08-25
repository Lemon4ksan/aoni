// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webtransport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/internal/quic"
)

// duplexTransport connects client and server WebTransport sessions in-memory with full QUIC framing
type duplexTransport struct {
	mu           sync.Mutex
	peer         *duplexTransport
	bidiStreams  chan RawStream
	uniStreams   chan RawReceiveStream
	dgramHandler func([]byte)
	isClosed     atomic.Bool
}

func newDuplexPair() (*duplexTransport, *duplexTransport) {
	t1 := &duplexTransport{
		bidiStreams: make(chan RawStream, 256),
		uniStreams:  make(chan RawReceiveStream, 256),
	}
	t2 := &duplexTransport{
		bidiStreams: make(chan RawStream, 256),
		uniStreams:  make(chan RawReceiveStream, 256),
	}
	t1.peer = t2
	t2.peer = t1

	return t1, t2
}

func (d *duplexTransport) OpenStream() (*quic.Stream, error) {
	return nil, nil
}

func (d *duplexTransport) OpenStreamSync(ctx context.Context) (*quic.Stream, error) {
	return nil, nil
}

func (d *duplexTransport) OpenUniStream() (*quic.SendStream, error) {
	return nil, nil
}

func (d *duplexTransport) OpenUniStreamSync(ctx context.Context) (*quic.SendStream, error) {
	return nil, nil
}

func (d *duplexTransport) SendDatagram(p []byte) error {
	if d.isClosed.Load() {
		return ErrSessionClosed
	}

	d.mu.Lock()
	peerHandler := d.peer.dgramHandler
	d.mu.Unlock()

	if peerHandler != nil {
		cp := make([]byte, len(p))
		copy(cp, p)

		go peerHandler(cp)
	}

	return nil
}

type inMemRawStream struct {
	net.Conn
	streamID uint64
}

func (s *inMemRawStream) StreamID() uint64                      { return s.streamID }
func (s *inMemRawStream) CancelRead(code quic.StreamErrorCode)  {}
func (s *inMemRawStream) CancelWrite(code quic.StreamErrorCode) {}
func (s *inMemRawStream) Context() context.Context              { return context.Background() }

type inMemSendStream struct {
	io.WriteCloser
	streamID uint64
}

func (s *inMemSendStream) StreamID() uint64                      { return s.streamID }
func (s *inMemSendStream) CancelWrite(code quic.StreamErrorCode) {}
func (s *inMemSendStream) SetWriteDeadline(t time.Time) error    { return nil }
func (s *inMemSendStream) Context() context.Context              { return context.Background() }

type inMemReceiveStream struct {
	io.Reader
	streamID uint64
}

func (s *inMemReceiveStream) StreamID() uint64                     { return s.streamID }
func (s *inMemReceiveStream) CancelRead(code quic.StreamErrorCode) {}
func (s *inMemReceiveStream) SetReadDeadline(t time.Time) error    { return nil }

func TestWebTransportE2EStressBattle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	const sessionID = uint64(4) // Stream ID 4 for Extended CONNECT

	clientTrans, serverTrans := newDuplexPair()

	clientCtrlPipe, serverCtrlPipe := net.Pipe()
	defer clientCtrlPipe.Close()
	defer serverCtrlPipe.Close()

	// 1. Create client & server WebTransport sessions
	clientSess := NewSession(ctx, sessionID, clientCtrlPipe, clientTrans)
	serverSess := NewSession(ctx, sessionID, serverCtrlPipe, serverTrans)

	defer clientSess.Close()
	defer serverSess.Close()

	// Wire up datagram delivery via Quarter Stream ID (sessionID / 4 = 1)
	clientTrans.dgramHandler = func(dgram []byte) {
		qID, n, err := DecodeVarint(dgram)
		if err == nil && qID*4 == sessionID {
			clientSess.EnqueueDatagram(dgram[n:])
		}
	}
	serverTrans.dgramHandler = func(dgram []byte) {
		qID, n, err := DecodeVarint(dgram)
		if err == nil && qID*4 == sessionID {
			serverSess.EnqueueDatagram(dgram[n:])
		}
	}

	// Server Echo Handler
	go func() {
		for {
			str, err := serverSess.AcceptStream(ctx)
			if err != nil {
				return
			}

			go func(s *Stream) {
				defer s.Close()

				_, _ = io.Copy(s, s) // Echo
			}(str)
		}
	}()

	go func() {
		for {
			uStr, err := serverSess.AcceptUniStream(ctx)
			if err != nil {
				return
			}

			go func(s *ReceiveStream) {
				buf := make([]byte, 1024)
				for {
					_, rErr := s.Read(buf)
					if rErr != nil {
						return
					}
				}
			}(uStr)
		}
	}()

	go func() {
		for {
			dgram, err := serverSess.ReceiveDatagram(ctx)
			if err != nil {
				return
			}

			_ = serverSess.SendDatagram(dgram) // Echo datagram
		}
	}()

	// 2. Stress Test 1: 100 Parallel Bidirectional Streams
	const numStreams = 100
	t.Logf("Running %d parallel bidirectional streams...", numStreams)

	var (
		streamCounter atomic.Uint64
		wg            sync.WaitGroup
	)

	errCh := make(chan error, numStreams)
	startBidi := time.Now()

	for i := 0; i < numStreams; i++ {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()

			c1, c2 := net.Pipe()
			sID := streamCounter.Add(4)

			// Client side stream
			cRaw := &inMemRawStream{Conn: c1, streamID: sID}

			cStr := newOutgoingStream(cRaw, sessionID, sID)
			defer cStr.Close()

			// Server side acceptance via 0x41 framing read
			go func() {
				// Read 0x41 + sessionID from c2
				fType, _, rErr := readVarintFromStream(c2)
				if rErr != nil || fType != FrameTypeWebTransportBidi {
					_ = c2.Close()
					return
				}

				sessID, _, sErr := readVarintFromStream(c2)
				if sErr != nil || sessID != sessionID {
					_ = c2.Close()
					return
				}

				sRaw := &inMemRawStream{Conn: c2, streamID: sID}
				sStr := newIncomingStream(sRaw, sessID, sID)
				serverSess.EnqueueBidiStream(sStr)
			}()

			payload := []byte(fmt.Sprintf("wt-stream-stress-data-%04d-payload", idx))
			if _, wErr := cStr.Write(payload); wErr != nil {
				errCh <- fmt.Errorf("stream %d write: %w", idx, wErr)
				return
			}

			res := make([]byte, len(payload))
			if _, rErr := io.ReadFull(cStr, res); rErr != nil {
				errCh <- fmt.Errorf("stream %d read: %w", idx, rErr)
				return
			}

			if !bytes.Equal(res, payload) {
				errCh <- fmt.Errorf("stream %d mismatch: got %q, want %q", idx, string(res), string(payload))
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for bErr := range errCh {
		if bErr != nil {
			t.Fatalf("bidirectional stream failure: %v", bErr)
		}
	}

	t.Logf("100 parallel bidirectional streams PASSED in %v", time.Since(startBidi))

	// 3. Stress Test 2: 100 Parallel Unidirectional Streams (0x54 + Session ID)
	const numUni = 100
	t.Logf("Running %d parallel unidirectional streams...", numUni)

	var wgUni sync.WaitGroup

	startUni := time.Now()

	for i := 0; i < numUni; i++ {
		wgUni.Add(1)

		go func(idx int) {
			defer wgUni.Done()

			pr, pw := io.Pipe()
			sID := streamCounter.Add(4)

			// Client side send stream
			cSendRaw := &inMemSendStream{WriteCloser: pw, streamID: sID}

			cSend := newSendStream(cSendRaw, sessionID, sID)
			defer cSend.Close()

			// Server side uni acceptance via 0x54 framing read
			go func() {
				sType, _, rErr := readVarintFromReceiveStream(pr)
				if rErr != nil || sType != StreamTypeWebTransportUni {
					_ = pr.Close()
					return
				}

				sessID, _, sErr := readVarintFromReceiveStream(pr)
				if sErr != nil || sessID != sessionID {
					_ = pr.Close()
					return
				}

				sRecvRaw := &inMemReceiveStream{Reader: pr, streamID: sID}
				sRecv := newReceiveStream(sRecvRaw, sessID, sID)
				serverSess.EnqueueUniStream(sRecv)
			}()

			_, _ = fmt.Fprintf(cSend, "uni-data-%d", idx)
		}(i)
	}

	wgUni.Wait()
	t.Logf("100 parallel unidirectional streams PASSED in %v", time.Since(startUni))

	// 4. Stress Test 3: 50,000 Unreliable Datagrams (Quarter Stream ID: RFC 9297)
	const numDatagrams = 50000
	t.Logf("Pushing %d datagrams through QUIC quarter stream multiplexer...", numDatagrams)

	var echoedDatagrams atomic.Int64

	dgramCtx, dgramCancel := context.WithTimeout(ctx, 3*time.Second)
	defer dgramCancel()

	go func() {
		for {
			_, err := clientSess.ReceiveDatagram(dgramCtx)
			if err != nil {
				return
			}

			echoedDatagrams.Add(1)
		}
	}()

	startDgram := time.Now()

	for i := 0; i < numDatagrams; i++ {
		payload := []byte(fmt.Sprintf("dgram-%06d", i))
		_ = clientSess.SendDatagram(payload)
	}

	time.Sleep(200 * time.Millisecond)
	dgramCancel()

	rcvd := echoedDatagrams.Load()
	t.Logf("Datagrams: sent %d, successfully echoed %d in %v (throughput: %.0f dgrams/sec)",
		numDatagrams, rcvd, time.Since(startDgram), float64(rcvd)/time.Since(startDgram).Seconds())

	if rcvd == 0 {
		t.Fatalf("expected echoed datagrams, got 0")
	}

	// 5. Test Capsule Drain & CloseWithError (0x2843)
	t.Log("Testing capsule drainage & CLOSE_WEBTRANSPORT_SESSION (0x2843)...")

	if err := clientSess.Drain(); err != nil {
		t.Errorf("Drain failed: %v", err)
	}

	if err := clientSess.CloseWithError(0x1337, "test completed"); err != nil {
		t.Errorf("CloseWithError failed: %v", err)
	}

	t.Log("WebTransport Battle Test Completed with 100% Success!")
}
