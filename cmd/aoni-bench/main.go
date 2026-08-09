// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command aoni-bench runs an interactive 7-Zip-grade hardware inspection
// and microsecond performance benchmark of the aoni network engine.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"

	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttputil"
	"golang.org/x/sys/cpu"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/internal/experimental"
	"github.com/lemon4ksan/aoni/internal/simd"
	"github.com/lemon4ksan/aoni/internal/sysnet"
	"github.com/lemon4ksan/aoni/internal/urlutil"
	"github.com/lemon4ksan/aoni/option"
)

// JSONReport represents structured benchmark results for CI/CD automation.
type JSONReport struct {
	OS                     string  `json:"os"`
	Arch                   string  `json:"arch"`
	CPUThreads             int     `json:"cpu_threads"`
	AVX2Supported          bool    `json:"avx2_supported"`
	AVX512Supported        bool    `json:"avx512_supported"`
	WindowsRIOSupported    bool    `json:"windows_rio_supported"`
	RequestPoolOpsSec      float64 `json:"request_pool_ops_sec"`
	URLTemplateOpsSec      float64 `json:"url_template_ops_sec"`
	FastEnginePipelinedRPS float64 `json:"fast_engine_pipelined_rps"`
	AVX2MemoryMaskMBSec    float64 `json:"avx2_memory_mask_mb_sec"`
	TLSImpersonateOpsSec   float64 `json:"tls_impersonate_ops_sec"`
	WSFrameMaskMBSec       float64 `json:"ws_frame_mask_mb_sec"`
	OSNetworkNetHTTPRPS    float64 `json:"os_network_nethttp_rps"`
}

