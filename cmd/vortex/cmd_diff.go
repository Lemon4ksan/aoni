// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/builder"
	"github.com/lemon4ksan/aoni/internal/codegen/cache"
	"github.com/lemon4ksan/aoni/internal/codegen/diff"
	"github.com/lemon4ksan/aoni/internal/codegen/git"
	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	"github.com/lemon4ksan/aoni/internal/codegen/merge"
	"github.com/lemon4ksan/aoni/internal/codegen/openapi"
	codeparser "github.com/lemon4ksan/aoni/internal/codegen/parser"
	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdDiff compares local Go interface contracts against an external OpenAPI specification or Git ref.
type CmdDiff struct{}

func (c *CmdDiff) Name() string      { return "diff" }
func (c *CmdDiff) Aliases() []string { return []string{"drift", "compare"} }
func (c *CmdDiff) Synopsis() string {
	return "Detect contract drift between local Go interfaces and OpenAPI specifications or Git refs"
}

func (c *CmdDiff) Usage() string {
	return "vortex diff [flags] [--against=<ref>] [<remote-spec.json|yaml>] [local-files...]"
}

func (c *CmdDiff) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		failOnDriftFlag bool
		strictFlag      bool
		jsonFlag        bool
		serviceFlag     string
		specFlag        string
		againstFlag     string
		addFlag         bool
	)

	BoolVar(
		fs,
		&failOnDriftFlag,
		"fail-on-drift",
		"",
		false,
		"Exit with non-zero code if breaking contract drift is detected",
	)
	BoolVar(
		fs,
		&strictFlag,
		"strict",
		"",
		false,
		"Exit with non-zero code on any drift (including non-breaking and ghosts)",
	)
	BoolVar(fs, &jsonFlag, "json", "", false, "Output report in JSON format")
	StringVar(fs, &serviceFlag, "service", "", "", "Filter comparison to a specific service interface name")
	StringVar(fs, &specFlag, "spec", "s", "", "Path to remote OpenAPI/Swagger JSON or YAML specification")
	StringVar(
		fs,
		&againstFlag,
		"against",
		"",
		"",
		"Compare local Go contracts against a Git branch, tag, or commit in-memory (e.g. --against=origin/main)",
	)
	BoolVar(
		fs,
		&addFlag,
		"add",
		"a",
		false,
		"Additive mode: inspect only incoming additions/enrichments and suppress ghost endpoints absent from spec/HAR",
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex diff — Contract Drift & Breaking Change Inspector\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex diff [flags] <spec.json|yaml|har> [paths...]\n")
		fmt.Fprintf(stderr, "  vortex diff --against=<branch|tag|commit> [paths...]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nExamples:\n")
		fmt.Fprintf(
			stderr,
			"  vortex diff ./openapi.json                      # Compare against OpenAPI specification\n",
		)
		fmt.Fprintf(
			stderr,
			"  vortex diff ./traffic.har ./pkg/api/api.go       # Check additive diff against captured HAR\n",
		)
		fmt.Fprintf(
			stderr,
			"  vortex diff --add ./traffic.har ./pkg/api        # Additive diff (ghost endpoints suppressed)\n",
		)
		fmt.Fprintf(
			stderr,
			"  vortex diff --against=main ./pkg/api             # Detect breaking changes against Git branch\n",
		)
		fmt.Fprintf(stderr, "  vortex diff --fail-on-drift ./openapi.json       # CI check (fails on breaking drift)\n")
	}

	positional, err := ParseInterspersedFlags(fs, args)
	if err != nil {
		return err
	}

	// Branch 1: Git-based comparison
	if againstFlag != "" {
		return c.runGitDiff(ctx, againstFlag, positional, jsonFlag, stdout)
	}

	// Branch 2: Spec-to-Spec direct comparison or HAR-to-HAR differential
	if len(positional) >= 2 && (strings.HasSuffix(positional[0], ".har") || isSpecFile(positional[0])) &&
		(strings.HasSuffix(positional[1], ".har") || isSpecFile(positional[1])) {
		cwd, _ := os.Getwd()

		rootDir, _, _ := project.FindRoot(cwd)
		if strings.HasSuffix(positional[0], ".har") && strings.HasSuffix(positional[1], ".har") {
			return c.runHARDifferential(ctx, rootDir, positional[0], positional[1], stdout)
		}

		return c.runSpecDiff(ctx, positional[0], positional[1], failOnDriftFlag, strictFlag, jsonFlag, stdout)
	}

	// Branch 2b: Diff a new HAR against cumulative cache stack automatically
	if len(positional) == 1 && strings.HasSuffix(positional[0], ".har") {
		cwd, _ := os.Getwd()

		rootDir, _, _ := project.FindRoot(cwd)
		if idx, _, _ := cache.LoadTrafficIndex(rootDir); idx != nil && len(idx.Entries) > 0 {
			return c.runHARDifferential(ctx, rootDir, "cache", positional[0], stdout)
		}
	}

	if specFlag != "" && len(positional) > 0 && isSpecFile(positional[0]) {
		if strings.HasSuffix(specFlag, ".har") && strings.HasSuffix(positional[0], ".har") {
			cwd, _ := os.Getwd()
			rootDir, _, _ := project.FindRoot(cwd)
			return c.runHARDifferential(ctx, rootDir, specFlag, positional[0], stdout)
		}

		return c.runSpecDiff(ctx, specFlag, positional[0], failOnDriftFlag, strictFlag, jsonFlag, stdout)
	}

	// Branch 3: Spec vs Local Go Contracts
	specFile := specFlag

	var localPaths []string

	if specFile == "" {
		if len(positional) == 0 {
			return errors.New(
				"missing OpenAPI specification or --against flag (e.g. `vortex diff swagger.json` or `vortex diff --against=origin/main`)",
			)
		}

		specFile = positional[0]
		localPaths = positional[1:]
	} else {
		localPaths = positional
	}

	// 1. Load remote OpenAPI specification
	doc, err := openapi.LoadSpec(specFile, nil)
	if err != nil {
		return fmt.Errorf("failed loading OpenAPI spec %q: %w", specFile, err)
	}

	rt, _ := NewRuntime("")

	files := rt.CollectFiles(localPaths)
	if len(files) == 0 {
		return errors.New("no Go source files found to compare against specification")
	}

	p := codeparser.NewParser()

	var (
		allServices []*ir.ServiceIR
		allStructs  []*ir.StructIR
	)

	for _, file := range files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if strings.HasSuffix(file, ".gen.go") {
			continue
		}

		root, parseErr := p.ParseFile(file)
		if parseErr != nil {
			continue
		}

		for _, s := range root.Services {
			if serviceFlag != "" && !strings.EqualFold(s.Name, serviceFlag) {
				continue
			}

			allServices = append(allServices, s)
		}

		allStructs = append(allStructs, root.Structs...)
	}

	if len(allServices) == 0 {
		return errors.New("no matching service interfaces found in local Go files")
	}

	localRoot := &ir.RootIR{
		Services: allServices,
		Structs:  allStructs,
	}

	localDesc := strings.Join(files, ", ")
	if len(files) > 3 {
		localDesc = fmt.Sprintf("%d files (%s...)", len(files), filepath.Base(files[0]))
	}

	// 3. Run semantic diff engine
	isAdditive := addFlag
	if strings.HasSuffix(strings.ToLower(specFile), ".har") && !strictFlag {
		isAdditive = true
	}

	engine := diff.NewEngine()
	report := engine.CompareWithOptions(localRoot, doc, localDesc, filepath.Base(specFile), diff.DiffOptions{
		Additive: isAdditive,
	})

	// 4. Render output
	reporter := NewReporter(stdout, stderr)
	if err := reporter.RenderDiff(report, jsonFlag); err != nil {
		return err
	}

	// 5. Check exit constraints
	if strictFlag && report.HasDrift() {
		return fmt.Errorf("contract drift detected under strict mode (%d issue(s))", len(report.Drifts))
	}

	if failOnDriftFlag && report.HasBreaking() {
		return fmt.Errorf("breaking contract drift detected (%d breaking issue(s))", report.BreakingCount())
	}

	return nil
}

