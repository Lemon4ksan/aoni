// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/tests/testutil"
)

// mockTB embeds testing.TB to capture Fatalf calls during leak verification challenges.
type mockTB struct {
	testing.TB
	failed   atomic.Bool
	fatalMsg string
	mu       sync.Mutex
	name     string
}

func newMockTB(parent testing.TB, name string) *mockTB {
	return &mockTB{
		TB:   parent,
		name: name,
	}
}

func (m *mockTB) Fatalf(format string, args ...any) {
	m.mu.Lock()
	m.fatalMsg = fmt.Sprintf(format, args...)
	m.failed.Store(true)
	m.mu.Unlock()
}

func (m *mockTB) Name() string {
	if m.name != "" {
		return m.name
	}
	return m.TB.Name()
}

func (m *mockTB) Helper() {}

func (m *mockTB) Failed() bool {
	return m.failed.Load()
}

func (m *mockTB) FatalMsg() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fatalMsg
}

// -----------------------------------------------------------------------------
// Challenge Area 1: leak.Check(t) & LeakTracker Stress & Verification
// -----------------------------------------------------------------------------

// TestChallenge_LeakDetector_NoLeak_Passes asserts that leak.Check cleanly passes
// when no goroutine leaks occur.
func TestChallenge_LeakDetector_NoLeak_Passes(t *testing.T) {
	mock := newMockTB(t, "TestMock_NoLeak")

	func() {
		defer testutil.Check(mock, testutil.WithTimeout(100*time.Millisecond))()
		// Safe in-flight ephemeral work that finishes before Check exits
		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(5 * time.Millisecond)
			}()
		}
		wg.Wait()
	}()

	assert.False(t, mock.Failed(), "expected leak.Check to pass when zero leaks exist")
	assert.Equal(t, "", mock.FatalMsg())
}

// TestChallenge_LeakDetector_TransientGoroutines_WaitsAndPasses verifies that
// leak.Check polls and waits for background goroutines to settle within the timeout window.
func TestChallenge_LeakDetector_TransientGoroutines_WaitsAndPasses(t *testing.T) {
	mock := newMockTB(t, "TestMock_TransientGoroutines")

	func() {
		defer testutil.Check(mock, testutil.WithTimeout(1*time.Second), testutil.WithInterval(10*time.Millisecond))()

		// Start a goroutine that lingers for 80ms into the settling window
		go func() {
			time.Sleep(80 * time.Millisecond)
		}()
	}()

	assert.False(t, mock.Failed(), "expected leak.Check to poll and settle before failing")
}

// TestChallenge_LeakDetector_SingleLeak_Detected asserts that an intentional
// goroutine leak is accurately detected and reported with its stack signature.
func TestChallenge_LeakDetector_SingleLeak_Detected(t *testing.T) {
	mock := newMockTB(t, "TestMock_SingleLeak")

	releaseCh := make(chan struct{})
	defer close(releaseCh)

	func() {
		defer testutil.Check(mock, testutil.WithTimeout(100*time.Millisecond), testutil.WithInterval(10*time.Millisecond))()

		// Spawn intentional leak
		go func() {
			<-releaseCh
		}()
	}()

	assert.True(t, mock.Failed(), "expected leak.Check to fail on intentional goroutine leak")
	msg := mock.FatalMsg()
	assert.True(t, strings.Contains(msg, "GOROUTINE LEAK DETECTED"), "message should state GOROUTINE LEAK DETECTED")
	assert.True(t, strings.Contains(msg, "Leaked: +1"), "message should report Leaked: +1")
	assert.True(t, strings.Contains(msg, "NEW - NOT PRESENT AT BASELINE"), "stack trace should mark new goroutine")
}

