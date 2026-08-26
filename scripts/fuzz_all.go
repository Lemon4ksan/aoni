// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type fuzzTarget struct {
	pkg  string
	name string
}

var targets = []fuzzTarget{
	{"./cookie", "FuzzParseSetCookieHeader"},
	{"./cookie", "FuzzNetscapeCookieExport"},
	{"./cookie", "FuzzProxyIsolatedJar"},
	{"./fingerprint/ja4", "FuzzParseExtensionsFromRaw"},
	{"./fingerprint/ja4", "FuzzComputeJA4"},
	{"./fingerprint/ja4", "FuzzComputeJA4H"},
	{"./grpc", "FuzzGRPCWebFraming"},
	{"./grpc", "FuzzGRPCStatusParsing"},
	{"./grpc", "FuzzGRPCTimeoutFormat"},
	{"./internal/fast/h2engine", "FuzzHPACKDecode"},
	{"./internal/fast/h2engine", "FuzzFrameRead"},
	{"./internal/fast/h3engine", "FuzzQPACKDecode"},
	{"./internal/fast/h3engine", "FuzzH3FrameHeaderRead"},
	{"./realtime/stream", "FuzzSSEStream"},
	{"./realtime/stream", "FuzzNDJSONStream"},
	{"./tunnel/masque", "FuzzMASQUEVarint"},
	{"./tunnel/masque", "FuzzIPPacketExtract"},
	{"./x/otel", "FuzzParseTraceParent"},
	{"./x/otel", "FuzzParseTraceID"},
	{"./x/otel", "FuzzParseSpanID"},
	{"./x/otel", "FuzzOTLPSpanAttributesSerialization"},
	{"./x/otel", "FuzzCarrierPropagation"},
	{"./netutil/dict", "FuzzParseUseAsDictionary"},
	{"./netutil/dict", "FuzzParseAvailableDictionary"},
	{"./netutil/dict", "FuzzMatchURLPattern"},
	{"./netutil/priority", "FuzzParsePriority"},
	{"./netutil/hints", "FuzzParseLinkHeader"},
	{"./netutil/nik", "FuzzNetworkIsolationKey"},
	{"./netutil/privacypass", "FuzzUnmarshalTokenChallenge"},
	{"./netutil/privacypass", "FuzzUnmarshalToken"},
	{"./netutil/privacypass", "FuzzParseWWWAuthenticate"},
	{"./netutil/spki", "FuzzSPKI"},
	{"./netutil/basic", "FuzzBasicAuth"},
	{"./netutil/bearer", "FuzzBearerAuth"},
	{"./netutil/sanitize", "FuzzSanitize"},
	{"./fingerprint/p0f", "FuzzParseP0fSignature"},
	{"./fingerprint/ech", "FuzzParseECHConfigList"},
	{"./fingerprint/ech", "FuzzParseECHBase64"},
	{"./fingerprint/h2", "FuzzParseSettings"},
	{"./resiliency/etag", "FuzzETag"},
	{"./codec/values", "FuzzValuesEncode"},
	{"./codec/extract", "FuzzExtract"},
	{"./codec/decode", "FuzzDecoders"},
	{"./realtime/ws", "FuzzWSFrameParse"},
	{"./realtime/ws", "FuzzWSCloseMessage"},
	{"./realtime/ws", "FuzzWSMask"},
	{"./realtime/ws", "FuzzWSAcceptKey"},
	{"./realtime/socket", "FuzzLengthPrefixedFramer"},
	{"./internal/fast/h1engine", "FuzzH1Request"},
	{"./internal/fast/h1engine", "FuzzH1Response"},
	{"./internal/fast/h1engine", "FuzzH1URI"},
	{"./internal/quic/quicvarint", "FuzzQUICVarint"},
	{"./internal/quic/internal/wire", "FuzzQUICFrameParser"},
	{"./webpush", "FuzzVAPIDKeys"},
	{"./webpush", "FuzzWebPushDecrypt"},
}

func main() {
	fuzzDuration := flag.String("fuzztime", "5s", "duration to fuzz each target")
	flag.Parse()

	fmt.Printf("=== Starting Heavy Fuzzing Suite (%d targets, %s each) ===\n\n", len(targets), *fuzzDuration)

	var failed []string
	startTotal := time.Now()

	for i, tgt := range targets {
		fmt.Printf("[%2d/%2d] Fuzzing %s :: %s (fuzztime=%s) ... ", i+1, len(targets), tgt.pkg, tgt.name, *fuzzDuration)
		start := time.Now()

		cmd := exec.Command("go", "test", "-fuzz=^"+tgt.name+"$", "-fuzztime="+*fuzzDuration, tgt.pkg)
		var outBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &outBuf

		err := cmd.Run()
		elapsed := time.Since(start).Round(time.Millisecond)

		if err != nil {
			fmt.Printf("FAILED (%s)\n", elapsed)
			fmt.Println("----------------- OUTPUT -----------------")
			fmt.Println(strings.TrimSpace(outBuf.String()))
			fmt.Println("------------------------------------------")
			failed = append(failed, fmt.Sprintf("%s :: %s", tgt.pkg, tgt.name))
		} else {
			fmt.Printf("PASSED (%s)\n", elapsed)
		}
	}

	totalElapsed := time.Since(startTotal).Round(time.Second)
	fmt.Printf("\n=== Fuzzing Suite Completed in %s ===\n", totalElapsed)

	if len(failed) > 0 {
		fmt.Printf("FAILURES (%d targets failed):\n", len(failed))
		for _, f := range failed {
			fmt.Printf("  - %s\n", f)
		}
		os.Exit(1)
	}

	fmt.Printf("SUCCESS: All %d fuzz targets passed with 0 panics and 0 errors!\n", len(targets))
}