func (c *CmdDiff) runGitDiff(
	ctx context.Context,
	targetRef string,
	paths []string,
	jsonOut bool,
	stdout io.Writer,
) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	rootDir, err := git.RootDir(ctx, cwd)
	if err != nil {
		return err
	}

	files := builder.CollectInputFiles("", paths)
	if len(files) == 0 {
		if cfg, _ := project.Load(rootDir); cfg != nil && len(cfg.Contracts) > 0 {
			for _, ct := range cfg.Contracts {
				files = append(files, filepath.Join(rootDir, ct.File))
			}
		}
	}

	if len(files) == 0 {
		return errors.New("no Go contract files found to compare")
	}

	p := codeparser.NewParser()
	reconciler := merge.NewReconciler()

	fmt.Fprintf(stdout, "⚡ [vortex diff] Comparing working tree against '%s':\n\n", targetRef)

	totalDeltas := 0

	for _, file := range files {
		relPath, relErr := filepath.Rel(rootDir, file)
		if relErr != nil {
			relPath = file
		}

		diskIR, parseErr := p.ParseFile(file)
		if parseErr != nil {
			continue
		}

		remoteBytes, showErr := git.ShowFile(ctx, rootDir, targetRef, relPath)
		if showErr != nil {
			continue
		}

		remoteIR, remoteErr := p.ParseSource(relPath, remoteBytes)
		if remoteErr != nil {
			continue
		}

		res, recErr := reconciler.Reconcile(nil, diskIR, remoteIR)
		if recErr != nil || len(res.Deltas) == 0 {
			continue
		}

		fmt.Fprintf(stdout, "● %s:\n", relPath)

		for _, d := range res.Deltas {
			totalDeltas++

			prefix := "[+]"
			switch d.Kind {
			case merge.DeltaModifyMethod, merge.DeltaModifyStruct:
				prefix = "[~]"
			case merge.DeltaDeprecate:
				prefix = "[-]"
			}

			fmt.Fprintf(stdout, "  %s %s: %s\n", prefix, d.EntityName, d.Description)
		}

		fmt.Fprintf(stdout, "\n")
	}

	if totalDeltas == 0 {
		fmt.Fprintf(stdout, "✔ Working tree is in sync with '%s' (0 drift).\n", targetRef)
	}

	return nil
}

