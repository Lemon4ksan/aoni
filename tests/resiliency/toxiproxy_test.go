package resiliency

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	toxiproxy "github.com/Shopify/toxiproxy/v2/client"
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
)

func TestToxiproxyResilience(t *testing.T) {
	// 1. Setup local target server
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer targetSrv.Close()

	targetPort := strings.Split(targetSrv.URL, ":")[2]

	// 2. Setup Toxiproxy Client
	toxiClient := toxiproxy.NewClient("127.0.0.1:8474")
	
	// Ensure clean state
	toxiClient.ResetState()

	// 3. Create Proxy
	// Toxiproxy runs in Docker, so to reach host we use host.docker.internal
	proxy, err := toxiClient.CreateProxy("aoni_test_target", "0.0.0.0:22222", "host.docker.internal:"+targetPort)
	if err != nil {
		t.Fatalf("Failed to create toxiproxy: %v (Is Toxiproxy running in Docker?)", err)
	}
	defer proxy.Delete()

	proxyURL := "http://127.0.0.1:22222"

	// 4. Test Timeout Resilience
	t.Run("Timeout_Resilience", func(t *testing.T) {
		client := aoni.NewClient(fast.NewClient())
		
		// Add timeout toxic: stops all data, does not close connection (blackhole)
		toxic, err := proxy.AddToxic("timeout_toxic", "timeout", "downstream", 1.0, toxiproxy.Attributes{
			"timeout": 0, // 0 = indefinitely drop data
		})
		if err != nil {
			t.Fatalf("Failed to add toxic: %v", err)
		}
		defer proxy.RemoveToxic(toxic.Name)

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		start := time.Now()
		_, err = client.Get(ctx, proxyURL)
		duration := time.Since(start)

		if err == nil {
			t.Fatal("Expected timeout error, got success")
		}

		if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "timeout") {
			t.Fatalf("Expected DeadlineExceeded or timeout error, got: %v", err)
		}

		if duration > 2*time.Second {
			t.Fatalf("Client hung for too long: %v", duration)
		}
	})

	// 5. Test ErrStaleConnection Auto-Recovery
	t.Run("ErrStaleConnection_Recovery", func(t *testing.T) {
		client := aoni.NewClient(fast.NewClient())

		// Warm up the connection pool (Keep-Alive)
		resp, err := client.Get(context.Background(), proxyURL)
		if err != nil {
			t.Fatalf("Failed initial warmup: %v", err)
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()

		// Now, simulate the server dropping the connection while it sits in the pool
		toxic, err := proxy.AddToxic("reset_toxic", "reset_peer", "downstream", 1.0, toxiproxy.Attributes{
			"timeout": 0,
		})
		if err != nil {
			t.Fatalf("Failed to add toxic: %v", err)
		}

		// Wait briefly to let the reset propagate
		time.Sleep(100 * time.Millisecond)
		
		// Remove the toxic so the RETRY will succeed
		proxy.RemoveToxic(toxic.Name)

		// Send another GET request. It will try to use the broken keep-alive connection,
		// get an EOF/RST, map it to ErrStaleConnection, and transparently retry!
		resp2, err := client.Get(context.Background(), proxyURL)
		if err != nil {
			t.Fatalf("Aoni failed to auto-recover from Stale TCP Connection: %v", err)
		}
		io.ReadAll(resp2.Body)
		resp2.Body.Close()

		if resp2.StatusCode != 200 {
			t.Fatalf("Expected 200 after recovery, got %d", resp2.StatusCode)
		}
	})
	
	// 6. Test HTTP 421 / Re-routing emulation
	// (If we use Latency + Slicer to test chunking issues)
	t.Run("Slicer_Chunking", func(t *testing.T) {
		client := aoni.NewClient(fast.NewClient())

		// Slice TCP data into 2-byte chunks delayed by 10ms
		toxic, err := proxy.AddToxic("slicer_toxic", "slicer", "downstream", 1.0, toxiproxy.Attributes{
			"average_size": 2,
			"size_variation": 0,
			"delay": 10000, // microseconds = 10ms
		})
		if err != nil {
			t.Fatalf("Failed to add toxic: %v", err)
		}
		defer proxy.RemoveToxic(toxic.Name)

		resp, err := client.Get(context.Background(), proxyURL)
		if err != nil {
			t.Fatalf("Failed to read sliced response: %v", err)
		}
		
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		
		if string(body) != "OK" {
			t.Fatalf("Expected body 'OK', got %q", string(body))
		}
	})
}
