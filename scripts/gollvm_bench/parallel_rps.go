// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// In-Memory Fast Pipe / Reactor for Contention-Free Multi-Core HTTP Benchmark
type InmemoryPipeListener struct {
	conns chan net.Conn
	done  chan struct{}
}

func NewInmemoryPipeListener() *InmemoryPipeListener {
	return &InmemoryPipeListener{
		conns: make(chan net.Conn, 1024),
		done:  make(chan struct{}),
	}
}

func (l *InmemoryPipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, io.EOF
	}
}

func (l *InmemoryPipeListener) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}

func (l *InmemoryPipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 80}
}

func (l *InmemoryPipeListener) Dial() (net.Conn, error) {
	c1, c2 := net.Pipe()
	l.conns <- c1
	return c2, nil
}

// HTTP Server Handler (zero-alloc response generator with keep-alive parser)
func startInmemoryHTTPServer(ln *InmemoryPipeListener) {
	respBytes := []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 42\r\nServer: aoni/silicon\r\n\r\n{\"status\":\"ok\",\"engine\":\"aoni-silicon\"}\n")

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
					// Parse HTTP request line & headers
					reqData := buf[:n]
					if !bytes.Contains(reqData, []byte("\r\n\r\n")) {
						continue
					}
					// Send HTTP response
					if _, err := conn.Write(respBytes); err != nil {
						return
					}
				}
			}(c)
		}
	}()
}

// Multi-Core High-Throughput HTTP Client Loop
func runParallelBenchmark(workers int, totalRequests int64) (rps float64, avgLatencyNs float64, p99LatencyNs float64) {
	ln := NewInmemoryPipeListener()
	defer ln.Close()
	startInmemoryHTTPServer(ln)

	reqPayload := []byte("GET /api/v1/resource HTTP/1.1\r\nHost: inmemory\r\nUser-Agent: aoni/gollvm-bench\r\nAccept: application/json\r\nConnection: keep-alive\r\n\r\n")

	reqsPerWorker := totalRequests / int64(workers)
	var completedCount int64

	var wg sync.WaitGroup
	wg.Add(workers)

	latencies := make([][]int64, workers)
	for i := range latencies {
		// Sample 1 out of 100 for latency recording
		latencies[i] = make([]int64, 0, reqsPerWorker/100+10)
	}

	start := time.Now()

	for w := 0; w < workers; w++ {
		workerID := w
		go func() {
			defer wg.Done()

			conn, err := ln.Dial()
			if err != nil {
				panic(err)
			}
			defer conn.Close()

			respBuf := make([]byte, 2048)
			sampleCounter := 0

			for i := int64(0); i < reqsPerWorker; i++ {
				var reqStart time.Time
				shouldSample := (sampleCounter % 100) == 0
				if shouldSample {
					reqStart = time.Now()
				}

				// 1. Send Request
				if _, err := conn.Write(reqPayload); err != nil {
					return
				}

				// 2. Read Response
				n, err := conn.Read(respBuf)
				if err != nil || n == 0 {
					return
				}

				// 3. Validate Framing
				if !bytes.Contains(respBuf[:n], []byte("200 OK")) {
					panic("invalid HTTP response")
				}

				if shouldSample {
					lat := time.Since(reqStart).Nanoseconds()
					latencies[workerID] = append(latencies[workerID], lat)
				}
				sampleCounter++
				atomic.AddInt64(&completedCount, 1)
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	totalDone := atomic.LoadInt64(&completedCount)
	rps = float64(totalDone) / elapsed.Seconds()
	avgLatencyNs = float64(elapsed.Nanoseconds()) / float64(totalDone) * float64(workers)

	// Collect samples for P99
	var allSamples []int64
	for _, l := range latencies {
		allSamples = append(allSamples, l...)
	}
	if len(allSamples) > 0 {
		// Approximate P99
		quickSort(allSamples, 0, len(allSamples)-1)
		p99Idx := int(float64(len(allSamples)) * 0.99)
		if p99Idx >= len(allSamples) {
			p99Idx = len(allSamples) - 1
		}
		p99LatencyNs = float64(allSamples[p99Idx])
	}

	return rps, avgLatencyNs, p99LatencyNs
}

func quickSort(arr []int64, low, high int) {
	if low < high {
		p := partition(arr, low, high)
		quickSort(arr, low, p-1)
		quickSort(arr, p+1, high)
	}
}

func partition(arr []int64, low, high int) int {
	pivot := arr[high]
	i := low - 1
	for j := low; j < high; j++ {
		if arr[j] < pivot {
			i++
			arr[i], arr[j] = arr[j], arr[i]
		}
	}
	arr[i+1], arr[high] = arr[high], arr[i+1]
	return i + 1
}

func main() {
	runtime.GOMAXPROCS(12)
	fmt.Printf("=================================================================\n")
	fmt.Printf("        AONI PARALLEL MULTI-CORE RPS & LATENCY STRESS TEST       \n")
	fmt.Printf("=================================================================\n")
	fmt.Printf("OS/Arch:      %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Compiler:     %s\n", runtime.Version())
	fmt.Printf("GOMAXPROCS:   %d CPU Cores\n", runtime.GOMAXPROCS(0))
	fmt.Printf("Concurrency:  12 Parallel Workers\n")
	fmt.Printf("Transactions: 2,400,000 HTTP Requests (In-Memory Reactor)\n")
	fmt.Printf("=================================================================\n\n")

	// Warmup
	_, _, _ = runParallelBenchmark(4, 100000)

	// Run Main Test (2.4M requests across 12 cores)
	totalReqs := int64(2400000)
	workers := 12

	rps, avgLat, p99Lat := runParallelBenchmark(workers, totalReqs)

	fmt.Printf("Throughput (RPS):  %14.2f req/sec\n", rps)
	fmt.Printf("Average Latency:   %14.2f ns/req (%.3f µs)\n", avgLat, avgLat/1000.0)
	fmt.Printf("P99 Tail Latency:  %14.2f ns     (%.3f µs)\n", p99Lat, p99Lat/1000.0)
	fmt.Printf("Total Completed:   %14d requests\n", totalReqs)
	fmt.Printf("-----------------------------------------------------------------\n")
}
