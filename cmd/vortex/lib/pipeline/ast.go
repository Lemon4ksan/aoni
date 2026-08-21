// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/builder"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/cache"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/enum"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/history"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/project"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/tuple"
)

// ASTPipeline coordinates AST analysis, tuple deobfuscation, refactoring, and code generation.
type ASTPipeline struct {
	TargetFile string
	RootDir    string
}

// NewASTPipeline creates a new pipeline targeting a specific Go contract file.
func NewASTPipeline(targetFile, rootDir string) *ASTPipeline {
	if rootDir == "" && targetFile != "" {
		rootDir, _, _ = project.FindRoot(filepath.Dir(targetFile))
	}

	return &ASTPipeline{
		TargetFile: targetFile,
		RootDir:    rootDir,
	}
}

// DeobfuscateTuples analyzes HAR fixtures and JS bundles to replace raw any/[]string parameters
// and return values with clean @aoni:tuple structs.
func (p *ASTPipeline) DeobfuscateTuples(
	ctx context.Context,
	jsGlobs []string,
	dryRun bool,
) (*tuple.DeobfuscateResult, error) {
	if p.TargetFile == "" {
		return nil, errors.New("target file is required")
	}

	return tuple.DeobfuscateFileWithJS(p.RootDir, p.TargetFile, jsGlobs, dryRun)
}

// RenameField renames a struct/tuple field or assigns a semantic name to an index tag (e.g. Field0 -> ModelID).
func (p *ASTPipeline) RenameField(ctx context.Context, structName, fieldName, newName string, dryRun bool) error {
	if p.TargetFile == "" {
		return errors.New("target file is required")
	}

	fset := token.NewFileSet()

	fileAst, err := parser.ParseFile(fset, p.TargetFile, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", p.TargetFile, err)
	}

	var (
		foundStruct bool
		foundField  bool
		oldIdent    *ast.Ident
	)

	ast.Inspect(fileAst, func(n ast.Node) bool {
		typeSpec, ok := n.(*ast.TypeSpec)
		if !ok || !strings.EqualFold(typeSpec.Name.Name, structName) {
			return true
		}

		foundStruct = true

		st, ok := typeSpec.Type.(*ast.StructType)
		if !ok || st.Fields == nil {
			return true
		}

		for _, field := range st.Fields.List {
			matched := false
			if len(field.Names) > 0 && strings.EqualFold(field.Names[0].Name, fieldName) {
				matched = true
			} else if field.Tag != nil {
				tagVal := strings.Trim(field.Tag.Value, "`")

				tagNum := strings.TrimPrefix(strings.TrimPrefix(fieldName, "Field"), "field")
				if strings.Contains(tagVal, fmt.Sprintf(`aoni:"%s"`, fieldName)) ||
					strings.Contains(tagVal, fmt.Sprintf(`aoni:"%s"`, tagNum)) ||
					strings.Contains(tagVal, fmt.Sprintf(`json:"%s"`, fieldName)) {
					matched = true
				}
			}

			if matched && len(field.Names) > 0 {
				foundField = true

				oldIdent = field.Names[0]
				if !dryRun {
					field.Names[0] = ast.NewIdent(newName)
				}

				break
			}
		}

		return false
	})

	if !foundStruct {
		return fmt.Errorf("struct %q not found in %s", structName, p.TargetFile)
	}

	if !foundField {
		return fmt.Errorf("field %q not found in struct %q", fieldName, structName)
	}

	if dryRun {
		return nil
	}

	// Record undo history
	if oldIdent != nil && p.RootDir != "" {
		_, _ = history.Record(
			p.RootDir,
			fmt.Sprintf("vortex ast rename --type=%s --field=%s --to=%s", structName, fieldName, newName),
			[]string{p.TargetFile},
		)
	}

	var buf strings.Builder
	if err := format.Node(&buf, fset, fileAst); err != nil {
		return fmt.Errorf("formatting modified AST: %w", err)
	}

	return os.WriteFile(p.TargetFile, []byte(buf.String()), 0o600)
}