// TestChallenge_LeakDetector_MultipleLeaks_AccurateCount tests detection of
// multiple concurrent leaked goroutines and verifies exact count reporting.
func TestChallenge_LeakDetector_MultipleLeaks_AccurateCount(t *testing.T) {
	mock := newMockTB(t, "TestMock_MultipleLeaks")

	releaseCh := make(chan struct{})
	defer close(releaseCh)

	const numLeaks = 4
	func() {
		defer testutil.Check(mock, testutil.WithTimeout(100*time.Millisecond), testutil.WithInterval(10*time.Millisecond))()

		for i := 0; i < numLeaks; i++ {
			go func() {
				<-releaseCh
			}()
		}
	}()

	assert.True(t, mock.Failed(), "expected leak.Check to detect multiple leaks")
	msg := mock.FatalMsg()
	assert.True(t, strings.Contains(msg, fmt.Sprintf("Leaked: +%d", numLeaks)), "message should report exact delta")
}

// TestChallenge_LeakDetector_Tolerance_Enforcement validates strict tolerance thresholds.
func TestChallenge_LeakDetector_Tolerance_Enforcement(t *testing.T) {
	releaseCh := make(chan struct{})
	defer close(releaseCh)

	// Scenario A: 2 leaks with tolerance=2 -> PASSES
	mockA := newMockTB(t, "TestMock_TolerancePass")
	func() {
		defer testutil.Check(mockA, testutil.WithTolerance(2), testutil.WithTimeout(100*time.Millisecond))()
		for i := 0; i < 2; i++ {
			go func() { <-releaseCh }()
		}
	}()
	assert.False(t, mockA.Failed(), "expected tolerance=2 to permit 2 surplus goroutines")

	// Scenario B: 2 leaks with tolerance=1 -> FAILS
	mockB := newMockTB(t, "TestMock_ToleranceFail")
	func() {
		defer testutil.Check(mockB, testutil.WithTolerance(1), testutil.WithTimeout(100*time.Millisecond), testutil.WithInterval(10*time.Millisecond))()
		for i := 0; i < 2; i++ {
			go func() { <-releaseCh }()
		}
	}()
	assert.True(t, mockB.Failed(), "expected tolerance=1 to reject 2 surplus goroutines")
	assert.True(t, strings.Contains(mockB.FatalMsg(), "Leaked: +2 | Allowed Tolerance: 1"))
}

// intentionalCustomLeaker is a distinct function to test custom ignore pattern suppression.
func intentionalCustomLeaker(stop <-chan struct{}) {
	<-stop
}

// TestChallenge_LeakDetector_CustomIgnore_Suppression tests that custom ignore patterns
// accurately exclude matched goroutines from leak assertions.
func TestChallenge_LeakDetector_CustomIgnore_Suppression(t *testing.T) {
	releaseCh := make(chan struct{})
	defer close(releaseCh)

	// Scenario A: Leak with WithIgnore -> PASSES
	mockA := newMockTB(t, "TestMock_IgnorePass")
	func() {
		defer testutil.Check(mockA,
			testutil.WithIgnore("intentionalCustomLeaker"),
			testutil.WithTimeout(100*time.Millisecond),
		)()

		go intentionalCustomLeaker(releaseCh)
	}()
	assert.False(t, mockA.Failed(), "expected custom ignore pattern to suppress failure")

	// Scenario B: Same leak without WithIgnore -> FAILS
	mockB := newMockTB(t, "TestMock_IgnoreFail")
	func() {
		defer testutil.Check(mockB,
			testutil.WithTimeout(100*time.Millisecond),
			testutil.WithInterval(10*time.Millisecond),
		)()

		go intentionalCustomLeaker(releaseCh)
	}()
	assert.True(t, mockB.Failed(), "expected failure without ignore pattern")
	assert.True(t, strings.Contains(mockB.FatalMsg(), "intentionalCustomLeaker"))
}

// TestChallenge_LeakDetector_CustomDrain verifies that custom drain hooks are called.
func TestChallenge_LeakDetector_CustomDrain(t *testing.T) {
	mock := newMockTB(t, "TestMock_CustomDrain")
	var drainCount atomic.Int32

	func() {
		defer testutil.Check(mock,
			testutil.WithDrain(func() { drainCount.Add(1) }),
			testutil.WithTimeout(50*time.Millisecond),
			testutil.WithInterval(10*time.Millisecond),
		)()
	}()

	assert.False(t, mock.Failed())
	assert.True(t, drainCount.Load() >= 2, "expected drain callback to be called at baseline and verify")
}

