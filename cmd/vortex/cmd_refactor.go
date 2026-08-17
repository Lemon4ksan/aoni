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

	"github.com/lemon4ksan/aoni/internal/codegen/pipeline"
	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdRefactor provides AST-level contract restructuring and renaming.
type CmdRefactor struct{}

func (c *CmdRefactor) Name() string      { return "ast" }
func (c *CmdRefactor) Aliases() []string { return []string{"refactor"} }
func (c *CmdRefactor) Synopsis() string {
	return "Refactor contracts via AST: tuple deobfuscation, split interfaces, and batch rename"
}

func (c *CmdRefactor) Usage() string {
	return "vortex ast [tuple|split|rename] [contract] [flags]"
}

func (c *CmdRefactor) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "review", "audit":
			return (&CmdReview{}).Run(ctx, args[1:], stdout, stderr)
		case "accept", "merge":
			return (&CmdAccept{}).Run(ctx, args[1:], stdout, stderr)
		case "undo", "revert":
			return (&CmdUndo{}).Run(ctx, args[1:], stdout, stderr)
		case "blame", "provenance":
			return (&CmdBlame{}).Run(ctx, args[1:], stdout, stderr)
		case "history", "journal":
			return (&CmdHistory{}).Run(ctx, args[1:], stdout, stderr)
		case "pick", "cherry-pick", "cp", "transplant":
			return (&CmdCherryPick{}).Run(ctx, args[1:], stdout, stderr)
		case "log", "timeline":
			return (&CmdLog{}).Run(ctx, args[1:], stdout, stderr)
		case "tag", "release":
			return (&CmdTag{}).Run(ctx, args[1:], stdout, stderr)
		}
	}

	fs := flag.NewFlagSet("ast", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		fromFlag    string
		toFlag      string
		methodsFlag string
		matchFlag   string
		replaceFlag string
		typeFlag    string
		fieldFlag   string
		outFlag     string
		dryRunFlag  bool
		genFlag     bool
		dirFlag     string
		jsFlag      string
	)

	StringVar(fs, &fromFlag, "from", "", "", "Source interface name or file path to refactor from")
	StringVar(fs, &toFlag, "to", "", "", "Target interface name or new field name")
	StringVar(
		fs,
		&methodsFlag,
		"methods",
		"m",
		"",
		"Glob pattern or comma-separated list of methods to split (e.g. 'Get*,List*')",
	)
	StringVar(fs, &matchFlag, "match", "", "", "Regex match pattern for renaming (e.g. 'Fetch(.*)')")
	StringVar(fs, &replaceFlag, "replace", "", "", "Replacement pattern for renaming (e.g. 'Get$1')")
	StringVar(
		fs,
		&typeFlag,
		"type",
		"t",
		"",
		"Target struct/tuple type name to rename field in (e.g. 'GenerateContentRequest')",
	)
	StringVar(fs, &fieldFlag, "field", "f", "", "Target field name or positional tag index (e.g. 'Field4' or '4')")
	StringVar(fs, &outFlag, "out", "o", "", "Output file for split interface (default: same file)")
	BoolVar(fs, &dryRunFlag, "dry-run", "", false, "Preview AST refactor without writing changes to disk")
	BoolVar(fs, &genFlag, "gen", "", true, "Automatically re-generate API clients after refactoring")
	StringVar(fs, &dirFlag, "dir", "", "", "Target workspace directory (default: current root)")
	StringVar(fs, &jsFlag, "js", "", "", "JavaScript bundle files or glob patterns for schema extraction (e.g. '*.js')")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex ast — AST Contract Refactoring & Tuple Deobfuscation\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex ast tuple [file.go|contract] [--js=\"*.js\"] [--dry-run]\n")
		fmt.Fprintf(
			stderr,
			"  vortex ast split --from=<Interface> --methods=\"Get*,List*\" --to=<NewInterface> [--out=path.go]\n",
		)
		fmt.Fprintf(stderr, "  vortex ast rename --match=\"Fetch(.*)\" --replace=\"Get$1\" [file.go|contract]\n")
		fmt.Fprintf(
			stderr,
			"  vortex ast rename --type=<Struct> --field=<FieldOrTag> --to=<NewName> [file.go|contract]\n\n",
		)
		fmt.Fprintf(stderr, "Examples:\n")
		fmt.Fprintf(stderr, "  vortex ast tuple pkg/api/client.go --js=\"*.js\"\n")
		fmt.Fprintf(stderr, "  vortex ast split --from=MarketAPI --methods=\"Get*,List*\" --to=MarketReaderAPI\n")
		fmt.Fprintf(
			stderr,
			"  vortex ast rename --match=\"Fetch(.*)\" --replace=\"Get$1\" pkg/api/market.go\n",
		)
		fmt.Fprintf(
			stderr,
			"  vortex ast rename --type=GenerateContentRequest --field=Field4 --to=MaxOutputTokens\n\n",
		)
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	posArgs, err := ParseInterspersedFlags(fs, args)
	if err != nil {
		return err
	}

	rt, err := NewRuntime(dirFlag)
	if err != nil {
		return err
	}

	action := "split"

	var targetArg string

	if len(posArgs) > 0 {
		first := strings.ToLower(posArgs[0])
		switch first {
		case "tuple", "tuples":
			action = "tuple"

			if len(posArgs) > 1 {
				targetArg = posArgs[1]
			}

		case "enum", "enums":
			action = "enum"

			if len(posArgs) > 1 {
				targetArg = posArgs[1]
			}

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
			if matchFlag != "" {
				action = "rename"
			}

			targetArg = posArgs[0]
		}
	}

	switch action {
	case "enum":
		target := targetArg
		if target == "" {
			target = fromFlag
		}

		ct, err := rt.ResolveContract(target)
		if err != nil {
			return err
		}

		pipe := pipeline.NewASTPipeline(ct.AbsPath, rt.RootDir)

		structTarget := typeFlag
		if structTarget == "" {
			structTarget = "ListModelsTuple"
		}

		specs, err := pipe.InferAndInjectEnums(ctx, structTarget, dryRunFlag)
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(rt.RootDir, ct.AbsPath)

		modeStr := ""
		if dryRunFlag {
			modeStr = " (Dry-Run)"
		}

		fmt.Fprintf(stdout, "✔ Successfully inferred and generated %d enum(s) for %s in %s%s:\n",
			len(specs), structTarget, filepath.ToSlash(relPath), modeStr)

		for _, s := range specs {
			fmt.Fprintf(stdout, "  * type %s %s (%d values)\n", s.Name, s.BaseType, len(s.Values))

			for _, v := range s.Values {
				fmt.Fprintf(stdout, "    ↳ %s = %v\n", v.ConstName, v.RawValue)
			}
		}

		if genFlag && !dryRunFlag {
			_ = pipe.TriggerCodegen(ctx)
		}

		return nil

	case "tuple":
		subAction := ""

		target := targetArg
		if len(posArgs) > 1 {
			candidate := strings.ToLower(posArgs[1])
			switch candidate {
			case "inspect", "analyze", "saliency":
				subAction = "inspect"

				if len(posArgs) > 2 {
					target = posArgs[2]
				} else {
					target = ""
				}

			case "apply", "rename-all":
				subAction = "apply"

				if len(posArgs) > 2 {
					target = posArgs[2]
				} else {
					target = ""
				}
			}
		}

		if target == "" {
			target = fromFlag
		}

		ct, err := rt.ResolveContract(target)
		if err != nil {
			return err
		}

		pipe := pipeline.NewASTPipeline(ct.AbsPath, rt.RootDir)

		if subAction == "inspect" {
			structTarget := typeFlag
			if structTarget == "" {
				structTarget = "ListModelsTuple"
			}

			report, err := pipe.InspectTuple(ctx, structTarget)
			if err != nil {
				return err
			}

			fmt.Fprint(stdout, report.RenderTable())

			return nil
		}

		if subAction == "apply" {
			structTarget := typeFlag
			if structTarget == "" {
				structTarget = "ListModelsTuple"
			}

			report, err := pipe.ApplyTupleSuggestions(ctx, structTarget, dryRunFlag)
			if err != nil {
				return err
			}

			relPath, _ := filepath.Rel(rt.RootDir, ct.AbsPath)

			modeStr := ""
			if dryRunFlag {
				modeStr = " (Dry-Run)"
			}

			fmt.Fprintf(
				stdout,
				"✔ Successfully applied semantic names & reserved fields to %s in %s%s (%d fields analyzed)\n",
				structTarget,
				filepath.ToSlash(relPath),
				modeStr,
				len(report.Indices),
			)

			if genFlag && !dryRunFlag {
				_ = pipe.TriggerCodegen(ctx)
			}

			return nil
		}

		var jsGlobs []string
		if jsFlag != "" {
			for _, p := range strings.Split(jsFlag, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					jsGlobs = append(jsGlobs, p)
				}
			}
		}

		res, err := pipe.DeobfuscateTuples(ctx, jsGlobs, dryRunFlag)
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(rt.RootDir, ct.AbsPath)
		if len(res.TuplesGenerated) == 0 {
			fmt.Fprintf(
				stdout,
				"No positional arrays or nested slices found requiring tuple deobfuscation in %s\n",
				filepath.ToSlash(relPath),
			)

			return nil
		}

		modeStr := ""
		if dryRunFlag {
			modeStr = " (Dry-Run)"
		}

		fmt.Fprintf(stdout, "Successfully deobfuscated %d tuple(s) in %s%s:\n",
			len(res.TuplesGenerated), filepath.ToSlash(relPath), modeStr)

		for i, tName := range res.TuplesGenerated {
			mName := tName
			if i < len(res.MethodsUpdated) {
				mName = res.MethodsUpdated[i]
			}

			fmt.Fprintf(stdout, "  * %s -> generated @aoni:tuple for %s\n", tName, mName)
		}

		if genFlag && !dryRunFlag {
			_ = pipe.TriggerCodegen(ctx)
		}

		return nil

	case "split":
		from := fromFlag
		if from == "" {
			from = targetArg
		}

		return c.runSplit(ctx, stdout, rt.RootDir, rt.Config, from, toFlag, methodsFlag, outFlag, dryRunFlag, genFlag)

	case "rename":
		target := targetArg
		if target == "" {
			target = fromFlag
		}

		var targetFiles []string
		if target != "" {
			if ct, err := rt.ResolveContract(target); err == nil && ct != nil {
				targetFiles = []string{ct.AbsPath}
			}
		}

		if len(targetFiles) == 0 {
			targetFiles = rt.CollectFiles(nil)
		}

		if len(targetFiles) == 0 {
			return errors.New("no contract files found to refactor")
		}

		if typeFlag != "" && (fieldFlag != "" || toFlag != "" || replaceFlag != "") {
			toName := toFlag
			if toName == "" {
				toName = replaceFlag
			}

			renamedAny := false
			for _, fPath := range targetFiles {
				pipe := pipeline.NewASTPipeline(fPath, rt.RootDir)

				err := pipe.RenameField(ctx, typeFlag, fieldFlag, toName, dryRunFlag)
				if err == nil {
					renamedAny = true
					relPath, _ := filepath.Rel(rt.RootDir, fPath)

					modeStr := ""
					if dryRunFlag {
						modeStr = " (Dry-Run)"
					}

					fmt.Fprintf(stdout, "✔ Successfully renamed field in %s%s\n",
						filepath.ToSlash(relPath), modeStr)

					if genFlag && !dryRunFlag {
						_ = pipe.TriggerCodegen(ctx)
					}
				}
			}

			if !renamedAny {
				return fmt.Errorf("field %q not found in struct %q across target files", fieldFlag, typeFlag)
			}

			return nil
		}

		for _, fPath := range targetFiles {
			pipe := pipeline.NewASTPipeline(fPath, rt.RootDir)
			relPath, _ := filepath.Rel(rt.RootDir, fPath)

			renamed, err := pipe.RenameMethods(ctx, matchFlag, replaceFlag, dryRunFlag)
			if err != nil {
				return err
			}

			if len(renamed) == 0 {
				fmt.Fprintf(stdout, "No method names matched pattern %q in %s\n", matchFlag, filepath.ToSlash(relPath))
				continue
			}

			for _, r := range renamed {
				fmt.Fprintf(stdout, "  ↳ %s\n", r)
			}

			modeStr := ""
			if dryRunFlag {
				modeStr = " (Dry-Run)"
			}

			fmt.Fprintf(
				stdout,
				"✔ Successfully renamed %d method(s) in %s%s\n",
				len(renamed),
				filepath.ToSlash(relPath),
				modeStr,
			)

			if genFlag && !dryRunFlag {
				_ = pipe.TriggerCodegen(ctx)
			}
		}

		return nil

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
		return errors.New("usage: vortex ast split --from=<Interface> --methods=\"Get*,List*\" --to=<NewInterface>")
	}

	rt, _ := NewRuntime(rootDir)

	ct, err := rt.ResolveContract(fromContract)
	if err != nil {
		return err
	}

	srcFile := ct.AbsPath

	fset := token.NewFileSet()

	fileAst, err := parser.ParseFile(fset, srcFile, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", srcFile, err)
	}

	patterns := strings.Split(methodsPattern, ",")
	for i, p := range patterns {
		patterns[i] = strings.TrimSpace(p)
	}

	var (
		matchedIface     *ast.InterfaceType
		matchedIfaceName string
		keptMethods      []*ast.Field
		movedMethods     []*ast.Field
	)

	ast.Inspect(fileAst, func(n ast.Node) bool {
		typeSpec, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}

		iface, ok := typeSpec.Type.(*ast.InterfaceType)
		if !ok || iface.Methods == nil {
			return true
		}

		if !strings.EqualFold(typeSpec.Name.Name, fromContract) && len(fileAst.Decls) > 1 {
			return true
		}

		matchedIface = iface
		matchedIfaceName = typeSpec.Name.Name

		for _, m := range iface.Methods.List {
			if len(m.Names) == 0 {
				keptMethods = append(keptMethods, m)
				continue
			}

			methodName := m.Names[0].Name
			if matchesAnyPattern(methodName, patterns) {
				movedMethods = append(movedMethods, m)
			} else {
				keptMethods = append(keptMethods, m)
			}
		}

		return false
	})

	if matchedIface == nil {
		return fmt.Errorf("interface %q not found in %s", fromContract, srcFile)
	}

	if len(movedMethods) == 0 {
		return fmt.Errorf("no methods matched pattern %q in interface %s", methodsPattern, matchedIfaceName)
	}

	matchedIface.Methods.List = keptMethods

	newIfaceDecl := &ast.GenDecl{
		Tok: token.TYPE,
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

	var buf bytes.Buffer
	if outPath == "" {
		fileAst.Decls = append(fileAst.Decls, newIfaceDecl)
		if err := format.Node(&buf, fset, fileAst); err != nil {
			return fmt.Errorf("formatting modified file: %w", err)
		}

		relSrc, _ := filepath.Rel(rootDir, srcFile)
		if dryRun {
			fmt.Fprintf(
				stdout,
				"⚡ [vortex ast split] Dry-Run (%s -> %s & %s)\n",
				filepath.ToSlash(relSrc),
				matchedIfaceName,
				toInterface,
			)

			return nil
		}

		if err := os.WriteFile(srcFile, buf.Bytes(), 0o600); err != nil {
			return err
		}

		fmt.Fprintf(stdout, "✔ Successfully split %s:\n  • %s: %d method(s) retained\n  • %s: %d method(s) extracted\n",
			filepath.ToSlash(relSrc), matchedIfaceName, len(keptMethods), toInterface, len(movedMethods))
	} else {
		destFile := outPath
		if !filepath.IsAbs(destFile) && rootDir != "" {
			destFile = filepath.Join(rootDir, destFile)
		}

		newFileAst := &ast.File{
			Name:  ast.NewIdent(fileAst.Name.Name),
			Decls: []ast.Decl{newIfaceDecl},
		}

		for _, decl := range fileAst.Decls {
			if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
				newFileAst.Decls = append([]ast.Decl{genDecl}, newFileAst.Decls...)
			}
		}

		var srcBuf, destBuf bytes.Buffer

		_ = format.Node(&srcBuf, fset, fileAst)
		_ = format.Node(&destBuf, token.NewFileSet(), newFileAst)

		relSrc, _ := filepath.Rel(rootDir, srcFile)
		relDest, _ := filepath.Rel(rootDir, destFile)

		if dryRun {
			fmt.Fprintf(
				stdout,
				"⚡ [vortex ast split] Dry-Run (%s -> %s)\n",
				filepath.ToSlash(relSrc),
				filepath.ToSlash(relDest),
			)

			return nil
		}

		_ = os.MkdirAll(filepath.Dir(destFile), 0o755)
		_ = os.WriteFile(srcFile, srcBuf.Bytes(), 0o600)
		_ = os.WriteFile(destFile, destBuf.Bytes(), 0o600)

		fmt.Fprintf(stdout, "✔ Successfully split %s -> %s (%s, %d methods)\n",
			filepath.ToSlash(relSrc), filepath.ToSlash(relDest), toInterface, len(movedMethods))
	}

	if autoGen && !dryRun {
		pipe := pipeline.NewASTPipeline(srcFile, rootDir)
		_ = pipe.TriggerCodegen(ctx)
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