// RenameMethods applies regex pattern renaming across all method signatures in the contract.
func (p *ASTPipeline) RenameMethods(
	ctx context.Context,
	matchPattern, replacePattern string,
	dryRun bool,
) ([]string, error) {
	if p.TargetFile == "" {
		return nil, errors.New("target file is required")
	}

	re, err := regexp.Compile(matchPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex match pattern %q: %w", matchPattern, err)
	}

	contentBytes, err := os.ReadFile(p.TargetFile)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", p.TargetFile, err)
	}

	content := string(contentBytes)

	fset := token.NewFileSet()

	fileAst, err := parser.ParseFile(fset, p.TargetFile, contentBytes, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", p.TargetFile, err)
	}

	var (
		renamed      []string
		replacements = make(map[string]string)
	)

	ast.Inspect(fileAst, func(n ast.Node) bool {
		iface, ok := n.(*ast.InterfaceType)
		if !ok || iface.Methods == nil {
			return true
		}

		for _, m := range iface.Methods.List {
			if len(m.Names) == 0 {
				continue
			}

			oldName := m.Names[0].Name
			if re.MatchString(oldName) {
				newName := re.ReplaceAllString(oldName, replacePattern)
				if newName != oldName {
					renamed = append(renamed, fmt.Sprintf("%s -> %s", oldName, newName))
					replacements[oldName] = newName
				}
			}
		}

		return true
	})

	if len(renamed) == 0 {
		return nil, nil
	}

	if dryRun {
		return renamed, nil
	}

	for oldName, newName := range replacements {
		methodPattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(oldName) + `\b\s*\(`)
		content = methodPattern.ReplaceAllString(content, newName+"(")
	}

	if p.RootDir != "" {
		_, _ = history.Record(
			p.RootDir,
			fmt.Sprintf("vortex refactor rename --match=%q --replace=%q", matchPattern, replacePattern),
			[]string{p.TargetFile},
		)
	}

	if err := os.WriteFile(filepath.Clean(p.TargetFile), []byte(content), 0o600); err != nil { //nolint:gosec
		return nil, err
	}

	return renamed, nil
}

