// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"

	"github.com/lemon4ksan/aoni/internal/codegen/spec"
)

func runExample(kind, outFile string) {
	if kind == "" || kind == "list" || kind == "help" {
		fmt.Print(spec.PrintExampleHelp())
		return
	}

	ex := spec.LookupExample(kind)
	if ex == nil {
		fmt.Fprintf(os.Stderr, "vortex: unknown template kind %q\n\n", kind)
		fmt.Print(spec.PrintExampleHelp())
		os.Exit(1)
	}

	if outFile != "" {
		if err := os.WriteFile(outFile, []byte(ex.SourceCode), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "vortex: failed writing %s: %v\n", outFile, err)
			os.Exit(1)
		}

		fmt.Printf("✔ Successfully created %s template (%s) in %s\n", ex.Kind, ex.Title, outFile)

		return
	}

	fmt.Print(ex.SourceCode)
}