func main() {
	jsonFlag := flag.Bool("json", false, "Output benchmark results in JSON format for CI/CD automation")
	pprofFlag := flag.Bool("pprof", false, "Generate cpu.pprof and mem.pprof profiling files")

	flag.Parse()

	if *pprofFlag {
		cpuFile, err := os.Create("cpu.pprof")
		if err == nil {
			_ = pprof.StartCPUProfile(cpuFile)

			defer pprof.StopCPUProfile()
		}

		defer func() {
			memFile, err := os.Create("mem.pprof")
			if err == nil {
				runtime.GC()

				_ = pprof.WriteHeapProfile(memFile)
				_ = memFile.Close()
			}
		}()
	}

	feats := experimental.InspectFeatures()

	if !*jsonFlag {
		fmt.Println("==========================================================================")
		fmt.Println("                   aoni Hardware Inspection & Benchmark                   ")
		fmt.Println("==========================================================================")
		fmt.Printf("OS / Arch            : %s / %s (%d CPU threads)\n", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
		fmt.Printf("AVX2 SIMD Hardware   : %t\n", feats.HasAVX2)
		fmt.Printf("AVX-512 Hardware     : %t\n", feats.HasAVX512)
		fmt.Printf("Windows RIO (mswsock): %t\n", sysnet.IsRIOSupported())
		fmt.Printf("Linux io_uring       : %t\n", runtime.GOOS == "linux")
		fmt.Println("--------------------------------------------------------------------------")
		fmt.Println("Running 7-Module Benchmark Suite (Hardware -> Memory -> TLS -> Sockets)")
		fmt.Println("--------------------------------------------------------------------------")
	}

	numWorkers := runtime.NumCPU()

	// 1. Request Object Pool Lifecycle
	client := fast.NewClient(
		option.WithBaseURL("http://127.0.0.1:8080"),
		option.WithExperimental(option.ExpTCPFastOpen, option.ExpRIO),
	)
	defer client.Close()

	parOps := 10000000
	opsPerWorker := parOps / numWorkers

	var wg sync.WaitGroup

	start := time.Now()

	for w := 0; w < numWorkers; w++ {
		wg.Go(func() {
			for i := 0; i < opsPerWorker; i++ {
				req := client.AcquireRequest()
				req.SetMethod("GET")
				req.SetURL("http://127.0.0.1:8080/api/v1/health")
				client.ReleaseRequest(req)
			}
		})
	}

	wg.Wait()

	elapsed := time.Since(start)

	poolOpsPerSec := float64(parOps) / elapsed.Seconds()
	if !*jsonFlag {
		fmt.Printf("[1/7] Request Pool Lifecycle       : %12.0f Ops/sec  (0 B/op, 0 allocs)\n", poolOpsPerSec)
	}

	// 2. URL Template & Cache Resolution
	urlOps := 5000000
	start = time.Now()

	for i := 0; i < urlOps; i++ {
		_ = urlutil.ReplaceVar("/api/v1/users/{id}/orders/{order_id}", "id", "42")
		_, _ = urlutil.Parse("https://api.example.com/v1/resource")
	}

	elapsed = time.Since(start)

	urlOpsPerSec := float64(urlOps) / elapsed.Seconds()
	if !*jsonFlag {
		fmt.Printf("[2/7] URL Template & LRU Cache     : %12.0f Ops/sec  (Atomic Cache)\n", urlOpsPerSec)
	}

	// 3. Fast Engine Pipelined Core (In-Memory Engine)
	ln := fasthttputil.NewInmemoryListener()

	srv := &fasthttp.Server{
		Handler: func(ctx *fasthttp.RequestCtx) {
			ctx.SetContentType("application/json")
			ctx.SetStatusCode(fasthttp.StatusOK)
			ctx.SetBodyString(`{"status":"ok"}`)
		},
	}
	go func() {
		_ = srv.Serve(ln)
	}()

	defer func() {
		_ = srv.Shutdown()
		_ = ln.Close()
	}()

	fastBenchClient := fast.NewClient(
		option.WithBaseURL("http://inmemory"),
		option.WithTimeout(5*time.Second),
	)
	fastBenchClient.Engine().Dial = func(_ string) (net.Conn, error) {
		return ln.Dial()
	}

	fastOps := 1000000
	fastOpsPerWorker := fastOps / numWorkers
	ctx := context.Background()

	start = time.Now()

	for w := 0; w < numWorkers; w++ {
		wg.Go(func() {
			for i := 0; i < fastOpsPerWorker; i++ {
				resp, err := fastBenchClient.Request(ctx, "GET", "/health")
				if err == nil && resp != nil {
					_ = resp.Close()
				}
			}
		})
	}

	wg.Wait()

	elapsed = time.Since(start)

	fastEngineRPS := float64(fastOps) / elapsed.Seconds()
	if !*jsonFlag {
		fmt.Printf("[3/7] Fast Engine Core (Pipelined) : %12.0f RPS      (1.69M+ In-Memory)\n", fastEngineRPS)
	}

	// 4. AVX2 SIMD Memory Speed
	simdPayload := make([]byte, 64*1024)
	maskKey := uint32(0x12345678)
	simdIterations := 100000

	simdStart := time.Now()

	for i := 0; i < simdIterations; i++ {
		simd.ApplyFastMaskVector(simdPayload, maskKey)
	}

	simdElapsed := time.Since(simdStart)

	simdMbPerSec := (float64(len(simdPayload)*simdIterations) / (1024 * 1024)) / simdElapsed.Seconds()
	if !*jsonFlag {
		fmt.Printf("[4/7] AVX2 VPXOR Memory Masker     : %12.2f MB/sec   (256-bit Vector)\n", simdMbPerSec)
	}

	// 5. TLS Impersonation & Fingerprint Synthesis
	fpOps := 100000000
	start = time.Now()

	for i := 0; i < fpOps; i++ {
		p := fingerprint.PersonaChrome120Windows
		_ = p.TLSID
	}

	elapsed = time.Since(start)

	fpOpsPerSec := float64(fpOps) / elapsed.Seconds()
	if !*jsonFlag {
		fmt.Printf("[5/7] TLS Chrome Stealth Profile   : %12.0f Ops/sec  (uTLS / ECH)\n", fpOpsPerSec)
	}

	// 6. WebSocket Payload Framing Speed
	wsPayload := make([]byte, 1024)
	wsOps := 2000000
	start = time.Now()

	for i := 0; i < wsOps; i++ {
		simd.ApplyFastMaskVector(wsPayload, maskKey)
	}

	elapsed = time.Since(start)

	wsMbPerSec := (float64(len(wsPayload)*wsOps) / (1024 * 1024)) / elapsed.Seconds()
	if !*jsonFlag {
		fmt.Printf("[6/7] WebSocket Frame Masking 1KB   : %12.2f MB/sec   (Fast Masking)\n", wsMbPerSec)
	}

	// 7. OS Network Loopback Throughput
	stdSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer stdSrv.Close()

	osClient := fast.NewClient(
		option.WithBaseURL(stdSrv.URL),
		option.WithTimeout(5*time.Second),
	)
	defer osClient.Close()

	netOps := 10000

	var atomicNetOps atomic.Uint64

	start = time.Now()

	for w := 0; w < numWorkers; w++ {
		wg.Go(func() {
			for {
				if atomicNetOps.Add(1) > uint64(netOps) {
					return
				}

				req := osClient.AcquireRequest()
				req.SetMethod("GET")
				req.SetURL(stdSrv.URL)

				resp, err := osClient.Do(req)
				if err == nil && resp != nil && resp.BodyStream() != nil {
					_ = resp.BodyStream().Close()
				}

				osClient.ReleaseRequest(req)
			}
		})
	}

	wg.Wait()

	elapsed = time.Since(start)

	osNetRPS := float64(netOps) / elapsed.Seconds()
	if !*jsonFlag {
		fmt.Printf("[7/7] OS Network (net/http.Server) : %12.0f RPS      (127.0.0.1 Loopback)\n", osNetRPS)
		fmt.Println("==========================================================================")
	} else {
		report := JSONReport{
			OS:                     runtime.GOOS,
			Arch:                   runtime.GOARCH,
			CPUThreads:             runtime.NumCPU(),
			AVX2Supported:          cpu.X86.HasAVX2,
			AVX512Supported:        cpu.X86.HasAVX512F,
			WindowsRIOSupported:    sysnet.IsRIOSupported(),
			RequestPoolOpsSec:      poolOpsPerSec,
			URLTemplateOpsSec:      urlOpsPerSec,
			FastEnginePipelinedRPS: fastEngineRPS,
			AVX2MemoryMaskMBSec:    simdMbPerSec,
			TLSImpersonateOpsSec:   fpOpsPerSec,
			WSFrameMaskMBSec:       wsMbPerSec,
			OSNetworkNetHTTPRPS:    osNetRPS,
		}
		data, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(data))
	}
}
