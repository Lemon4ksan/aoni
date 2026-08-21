// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package perf

import (
	"bufio"
	"cmp"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/project"
)

// CoverageStats records statement coverage counts and percentage metrics.
type CoverageStats struct {
	TotalStatements   int
	CoveredStatements int
}

func (s *CoverageStats) Percent() float64 {
	if s.TotalStatements == 0 {
		return 0.0
	}

	return float64(s.CoveredStatements) / float64(s.TotalStatements) * 100
}

type blockInfo struct {
	Statements int
	Hits       int
}

type pkgRow struct {
	Name  string
	Stats *CoverageStats
}

// CmdCover analyzes Go coverage profiles and reports deduplicated core statistics.
type CmdCover struct{}

func (c *CmdCover) Name() string      { return "cover" }
func (c *CmdCover) Aliases() []string { return []string{"coverage"} }
func (c *CmdCover) Synopsis() string  { return "Deduplicated core test coverage analyzer" }
func (c *CmdCover) Usage() string     { return "vortex perf cover [flags]" }

func (c *CmdCover) Run(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("cover", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		coverageFilePath = fs.String("file", "./coverage.out", "Path to Go coverage profile file")
		sortBy           = fs.String("sort", "percent", "Sort package coverage by 'name' or 'percent'")
		minPercent       = fs.Float64("min", 0.0, "Minimum required core coverage percentage (fails if below)")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex perf cover — Deduplicated Core Code Coverage Profile Analyzer\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex perf cover [-file=coverage.out] [-sort=percent|name] [-min=80.0]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	filePath := *coverageFilePath
	sortVal := *sortBy
	minVal := *minPercent

	if cwd, err := os.Getwd(); err == nil {
		if cfg, _ := project.Load(cwd); cfg != nil {
			if *coverageFilePath == "./coverage.out" && cfg.Coverage.File != "" {
				filePath = cfg.Coverage.File
			}

			if *sortBy == "percent" && cfg.Coverage.Sort != "" {
				sortVal = cfg.Coverage.Sort
			}

			if *minPercent == 0.0 && cfg.Coverage.Min > 0 {
				minVal = cfg.Coverage.Min
			}
		}
	}

	return c.analyzeCoverageProfile(stdout, filePath, sortVal, minVal)
}

func (c *CmdCover) analyzeCoverageProfile(stdout io.Writer, coverageFilePath, sortBy string, minPercent float64) error {
	file, err := os.Open(coverageFilePath)
	if err != nil {
		return fmt.Errorf(
			"cannot open %s: %w\nTip: Run 'go test -coverprofile=%s ./...' first",
			coverageFilePath,
			err,
			coverageFilePath,
		)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// Skip first line (mode: set)
	if scanner.Scan() {
		_ = scanner.Text()
	}

	mergedBlocks := make(map[string]*blockInfo)

	for scanner.Scan() {
		line := scanner.Text()

		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}

		filePathAndRange := parts[0]

		statements, sErr := strconv.Atoi(parts[1])
		if sErr != nil {
			continue
		}

		hits, hErr := strconv.Atoi(parts[2])
		if hErr != nil {
			continue
		}

		if block, exists := mergedBlocks[filePathAndRange]; exists {
			block.Hits += hits
		} else {
			mergedBlocks[filePathAndRange] = &blockInfo{
				Statements: statements,
				Hits:       hits,
			}
		}
	}

	generated := &CoverageStats{}
	examplesCmdTest := &CoverageStats{}
	coreHandwritten := &CoverageStats{}
	packageStats := make(map[string]*CoverageStats)

	for filePathAndRange, block := range mergedBlocks {
		statements := block.Statements
		isCovered := block.Hits > 0

		isGenerated := strings.Contains(filePathAndRange, "generated.go") ||
			strings.Contains(filePathAndRange, "enums.go") ||
			strings.Contains(filePathAndRange, ".gen.go") ||
			strings.Contains(filePathAndRange, ".pb.go")

		isExampleCmdTest := strings.Contains(filePathAndRange, "cmd/") ||
			strings.Contains(filePathAndRange, "examples/") ||
			strings.Contains(filePathAndRange, "test/") ||
			strings.Contains(filePathAndRange, "tests/") ||
			strings.Contains(filePathAndRange, "protobuf/custom")

		pkgPath := filePathAndRange
		if idx := strings.LastIndex(filePathAndRange, ".go:"); idx != -1 {
			pkgPath = filePathAndRange[:idx]
		}

		if idx := strings.LastIndex(pkgPath, "/"); idx != -1 {
			pkgPath = pkgPath[:idx]
		}

		switch {
		case isGenerated:
			generated.TotalStatements += statements
			if isCovered {
				generated.CoveredStatements += statements
			}
		case isExampleCmdTest:
			examplesCmdTest.TotalStatements += statements
			if isCovered {
				examplesCmdTest.CoveredStatements += statements
			}
		default:
			coreHandwritten.TotalStatements += statements
			if isCovered {
				coreHandwritten.CoveredStatements += statements
			}

			if _, ok := packageStats[pkgPath]; !ok {
				packageStats[pkgPath] = &CoverageStats{}
			}

			packageStats[pkgPath].TotalStatements += statements
			if isCovered {
				packageStats[pkgPath].CoveredStatements += statements
			}
		}
	}

	fmt.Fprintln(stdout, "==========================================================================")
	fmt.Fprintln(stdout, "           aoni Deduplicated Coverage Profile Analysis                    ")
	fmt.Fprintln(stdout, "==========================================================================")

	totalStats := &CoverageStats{
		TotalStatements:   generated.TotalStatements + examplesCmdTest.TotalStatements + coreHandwritten.TotalStatements,
		CoveredStatements: generated.CoveredStatements + examplesCmdTest.CoveredStatements + coreHandwritten.CoveredStatements,
	}

	fmt.Fprintf(stdout, "Total Codebase     : %6d / %6d statements (%6.2f%%)\n",
		totalStats.CoveredStatements, totalStats.TotalStatements, totalStats.Percent())
	fmt.Fprintf(stdout, "Generated Code     : %6d / %6d statements (%6.2f%%)\n",
		generated.CoveredStatements, generated.TotalStatements, generated.Percent())
	fmt.Fprintf(stdout, "Examples/Cmd/Tests : %6d / %6d statements (%6.2f%%)\n",
		examplesCmdTest.CoveredStatements, examplesCmdTest.TotalStatements, examplesCmdTest.Percent())
	fmt.Fprintf(stdout, "Core Library       : %6d / %6d statements (%6.2f%%)\n",
		coreHandwritten.CoveredStatements, coreHandwritten.TotalStatements, coreHandwritten.Percent())
	fmt.Fprintln(stdout, "--------------------------------------------------------------------------")

	rows := make([]pkgRow, 0, len(packageStats))
	for name, stats := range packageStats {
		rows = append(rows, pkgRow{Name: name, Stats: stats})
	}

	if sortBy == "percent" {
		slices.SortFunc(rows, func(a, b pkgRow) int {
			return cmp.Or(
				cmp.Compare(b.Stats.Percent(), a.Stats.Percent()),
				cmp.Compare(a.Name, b.Name),
			)
		})
	} else {
		slices.SortFunc(rows, func(a, b pkgRow) int {
			return cmp.Compare(a.Name, b.Name)
		})
	}

	fmt.Fprintf(stdout, "%-52s %10s %10s\n", "Package (Core Handwritten)", "Coverage", "Statements")
	fmt.Fprintln(stdout, strings.Repeat("-", 74))

	for _, r := range rows {
		fmt.Fprintf(stdout, "%-52s %9.2f%% (%d/%d)\n",
			r.Name, r.Stats.Percent(), r.Stats.CoveredStatements, r.Stats.TotalStatements)
	}

	fmt.Fprintln(stdout, "==========================================================================")

	if minPercent > 0 && coreHandwritten.Percent() < minPercent {
		return fmt.Errorf("core coverage (%.2f%%) is below required threshold (%.2f%%)",
			coreHandwritten.Percent(), minPercent)
	}

	return nil
}