// InspectTuple extracts HAR traffic samples and produces a multi-sample saliency analysis report.
func (p *ASTPipeline) InspectTuple(ctx context.Context, structName string) (*tuple.TupleAnalysisReport, error) {
	if p.TargetFile == "" {
		return nil, errors.New("target file is required")
	}

	contentBytes, err := os.ReadFile(p.TargetFile)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", p.TargetFile, err)
	}

	fset := token.NewFileSet()

	fileAst, err := parser.ParseFile(fset, p.TargetFile, contentBytes, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", p.TargetFile, err)
	}

	// 1. Locate source HAR file and target RPC method
	var (
		sourceSpec string
		targetRPC  string
	)

	methodPrefix := strings.TrimSuffix(structName, "Tuple")
	methodPrefix = strings.TrimSuffix(methodPrefix, "Request")

	for _, cg := range fileAst.Comments {
		for _, c := range cg.List {
			text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
			if strings.HasPrefix(text, "@source") {
				parts := strings.SplitN(text, " ", 2)
				if len(parts) == 2 {
					sourceSpec = strings.Trim(parts[1], `"`+"'")
				}
			}
		}
	}

	ast.Inspect(fileAst, func(n ast.Node) bool {
		if iface, ok := n.(*ast.InterfaceType); ok && iface.Methods != nil {
			for _, m := range iface.Methods.List {
				if len(m.Names) > 0 && strings.EqualFold(m.Names[0].Name, methodPrefix) {
					if m.Doc != nil {
						for _, c := range m.Doc.List {
							t := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
							if strings.HasPrefix(t, "@post") || strings.HasPrefix(t, "@get") {
								parts := strings.SplitN(t, " ", 2)
								if len(parts) == 2 {
									targetRPC = strings.Trim(parts[1], `"`+"'")
								}
							}
						}
					}
				}
			}
		}

		return true
	})

	if targetRPC == "" {
		targetRPC = methodPrefix
	}

	// 2. Collect all available HAR files (sourceSpec, project root, and .vortex cache)
	var allHARBytes [][]byte

	if sourceSpec != "" {
		harPath := sourceSpec
		if !filepath.IsAbs(harPath) && p.RootDir != "" {
			harPath = filepath.Join(p.RootDir, harPath)
		}

		if data, err := os.ReadFile(filepath.Clean(harPath)); err == nil { //nolint:gosec
			allHARBytes = append(allHARBytes, data)
		}
	}

	if p.RootDir != "" {
		rootHARs, _ := filepath.Glob(filepath.Join(p.RootDir, "*.har"))
		for _, hPath := range rootHARs {
			if data, err := os.ReadFile(filepath.Clean(hPath)); err == nil { //nolint:gosec
				allHARBytes = append(allHARBytes, data)
			}
		}

		if idx, _, _ := cache.LoadTrafficIndex(p.RootDir); idx != nil {
			for k := range idx.Entries {
				if data, _, err := cache.GetTraffic(p.RootDir, k); err == nil && len(data) > 0 {
					allHARBytes = append(allHARBytes, data)
				}
			}
		}
	}

	if len(allHARBytes) == 0 {
		return nil, fmt.Errorf("no HAR traffic captures found in project (searched %q and .vortex/cache)", sourceSpec)
	}

	endpointKey := targetRPC
	if strings.Contains(endpointKey, "/") {
		endpointKey = endpointKey[strings.LastIndex(endpointKey, "/")+1:]
	}

	var (
		samples         [][]any
		matchedEndpoint string
		discoveredNames = make(map[int]string)
	)

	for _, harBytes := range allHARBytes {
		harResponses := tuple.ExtractHARResponses(harBytes, endpointKey)
		for ep, list := range harResponses {
			if len(list) > 0 {
				samples = append(samples, list...)
				matchedEndpoint = ep
			}
		}

		baseKey := strings.TrimPrefix(strings.TrimPrefix(endpointKey, "Get"), "Update")
		baseKey = strings.TrimPrefix(baseKey, "List")
		baseKey = strings.ToLower(baseKey)

		masks := tuple.ExtractFieldMasks(harBytes)
		for url, m := range masks {
			urlLower := strings.ToLower(url)
			if strings.Contains(urlLower, baseKey) {
				for idx, name := range m {
					discoveredNames[idx] = name
				}
			}
		}
	}

	if len(samples) == 0 {
		return nil, fmt.Errorf(
			"no response tuple samples found for %s in %d traffic captures",
			targetRPC,
			len(allHARBytes),
		)
	}

	return tuple.AnalyzeTupleSamples(structName, matchedEndpoint, samples, discoveredNames), nil
}

