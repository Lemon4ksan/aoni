// Copyright 2026 The Aoni Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/builder"
	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdRefactor provides AST-level contract restructuring and renaming.
type CmdRefactor struct{}

func (c *CmdRefactor) Name() string      { return "refactor" }
func (c *CmdRefactor) Aliases() []string { return []string{"rebase", "rf", "reorganize"} }
func (c *CmdRefactor) Synopsis() string {
	return "Refactor contracts via AST: split monolithic interfaces and batch rename methods"
}

func (c *CmdRefactor) Usage() string {
	return "vortex refactor [split|rename] [contract] [flags]"
}

func (c *CmdRefactor) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("refactor", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		fromFlag    = fs.String("from", "", "Source interface name or file path to refactor from")
		toFlag      = fs.String("to", "", "Target interface name for split operation")
		methodsFlag = fs.String(
			"methods",
			"",
			"Glob pattern or comma-separated list of methods to split (e.g. 'Get*,List*')",
		)
		matchFlag   = fs.String("match", "", "Regex match pattern for renaming (e.g. 'Fetch(.*)')")
		replaceFlag = fs.String("replace", "", "Replacement pattern for renaming (e.g. 'Get$1')")
		outFlag     = fs.String("out", "", "Output file for split interface (default: same file)")
		dryRunFlag  = fs.Bool("dry-run", false, "Preview AST refactor without writing changes to disk")
		genFlag     = fs.Bool("gen", true, "Automatically re-generate API clients after refactoring")
		dirFlag     = fs.String("dir", "", "Target workspace directory (default: current root)")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex refactor — AST Contract Refactoring & Reorganizer\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex refactor split --from=<Interface> --methods=\"Get*,List*\" --to=<NewInterface>\n")
		fmt.Fprintf(stderr, "  vortex refactor rename --match=\"Fetch(.*)\" --replace=\"Get$1\" [file.go|contract]\n\n")
		fmt.Fprintf(stderr, "Examples:\n")
		fmt.Fprintf(stderr, "  vortex refactor split --from=MarketAPI --methods=\"Get*,List*\" --to=MarketReaderAPI\n")
		fmt.Fprintf(
			stderr,
			"  vortex refactor rename --match=\"Fetch(.*)\" --replace=\"Get$1\" pkg/services/bptf/api.go\n",
		)
		fmt.Fprintf(
			stderr,
			"  vortex refactor split --from=PriceDB --methods=\"Predict*\" --to=PredictAPI --out=pkg/services/pricedb/predict.go\n\n",
		)
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	var flags, nonFlags []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if (arg == "-from" || arg == "-to" || arg == "-methods" || arg == "-match" ||
				arg == "-replace" || arg == "-out" || arg == "-dir") &&
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

	action := "split"

	var targetArg string

	posArgs := fs.Args()
	if len(posArgs) > 0 {
		first := strings.ToLower(posArgs[0])
		switch first {
		case "split":
			action = "split"

			if len(posArgs) > 1 {
				targetArg = posArgs[1]
			}
		case "rename":
			action = "rename"

			if len(posArgs) > 1 {
				targetArg = posArgs[1]
			}
		default:
			if *matchFlag != "" {
				action = "rename"
			}

			targetArg = posArgs[0]
		}
	}

	switch action {
	case "split":
		from := *fromFlag
		if from == "" {
			from = targetArg
		}

		return c.runSplit(ctx, stdout, targetDir, cfg, from, *toFlag, *methodsFlag, *outFlag, *dryRunFlag, *genFlag)

	case "rename":
		target := targetArg
		if target == "" {
			target = *fromFlag
		}

		return c.runRename(ctx, stdout, targetDir, cfg, target, *matchFlag, *replaceFlag, *dryRunFlag, *genFlag)

	default:
		return fmt.Errorf("unknown refactor action %q", action)
	}
}

