// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/builder"
	"github.com/lemon4ksan/aoni/internal/codegen/cache"
	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdEnv scans Go contracts for referenced environment variables and generates .env templates.
type CmdEnv struct{}

func (c *CmdEnv) Name() string      { return "env" }
func (c *CmdEnv) Aliases() []string { return []string{"dotenv"} }
func (c *CmdEnv) Synopsis() string {
	return "Scan contracts for ${VAR_NAME} references and generate .env.example / .env"
}

func (c *CmdEnv) Usage() string {
	return "vortex env [file.go|package] [--out=.env.example] [--fill] [flags]"
}

func (c *CmdEnv) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("env", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		outFileFlag = fs.String("out", ".env.example", "Output file for environment variables (use '-' for stdout)")
		fillFlag    = fs.Bool("fill", false, "Pre-fill variables from local .vortex/cache/secrets.json vault")
		dirFlag     = fs.String("dir", "", "Target workspace directory (default: current root)")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex env — Contract Environment & Secrets Template Generator\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex env [contract.go] [--out=.env.example] [--fill]\n\n")
		fmt.Fprintf(stderr, "Examples:\n")
		fmt.Fprintf(stderr, "  vortex env                              # Scan all contracts and write .env.example\n")
		fmt.Fprintf(stderr, "  vortex env --fill --out=.env.local      # Generate local .env filled from secrets vault\n")
		fmt.Fprintf(stderr, "  vortex env pkg/agy/unleash.go --out=-   # Print required variables for Unleash to stdout\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	var flags, nonFlags []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if (arg == "-out" || arg == "-dir") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			nonFlags = append(nonFlags, arg)
		}
	}

	if err := fs.Parse(append(flags, nonFlags...)); err != nil {
		return err
	}

	rootDir := *dirFlag
	if rootDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}

		discoveredRoot, _, _ := project.FindRoot(cwd)
		rootDir = discoveredRoot
	}

	cfg, _ := project.Load(rootDir)

	var targetFiles []string
	posArgs := fs.Args()
	if len(posArgs) > 0 {
		for _, arg := range posArgs {
			resolved := resolveFilePath(rootDir, cfg, arg)
			if resolved != "" {
				targetFiles = append(targetFiles, resolved)
			}
		}
	}

	if len(targetFiles) == 0 {
		targetFiles = builder.CollectInputFiles(rootDir, []string{"./..."}, builder.CollectOptions{})
	}

	if len(targetFiles) == 0 {
		return fmt.Errorf("no contract files found in %s", rootDir)
	}

	// Regex for finding ${VAR_NAME}
	varRegex := regexp.MustCompile(`\$\{([a-zA-Z0-9_]+)\}`)
	varMap := make(map[string][]string) // varName -> []origins

	fset := token.NewFileSet()
	for _, fPath := range targetFiles {
		content, err := os.ReadFile(fPath)
		if err != nil {
			continue
		}

		relPath, _ := filepath.Rel(rootDir, fPath)
		relPath = filepath.ToSlash(relPath)

		// Find in raw text comments and string literals
		matches := varRegex.FindAllStringSubmatch(string(content), -1)
		for _, m := range matches {
			if len(m) > 1 {
				varName := m[1]
				varMap[varName] = append(varMap[varName], relPath)
			}
		}

		// Also check AST doc comments
		fileAst, err := parser.ParseFile(fset, fPath, content, parser.ParseComments)
		if err == nil && fileAst.Comments != nil {
			for _, cg := range fileAst.Comments {
				for _, c := range cg.List {
					m := varRegex.FindAllStringSubmatch(c.Text, -1)
					for _, match := range m {
						if len(match) > 1 {
							varName := match[1]
							varMap[varName] = append(varMap[varName], relPath)
						}
					}
				}
			}
		}
	}

	if len(varMap) == 0 {
		fmt.Fprintf(stdout, "No ${VAR_NAME} environment references found in contracts.\n")
		return nil
	}

	// De-duplicate origins
	for k := range varMap {
		uniq := make(map[string]bool)
		var list []string
		for _, o := range varMap[k] {
			if !uniq[o] {
				uniq[o] = true
				list = append(list, o)
			}
		}
		sort.Strings(list)
		varMap[k] = list
	}

	// Load vault if fill is enabled
	vault, _, _ := cache.LoadSecrets(rootDir)

	var varNames []string
	for k := range varMap {
		varNames = append(varNames, k)
	}
	sort.Strings(varNames)

	var buf bytes.Buffer
	buf.WriteString("# =============================================================================\n")
	buf.WriteString("# Environment & Security Credentials (Generated by Vortex)\n")
	buf.WriteString("# =============================================================================\n\n")

	for _, vName := range varNames {
		origins := strings.Join(varMap[vName], ", ")
		buf.WriteString(fmt.Sprintf("# Required by: %s\n", origins))

		val := ""
		if *fillFlag && vault != nil {
			if s, ok := vault.Secrets[vName]; ok {
				val = s.Value
			}
		}

		buf.WriteString(fmt.Sprintf("%s=%s\n\n", vName, val))
	}

	outPath := *outFileFlag
	if outPath == "-" || outPath == "" {
		fmt.Fprint(stdout, buf.String())
		return nil
	}

	if !filepath.IsAbs(outPath) {
		outPath = filepath.Join(rootDir, outPath)
	}

	if err := os.WriteFile(outPath, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", outPath, err)
	}

	relOut, _ := filepath.Rel(rootDir, outPath)
	fmt.Fprintf(stdout, "Generated %s with %d required variable(s)\n",
		filepath.ToSlash(relOut), len(varNames))

	return nil
}