func (c *CmdDiff) runSpecDiff(
	_ context.Context,
	baseSpec, headSpec string,
	failOnDrift, strict, jsonOutput bool,
	stdout io.Writer,
) error {
	baseDoc, err := openapi.LoadSpec(baseSpec, nil)
	if err != nil {
		return fmt.Errorf("failed loading base spec %q: %w", baseSpec, err)
	}

	headDoc, err := openapi.LoadSpec(headSpec, nil)
	if err != nil {
		return fmt.Errorf("failed loading target spec %q: %w", headSpec, err)
	}

	engine := diff.NewEngine()
	report := engine.CompareSpecs(baseDoc, headDoc, baseSpec, headSpec)

	if jsonOutput {
		data, jErr := report.RenderJSON()
		if jErr != nil {
			return jErr
		}

		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprint(stdout, report.Render(true))
	}

	if failOnDrift && report.HasBreaking() {
		return fmt.Errorf("diff detected %d breaking contract change(s)", report.BreakingCount())
	}

	if strict && report.HasDrift() {
		return fmt.Errorf("diff detected %d contract drift(s)", len(report.Drifts))
	}

	return nil
}

func isSpecFile(p string) bool {
	for _, token := range strings.Split(p, ",") {
		token = strings.TrimSpace(token)
		if strings.HasSuffix(token, ".har") || strings.HasSuffix(token, ".json") ||
			strings.HasSuffix(token, ".yaml") || strings.HasSuffix(token, ".yml") {
			return true
		}
	}

	return false
}