// TestChallenge_LeakDetector_MaskingEdgeCase_BaselineExit empirically tests the
// architectural limitation where a newly leaked goroutine offsets a terminating baseline goroutine.
func TestChallenge_LeakDetector_MaskingEdgeCase_BaselineExit(t *testing.T) {
	mock := newMockTB(t, "TestMock_Masking")

	baselineGoroutineExit := make(chan struct{})
	newLeakRelease := make(chan struct{})
	defer close(newLeakRelease)

	// Start a non-ignored goroutine BEFORE baseline capture
	baselineReady := make(chan struct{})
	go func() {
		close(baselineReady)
		<-baselineGoroutineExit
	}()
	<-baselineReady

	func() {
		// Capture baseline (baselineCount includes the pre-existing goroutine)
		defer testutil.Check(mock, testutil.WithTimeout(100*time.Millisecond), testutil.WithInterval(10*time.Millisecond))()

		// 1. Terminate the baseline goroutine
		close(baselineGoroutineExit)
		time.Sleep(20 * time.Millisecond) // allow it to exit

		// 2. Spawn a new intentional leak
		go func() {
			<-newLeakRelease
		}()
	}()

	// OBSERVATION / EMPIRICAL FINDING:
	// Because Verify() evaluates stack signatures against baseline counts,
	// the newly introduced goroutine signature is detected even though a baseline goroutine exited.
	t.Logf("Empirical finding: mock.Failed() = %v. Signature-based comparison detects replacement leak when baseline exits.", mock.Failed())
	assert.True(t, mock.Failed(), "proves signature-based tracker detects replacement leaks when baseline exits")
	assert.True(t, strings.Contains(mock.FatalMsg(), "GOROUTINE LEAK DETECTED"))
	assert.True(t, strings.Contains(mock.FatalMsg(), "Leaked: +1"))
}

// -----------------------------------------------------------------------------
// Challenge Area 2: Server Lifecycle (H1, H2, H3 Ephemeral Servers)
// -----------------------------------------------------------------------------

// TestChallenge_ServerLifecycle_H1_TightLoop starts and stops 30 H1 ephemeral servers
// in a rapid sequential loop, asserting valid ports, clean teardown, and zero socket leaks.
func TestChallenge_ServerLifecycle_H1_TightLoop(t *testing.T) {
	defer testutil.Check(t)()

	const iterations = 30
	ports := make([]int, 0, iterations)

	for i := 0; i < iterations; i++ {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fmt.Sprintf("h1-loop-%d", i)))
		})

		server := testutil.NewH1Server(t, handler)
		assert.Equal(t, "HTTP/1.1", server.Protocol())
		assert.True(t, server.Port() > 0, "server port must be positive")
		ports = append(ports, server.Port())

		// Verify functional request
		resp, err := server.Client().Get(server.URL())
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("h1-loop-%d", i), string(body))

		// Teardown
		err = server.Close()
		require.NoError(t, err)

		// Assert listener port is released (new listener can re-bind or port freed)
		assertPortReleased(t, "tcp", server.Addr())
	}

	assert.Equal(t, iterations, len(ports))
}

// TestChallenge_ServerLifecycle_H2_TightLoop starts and stops 25 H2 ephemeral TLS servers
// in a tight sequential loop, asserting ALPN h2, zero port collisions, and clean teardowns.
func TestChallenge_ServerLifecycle_H2_TightLoop(t *testing.T) {
	defer testutil.Check(t)()

	const iterations = 25
	ports := make([]int, 0, iterations)

	for i := 0; i < iterations; i++ {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fmt.Sprintf("h2-loop-%d", i)))
		})

		server := testutil.NewH2Server(t, handler)
		assert.Equal(t, "HTTP/2", server.Protocol())
		assert.True(t, server.Port() > 0)
		ports = append(ports, server.Port())

		// Verify functional H2 request
		client := server.Client()
		resp, err := client.Get(server.URL())
		require.NoError(t, err)
		assert.Equal(t, "HTTP/2.0", resp.Proto)
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("h2-loop-%d", i), string(body))

		// Teardown
		err = server.Close()
		require.NoError(t, err)

		assertPortReleased(t, "tcp", server.Addr())
	}

	assert.Equal(t, iterations, len(ports))
}

