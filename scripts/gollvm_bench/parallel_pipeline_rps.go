// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

var huffmanCodes = [256]uint32{
	0x1ff8, 0x7ff72, 0x7ff73, 0x7ff74, 0x7ff75, 0x7ff76, 0x7ff77, 0x7ff78,
	0x7ff79, 0x7ff7a, 0x7ff7b, 0x7ff7c, 0x7ff7d, 0x7ff7e, 0x7ff7f, 0x7ff80,
	0x7ff81, 0x7ff82, 0x7ff83, 0x7ff84, 0x7ff85, 0x7ff86, 0x7ff87, 0x7ff88,
	0x7ff89, 0x7ff8a, 0x7ff8b, 0x7ff8c, 0x7ff8d, 0x7ff8e, 0x7ff8f, 0x7ff90,
	0x14, 0x3f8, 0x3f9, 0xffa, 0x1ff9, 0x15, 0xf8, 0x7fa,
	0x3fa, 0x3fb, 0xf9, 0x7fb, 0xfa, 0x16, 0x17, 0x18,
	0x0, 0x1, 0x2, 0x19, 0x1a, 0x1b, 0x1c, 0x1d,
	0x1e, 0x1f, 0x5c, 0xfb, 0x7ff91, 0x1ffb, 0x7ff92, 0xffb,
	0x7ff93, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26,
	0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e,
	0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36,
	0x37, 0x38, 0x39, 0x5d, 0x7ff94, 0x7ff95, 0x7ff96, 0x7c,
	0x7ff97, 0x3, 0x2, 0x4, 0x5, 0x6, 0x7, 0x8,
	0x9, 0xa, 0xb, 0xc, 0xd, 0xe, 0xf, 0x10,
	0x11, 0x12, 0x13, 0x20, 0x21, 0x22, 0x23, 0x24,
	0x25, 0x26, 0x27, 0x7ff98, 0x7ff99, 0x7ff9a, 0x7ff9b, 0x7ff9c,
}

var huffmanBits = [256]uint8{
	13, 23, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28,
	28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28,
	6, 10, 10, 12, 13, 6, 8, 11, 10, 10, 8, 11, 8, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 7, 8, 23, 13, 23, 12,
	23, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 7, 23, 23, 23, 7,
	23, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 6, 6, 6, 6, 6, 6, 6, 6, 23, 23, 23, 23, 23,
}

func encodeHuffmanFast(dst []byte, src []byte) int {
	var cur uint64
	var n uint
	idx := 0
	for _, b := range src {
		code := uint64(huffmanCodes[b])
		bits := uint(huffmanBits[b])
		cur = (cur << bits) | code
		n += bits
		for n >= 8 {
			n -= 8
			dst[idx] = byte(cur >> n)
			idx++
		}
	}
	if n > 0 {
		dst[idx] = byte((cur << (8 - n)) | (1<<(8-n) - 1))
		idx++
	}
	return idx
}

func caseFoldMatch(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		c1, c2 := a[i], b[i]
		if c1 >= 'A' && c1 <= 'Z' {
			c1 += 'a' - 'A'
		}
		if c2 >= 'A' && c2 <= 'Z' {
			c2 += 'a' - 'A'
		}
		if c1 != c2 {
			return false
		}
	}
	return true
}

type PipelineListener struct {
	conns chan net.Conn
	done  chan struct{}
}

func NewPipelineListener() *PipelineListener {
	return &PipelineListener{
		conns: make(chan net.Conn, 1024),
		done:  make(chan struct{}),
	}
}

func (l *PipelineListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, io.EOF
	}
}

func (l *PipelineListener) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}

func (l *PipelineListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 80}
}

func (l *PipelineListener) Dial() (net.Conn, error) {
	c1, c2 := net.Pipe()
	l.conns <- c1
	return c2, nil
}

func startPipelineHTTPServer(ln *PipelineListener) {
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
					if _, err := conn.Write(respBytes); err != nil {
						return
					}
				}
			}(c)
		}
	}()
}

func runParallelFullPipeline(workers int, totalRequests int64) (rps float64, avgLatNs float64) {
	ln := NewPipelineListener()
	defer ln.Close()
	startPipelineHTTPServer(ln)

	reqPayload := []byte("GET /api/v1/resource HTTP/1.1\r\nHost: inmemory\r\nUser-Agent: aoni-silicon-client/v1.0\r\nAccept: application/json\r\nConnection: keep-alive\r\n\r\n")
	headersToMatch := "accept"

	reqsPerWorker := totalRequests / int64(workers)
	var completedCount int64

	var wg sync.WaitGroup
	wg.Add(workers)

	start := time.Now()

	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()

			conn, err := ln.Dial()
			if err != nil {
				panic(err)
			}
			defer conn.Close()

			respBuf := make([]byte, 2048)
			hpackDst := make([]byte, 512)
			varintBuf := make([]byte, 10)

			for i := int64(0); i < reqsPerWorker; i++ {
				// 1. HTTP Socket I/O
				if _, err := conn.Write(reqPayload); err != nil {
					return
				}
				n, err := conn.Read(respBuf)
				if err != nil || n == 0 {
					return
				}

				// 2. Compute: Header matching
				_ = caseFoldMatch("Accept", headersToMatch)

				// 3. Compute: HPACK Huffman compression of request path
				_ = encodeHuffmanFast(hpackDst, reqPayload[:30])

				// 4. Compute: Varint packet framing
				binary.PutUvarint(varintBuf, uint64(n))

				atomic.AddInt64(&completedCount, 1)
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	totalDone := atomic.LoadInt64(&completedCount)
	rps = float64(totalDone) / elapsed.Seconds()
	avgLatNs = float64(elapsed.Nanoseconds()) / float64(totalDone) * float64(workers)
	return rps, avgLatNs
}

func main() {
	runtime.GOMAXPROCS(12)
	fmt.Printf("=================================================================\n")
	fmt.Printf("   AONI FULL PROTOCOL PIPELINE PARALLEL BENCHMARK (12 CORES)     \n")
	fmt.Printf("   Workload: HTTP Socket I/O + HPACK Huffman + Varint + Match    \n")
	fmt.Printf("=================================================================\n")
	fmt.Printf("OS/Arch:      %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Compiler:     %s\n", runtime.Version())
	fmt.Printf("Concurrency:  12 Parallel Workers (1,200,000 Total Transactions)\n")
	fmt.Printf("=================================================================\n\n")

	totalReqs := int64(1200000)
	workers := 12

	rps, avgLat := runParallelFullPipeline(workers, totalReqs)

	fmt.Printf("Pipeline Throughput (RPS):  %14.2f req/sec\n", rps)
	fmt.Printf("Average Request Latency:    %14.2f ns (%.3f µs)\n", avgLat, avgLat/1000.0)
	fmt.Printf("Total Processed:            %14d requests\n", totalReqs)
	fmt.Printf("-----------------------------------------------------------------\n")
}
