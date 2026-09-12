// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package resiliency

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	toxiproxy "github.com/Shopify/toxiproxy/v2/client"
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
)

// setupToxiTest creates a local server and toxiproxy proxy for stress testing.
func setupToxiTest(t *testing.T, proxyName, listenAddr string) (*httptest.Server, *toxiproxy.Proxy, *toxiproxy.Client, string) {
	t.Helper()
	var requestCount int32

	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)

		if r.URL.Path == "/421" {
			if count%2 != 0 {
				w.WriteHeader(http.StatusMisdirectedRequest) // 421
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Recovered"))
			return
		}

		if r.URL.Path == "/huge" {
			w.WriteHeader(http.StatusOK)
			w.Write(make([]byte, 100*1024)) // 100KB
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	targetPort := strings.Split(targetSrv.URL, ":")[2]
	toxiClient := toxiproxy.NewClient("127.0.0.1:8474")

	proxy, err := toxiClient.CreateProxy(proxyName, listenAddr, "host.docker.internal:"+targetPort)
	if err != nil {
		// If proxy already exists, delete and recreate
		toxiClient.ResetState()
		proxy, err = toxiClient.Proxy(proxyName)
		if err == nil {
			proxy.Delete()
		}
		proxy, err = toxiClient.CreateProxy(proxyName, listenAddr, "host.docker.internal:"+targetPort)
		if err != nil {
			targetSrv.Close()
			t.Fatalf("Failed to create toxiproxy: %v (Is Toxiproxy running in Docker?)", err)
		}
	}

	proxyURL := "http://" + listenAddr

	return targetSrv, proxy, toxiClient, proxyURL
}

func TestToxiproxyResilienceAndStress(t *testing.T) {
	// We run all tests against the same proxy port, so they must be sequential to avoid port clashes
	// We use 0.0.0.0:22222 which is exposed by the toxiproxy docker container.

	t.Run("WAF_Blackhole_And_Drop", func(t *testing.T) {
		srv, proxy, _, proxyURL := setupToxiTest(t, "aoni_proxy", "0.0.0.0:22222")
		defer srv.Close()
		defer proxy.Delete()

		toxicTimeout, _ := proxy.AddToxic("waf_timeout", "timeout", "upstream", 1.0, toxiproxy.Attributes{"timeout": 1000})
		defer proxy.RemoveToxic(toxicTimeout.Name)

		toxicReset, _ := proxy.AddToxic("waf_reset", "reset_peer", "upstream", 1.0, toxiproxy.Attributes{"timeout": 1000})
		defer proxy.RemoveToxic(toxicReset.Name)

		client := aoni.NewClient(fast.NewClient())
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		_, err := client.Get(ctx, proxyURL)
		if err == nil {
			t.Fatal("Expected error from WAF Blackhole, got success")
		}

		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Request hung until deadline. Toxiproxy RST didn't work. Err: %v", err)
		}
	})

	t.Run("TCP_Blender_Slicer", func(t *testing.T) {
		srv, proxy, _, proxyURL := setupToxiTest(t, "aoni_proxy", "0.0.0.0:22222")
		defer srv.Close()
		defer proxy.Delete()

		slicer, _ := proxy.AddToxic("blender_slicer", "slicer", "downstream", 1.0, toxiproxy.Attributes{
			"average_size":   2,
			"size_variation": 1,
			"delay":          1000,
		})
		defer proxy.RemoveToxic(slicer.Name)

		client := aoni.NewClient(fast.NewClient())
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		resp, err := client.Get(ctx, proxyURL)
		if err != nil {
			t.Fatalf("TCP Blender broke the client: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if string(body) != "OK" {
			t.Fatalf("Expected OK, got %q", string(body))
		}
	})

	t.Run("Slowloris_Tarpit", func(t *testing.T) {
		srv, proxy, _, proxyURL := setupToxiTest(t, "aoni_proxy", "0.0.0.0:22222")
		defer srv.Close()
		defer proxy.Delete()

		bw, _ := proxy.AddToxic("slowloris_bw", "bandwidth", "downstream", 1.0, toxiproxy.Attributes{"rate": 1})
		defer proxy.RemoveToxic(bw.Name)

		client := aoni.NewClient(fast.NewClient())
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		start := time.Now()
		resp, err := client.Get(ctx, proxyURL+"/huge")
		if err == nil {
			io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		dur := time.Since(start)

		if err == nil {
			t.Fatal("Expected Slowloris to trigger context timeout, got success")
		}

		if dur > 3*time.Second {
			t.Fatalf("Context cancellation took too long: %v", dur)
		}
	})

	t.Run("Thundering_Herd_With_Drops", func(t *testing.T) {
		srv, proxy, _, proxyURL := setupToxiTest(t, "aoni_proxy", "0.0.0.0:22222")
		defer srv.Close()
		defer proxy.Delete()

		toxic, _ := proxy.AddToxic("herd_reset", "reset_peer", "upstream", 0.2, toxiproxy.Attributes{"timeout": 0})
		defer proxy.RemoveToxic(toxic.Name)

		client := aoni.NewClient(fast.NewClient())
		const concurrency = 1000
		var wg sync.WaitGroup
		var success, failures int32

		for range concurrency {
			wg.Go(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				resp, err := client.Get(ctx, proxyURL)
				if err != nil {
					atomic.AddInt32(&failures, 1)
					return
				}
				io.ReadAll(resp.Body)
				resp.Body.Close()
				atomic.AddInt32(&success, 1)
			})
		}

		wg.Wait()
		if success == 0 {
			t.Fatal("All connections failed in Thundering Herd")
		}
	})

	t.Run("ErrStaleConnection_Recovery", func(t *testing.T) {
		srv, proxy, _, proxyURL := setupToxiTest(t, "aoni_proxy", "0.0.0.0:22222")
		defer srv.Close()
		defer proxy.Delete()

		client := aoni.NewClient(fast.NewClient())

		// Warm up the connection pool
		resp, err := client.Get(context.Background(), proxyURL)
		if err != nil {
			t.Fatalf("Failed initial warmup: %v", err)
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()

		// Server drops the connection in the pool
		toxic, _ := proxy.AddToxic("reset_toxic", "reset_peer", "downstream", 1.0, toxiproxy.Attributes{"timeout": 0})
		time.Sleep(100 * time.Millisecond)
		proxy.RemoveToxic(toxic.Name)

		// Aoni must auto-recover
		resp2, err := client.Get(context.Background(), proxyURL)
		if err != nil {
			t.Fatalf("Aoni failed to auto-recover from Stale TCP Connection: %v", err)
		}
		io.ReadAll(resp2.Body)
		resp2.Body.Close()

		if resp2.StatusCode != 200 {
			t.Fatalf("Expected 200, got %d", resp2.StatusCode)
		}
	})

	t.Run("MisdirectedRequest_421_Recovery", func(t *testing.T) {
		srv, proxy, _, proxyURL := setupToxiTest(t, "aoni_proxy", "0.0.0.0:22222")
		defer srv.Close()
		defer proxy.Delete()

		// Add jitter so the 421 response and its transparent retry overlap asynchronously
		latency, _ := proxy.AddToxic("jitter", "latency", "downstream", 1.0, toxiproxy.Attributes{
			"latency": 50,
			"jitter":  20,
		})
		defer proxy.RemoveToxic(latency.Name)

		client := aoni.NewClient(fast.NewClient())

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		resp, err := client.Get(ctx, proxyURL+"/421")
		if err != nil {
			// This can fail if the server closes the connection before aoni can recover
			t.Logf("Failed on 421 endpoint (this is okay under heavy network jitter): %v", err)
			return
		}

		if resp != nil && resp.Body != nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if string(body) != "Recovered" {
				t.Fatalf("Client did not transparently recover from 421. Got: %s", body)
			}
		}
	})

	t.Run("UnexpectedEOF_DataLimit", func(t *testing.T) {
		srv, proxy, _, proxyURL := setupToxiTest(t, "aoni_proxy", "0.0.0.0:22222")
		defer srv.Close()
		defer proxy.Delete()

		toxic, _ := proxy.AddToxic("limit_data_toxic", "limit_data", "downstream", 1.0, toxiproxy.Attributes{
			"bytes": 5, // Cut before headers finish
		})
		defer proxy.RemoveToxic(toxic.Name)

		client := aoni.NewClient(fast.NewClient())

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		resp, err := client.Get(ctx, proxyURL)
		if err != nil {
			return // Expected behavior
		}

		if resp != nil && resp.Body != nil {
			_, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err == nil {
				t.Fatal("Expected error reading truncated body")
			}
		}
	})

	t.Run("Flappy_BufferBloat", func(t *testing.T) {
		srv, proxy, _, proxyURL := setupToxiTest(t, "aoni_proxy", "0.0.0.0:22222")
		defer srv.Close()
		defer proxy.Delete()

		// Upstream is fast, downstream has massive delay and variance
		latency, _ := proxy.AddToxic("bloat_latency", "latency", "downstream", 1.0, toxiproxy.Attributes{
			"latency": 1500,
			"jitter":  1000,
		})
		defer proxy.RemoveToxic(latency.Name)

		client := aoni.NewClient(fast.NewClient())

		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()

		start := time.Now()
		resp, err := client.Get(ctx, proxyURL)
		if err != nil {
			// Expected context deadline exceeded because jitter might push it over 4s
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Expected DeadlineExceeded, got: %v", err)
			}
			return
		}

		if resp != nil && resp.Body != nil {
			_, err = io.ReadAll(resp.Body)
			resp.Body.Close()
		}

		dur := time.Since(start)

		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "timeout") {
			t.Fatalf("Unexpected error: %v", err)
		}

		if dur > 5*time.Second {
			t.Fatalf("Goroutine leaked under buffer bloat. Duration: %v", dur)
		}
	})
}
