// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
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
	{"./netutil/dict", "FuzzParseUseAsDictionary"},
	{"./netutil/dict", "FuzzParseAvailableDictionary"},
	{"./netutil/dict", "FuzzMatchURLPattern"},
	{"./netutil/hints", "FuzzParseLinkHeader"},
	{"./realtime/stream", "FuzzSSEStream"},
	{"./realtime/stream", "FuzzNDJSONStream"},
	{"./realtime/ws", "FuzzWSFrameParse"},
	{"./realtime/ws", "FuzzWSCloseMessage"},
	{"./realtime/ws", "FuzzWSMask"},
	{"./realtime/ws", "FuzzWSAcceptKey"},
	{"./tunnel/masque", "FuzzMASQUEVarint"},
	{"./tunnel/masque", "FuzzIPPacketExtract"},
	{"./codec/values", "FuzzValuesEncode"},
	{"./codec/extract", "FuzzExtract"},
	{"./codec/decode", "FuzzDecoders"},
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

		// #nosec G204
		cmd := exec.CommandContext(
			context.Background(),
			"go", "test",
			"-fuzz=^"+tgt.name+"$",
			"-fuzztime="+*fuzzDuration,
			tgt.pkg,
		)

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
