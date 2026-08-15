// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

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

type BlockInfo struct {
	Statements int
	Hits       int
}

type pkgRow struct {
	Name  string
	Stats *CoverageStats
}

func runCover(args []string) {
	fs := flag.NewFlagSet("vortex cover", flag.ExitOnError)

	var (
		coverageFilePath = fs.String("file", "./coverage.out", "Path to Go coverage profile file")
		sortBy           = fs.String("sort", "percent", "Sort package coverage by 'name' or 'percent'")
		minPercent       = fs.Float64("min", 0.0, "Minimum required core coverage percentage (fails if below)")
	)

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "vortex cover — Deduplicated Core Code Coverage Profile Analyzer\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  vortex cover [-file=coverage.out] [-sort=percent|name] [-min=80.0]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  go test -coverprofile=coverage.out ./...\n")
		fmt.Fprintf(os.Stderr, "  vortex cover\n")
		fmt.Fprintf(os.Stderr, "  vortex cover -sort=percent -min=85.0\n")
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if err := analyzeCoverageProfile(*coverageFilePath, *sortBy, *minPercent); err != nil {
		fmt.Fprintf(os.Stderr, "vortex cover: %v\n", err)
		os.Exit(1)
	}
}

func analyzeCoverageProfile(coverageFilePath, sortBy string, minPercent float64) error {
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

	mergedBlocks := make(map[string]*BlockInfo)

	for scanner.Scan() {
		line := scanner.Text()

		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}

		filePathAndRange := parts[0]

		statements, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}

		hits, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}

		if block, exists := mergedBlocks[filePathAndRange]; exists {
			block.Hits += hits
		} else {
			mergedBlocks[filePathAndRange] = &BlockInfo{
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

	fmt.Println("==========================================================================")
	fmt.Println("           aoni Deduplicated Coverage Profile Analysis                    ")
	fmt.Println("==========================================================================")

	totalStats := &CoverageStats{
		TotalStatements:   generated.TotalStatements + examplesCmdTest.TotalStatements + coreHandwritten.TotalStatements,
		CoveredStatements: generated.CoveredStatements + examplesCmdTest.CoveredStatements + coreHandwritten.CoveredStatements,
	}

	fmt.Printf("Total Codebase     : %6d / %6d statements (%6.2f%%)\n",
		totalStats.CoveredStatements, totalStats.TotalStatements, totalStats.Percent())
	fmt.Printf("Generated Code     : %6d / %6d statements (%6.2f%%)\n",
		generated.CoveredStatements, generated.TotalStatements, generated.Percent())
	fmt.Printf("Examples/Cmd/Tests : %6d / %6d statements (%6.2f%%)\n",
		examplesCmdTest.CoveredStatements, examplesCmdTest.TotalStatements, examplesCmdTest.Percent())
	fmt.Printf("Core Library       : %6d / %6d statements (%6.2f%%)\n",
		coreHandwritten.CoveredStatements, coreHandwritten.TotalStatements, coreHandwritten.Percent())
	fmt.Println("--------------------------------------------------------------------------")

	rows := make([]pkgRow, 0, len(packageStats))
	for name, stats := range packageStats {
		rows = append(rows, pkgRow{Name: name, Stats: stats})
	}

	if sortBy == "percent" {
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Stats.Percent() == rows[j].Stats.Percent() {
				return rows[i].Name < rows[j].Name
			}

			return rows[i].Stats.Percent() > rows[j].Stats.Percent()
		})
	} else {
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].Name < rows[j].Name
		})
	}

	fmt.Printf("%-52s %10s %10s\n", "Package (Core Handwritten)", "Coverage", "Statements")
	fmt.Println(strings.Repeat("-", 74))

	for _, r := range rows {
		fmt.Printf("%-52s %9.2f%% (%d/%d)\n",
			r.Name, r.Stats.Percent(), r.Stats.CoveredStatements, r.Stats.TotalStatements)
	}

	fmt.Println("==========================================================================")

	if minPercent > 0 && coreHandwritten.Percent() < minPercent {
		return fmt.Errorf("core coverage (%.2f%%) is below required threshold (%.2f%%)",
			coreHandwritten.Percent(), minPercent)
	}

	return nil
}
