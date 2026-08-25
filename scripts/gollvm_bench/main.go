// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"time"
)

// 1. SWAR / Vectorized Delimiter Search (HTTP/1.1 & HTTP/2 framing)

func BenchmarkSWARDelimiterScan(data []byte, iterations int) (time.Duration, int64) {
	needle := []byte("\r\n\r\n")
	start := time.Now()
	matches := 0
	for i := 0; i < iterations; i++ {
		pos := 0
		for pos+4 <= len(data) {
			idx := bytes.Index(data[pos:], needle)
			if idx < 0 {
				break
			}
			matches++
			pos += idx + 4
		}
	}
	return time.Since(start), int64(iterations * len(data))
}

// 2. HPACK Huffman Bit-Stream Encoder (HTTP/2 / HTTP/3 QPACK)
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

// 2. WebSocket Vector Frame Masking (RFC 6455 64-bit unrolled XOR)

//go:noinline
func BenchmarkWebSocketMasking(payload []byte, mask [4]byte, iterations int) (time.Duration, int64) {
	maskKey := uint64(binary.LittleEndian.Uint32(mask[:]))
	maskKey = maskKey | (maskKey << 32)

	start := time.Now()
	for iter := 0; iter < iterations; iter++ {
		p := payload
		// 8-byte unrolled loop
		for len(p) >= 8 {
			v := binary.LittleEndian.Uint64(p)
			binary.LittleEndian.PutUint64(p, v^maskKey)
			p = p[8:]
		}
		for i := 0; i < len(p); i++ {
			p[i] ^= mask[i%4]
		}
	}
	return time.Since(start), int64(iterations * len(payload))
}

