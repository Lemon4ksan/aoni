// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webtransport_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/x/webtransport"
)

func TestLiveChromiumEchoServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	targetURL := "https://echo.webtransport.day:443/echo"

	t.Logf("Connecting to live Chromium WebTransport echo server: %s", targetURL)

	sess, err := webtransport.Dial(ctx, targetURL,
		webtransport.WithTLSConfig(&tls.Config{
			ServerName: "echo.webtransport.day",
			NextProtos: []string{"h3"},
			MinVersion: tls.VersionTLS13,
		}),
	)
	if err != nil {
		t.Logf("Live internet connection to %s failed (network/UDP blocked): %v", targetURL, err)
		t.Skip("skipping live internet test due to network/firewall unreachable")
		return
	}
	defer sess.Close()

	t.Logf("Connected! Session ID: %d", sess.SessionID())

	// Step 1: Test parallel bidirectional streams (100 streams)
	const numStreams = 100

	var wg sync.WaitGroup

	errCh := make(chan error, numStreams)

	t.Logf("Opening %d parallel bidirectional streams...", numStreams)

	start := time.Now()

	for i := 0; i < numStreams; i++ {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()

			strCtx, strCancel := context.WithTimeout(ctx, 10*time.Second)
			defer strCancel()

			str, sErr := sess.OpenStreamSync(strCtx)
			if sErr != nil {
				errCh <- fmt.Errorf("stream %d open failed: %w", idx, sErr)
				return
			}
			defer str.Close()

			payload := []byte(fmt.Sprintf("aoni-webtransport-test-stream-%d-payload-data", idx))
			if _, wErr := str.Write(payload); wErr != nil {
				errCh <- fmt.Errorf("stream %d write failed: %w", idx, wErr)
				return
			}

			buf := make([]byte, len(payload))
			if _, rErr := io.ReadFull(str, buf); rErr != nil {
				errCh <- fmt.Errorf("stream %d read failed: %w", idx, rErr)
				return
			}

			if !bytes.Equal(buf, payload) {
				errCh <- fmt.Errorf("stream %d mismatch: got %q, want %q", idx, string(buf), string(payload))
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for sErr := range errCh {
		if sErr != nil {
			t.Fatalf("bidirectional stream error: %v", sErr)
		}
	}

	t.Logf("100 parallel bidirectional streams completed in %v", time.Since(start))

	// Step 2: Test Datagrams (Send & Receive)
	t.Log("Testing unreliable datagrams...")

	dgramPayload := []byte("aoni-webtransport-quic-datagram-test")
	if dErr := sess.SendDatagram(dgramPayload); dErr != nil {
		t.Logf("Datagram send error: %v", dErr)
	} else {
		dCtx, dCancel := context.WithTimeout(ctx, 3*time.Second)
		defer dCancel()

		recv, rErr := sess.ReceiveDatagram(dCtx)
		if rErr != nil {
			t.Logf("Datagram receive timeout/error: %v (unreliable UDP packet drop allowed)", rErr)
		} else {
			t.Logf("Received echoed datagram (%d bytes): %q", len(recv), string(recv))

			if bytes.Equal(recv, dgramPayload) {
				t.Log("Datagram payload verified with exact byte parity!")
			}
		}
	}

	// Step 3: Test CLOSE_WEBTRANSPORT_SESSION capsule
	t.Log("Sending CLOSE_WEBTRANSPORT_SESSION (0x2843) capsule...")

	if cErr := sess.CloseWithError(0x00, "clean test shutdown"); cErr != nil {
		t.Errorf("CloseWithError failed: %v", cErr)
	}

	t.Log("Session closed cleanly!")
}
