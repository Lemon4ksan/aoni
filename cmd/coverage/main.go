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

func main() {
	coverageFilePath := flag.String("file", "./coverage.out", "path to the go coverage profile file")
	sortBy := flag.String(
		"sort",
		"name",
		"sort package coverage by 'name' (alphabetical) or 'percent' (coverage percentage)",
	)

	flag.Parse()

	file, err := os.Open(*coverageFilePath)
	if err != nil {
		fmt.Printf("Error opening %s: %v\n", *coverageFilePath, err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// Skip first line (mode: set)
	if scanner.Scan() {
		_ = scanner.Text()
	}

	// Map to merge duplicate blocks
	// Key: filePathAndRange (e.g., github.com/lemon4ksan/aoni/client.go:21.43,26.2)
	mergedBlocks := make(map[string]*BlockInfo)

	for scanner.Scan() {
		line := scanner.Text()

		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}

		filePathAndRange := parts[0]
		statementsStr := parts[1]
		hitsStr := parts[2]

		statements, err := strconv.Atoi(statementsStr)
		if err != nil {
			continue
		}

		hits, err := strconv.Atoi(hitsStr)
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

	// Stats by category
	generated := &CoverageStats{}
	examplesCmdTest := &CoverageStats{}
	coreHandwritten := &CoverageStats{}

	// Stats by package
	packageStats := make(map[string]*CoverageStats)

	for filePathAndRange, block := range mergedBlocks {
		statements := block.Statements
		isCovered := block.Hits > 0

		// Identify category
		isGenerated := strings.Contains(filePathAndRange, "generated.go") ||
			strings.Contains(filePathAndRange, "enums.go") ||
			strings.Contains(filePathAndRange, ".pb.go")

		isExampleCmdTest := strings.Contains(filePathAndRange, "cmd/") ||
			strings.Contains(filePathAndRange, "examples/") ||
			strings.Contains(filePathAndRange, "test/") ||
			strings.Contains(filePathAndRange, "tests/") ||
			strings.Contains(filePathAndRange, "protobuf/custom")

		// Get package path
		pkgPath := filePathAndRange
		if idx := strings.LastIndex(filePathAndRange, ".go:"); idx != -1 {
			pkgPath = filePathAndRange[:idx]
		}

		if idx := strings.LastIndex(pkgPath, "/"); idx != -1 {
			pkgPath = pkgPath[:idx]
		}

		// Update categories
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

			// Track package level coverage for core handwritten code
			if _, ok := packageStats[pkgPath]; !ok {
				packageStats[pkgPath] = &CoverageStats{}
			}

			packageStats[pkgPath].TotalStatements += statements
			if isCovered {
				packageStats[pkgPath].CoveredStatements += statements
			}
		}
	}

	fmt.Println("=== OVERALL COVERAGE ANALYSIS (DEDUPLICATED) ===")

	totalStats := &CoverageStats{
		TotalStatements:   generated.TotalStatements + examplesCmdTest.TotalStatements + coreHandwritten.TotalStatements,
		CoveredStatements: generated.CoveredStatements + examplesCmdTest.CoveredStatements + coreHandwritten.CoveredStatements,
	}
	fmt.Printf(
		"Total Codebase: %d / %d statements (%.2f%%)\n",
		totalStats.CoveredStatements,
		totalStats.TotalStatements,
		totalStats.Percent(),
	)
	fmt.Printf(
		"Generated Code: %d / %d statements (%.2f%%)\n",
		generated.CoveredStatements,
		generated.TotalStatements,
		generated.Percent(),
	)
	fmt.Printf(
		"Examples/Cmd/Tests: %d / %d statements (%.2f%%)\n",
		examplesCmdTest.CoveredStatements,
		examplesCmdTest.TotalStatements,
		examplesCmdTest.Percent(),
	)
	fmt.Printf(
		"Core Handwritten Code: %d / %d statements (%.2f%%)\n",
		coreHandwritten.CoveredStatements,
		coreHandwritten.TotalStatements,
		coreHandwritten.Percent(),
	)
	fmt.Println()

	fmt.Println("=== CORE HANDWRITTEN PACKAGE COVERAGE ===")

	type PackageCoverage struct {
		Name  string
		Stats *CoverageStats
	}

	var sortedPackages []PackageCoverage
	for pkg, stats := range packageStats {
		sortedPackages = append(sortedPackages, PackageCoverage{Name: pkg, Stats: stats})
	}

	if *sortBy == "percent" {
		sort.Slice(sortedPackages, func(i, j int) bool {
			pi := sortedPackages[i].Stats.Percent()

			pj := sortedPackages[j].Stats.Percent()
			if pi != pj {
				return pi > pj // Descending by percentage
			}

			return sortedPackages[i].Name < sortedPackages[j].Name
		})
	} else {
		sort.Slice(sortedPackages, func(i, j int) bool {
			return sortedPackages[i].Name < sortedPackages[j].Name // Alphabetical by name
		})
	}

	// Find max package name length for alignment
	maxLen := 0
	for _, pc := range sortedPackages {
		if len(pc.Name) > maxLen {
			maxLen = len(pc.Name)
		}
	}

	// Print sorted packages
	for _, pc := range sortedPackages {
		fmt.Printf(
			"- %-*s: %4d / %4d statements (%6.2f%%)\n",
			maxLen,
			pc.Name,
			pc.Stats.CoveredStatements,
			pc.Stats.TotalStatements,
			pc.Stats.Percent(),
		)
	}
}
