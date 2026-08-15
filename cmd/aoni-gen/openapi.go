// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io"
	"os"

	"github.com/lemon4ksan/aoni/internal/codegen/openapi"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

func runOpenAPIExport(filePath, outFile, title string, asYAML bool) {
	var (
		src []byte
		err error
	)

	if filePath == "-" || filePath == "" {
		src, err = io.ReadAll(os.Stdin)
	} else {
		src, err = os.ReadFile(filePath)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "aoni-gen openapi: failed reading source: %v\n", err)
		os.Exit(1)
	}

	p := parser.NewParser()

	root, err := p.ParseSource("contract.go", src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aoni-gen openapi: failed parsing contract: %v\n", err)
		os.Exit(1)
	}

	exportCfg := openapi.ExportConfig{
		Title:  title,
		AsYAML: asYAML,
	}

	specBytes, err := openapi.ExportOpenAPI(root, exportCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aoni-gen openapi: %v\n", err)
		os.Exit(1)
	}

	if outFile != "" && outFile != "-" {
		if err := os.WriteFile(outFile, specBytes, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "aoni-gen openapi: failed writing %s: %v\n", outFile, err)
			os.Exit(1)
		}

		fmt.Printf("✔ Exported OpenAPI 3.1 specification to %s (%d bytes)\n", outFile, len(specBytes))

		return
	}

	fmt.Print(string(specBytes))
}
