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
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/builder"
	"github.com/lemon4ksan/aoni/internal/codegen/history"
	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdCherryPick transplants methods, DTO structs, and directives across Go contracts.
type CmdCherryPick struct{}

func (c *CmdCherryPick) Name() string      { return "cherry-pick" }
func (c *CmdCherryPick) Aliases() []string { return []string{"cp", "transplant", "pick"} }
func (c *CmdCherryPick) Synopsis() string {
	return "Transplant methods, DTO structs, and directives between contracts via AST"
}

func (c *CmdCherryPick) Usage() string {
	return "vortex cherry-pick <source_file:MethodOrDTO> --to=<target_file> [--dry-run] [flags]"
}

func (c *CmdCherryPick) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("cherry-pick", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		toFlag     = fs.String("to", "", "Target contract Go source file or contract name")
		dryRunFlag = fs.Bool("dry-run", false, "Preview transplanted AST code without modifying files on disk")
		genFlag    = fs.Bool("gen", true, "Automatically re-generate API clients after transplantation")
		dirFlag    = fs.String("dir", "", "Target workspace directory (default: current root)")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex cherry-pick — AST-Level Method & DTO Transplantation Tool\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex cherry-pick <source_file:MethodOrDTO> --to=<target_file> [--dry-run]\n\n")
		fmt.Fprintf(stderr, "Examples:\n")
		fmt.Fprintf(
			stderr,
			"  vortex cherry-pick pkg/services/mannco/api.go:GetItemPrices --to=pkg/services/bptf/api.go\n",
		)
		fmt.Fprintf(stderr, "  vortex cherry-pick Mannco:ItemDTO --to=Bptf\n")
		fmt.Fprintf(
			stderr,
			"  vortex cherry-pick pkg/services/pricedb/api.go:PredictSpell --to=pkg/services/crit/api.go --dry-run\n\n",
		)
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	var flags, nonFlags []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if (arg == "-to" || arg == "-dir") &&
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
	if len(posArgs) == 0 || *toFlag == "" {
		return errors.New("usage: vortex cherry-pick <source_file:MethodOrDTO> --to=<target_file>")
	}

	sourceSpec := posArgs[0]

	lastColon := strings.LastIndex(sourceSpec, ":")
	if lastColon == -1 || lastColon == 0 || lastColon == len(sourceSpec)-1 {
		return fmt.Errorf(
			"invalid cherry-pick target %q (expected format: path/to/file.go:MethodName or Contract:MethodName)",
			sourceSpec,
		)
	}

	srcFileOrContract := sourceSpec[:lastColon]
	targetSymbol := sourceSpec[lastColon+1:]
	destFileOrContract := *toFlag

	// Resolve source file path
	srcFile := resolveFilePath(targetDir, cfg, srcFileOrContract)
	destFile := resolveFilePath(targetDir, cfg, destFileOrContract)

	if srcFile == "" || destFile == "" {
		return fmt.Errorf(
			"could not resolve source (%s) or target (%s) contract paths",
			srcFileOrContract,
			destFileOrContract,
		)
	}

	if srcFile == destFile {
		return errors.New("source and target contract files must be different")
	}

	// 1. Parse Source AST
	srcFset := token.NewFileSet()

	srcAst, err := parser.ParseFile(srcFset, srcFile, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parsing source file %s: %w", srcFile, err)
	}

	// 2. Parse Target AST
	destFset := token.NewFileSet()

	destAst, err := parser.ParseFile(destFset, destFile, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parsing target file %s: %w", destFile, err)
	}

	// 3. Find Symbol (Method in interface OR Struct Type)
	var (
		matchedMethod     *ast.Field
		matchedStruct     *ast.TypeSpec
		matchedStructDecl *ast.GenDecl
	)

	allSrcStructs := make(map[string]*ast.GenDecl)

	ast.Inspect(srcAst, func(n ast.Node) bool {
		genDecl, isGen := n.(*ast.GenDecl)
		if isGen && genDecl.Tok == token.TYPE {
			for _, spec := range genDecl.Specs {
				typeSpec, isType := spec.(*ast.TypeSpec)
				if !isType {
					continue
				}

				if iface, isIface := typeSpec.Type.(*ast.InterfaceType); isIface && iface.Methods != nil {
					for _, m := range iface.Methods.List {
						if len(m.Names) > 0 && strings.EqualFold(m.Names[0].Name, targetSymbol) {
							matchedMethod = m
						}
					}
				}

				if _, isStruct := typeSpec.Type.(*ast.StructType); isStruct {
					allSrcStructs[typeSpec.Name.Name] = genDecl
					if strings.EqualFold(typeSpec.Name.Name, targetSymbol) {
						matchedStruct = typeSpec
						matchedStructDecl = genDecl
					}
				}
			}
		}

		return true
	})

	if matchedMethod == nil && matchedStruct == nil {
		return fmt.Errorf("symbol %q (method or struct DTO) not found in %s", targetSymbol, srcFile)
	}

	// 4. Discover Dependent Structs (Transitive Closure)
	neededStructNames := make(map[string]bool)

	if matchedMethod != nil {
		collectTypesFromField(matchedMethod, neededStructNames)
	} else if matchedStruct != nil {
		collectTypesFromTypeSpec(matchedStruct, neededStructNames)
	}

	// Expand transitive nested struct dependencies
	for {
		prevCount := len(neededStructNames)
		for name := range neededStructNames {
			if decl, exists := allSrcStructs[name]; exists {
				for _, spec := range decl.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						collectTypesFromTypeSpec(ts, neededStructNames)
					}
				}
			}
		}

		if len(neededStructNames) == prevCount {
			break
		}
	}

	// Existing target structs to avoid duplication
	existingDestStructs := make(map[string]bool)

	var destInterface *ast.InterfaceType

	ast.Inspect(destAst, func(n ast.Node) bool {
		genDecl, isGen := n.(*ast.GenDecl)
		if isGen && genDecl.Tok == token.TYPE {
			for _, spec := range genDecl.Specs {
				if typeSpec, isType := spec.(*ast.TypeSpec); isType {
					if iface, isIface := typeSpec.Type.(*ast.InterfaceType); isIface {
						destInterface = iface
					}

					if _, isStruct := typeSpec.Type.(*ast.StructType); isStruct {
						existingDestStructs[typeSpec.Name.Name] = true
					}
				}
			}
		}

		return true
	})

	if matchedMethod != nil && destInterface == nil {
		return fmt.Errorf("target file %s does not contain any service interface to insert method", destFile)
	}

	// 5. Perform AST Transplant
	transplantedStructs := 0

	// Insert Method into Target Interface
	if matchedMethod != nil {
		destInterface.Methods.List = append(destInterface.Methods.List, matchedMethod)
	}

	// Insert Structs into Target File AST
	if matchedStructDecl != nil && !existingDestStructs[matchedStruct.Name.Name] {
		destAst.Decls = append(destAst.Decls, matchedStructDecl)
		existingDestStructs[matchedStruct.Name.Name] = true
		transplantedStructs++
	}

	for name := range neededStructNames {
		if !existingDestStructs[name] {
			if decl, exists := allSrcStructs[name]; exists {
				destAst.Decls = append(destAst.Decls, decl)
				existingDestStructs[name] = true
				transplantedStructs++
			}
		}
	}

	// 6. Format Modified Target Code
	var buf bytes.Buffer
	if err := format.Node(&buf, destFset, destAst); err != nil {
		return fmt.Errorf("formatting transplanted AST: %w", err)
	}

	relSrc, _ := filepath.Rel(targetDir, srcFile)
	relDest, _ := filepath.Rel(targetDir, destFile)

	if *dryRunFlag {
		fmt.Fprintf(stdout, "⚡ [vortex cherry-pick] Dry-Run AST Transplant (%s:%s -> %s)\n\n",
			filepath.ToSlash(relSrc), targetSymbol, filepath.ToSlash(relDest))

		if matchedMethod != nil {
			fmt.Fprintf(stdout, "  ✔ Method %s -> added to target interface\n", targetSymbol)
		}

		fmt.Fprintf(stdout, "  ✔ %d transitive DTO struct(s) prepared for transplant\n\n", transplantedStructs)
		fmt.Fprintf(stdout, "%s\n", buf.String())

		return nil
	}

	_, _ = history.Record(
		targetDir,
		fmt.Sprintf("vortex cherry-pick %s:%s --to=%s", srcFileOrContract, targetSymbol, destFileOrContract),
		[]string{destFile},
	)

	if err := os.WriteFile(destFile, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("writing modified target file %s: %w", destFile, err)
	}

	fmt.Fprintf(stdout, "✔ Successfully cherry-picked %s into %s\n", targetSymbol, filepath.ToSlash(relDest))

	if matchedMethod != nil {
		fmt.Fprintf(stdout, "  • Method:       %s\n", targetSymbol)
	}

	fmt.Fprintf(stdout, "  • Transitive:   %d dependent DTO struct(s) copied\n", transplantedStructs)

	// 7. Auto-regenerate if enabled
	if *genFlag {
		b := builder.New(builder.Config{})

		results, bErr := b.BuildFiles(ctx, []string{destFile})
		if bErr == nil && len(results) > 0 && !results[0].Skipped {
			fmt.Fprintf(
				stdout,
				"  • Regenerated:  %s (%d bytes)\n",
				filepath.ToSlash(results[0].OutputFile),
				results[0].BytesCount,
			)
		}
	}

	return nil
}