func (c *CmdRefactor) runSplit(
	ctx context.Context,
	stdout io.Writer,
	rootDir string,
	cfg *project.Config,
	fromContract, toInterface, methodsPattern, outPath string,
	dryRun, autoGen bool,
) error {
	if fromContract == "" || toInterface == "" || methodsPattern == "" {
		return errors.New(
			"usage: vortex refactor split --from=<Interface> --methods=\"Pattern*\" --to=<NewInterface> [--out=path.go]",
		)
	}

	srcFile := resolveFilePath(rootDir, cfg, fromContract)
	if srcFile == "" {
		return fmt.Errorf("could not resolve source contract %q", fromContract)
	}

	fset := token.NewFileSet()

	fileAst, err := parser.ParseFile(fset, srcFile, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", srcFile, err)
	}

	patterns := strings.Split(methodsPattern, ",")
	for i := range patterns {
		patterns[i] = strings.TrimSpace(patterns[i])
	}

	var (
		matchedIface     *ast.InterfaceType
		matchedIfaceName string
		movedMethods     []*ast.Field
		keptMethods      []*ast.Field
	)

	ast.Inspect(fileAst, func(n ast.Node) bool {
		genDecl, ok := n.(*ast.GenDecl)
		if ok && genDecl.Tok == token.TYPE {
			for _, spec := range genDecl.Specs {
				if typeSpec, isType := spec.(*ast.TypeSpec); isType {
					if iface, isIface := typeSpec.Type.(*ast.InterfaceType); isIface {
						if matchedIface == nil || strings.EqualFold(typeSpec.Name.Name, fromContract) {
							matchedIface = iface
							matchedIfaceName = typeSpec.Name.Name
						}
					}
				}
			}
		}

		return true
	})

	if matchedIface == nil || matchedIface.Methods == nil {
		return fmt.Errorf("no interface found in %s", srcFile)
	}

	for _, m := range matchedIface.Methods.List {
		if len(m.Names) == 0 {
			keptMethods = append(keptMethods, m)
			continue
		}

		name := m.Names[0].Name
		if matchesAnyPattern(name, patterns) {
			movedMethods = append(movedMethods, m)
		} else {
			keptMethods = append(keptMethods, m)
		}
	}

	if len(movedMethods) == 0 {
		return fmt.Errorf("no methods matched pattern %q in %s", methodsPattern, matchedIfaceName)
	}

	matchedIface.Methods.List = keptMethods

	newIfaceDecl := &ast.GenDecl{
		Tok: token.TYPE,
		Doc: &ast.CommentGroup{
			List: []*ast.Comment{
				{Text: "// " + toInterface + " extracted via vortex refactor split from " + matchedIfaceName},
				{Text: "// @aoni:service"},
			},
		},
		Specs: []ast.Spec{
			&ast.TypeSpec{
				Name: ast.NewIdent(toInterface),
				Type: &ast.InterfaceType{
					Methods: &ast.FieldList{
						List: movedMethods,
					},
				},
			},
		},
	}

	if outPath == "" {
		// Append to the same file
		fileAst.Decls = append(fileAst.Decls, newIfaceDecl)

		var buf bytes.Buffer
		if err := format.Node(&buf, fset, fileAst); err != nil {
			return fmt.Errorf("formatting modified file: %w", err)
		}

		relSrc, _ := filepath.Rel(rootDir, srcFile)
		if dryRun {
			fmt.Fprintf(stdout, "⚡ [vortex refactor split] Dry-Run (%s -> %s & %s)\n\n",
				filepath.ToSlash(relSrc), matchedIfaceName, toInterface)
			fmt.Fprintf(stdout, "  ✔ Split %d method(s) into %s\n", len(movedMethods), toInterface)
			fmt.Fprintf(stdout, "  ✔ Retained %d method(s) in %s\n\n", len(keptMethods), matchedIfaceName)
			fmt.Fprintf(stdout, "%s\n", buf.String())

			return nil
		}

		if err := os.WriteFile(srcFile, buf.Bytes(), 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", srcFile, err)
		}

		fmt.Fprintf(stdout, "✔ Successfully split %s:\n", filepath.ToSlash(relSrc))
		fmt.Fprintf(stdout, "  • %s: %d method(s) retained\n", matchedIfaceName, len(keptMethods))
		fmt.Fprintf(stdout, "  • %s: %d method(s) extracted\n", toInterface, len(movedMethods))
	} else {
		// Write to separate new file
		destFile := outPath
		if !filepath.IsAbs(destFile) {
			destFile = filepath.Join(rootDir, destFile)
		}

		newFileAst := &ast.File{
			Name: ast.NewIdent(fileAst.Name.Name),
			Decls: []ast.Decl{
				newIfaceDecl,
			},
		}

		// Copy imports
		for _, decl := range fileAst.Decls {
			if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
				newFileAst.Decls = append([]ast.Decl{genDecl}, newFileAst.Decls...)
			}
		}

		var srcBuf, destBuf bytes.Buffer
		if err := format.Node(&srcBuf, fset, fileAst); err != nil {
			return err
		}

		if err := format.Node(&destBuf, token.NewFileSet(), newFileAst); err != nil {
			return err
		}

		relSrc, _ := filepath.Rel(rootDir, srcFile)
		relDest, _ := filepath.Rel(rootDir, destFile)

		if dryRun {
			fmt.Fprintf(stdout, "⚡ [vortex refactor split] Dry-Run (%s -> %s)\n\n",
				filepath.ToSlash(relSrc), filepath.ToSlash(relDest))
			fmt.Fprintf(
				stdout,
				"  ✔ Extracted %s (%d methods) into %s\n",
				toInterface,
				len(movedMethods),
				filepath.ToSlash(relDest),
			)

			return nil
		}

		if err := os.MkdirAll(filepath.Dir(destFile), 0o750); err != nil {
			return err
		}

		if err := os.WriteFile(srcFile, srcBuf.Bytes(), 0o600); err != nil {
			return err
		}

		if err := os.WriteFile(destFile, destBuf.Bytes(), 0o600); err != nil {
			return err
		}

		fmt.Fprintf(stdout, "✔ Successfully split %s -> %s (%s, %d methods)\n",
			filepath.ToSlash(relSrc), filepath.ToSlash(relDest), toInterface, len(movedMethods))
	}

	if autoGen {
		b := builder.New(builder.Config{})
		_, _ = b.BuildFiles(ctx, []string{srcFile})
	}

	return nil
}