type harEntryDiff struct {
	Endpoint string
	Struct   string
	Field    string
	Tag      string
	OldVal   string
	NewVal   string
}

func (c *CmdDiff) runHARDifferential(
	_ context.Context,
	rootDir, fileA, fileB string,
	stdout io.Writer,
) error {
	type harSimpleEntry struct {
		Request struct {
			Method   string `json:"method"`
			URL      string `json:"url"`
			PostData *struct {
				Text string `json:"text"`
			} `json:"postData"`
		} `json:"request"`
		Response struct {
			Status  int `json:"status"`
			Content struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"response"`
	}

	type harSimpleDoc struct {
		Log struct {
			Entries []harSimpleEntry `json:"entries"`
		} `json:"log"`
	}

	var docA, docB harSimpleDoc

	// 1. Load Base Dataset (cumulative cache stack or individual file)
	if fileA == "cache" || fileA == "stack" || fileA == "@cache" {
		idx, _, _ := cache.LoadTrafficIndex(rootDir)
		if idx == nil || len(idx.Entries) == 0 {
			return errors.New("no cached traffic sessions found in .vortex/cache/traffic/")
		}

		sessionCount := 0
		for k := range idx.Entries {
			if data, _, err := cache.GetTraffic(rootDir, k); err == nil && len(data) > 0 {
				var subDoc harSimpleDoc
				if json.Unmarshal(data, &subDoc) == nil && len(subDoc.Log.Entries) > 0 {
					docA.Log.Entries = append(docA.Log.Entries, subDoc.Log.Entries...)
					sessionCount++
				}
			}
		}

		fileA = fmt.Sprintf("Cumulative Cache Stack (%d session(s), %d entries)", sessionCount, len(docA.Log.Entries))
	} else {
		dataA, err := os.ReadFile(fileA)
		if err != nil {
			if cData, _, cErr := cache.GetTraffic(rootDir, fileA); cErr == nil {
				dataA = cData
			} else {
				return fmt.Errorf("reading base HAR %s: %w", fileA, err)
			}
		}

		if err := json.Unmarshal(dataA, &docA); err != nil {
			return fmt.Errorf("parsing base HAR JSON %s: %w", fileA, err)
		}
	}

	// 2. Load Target Dataset
	dataB, err := os.ReadFile(fileB)
	if err != nil {
		if cData, _, cErr := cache.GetTraffic(rootDir, fileB); cErr == nil {
			dataB = cData
		} else {
			return fmt.Errorf("reading target HAR %s: %w", fileB, err)
		}
	}

	if err := json.Unmarshal(dataB, &docB); err != nil {
		return fmt.Errorf("parsing target HAR JSON %s: %w", fileB, err)
	}

	entriesB := make(map[string]harSimpleEntry)
	for _, eb := range docB.Log.Entries {
		key := normalizeRouteKey(eb.Request.Method, eb.Request.URL)
		entriesB[key] = eb
	}

	rt, _ := NewRuntime(rootDir)
	p := codeparser.NewParser()

	var (
		allServices []*ir.ServiceIR
		allStructs  []*ir.StructIR
		allTuples   []*ir.TupleIR
	)

	for _, f := range rt.CollectFiles(nil) {
		if strings.HasSuffix(f, ".gen.go") || strings.HasSuffix(f, "_test.go") {
			continue
		}

		if root, err := p.ParseFile(f); err == nil {
			allServices = append(allServices, root.Services...)
			allStructs = append(allStructs, root.Structs...)
			allTuples = append(allTuples, root.Tuples...)
		}
	}

	var deltas []harEntryDiff

	for _, ea := range docA.Log.Entries {
		key := normalizeRouteKey(ea.Request.Method, ea.Request.URL)
		if isNoiseEndpoint(key) {
			continue
		}

		eb, found := entriesB[key]
		if !found {
			continue
		}

		// 1. Compare Request Payloads
		if ea.Request.PostData != nil && eb.Request.PostData != nil &&
			ea.Request.PostData.Text != "" && eb.Request.PostData.Text != "" {
			var bodyA, bodyB any

			textA := strings.TrimPrefix(ea.Request.PostData.Text, ")]}'\n")

			textB := strings.TrimPrefix(eb.Request.PostData.Text, ")]}'\n")
			if errA := json.Unmarshal([]byte(textA), &bodyA); errA == nil {
				if errB := json.Unmarshal([]byte(textB), &bodyB); errB == nil {
					structName, fieldMap := resolveStructForRoute(key, true, allServices, allStructs, allTuples)
					compareJSONNodes("", bodyA, bodyB, func(path string, valA, valB any) {
						fieldName, tag := resolveFieldFromPath(path, fieldMap)
						deltas = append(deltas, harEntryDiff{
							Endpoint: key,
							Struct:   structName,
							Field:    fieldName,
							Tag:      tag,
							OldVal:   formatDeltaValue(valA),
							NewVal:   formatDeltaValue(valB),
						})
					})
				}
			}
		}

		// 2. Compare Response Payloads
		if ea.Response.Content.Text != "" && eb.Response.Content.Text != "" {
			var bodyA, bodyB any

			textA := strings.TrimPrefix(ea.Response.Content.Text, ")]}'\n")

			textB := strings.TrimPrefix(eb.Response.Content.Text, ")]}'\n")
			if errA := json.Unmarshal([]byte(textA), &bodyA); errA == nil {
				if errB := json.Unmarshal([]byte(textB), &bodyB); errB == nil {
					structName, fieldMap := resolveStructForRoute(key, false, allServices, allStructs, allTuples)
					compareJSONNodes("", bodyA, bodyB, func(path string, valA, valB any) {
						fieldName, tag := resolveFieldFromPath(path, fieldMap)
						deltas = append(deltas, harEntryDiff{
							Endpoint: key,
							Struct:   structName,
							Field:    fieldName,
							Tag:      tag,
							OldVal:   formatDeltaValue(valA),
							NewVal:   formatDeltaValue(valB),
						})
					})
				}
			}
		}
	}

	// 3. New endpoints captured only in session B
	seenA := make(map[string]bool)
	for _, ea := range docA.Log.Entries {
		seenA[normalizeRouteKey(ea.Request.Method, ea.Request.URL)] = true
	}

	for _, eb := range docB.Log.Entries {
		key := normalizeRouteKey(eb.Request.Method, eb.Request.URL)
		if seenA[key] || isNoiseEndpoint(key) {
			continue
		}

		if eb.Request.PostData != nil && eb.Request.PostData.Text != "" {
			var bodyB any

			textB := strings.TrimPrefix(eb.Request.PostData.Text, ")]}'\n")
			if errB := json.Unmarshal([]byte(textB), &bodyB); errB == nil {
				structName, fieldMap := resolveStructForRoute(key, true, allServices, allStructs, allTuples)
				compareJSONNodes("", nil, bodyB, func(path string, _, valB any) {
					fieldName, tag := resolveFieldFromPath(path, fieldMap)
					deltas = append(deltas, harEntryDiff{
						Endpoint: key,
						Struct:   structName,
						Field:    fieldName,
						Tag:      tag,
						OldVal:   "<nil>",
						NewVal:   formatDeltaValue(valB),
					})
				})
			}
		}

		if eb.Response.Content.Text != "" {
			var bodyB any

			textB := strings.TrimPrefix(eb.Response.Content.Text, ")]}'\n")
			if errB := json.Unmarshal([]byte(textB), &bodyB); errB == nil {
				structName, fieldMap := resolveStructForRoute(key, false, allServices, allStructs, allTuples)
				compareJSONNodes("", nil, bodyB, func(path string, _, valB any) {
					fieldName, tag := resolveFieldFromPath(path, fieldMap)
					deltas = append(deltas, harEntryDiff{
						Endpoint: key,
						Struct:   structName,
						Field:    fieldName,
						Tag:      tag,
						OldVal:   "<nil>",
						NewVal:   formatDeltaValue(valB),
					})
				})
			}
		}
	}

	if len(deltas) == 0 {
		fmt.Fprintf(
			stdout,
			"✔ Traffic comparison: 0 parameter delta(s) between %s and %s\n",
			filepath.Base(fileA),
			filepath.Base(fileB),
		)

		return nil
	}

	fmt.Fprintf(stdout, "🔍 Traffic Diff (%s ↔ %s): %d parameter delta(s)\n\n",
		filepath.Base(fileA), filepath.Base(fileB), len(deltas))

	grouped := make(map[string][]harEntryDiff)

	var groupOrder []string
	for _, d := range deltas {
		groupKey := d.Struct + " (" + d.Endpoint + ")"
		if len(grouped[groupKey]) == 0 {
			groupOrder = append(groupOrder, groupKey)
		}

		grouped[groupKey] = append(grouped[groupKey], d)
	}

	for _, gKey := range groupOrder {
		items := grouped[gKey]
		fmt.Fprintf(stdout, "📍 %s\n", gKey)

		for _, it := range items {
			tagInfo := ""
			if it.Tag != "" {
				tagInfo = fmt.Sprintf(" (tag %s)", it.Tag)
			}

			fmt.Fprintf(stdout, "  • %s%s: %s ➔ %s      ➜ vortex ast rename --type=%s --field=%s --to=<NAME>\n",
				it.Field, tagInfo, it.OldVal, it.NewVal, it.Struct, it.Field)
		}

		fmt.Fprintf(stdout, "\n")
	}

	return nil
}

func isNoiseEndpoint(key string) bool {
	lower := strings.ToLower(key)

	return strings.Contains(lower, "/log") ||
		strings.Contains(lower, "playlog") ||
		strings.Contains(lower, "google-analytics") ||
		strings.Contains(lower, "telemetry") ||
		strings.Contains(lower, "doubleclick") ||
		strings.Contains(lower, "upload/drive")
}

func normalizeRouteKey(method, rawURL string) string {
	m := strings.ToUpper(strings.TrimSpace(method))
	if m == "" {
		m = "POST"
	}

	path := rawURL
	if u, err := url.Parse(rawURL); err == nil && u.Path != "" {
		path = u.Path
	}

	return m + " " + path
}

func compareJSONNodes(prefix string, a, b any, onDelta func(path string, oldVal, newVal any)) {
	if a == nil && b == nil {
		return
	}

	depth := strings.Count(prefix, ".")
	if depth >= 3 {
		if fmt.Sprintf("%v", a) != fmt.Sprintf("%v", b) {
			onDelta(prefix, a, b)
		}

		return
	}

	if a == nil && b != nil {
		switch valB := b.(type) {
		case []any:
			limit := len(valB)
			if limit > 2 && depth >= 1 {
				limit = 2 // Fold repeated slice items for cleaner DX
			}

			for i := 0; i < limit; i++ {
				indexPath := strconv.Itoa(i)
				if prefix != "" {
					indexPath = prefix + "." + indexPath
				}

				compareJSONNodes(indexPath, nil, valB[i], onDelta)
			}

			return

		case map[string]any:
			for k, itemB := range valB {
				keyPath := k
				if prefix != "" {
					keyPath = prefix + "." + k
				}

				compareJSONNodes(keyPath, nil, itemB, onDelta)
			}

			return
		}

		onDelta(prefix, a, b)

		return
	}

	if a != nil && b == nil {
		onDelta(prefix, a, b)
		return
	}

	switch valA := a.(type) {
	case []any:
		if valB, ok := b.([]any); ok {
			maxLen := len(valA)
			if len(valB) > maxLen {
				maxLen = len(valB)
			}

			limit := maxLen
			if limit > 2 && depth >= 1 {
				limit = 2 // Fold repeated slice items
			}

			for i := 0; i < limit; i++ {
				indexPath := strconv.Itoa(i)
				if prefix != "" {
					indexPath = prefix + "." + indexPath
				}

				var itemA, itemB any
				if i < len(valA) {
					itemA = valA[i]
				}

				if i < len(valB) {
					itemB = valB[i]
				}

				compareJSONNodes(indexPath, itemA, itemB, onDelta)
			}

			return
		}

		onDelta(prefix, a, b)

	case map[string]any:
		if valB, ok := b.(map[string]any); ok {
			allKeys := make(map[string]bool)
			for k := range valA {
				allKeys[k] = true
			}

			for k := range valB {
				allKeys[k] = true
			}

			for k := range allKeys {
				keyPath := k
				if prefix != "" {
					keyPath = prefix + "." + k
				}

				compareJSONNodes(keyPath, valA[k], valB[k], onDelta)
			}

			return
		}

		onDelta(prefix, a, b)

	default:
		if fmt.Sprintf("%v", a) != fmt.Sprintf("%v", b) {
			onDelta(prefix, a, b)
		}
	}
}

func resolveStructForRoute(
	routeKey string,
	isRequest bool,
	services []*ir.ServiceIR,
	structs []*ir.StructIR,
	tuples []*ir.TupleIR,
) (string, map[string]string) {
	parts := strings.SplitN(routeKey, " ", 2)

	path := routeKey
	if len(parts) == 2 {
		path = parts[1]
	}

	var matchedMethod *ir.MethodIR
	for _, s := range services {
		for _, m := range s.Methods {
			if m.Path != nil && (strings.EqualFold(m.Path.RawTemplate, path) ||
				strings.HasSuffix(path, m.Path.RawTemplate)) {
				matchedMethod = m
				break
			}
		}

		if matchedMethod != nil {
			break
		}
	}

	structName := ""
	if matchedMethod != nil {
		if isRequest {
			for _, p := range matchedMethod.Params {
				pType := strings.TrimPrefix(strings.TrimPrefix(p.GoType.Name, "*"), "[]")
				if pType != "context.Context" && pType != "aoni.RequestModifier" && pType != "" {
					structName = pType
					break
				}
			}
		} else if matchedMethod.Return != nil {
			structName = matchedMethod.Return.SuccessType.Name
		}
	}

	structName = strings.TrimPrefix(strings.TrimPrefix(structName, "*"), "[]")

	if structName == "" || structName == "any" {
		methodTerminal := deriveTerminalName(path)
		if isRequest {
			structName = methodTerminal + "Request"
		} else {
			structName = methodTerminal + "Tuple"
		}
	}

	fieldMap := make(map[string]string)
	for _, st := range structs {
		if strings.EqualFold(st.Name, structName) {
			for _, f := range st.Fields {
				if f.CustomTag != "" {
					tagVal := reflect.StructTag(strings.Trim(f.CustomTag, "`"))
					if aVal := tagVal.Get("aoni"); aVal != "" {
						fieldMap[aVal] = f.GoName
					}
				}
			}

			break
		}
	}

	for _, tup := range tuples {
		if strings.EqualFold(tup.Name, structName) {
			for _, f := range tup.Fields {
				if f.PathStr != "" {
					fieldMap[f.PathStr] = f.GoName
				} else if f.Index >= 0 {
					fieldMap[strconv.Itoa(f.Index)] = f.GoName
				}
			}

			break
		}
	}

	return structName, fieldMap
}

func deriveTerminalName(path string) string {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(path, "/$rpc/"), "/")
	segments := strings.Split(trimmed, "/")
	last := segments[len(segments)-1]
	parts := strings.Split(last, ".")

	return parts[len(parts)-1]
}

func resolveFieldFromPath(path string, fieldMap map[string]string) (string, string) {
	tag := path
	if name, ok := fieldMap[path]; ok {
		return name, tag
	}

	cleanPath := strings.TrimPrefix(path, "0.")
	if name, ok := fieldMap[cleanPath]; ok {
		return name, cleanPath
	}

	return "Field" + strings.ReplaceAll(path, ".", "_"), tag
}

func formatDeltaValue(val any) string {
	if val == nil {
		return "null"
	}

	s := fmt.Sprintf("%v", val)
	if str, ok := val.(string); ok {
		s = fmt.Sprintf("%q", str)
	}

	if len(s) > 35 {
		s = s[:32] + "..."
	}

	return s
}