func collectTypesFromField(field *ast.Field, typeNames map[string]bool) {
	if field == nil {
		return
	}

	collectTypesFromExpr(field.Type, typeNames)
}

func collectTypesFromTypeSpec(ts *ast.TypeSpec, typeNames map[string]bool) {
	if ts == nil {
		return
	}

	if structType, ok := ts.Type.(*ast.StructType); ok && structType.Fields != nil {
		for _, f := range structType.Fields.List {
			collectTypesFromExpr(f.Type, typeNames)
		}
	}
}

func collectTypesFromExpr(expr ast.Expr, typeNames map[string]bool) {
	if expr == nil {
		return
	}

	switch t := expr.(type) {
	case *ast.Ident:
		if !isBuiltinGoType(t.Name) {
			typeNames[t.Name] = true
		}
	case *ast.StarExpr:
		collectTypesFromExpr(t.X, typeNames)
	case *ast.ArrayType:
		collectTypesFromExpr(t.Elt, typeNames)
	case *ast.MapType:
		collectTypesFromExpr(t.Key, typeNames)
		collectTypesFromExpr(t.Value, typeNames)
	case *ast.FuncType:
		if t.Params != nil {
			for _, f := range t.Params.List {
				collectTypesFromExpr(f.Type, typeNames)
			}
		}

		if t.Results != nil {
			for _, f := range t.Results.List {
				collectTypesFromExpr(f.Type, typeNames)
			}
		}
	}
}

func isBuiltinGoType(name string) bool {
	switch name {
	case "bool", "byte", "complex64", "complex128", "error", "float32", "float64",
		"int", "int8", "int16", "int32", "int64", "rune", "string",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "any":
		return true
	}

	return false
}

func resolveFilePath(rootDir string, cfg *project.Config, input string) string {
	if cfg != nil {
		for _, ct := range cfg.Contracts {
			if strings.EqualFold(ct.Name, input) || strings.EqualFold(ct.Package, input) {
				return filepath.Join(rootDir, ct.File)
			}
		}
	}

	if filepath.IsAbs(input) {
		return input
	}

	return filepath.Join(rootDir, input)
}