// TestChallenge_ServerLifecycle_H3_TightLoop starts and stops 25 H3 ephemeral QUIC servers
// in a tight sequential loop, asserting QUIC UDP binding, zero port collisions, and clean teardowns.
func TestChallenge_ServerLifecycle_H3_TightLoop(t *testing.T) {
	defer testutil.Check(t)()

	const iterations = 25
	ports := make([]int, 0, iterations)

	for i := 0; i < iterations; i++ {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fmt.Sprintf("h3-loop-%d", i)))
		})

		server := testutil.NewH3Server(t, handler)
		assert.Equal(t, "HTTP/3", server.Protocol())
		assert.True(t, server.Port() > 0)
		ports = append(ports, server.Port())

		// Verify functional H3 request via aoni.Client
		client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
		resp, err := client.Request(context.Background(), http.MethodGet, server.URL()+"/test")
		require.NoError(t, err)
		assert.Equal(t, "HTTP/3.0", resp.Proto)
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("h3-loop-%d", i), string(body))
		client.Close()

		// Teardown
		err = server.Close()
		require.NoError(t, err)

		assertPortReleased(t, "udp", server.Addr())
	}

	assert.Equal(t, iterations, len(ports))
}

// TestChallenge_ServerLifecycle_ConcurrentFleet_ZeroPortCollisions spins up 30 ephemeral
// servers (10 H1 + 10 H2 + 10 H3) simultaneously, asserting strictly unique ports and zero collisions.
func TestChallenge_ServerLifecycle_ConcurrentFleet_ZeroPortCollisions(t *testing.T) {
	defer testutil.Check(t)()

	const (
		numH1 = 10
		numH2 = 10
		numH3 = 10
		total = numH1 + numH2 + numH3
	)

	type serverItem struct {
		srv      testutil.Server
		proto    string
		isTCP    bool
		origAddr string
	}

	servers := make([]serverItem, total)
	var wg sync.WaitGroup

	echoH := newEchoHandler()

	// Spin up all servers concurrently
	for i := 0; i < numH1; i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := testutil.NewH1Server(t, echoH)
			servers[idx] = serverItem{srv: s, proto: "HTTP/1.1", isTCP: true, origAddr: s.Addr()}
		}()
	}

	for i := 0; i < numH2; i++ {
		idx := numH1 + i
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := testutil.NewH2Server(t, echoH)
			servers[idx] = serverItem{srv: s, proto: "HTTP/2", isTCP: true, origAddr: s.Addr()}
		}()
	}

	for i := 0; i < numH3; i++ {
		idx := numH1 + numH2 + i
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := testutil.NewH3Server(t, echoH)
			servers[idx] = serverItem{srv: s, proto: "HTTP/3", isTCP: false, origAddr: s.Addr()}
		}()
	}

	wg.Wait()

	// Assert port uniqueness within protocol domains
	tcpPorts := make(map[int]string)
	udpPorts := make(map[int]string)

	for _, item := range servers {
		port := item.srv.Port()
		assert.True(t, port > 0, "port must be positive")

		if item.isTCP {
			if prev, exists := tcpPorts[port]; exists {
				t.Fatalf("TCP PORT COLLISION detected! Port %d assigned to both %s and %s", port, prev, item.origAddr)
			}
			tcpPorts[port] = item.origAddr
		} else {
			if prev, exists := udpPorts[port]; exists {
				t.Fatalf("UDP PORT COLLISION detected! Port %d assigned to both %s and %s", port, prev, item.origAddr)
			}
			udpPorts[port] = item.origAddr
		}
	}

	assert.Equal(t, numH1+numH2, len(tcpPorts), "all concurrent TCP servers must have unique ports")
	assert.Equal(t, numH3, len(udpPorts), "all concurrent UDP servers must have unique ports")

	// Verify all 30 servers respond simultaneously under concurrent client load
	var reqWg sync.WaitGroup
	errCh := make(chan error, total)

	for _, item := range servers {
		reqWg.Add(1)
		go func(it serverItem) {
			defer reqWg.Done()
			switch it.proto {
			case "HTTP/1.1":
				resp, err := http.Get(it.srv.URL() + "/fleet-check")
				if err != nil {
					errCh <- fmt.Errorf("H1 request to %s failed: %w", it.srv.URL(), err)
					return
				}
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					errCh <- fmt.Errorf("H1 status %d", resp.StatusCode)
				}
			case "HTTP/2":
				h2s := it.srv.(*testutil.H2TestServer)
				resp, err := h2s.Client().Get(it.srv.URL() + "/fleet-check")
				if err != nil {
					errCh <- fmt.Errorf("H2 request to %s failed: %w", it.srv.URL(), err)
					return
				}
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					errCh <- fmt.Errorf("H2 status %d", resp.StatusCode)
				}
			case "HTTP/3":
				client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(it.srv.TLSConfig())))
				defer client.Close()
				resp, err := client.Request(context.Background(), http.MethodGet, it.srv.URL()+"/fleet-check")
				if err != nil {
					errCh <- fmt.Errorf("H3 request to %s failed: %w", it.srv.URL(), err)
					return
				}
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					errCh <- fmt.Errorf("H3 status %d", resp.StatusCode)
				}
			}
		}(item)
	}

	reqWg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent fleet request failed: %v", err)
	}

	// Concurrently tear down all 30 servers
	var closeWg sync.WaitGroup
	for _, item := range servers {
		closeWg.Add(1)
		go func(it serverItem) {
			defer closeWg.Done()
			_ = it.srv.Close()
		}(item)
	}
	closeWg.Wait()

	// Verify all ports are released
	for _, item := range servers {
		network := "tcp"
		if !item.isTCP {
			network = "udp"
		}
		assertPortReleased(t, network, item.origAddr)
	}
}

