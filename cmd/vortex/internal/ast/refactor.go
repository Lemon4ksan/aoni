// Copyright 2026 The Aoni Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package ast

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/base"
	"github.com/lemon4ksan/aoni/internal/codegen/pipeline"
)

func (c *Cmd) runEnum(
	ctx context.Context,
	rt *base.Runtime,
	target string,
	dryRunFlag, genFlag bool,
	stdout, _ io.Writer,
) error {
	ct, err := rt.ResolveContract(target)
	if err != nil {
		return err
	}

	pipe := pipeline.NewASTPipeline(ct.AbsPath, rt.RootDir)

	structTarget := "ListModelsTuple"

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
}

func (c *Cmd) runTuple(
	ctx context.Context,
	rt *base.Runtime,
	target, jsFlag string,
	dryRunFlag, genFlag bool,
	stdout, _ io.Writer,
) error {
	ct, err := rt.ResolveContract(target)
	if err != nil {
		return err
	}

	pipe := pipeline.NewASTPipeline(ct.AbsPath, rt.RootDir)

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
}

func (c *Cmd) runRename(
	ctx context.Context,
	rt *base.Runtime,
	target, matchFlag, replaceFlag, typeFlag, fieldFlag, toFlag string,
	dryRunFlag, genFlag bool,
	stdout, _ io.Writer,
) error {
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
}

func (c *Cmd) runSplit(
	ctx context.Context,
	rt *base.Runtime,
	fromContract, toInterface, methodsPattern, outPath string,
	dryRun, autoGen bool,
	stdout, _ io.Writer,
) error {
	if fromContract == "" || toInterface == "" || methodsPattern == "" {
		return errors.New("usage: vortex ast split --from=<Interface> --methods=\"Get*,List*\" --to=<NewInterface>")
	}

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

		relSrc, _ := filepath.Rel(rt.RootDir, srcFile)
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
		if !filepath.IsAbs(destFile) && rt.RootDir != "" {
			destFile = filepath.Join(rt.RootDir, destFile)
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

		relSrc, _ := filepath.Rel(rt.RootDir, srcFile)
		relDest, _ := filepath.Rel(rt.RootDir, destFile)

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
		pipe := pipeline.NewASTPipeline(srcFile, rt.RootDir)
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
