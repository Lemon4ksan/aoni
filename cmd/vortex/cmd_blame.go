// Copyright 2026 The Aoni Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/git"
	codeparser "github.com/lemon4ksan/aoni/internal/codegen/parser"
	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdBlame inspects and displays contract and method provenance.
type CmdBlame struct{}

func (c *CmdBlame) Name() string      { return "blame" }
func (c *CmdBlame) Aliases() []string { return []string{"pvn", "provenance", "origin"} }
func (c *CmdBlame) Synopsis() string {
	return "Inspect method, DTO, and directive provenance (Git commit, author, upstream spec, inferred rule)"
}

func (c *CmdBlame) Usage() string {
	return "vortex blame [file.go|contract] [flags]"
}

// BlameEntry represents the provenance metadata for a single contract element.
type BlameEntry struct {
	LineNumber int    `json:"line_number"`
	Type       string `json:"type"` // "method", "directive", "dto", "field"
	Name       string `json:"name"`
	Origin     string `json:"origin"`
	Author     string `json:"author,omitempty"`
	Commit     string `json:"commit,omitempty"`
	Date       string `json:"date,omitempty"`
}

func (c *CmdBlame) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("blame", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		dirFlag    = fs.String("dir", "", "Target workspace directory (default: current repository root)")
		jsonFlag   = fs.Bool("json", false, "Output report in JSON format")
		methodFlag = fs.String("method", "", "Filter blame output to a specific method name")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex blame — Contract Lineage & Provenance Inspector\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex blame <file.go|ContractName> [--json] [--method=name]\n\n")
		fmt.Fprintf(stderr, "Examples:\n")
		fmt.Fprintf(stderr, "  vortex blame pkg/services/bptf/api.go\n")
		fmt.Fprintf(stderr, "  vortex blame Market\n")
		fmt.Fprintf(stderr, "  vortex blame pkg/services/pricedb/api.go --method=GetPrice\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	var flags, nonFlags []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if (arg == "-dir" || arg == "-method") &&
				i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
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

	targetDir := *dirFlag
	if targetDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}

		rootDir, _, _ := project.FindRoot(cwd)
		targetDir = rootDir
	}

	cfg, _ := project.Load(targetDir)

	posArgs := fs.Args()
	if len(posArgs) == 0 {
		if cfg != nil && len(cfg.Contracts) > 0 {
			// Blame first contract
			posArgs = []string{cfg.Contracts[0].File}
		} else {
			return errors.New("usage: vortex blame <file.go|contract>")
		}
	}

	targetFile := posArgs[0]

	var matchedContract *project.ContractConfig

	if cfg != nil {
		for _, ct := range cfg.Contracts {
			if strings.EqualFold(ct.Name, targetFile) || strings.EqualFold(ct.Package, targetFile) ||
				strings.EqualFold(ct.File, targetFile) || strings.HasSuffix(ct.File, "/"+targetFile) ||
				strings.HasSuffix(ct.File, "\\"+targetFile) {
				matchedContract = &ct
				targetFile = filepath.Join(cfg.RootDir, ct.File)
				break
			}
		}
	}

	if !filepath.IsAbs(targetFile) {
		targetFile = filepath.Join(targetDir, targetFile)
	}

	if _, err := os.Stat(targetFile); err != nil {
		return fmt.Errorf("file not found %s: %w", targetFile, err)
	}

	// 1. AST Parsing
	fset := token.NewFileSet()

	node, err := parser.ParseFile(fset, targetFile, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parsing Go AST for %s: %w", targetFile, err)
	}

	// 2. Vortex Semantic IR Parsing
	vortexParser := codeparser.NewParser()
	irRoot, _ := vortexParser.ParseFile(targetFile)

	// 3. Git Blame Resolution
	gitBlames, _ := git.BlameFile(ctx, targetDir, targetFile)

	// Upstream origin string
	upstreamOrigin := "[human] manual source"
	if matchedContract != nil && matchedContract.Upstream != nil && matchedContract.Upstream.Source != "" {
		upstreamOrigin = "[upstream] " + filepath.ToSlash(matchedContract.Upstream.Source)
	}

	var entries []BlameEntry

	// Walk AST to extract methods, directives, structs
	ast.Inspect(node, func(n ast.Node) bool {
		typeSpec, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}

		if iface, isIface := typeSpec.Type.(*ast.InterfaceType); isIface && iface.Methods != nil {
			for _, m := range iface.Methods.List {
				if len(m.Names) == 0 {
					continue
				}

				methodName := m.Names[0].Name
				if *methodFlag != "" && !strings.EqualFold(methodName, *methodFlag) {
					continue
				}

				line := fset.Position(m.Pos()).Line
				blame := gitBlames[line]

				origin := upstreamOrigin
				// Check directives from doc comments
				var directives []string
				if m.Doc != nil {
					for _, c := range m.Doc.List {
						txt := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
						if strings.HasPrefix(txt, "@") {
							directives = append(directives, txt)
						}
					}
				}

				entries = append(entries, BlameEntry{
					LineNumber: line,
					Type:       "method",
					Name:       methodName,
					Origin:     origin,
					Author:     blame.Author,
					Commit:     blame.Commit,
					Date:       blame.Date,
				})

				// Record directives as sub-entries
				for _, dir := range directives {
					dirOrigin := "[human] manual directive"
					switch {
					case strings.Contains(dir, "@version") || strings.Contains(dir, "@since"):
						dirOrigin = "[contract] semver anchor"
					case strings.Contains(dir, "@get") || strings.Contains(dir, "@post") ||
						strings.Contains(dir, "@put") || strings.Contains(dir, "@delete"):
						dirOrigin = origin
					case strings.Contains(dir, "@unwrap") || strings.Contains(dir, "@retry"):
						dirOrigin = "[human] resilience tuning"
					}

					entries = append(entries, BlameEntry{
						LineNumber: line,
						Type:       "directive",
						Name:       "• " + dir,
						Origin:     dirOrigin,
						Author:     blame.Author,
						Commit:     blame.Commit,
						Date:       blame.Date,
					})
				}
			}
		}

		if structType, isStruct := typeSpec.Type.(*ast.StructType); isStruct {
			structName := typeSpec.Name.Name
			line := fset.Position(typeSpec.Pos()).Line
			blame := gitBlames[line]

			fieldCount := 0
			if structType.Fields != nil {
				fieldCount = len(structType.Fields.List)
			}

			entries = append(entries, BlameEntry{
				LineNumber: line,
				Type:       "dto",
				Name:       structName,
				Origin:     fmt.Sprintf("%s (%d fields)", upstreamOrigin, fieldCount),
				Author:     blame.Author,
				Commit:     blame.Commit,
				Date:       blame.Date,
			})
		}

		return true
	})

	if *jsonFlag {
		data, jErr := json.MarshalIndent(entries, "", "  ")
		if jErr != nil {
			return jErr
		}

		fmt.Fprintln(stdout, string(data))

		return nil
	}

	relFile, _ := filepath.Rel(targetDir, targetFile)

	contractTitle := filepath.Base(targetFile)
	if irRoot != nil && len(irRoot.Services) > 0 {
		contractTitle = fmt.Sprintf("%s (%s)", relFile, irRoot.Services[0].Name)
	}

	fmt.Fprintf(stdout, "⚡ Vortex Contract Provenance (%s)\n\n", filepath.ToSlash(contractTitle))
	fmt.Fprintf(
		stdout,
		"  %-5s  %-30s %-36s %s\n",
		"LINE",
		"METHOD / DIRECTIVE / DTO",
		"PROVENANCE ORIGIN",
		"GIT AUTHOR & COMMIT",
	)
	fmt.Fprintf(stdout, "  %s\n", strings.Repeat("─", 100))

	for _, e := range entries {
		authorCommit := "-"
		if e.Author != "" {
			authorCommit = fmt.Sprintf("%s (%s, %s)", e.Author, e.Commit, e.Date)
		}

		nameStr := e.Name
		if e.Type == "directive" {
			nameStr = "  " + e.Name
		}

		fmt.Fprintf(
			stdout,
			"  %-5d  %-30s %-36s %s\n",
			e.LineNumber,
			truncateString(nameStr, 30),
			truncateString(e.Origin, 35),
			authorCommit,
		)
	}

	return nil
}