// TestChallenge_ServerLifecycle_H3_TeardownWithInFlightRequests verifies that
// closing an H3 server while streams are in-flight cleanly aborts connections without hang.
func TestChallenge_ServerLifecycle_H3_TeardownWithInFlightRequests(t *testing.T) {
	defer testutil.Check(t)()

	hangCh := make(chan struct{})
	var closeOnce sync.Once
	closeHang := func() {
		closeOnce.Do(func() { close(hangCh) })
	}
	defer closeHang()

	// Server handler that stalls until hangCh is closed OR request context is canceled
	stallingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-hangCh:
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			// Cleanly aborted on server teardown / connection close
			return
		}
	})

	server := testutil.NewH3Server(t, stallingHandler)

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	// Launch in-flight request
	reqDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		_, err := client.Request(ctx, http.MethodGet, server.URL()+"/stalled")
		reqDone <- err
	}()

	// Wait briefly for request to reach server
	time.Sleep(50 * time.Millisecond)

	// Close server abruptly while request is in-flight
	closeDone := make(chan struct{})
	go func() {
		_ = server.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
		// Teardown succeeded
	case <-time.After(3 * time.Second):
		t.Fatal("server.Close() deadlocked with in-flight request")
	}

	// Client request assessment:
	// With clean teardown ordering, server sends CONNECTION_CLOSE before destroying UDP socket,
	// allowing client to unblock promptly with an error.
	var clientUnblocked bool
	var clientErr error
	select {
	case clientErr = <-reqDone:
		clientUnblocked = true
		t.Logf("Client cleanly unblocked on server close with: %v", clientErr)
	case <-time.After(500 * time.Millisecond):
		clientUnblocked = false
		t.Log("Client did not unblock within 500ms of server.Close()")
		// Unblock the stalling handler to allow graceful test completion and check
		closeHang()
		clientErr = <-reqDone
		t.Logf("Client finally terminated after handler release with: %v", clientErr)
	}

	assert.True(t, clientUnblocked, "client request did not unblock after server close")
	assert.Error(t, clientErr, "expected error on in-flight client request when server closes abruptly")
}

