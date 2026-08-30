package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
)

var respBytes = []byte("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 2\r\nConnection: keep-alive\r\n\r\nOK")

func startMockServer() (net.Listener, string) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				if tcpConn, ok := c.(*net.TCPConn); ok {
					_ = tcpConn.SetNoDelay(true)
				}
				buf := make([]byte, 8192)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					req := buf[:n]
					numReqs := bytes.Count(req, []byte("\r\n\r\n"))
					if numReqs == 0 {
						numReqs = 1
					}
					if numReqs == 1 {
						if _, wErr := c.Write(respBytes); wErr != nil {
							return
						}
					} else {
						batch := bytes.Repeat(respBytes, numReqs)
						if _, wErr := c.Write(batch); wErr != nil {
							return
						}
					}
				}
			}(conn)
		}
	}()

	return listener, listener.Addr().String()
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	listener, addr := startMockServer()
	defer listener.Close()

	targetURL := "http://" + addr + "/"

	client := fast.NewClient()

	// Warmup
	req := fast.NewRequest(nil)
	req.SetURIBytes([]byte(targetURL))
	resp, err := client.Do(req)
	if err == nil {
		resp.Close()
	}
	req.Release()

	// Start CPU Profiling
	cpuFile, err := os.Create("cpu.pprof")
	if err != nil {
		panic(err)
	}
	defer cpuFile.Close()

	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		panic(err)
	}

	fmt.Printf(">>> Profiling 12 threads x 256 in-flight pipeline for 5 seconds against %s...\n", targetURL)

	var completed atomic.Uint64
	var stop atomic.Bool

	var wg sync.WaitGroup
	wg.Add(12)

	startTime := time.Now()

	for i := 0; i < 12; i++ {
		go func() {
			defer wg.Done()

			batchSize := 256
			reqs := make([]*h1engine.Request, batchSize)
			resps := make([]*h1engine.Response, batchSize)
			for j := 0; j < batchSize; j++ {
				r := h1engine.AcquireRequest()
				r.Header.SetMethodBytes([]byte("GET"))
				r.SetRequestURIBytes([]byte(targetURL))
				reqs[j] = r
				resps[j] = h1engine.AcquireResponse()
			}

			rawClient := client.Unwrap()

			for !stop.Load() {
				err := rawClient.DoPipeline(reqs, resps)
				if err == nil {
					completed.Add(uint64(batchSize))
				}
			}

			for j := 0; j < batchSize; j++ {
				h1engine.ReleaseRequest(reqs[j])
				h1engine.ReleaseResponse(resps[j])
			}
		}()
	}

	time.Sleep(5 * time.Second)
	stop.Store(true)
	wg.Wait()

	pprof.StopCPUProfile()

	elapsed := time.Since(startTime).Seconds()
	totalReqs := completed.Load()
	rps := float64(totalReqs) / elapsed

	fmt.Printf("====================================================================\n")
	fmt.Printf(" Completed: %d requests in %.3f seconds\n", totalReqs, elapsed)
	fmt.Printf(" Average RPS: %.0f RPS\n", rps)
	fmt.Printf(" CPU profile written to cpu.pprof\n")
	fmt.Printf("====================================================================\n")
}