// ApplyTupleSuggestions applies semantic and reserved names from multi-sample tuple analysis.
func (p *ASTPipeline) ApplyTupleSuggestions(
	ctx context.Context,
	structName string,
	dryRun bool,
) (*tuple.TupleAnalysisReport, error) {
	report, err := p.InspectTuple(ctx, structName)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()

	fileAst, err := parser.ParseFile(fset, p.TargetFile, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", p.TargetFile, err)
	}

	indexMap := make(map[int]tuple.TupleIndexReport)
	for _, idx := range report.Indices {
		indexMap[idx.Index] = idx
	}

	var targetStruct *ast.TypeSpec
	ast.Inspect(fileAst, func(n ast.Node) bool {
		if ts, ok := n.(*ast.TypeSpec); ok && strings.EqualFold(ts.Name.Name, structName) {
			targetStruct = ts
			return false
		}

		return true
	})

	if targetStruct == nil {
		return nil, fmt.Errorf("struct %q not found in %s", structName, p.TargetFile)
	}

	st, ok := targetStruct.Type.(*ast.StructType)
	if !ok || st.Fields == nil {
		return nil, fmt.Errorf("type %q is not a struct in %s", structName, p.TargetFile)
	}

	for _, field := range st.Fields.List {
		if field.Tag == nil || len(field.Names) == 0 {
			continue
		}

		tagVal := strings.Trim(field.Tag.Value, "`")

		var idxNum int

		n, _ := fmt.Sscanf(tagVal, `aoni:"%d"`, &idxNum)
		if n == 0 {
			continue
		}

		if idxReport, ok := indexMap[idxNum]; ok && idxReport.DefaultName != "" {
			if !dryRun {
				field.Names[0] = ast.NewIdent(idxReport.DefaultName)
				if idxReport.IsReserved {
					field.Type = ast.NewIdent("any")
				}
			}
		}
	}

	if dryRun {
		return report, nil
	}

	var buf strings.Builder
	if err := format.Node(&buf, fset, fileAst); err != nil {
		return nil, fmt.Errorf("formatting modified struct AST: %w", err)
	}

	if p.RootDir != "" {
		_, _ = history.Record(p.RootDir, "vortex ast tuple apply --type="+structName, []string{p.TargetFile})
	}

	if err := os.WriteFile(filepath.Clean(p.TargetFile), []byte(buf.String()), 0o600); err != nil { //nolint:gosec
		return nil, err
	}

	return report, nil
}

// InferAndInjectEnums analyzes traffic and generates typed Go enums for string/int fields.
func (p *ASTPipeline) InferAndInjectEnums(
	ctx context.Context,
	structName string,
	dryRun bool,
) ([]enum.EnumSpec, error) {
	if p.TargetFile == "" {
		return nil, errors.New("target file is required")
	}

	contentBytes, err := os.ReadFile(filepath.Clean(p.TargetFile)) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", p.TargetFile, err)
	}

	fset := token.NewFileSet()

	fileAst, err := parser.ParseFile(fset, p.TargetFile, contentBytes, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", p.TargetFile, err)
	}

	var sourceSpec string
	for _, cg := range fileAst.Comments {
		for _, c := range cg.List {
			text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
			if strings.HasPrefix(text, "@source") {
				parts := strings.SplitN(text, " ", 2)
				if len(parts) == 2 {
					sourceSpec = strings.Trim(parts[1], `"`+"'")
				}
			}
		}
	}

	harPath := sourceSpec
	if !filepath.IsAbs(harPath) && p.RootDir != "" && harPath != "" {
		harPath = filepath.Join(p.RootDir, harPath)
	}

	cleanHarPath := filepath.Clean(harPath)
	if _, err := os.Stat(cleanHarPath); err != nil { //nolint:gosec
		candidates, _ := filepath.Glob(filepath.Join(p.RootDir, "*.har"))
		if len(candidates) > 0 {
			cleanHarPath = filepath.Clean(candidates[0])
		}
	}

	if _, err := os.Stat(cleanHarPath); err != nil { //nolint:gosec
		return nil, fmt.Errorf("could not locate source HAR file %q", sourceSpec)
	}

	harBytes, err := os.ReadFile(cleanHarPath) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("reading HAR file %s: %w", cleanHarPath, err)
	}

	specs := enum.ExtractEnumsFromHAR(harBytes, structName)
	if len(specs) == 0 {
		return nil, fmt.Errorf("no candidate enums found in %s", filepath.Base(harPath))
	}

	if dryRun {
		return specs, nil
	}

	if err := enum.InjectEnums(p.TargetFile, structName, specs); err != nil {
		return nil, err
	}

	if p.RootDir != "" {
		_, _ = history.Record(p.RootDir, "vortex ast enum infer --type="+structName, []string{p.TargetFile})
	}

	return specs, nil
}

// TriggerCodegen triggers zero-allocation client and mock generation for the target contract.
func (p *ASTPipeline) TriggerCodegen(ctx context.Context) error {
	if p.TargetFile == "" {
		return nil
	}

	b := builder.New(builder.Config{})
	_, err := b.BuildFiles(ctx, []string{p.TargetFile})

	return err
}