func (c *CmdRefactor) runRename(
	ctx context.Context,
	stdout io.Writer,
	rootDir string,
	cfg *project.Config,
	target, matchPattern, replacePattern string,
	dryRun, autoGen bool,
) error {
	if matchPattern == "" || replacePattern == "" {
		return errors.New(
			"usage: vortex refactor rename --match=\"Pattern\" --replace=\"Replacement\" [file.go|contract]",
		)
	}

	targetFile := resolveFilePath(rootDir, cfg, target)
	if targetFile == "" {
		return fmt.Errorf("could not resolve target contract %q", target)
	}

	re, err := regexp.Compile(matchPattern)
	if err != nil {
		return fmt.Errorf("invalid regex match pattern %q: %w", matchPattern, err)
	}

	fset := token.NewFileSet()

	fileAst, err := parser.ParseFile(fset, targetFile, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", targetFile, err)
	}

	renamedCount := 0
	ast.Inspect(fileAst, func(n ast.Node) bool {
		if iface, isIface := n.(*ast.InterfaceType); isIface && iface.Methods != nil {
			for _, m := range iface.Methods.List {
				for _, id := range m.Names {
					if re.MatchString(id.Name) {
						oldName := id.Name

						newName := re.ReplaceAllString(id.Name, replacePattern)
						if oldName != newName {
							id.Name = newName
							renamedCount++

							fmt.Fprintf(stdout, "  ↳ %s -> %s\n", oldName, newName)
						}
					}
				}
			}
		}

		return true
	})

	if renamedCount == 0 {
		fmt.Fprintf(stdout, "No method names matched pattern %q in %s\n", matchPattern, targetFile)
		return nil
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, fileAst); err != nil {
		return fmt.Errorf("formatting renamed AST: %w", err)
	}

	relPath, _ := filepath.Rel(rootDir, targetFile)
	if dryRun {
		fmt.Fprintf(
			stdout,
			"\n⚡ [vortex refactor rename] Dry-Run (%s: %d method(s) renamed)\n\n",
			filepath.ToSlash(relPath),
			renamedCount,
		)
		return nil
	}

	if err := os.WriteFile(targetFile, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", targetFile, err)
	}

	fmt.Fprintf(stdout, "✔ Successfully renamed %d method(s) in %s\n", renamedCount, filepath.ToSlash(relPath))

	if autoGen {
		b := builder.New(builder.Config{})
		_, _ = b.BuildFiles(ctx, []string{targetFile})
	}

	return nil
}

func matchesAnyPattern(name string, patterns []string) bool {
	for _, p := range patterns {
		if p == "*" {
			return true
		}

		if strings.HasSuffix(p, "*") {
			prefix := strings.TrimSuffix(p, "*")
			if strings.HasPrefix(name, prefix) {
				return true
			}
		} else if strings.EqualFold(name, p) {
			return true
		}
	}

	return false
}
