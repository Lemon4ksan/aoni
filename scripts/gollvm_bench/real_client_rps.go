// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
	"github.com/lemon4ksan/aoni/option"
)

func main() {
	runtime.GOMAXPROCS(12)

	// 1. In-memory listener
	ln := h1engine.NewInmemoryListener()
	defer ln.Close()

	respBytes := []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 42\r\nConnection: keep-alive\r\n\r\n{\"status\":\"ok\",\"engine\":\"aoni-fast\"}\n")

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				buf := make([]byte, 2048)
				for {
					n, err := conn.Read(buf)
					if err != nil || n == 0 {
						return
					}
					if _, err := conn.Write(respBytes); err != nil {
						return
					}
				}
			}(c)
		}
	}()

	// 2. Real aoni/fast client
	client := fast.NewClient(
		option.WithBaseURL("http://inmemory"),
		option.WithTimeout(5*time.Second),
	)
	client.Engine().Dial = func(_ string) (net.Conn, error) {
		return ln.Dial()
	}

	workers := 12
	totalReqs := int64(1200000)
	reqsPerWorker := totalReqs / int64(workers)

	var completed int64
	var wg sync.WaitGroup
	wg.Add(workers)

	start := time.Now()

	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			ctx := context.Background()

			for i := int64(0); i < reqsPerWorker; i++ {
				resp, err := client.Request(ctx, "GET", "/test")
				if err != nil {
					return
				}
				_ = resp.Close()
				atomic.AddInt64(&completed, 1)
			}
		}()
	}

	wg.Wait()
	dur := time.Since(start)
	totalDone := atomic.LoadInt64(&completed)

	rps := float64(totalDone) / dur.Seconds()
	avgLat := float64(dur.Nanoseconds()) / float64(totalDone) * float64(workers)

	fmt.Printf("=================================================================\n")
	fmt.Printf("         REAL AONI/FAST.CLIENT PARALLEL RPS BENCHMARK            \n")
	fmt.Printf("=================================================================\n")
	fmt.Printf("Compiler:         %s\n", runtime.Version())
	fmt.Printf("Concurrency:      %d Workers\n", workers)
	fmt.Printf("Total Requests:   %d\n", totalDone)
	fmt.Printf("Real Client RPS:  %.2f req/sec\n", rps)
	fmt.Printf("Average Latency:  %.2f ns (%.3f µs)\n", avgLat, avgLat/1000.0)
	fmt.Printf("=================================================================\n")
}