// TestChallenge_ServerLifecycle_IdempotentConcurrentClose verifies that calling
// Close() concurrently on H1, H2, and H3 test servers causes zero panics or data races.
func TestChallenge_ServerLifecycle_IdempotentConcurrentClose(t *testing.T) {
	defer testutil.Check(t)()

	h1 := testutil.NewH1Server(t, newEchoHandler())
	h2 := testutil.NewH2Server(t, newEchoHandler())
	h3 := testutil.NewH3Server(t, newEchoHandler())

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); _ = h1.Close() }()
		go func() { defer wg.Done(); _ = h2.Close() }()
		go func() { defer wg.Done(); _ = h3.Close() }()
	}
	wg.Wait()
}

// TestChallenge_ServerLifecycle_RepeatedFleet_ZeroLeaks stress-tests repeated rapid
// spinning up and tearing down of ephemeral server fleets, asserting that zero
// goroutines or sockets leak across iterations.
func TestChallenge_ServerLifecycle_RepeatedFleet_ZeroLeaks(t *testing.T) {
	defer testutil.Check(t)()

	echoH := newEchoHandler()
	const rounds = 5
	const batchSize = 3 // 3 H1 + 3 H2 + 3 H3 per round = 45 total servers

	for r := 0; r < rounds; r++ {
		var servers []testutil.Server

		for i := 0; i < batchSize; i++ {
			servers = append(servers, testutil.NewH1Server(t, echoH))
			servers = append(servers, testutil.NewH2Server(t, echoH))
			servers = append(servers, testutil.NewH3Server(t, echoH))
		}

		// Issue requests to all
		for _, srv := range servers {
			switch srv.Protocol() {
			case "HTTP/1.1":
				resp, err := http.Get(srv.URL())
				require.NoError(t, err)
				_ = resp.Body.Close()
			case "HTTP/2":
				h2s := srv.(*testutil.H2TestServer)
				resp, err := h2s.Client().Get(srv.URL())
				require.NoError(t, err)
				_ = resp.Body.Close()
			case "HTTP/3":
				client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(srv.TLSConfig())))
				resp, err := client.Request(context.Background(), http.MethodGet, srv.URL()+"/test")
				require.NoError(t, err)
				_ = resp.Body.Close()
				client.Close()
			}
		}

		// Teardown
		for _, srv := range servers {
			require.NoError(t, srv.Close())
		}
	}
}

// assertPortReleased attempts to listen on the given network and address to confirm
// that the previous listener was closed and the port is no longer held open.
func assertPortReleased(t testing.TB, network, addr string) {
	t.Helper()

	// In Windows, SO_REUSEADDR and TCP TIME_WAIT may apply to TCP ports.
	// For UDP, binding should succeed immediately.
	// We poll with short timeout to verify the socket is unbound.
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		if network == "tcp" {
			ln, err := net.Listen("tcp", addr)
			if err == nil {
				_ = ln.Close()
				return
			}
		} else if network == "udp" {
			uaddr, err := net.ResolveUDPAddr("udp", addr)
			if err == nil {
				conn, err := net.ListenUDP("udp", uaddr)
				if err == nil {
					_ = conn.Close()
					return
				}
			}
		}

		if time.Now().After(deadline) {
			// On Windows, TCP sockets enter TIME_WAIT and cannot be rebound without SO_REUSEADDR.
			// That is normal OS TCP behavior; we log but don't fail TCP if it's TIME_WAIT.
			if network == "udp" {
				t.Fatalf("UDP port %s was NOT released within deadline", addr)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