func BenchmarkHPACKHuffman(input []byte, iterations int) (time.Duration, int64) {
	dst := make([]byte, len(input)*2+32)
	start := time.Now()
	totalBytes := int64(0)
	for i := 0; i < iterations; i++ {
		var cur uint64
		var n uint
		idx := 0
		for _, b := range input {
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
		totalBytes += int64(len(input))
	}
	return time.Since(start), totalBytes
}

// 3. CRC32 / FNV-1a Hash (L1 Fast Table Routing & Cache Indexing)

//go:noinline
func BenchmarkFNV1aHash(data []byte, iterations int) (time.Duration, int64, uint64) {
	start := time.Now()
	var dummy uint64
	for i := 0; i < iterations; i++ {
		h := uint64(14695981039346656037)
		for _, b := range data {
			h ^= uint64(b)
			h *= 1099511628211
		}
		dummy += h
	}
	return time.Since(start), int64(iterations * len(data)), dummy
}

// 4. Case-Insensitive Header Matcher (ASCII Vectorized Lowercasing & Comparison)

//go:noinline
func BenchmarkHeaderCaseFold(headers []string, iterations int) (time.Duration, int64, int) {
	target := "accept-encoding"
	start := time.Now()
	matches := 0
	for i := 0; i < iterations; i++ {
		for _, h := range headers {
			if len(h) == len(target) {
				equal := true
				for j := 0; j < len(h); j++ {
					c1 := h[j]
					c2 := target[j]
					if c1 >= 'A' && c1 <= 'Z' {
						c1 += 'a' - 'A'
					}
					if c1 != c2 {
						equal = false
						break
					}
				}
				if equal {
					matches++
				}
			}
		}
	}
	return time.Since(start), int64(iterations * len(headers)), matches
}

// 5. QUIC / Protobuf Varint Fast Encoder & Decoder

//go:noinline
func BenchmarkVarintCodec(values []uint64, iterations int) (time.Duration, int64, uint64) {
	buf := make([]byte, 10)
	start := time.Now()
	var sum uint64
	for i := 0; i < iterations; i++ {
		for _, v := range values {
			n := binary.PutUvarint(buf, v)
			dec, _ := binary.Uvarint(buf[:n])
			sum += dec
		}
	}
	return time.Since(start), int64(iterations * len(values)), sum
}

// 6. EWMA Latency Tracker & Jitter Calculator (Float64 Vectorized Arithmetic)

//go:noinline
func BenchmarkEWMALatencyJitter(samples []float64, iterations int) (time.Duration, int64, float64) {
	alpha := 0.125
	beta := 0.250
	start := time.Now()
	var finalEWMA float64
	for i := 0; i < iterations; i++ {
		rtt := samples[0]
		variance := rtt / 2.0
		for _, s := range samples {
			diff := s - rtt
			if diff < 0 {
				diff = -diff
			}
			variance = (1.0-beta)*variance + beta*diff
			rtt = (1.0-alpha)*rtt + alpha*s
		}
		finalEWMA += rtt + 4.0*variance
	}
	return time.Since(start), int64(iterations * len(samples)), finalEWMA
}

func main() {
	fmt.Printf("=== Aoni Silicon Subsystem Benchmark [%s/%s] ===\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Compiler: %s | GOMAXPROCS: %d\n\n", runtime.Version(), runtime.GOMAXPROCS(0))

	// Data preparation
	rand.Seed(42)
	payload1MB := make([]byte, 1024*1024)
	for i := range payload1MB {
		payload1MB[i] = byte(rand.Intn(256))
	}
	// Insert delimiters
	for i := 1000; i < len(payload1MB)-10; i += 2048 {
		copy(payload1MB[i:], []byte("\r\n\r\n"))
	}

	hpackHeaders := []byte("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36 aoni/v1.0.0 (High-Performance Silicon Reactor)")

	headerList := []string{
		"Host", "User-Agent", "Accept", "Accept-Encoding", "ACCEPT-ENCODING",
		"Content-Type", "Authorization", "X-Request-ID", "accept-encoding",
		"Transfer-Encoding", "Sec-WebSocket-Key", "Sec-WebSocket-Version",
	}

	varintValues := make([]uint64, 1024)
	for i := range varintValues {
		varintValues[i] = uint64(rand.Int63n(1 << 60))
	}

	ewmaSamples := make([]float64, 2048)
	for i := range ewmaSamples {
		ewmaSamples[i] = 0.5 + rand.Float64()*15.0
	}

	// Warmup
	_, _ = BenchmarkSWARDelimiterScan(payload1MB[:4096], 10)

	// 1. SWAR Delimiter Scan
	dur1, bytes1 := BenchmarkSWARDelimiterScan(payload1MB, 200)
	mb1 := float64(bytes1) / (1024 * 1024) / dur1.Seconds()
	fmt.Printf("SWAR_Delimiter_Scan_1MB:       %10.2f MB/s  (%v for %d MB)\n", mb1, dur1, bytes1/(1024*1024))

	// 2. WebSocket 64-bit Vector Frame Masking
	mask := [4]byte{0xfa, 0xce, 0xba, 0xbe}
	durWS, bytesWS := BenchmarkWebSocketMasking(payload1MB, mask, 500)
	mbWS := float64(bytesWS) / (1024 * 1024) / durWS.Seconds()
	fmt.Printf("WebSocket_Frame_Masking_XOR:   %10.2f MB/s  (%v for %d MB)\n", mbWS, durWS, bytesWS/(1024*1024))

	// 3. HPACK Huffman Encoder
	dur2, bytes2 := BenchmarkHPACKHuffman(hpackHeaders, 200000)
	mb2 := float64(bytes2) / (1024 * 1024) / dur2.Seconds()
	nsOp2 := float64(dur2.Nanoseconds()) / 200000.0
	fmt.Printf("HPACK_Huffman_Encoder:         %10.2f MB/s  (%6.2f ns/op)\n", mb2, nsOp2)

	// 4. FNV1a Hash
	dur3, bytes3, hashVal := BenchmarkFNV1aHash(payload1MB[:65536], 1000)
	mb3 := float64(bytes3) / (1024 * 1024) / dur3.Seconds()
	fmt.Printf("FNV1a_Fast_Hash_64KB:          %10.2f MB/s  (%v, check=0x%x)\n", mb3, dur3, hashVal)

	// 5. Header Case-Fold Matcher
	dur4, items4, matchVal := BenchmarkHeaderCaseFold(headerList, 200000)
	nsOp4 := float64(dur4.Nanoseconds()) / float64(items4)
	fmt.Printf("Header_CaseFold_ASCII:         %10.2f ns/match (%v, matches=%d)\n", nsOp4, dur4, matchVal)

	// 6. Varint Codec
	dur5, items5, sumVal := BenchmarkVarintCodec(varintValues, 10000)
	nsOp5 := float64(dur5.Nanoseconds()) / float64(items5)
	fmt.Printf("QUIC_Varint_EncodeDecode:      %10.2f ns/op    (%v, sum=%d)\n", nsOp5, dur5, sumVal)

	// 7. EWMA Latency & Jitter Filter
	dur6, items6, ewmaVal := BenchmarkEWMALatencyJitter(ewmaSamples, 20000)
	nsOp6 := float64(dur6.Nanoseconds()) / float64(items6)
	fmt.Printf("EWMA_Latency_Jitter_Filter:    %10.2f ns/sample (%v, val=%.2f)\n", nsOp6, dur6, ewmaVal)

	fmt.Println(strings.Repeat("-", 65))
}
