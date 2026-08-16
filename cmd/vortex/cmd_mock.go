// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/lemon4ksan/aoni/internal/codegen/builder"
	"github.com/lemon4ksan/aoni/internal/tui"
)

// CmdMock generates zero-dependency in-memory mock test servers for API contracts.
type CmdMock struct{}

func (c *CmdMock) Name() string      { return "mock" }
func (c *CmdMock) Aliases() []string { return []string{"mock-server", "mockgen"} }
func (c *CmdMock) Synopsis() string {
	return "Generate zero-dependency in-memory virtual test server for integration tests"
}
func (c *CmdMock) Usage() string { return "vortex mock [flags] [packages/files...]" }

func (c *CmdMock) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mock", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		outFlag = fs.String("out", "", "Destination file for generated mock server (default: <file>_mock.gen.go)")
		pkgFlag = fs.String("pkg", "", "Override package name for emitted mock")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex mock — Generate In-Memory Virtual Test Servers for Contracts\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex mock [flags] [packages/files...]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nExamples:\n")
		fmt.Fprintf(stderr, "  vortex mock ./pkg/api/api.go                    # Generate in-memory mock server\n")
		fmt.Fprintf(stderr, "  vortex mock -out=./pkg/api/mock.gen.go ./pkg/api/api.go\n")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	inputPaths := fs.Args()

	files := builder.CollectInputFiles("", inputPaths)
	if len(files) == 0 {
		return errors.New("no Go source files found to generate mocks for")
	}

	b := builder.New(builder.Config{
		OutFlag: *outFlag,
		PkgFlag: *pkgFlag,
	})

	generatedCount := 0

	for _, file := range files {
		res, err := b.BuildMock(ctx, file, *outFlag)
		if err != nil {
			return fmt.Errorf("mock compilation failed for %s: %w", file, err)
		}

		if res.Skipped {
			continue
		}

		generatedCount++

		fmt.Fprintf(stdout, "✔ Generated Mock Server: %s -> %s (%s, %d services)\n",
			filepath.Base(file),
			filepath.Base(res.OutputFile),
			tui.Cyan(fmt.Sprintf("%d bytes", res.BytesCount)),
			res.ServicesCount,
		)
	}

	if generatedCount == 0 {
		fmt.Fprintln(stdout, "No services found to generate mocks for.")
	}

	return nil
}
