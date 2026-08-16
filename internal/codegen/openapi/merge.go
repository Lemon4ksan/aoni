// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"bytes"
	"fmt"
	"go/format"
	"slices"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

// MergeConfig defines parameters and operational modes for semantic contract reconciliation.
type MergeConfig struct {
	SpecFile       string            // Path or identifier of upstream OpenAPI specification
	PackageName    string            // Target Go package name
	ServiceName    string            // Target service interface name
	Prune          bool              // If true, delete missing endpoints instead of soft deprecating
	Overwrite      bool              // If true, discard existing file and perform fresh generation
	DryRun         bool              // If true, calculate diff and summary without disk changes
	Interactive    bool              // If true, prompt for ambiguous renames/deletions
	SkipDeprecated bool              // Ignore deprecated endpoints in incoming OpenAPI spec
	TypeMap        map[string]string // Custom type mappings (e.g. steam_id -> id.ID)
}

// MergeSummary tracks all semantic actions executed during contract reconciliation.
type MergeSummary struct {
	SpecSource        string   `json:"spec_source"`
	SpecVersion       string   `json:"spec_version"`
	Service           string   `json:"service"`
	AddedMethods      []string `json:"added_methods"`
	UpdatedMethods    []string `json:"updated_methods"`
	DeprecatedMethods []string `json:"deprecated_methods"`
	PrunedMethods     []string `json:"pruned_methods"`
}

// HasChanges reports whether any structural modifications occurred.
func (s *MergeSummary) HasChanges() bool {
	return len(s.AddedMethods) > 0 || len(s.UpdatedMethods) > 0 ||
		len(s.DeprecatedMethods) > 0 || len(s.PrunedMethods) > 0
}

// Render formats a human-readable terminal report of the merge.
func (s *MergeSummary) Render(targetPath string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "⚡ [vortex merge] Merging %q into %q\n", s.SpecSource, targetPath)

	if s.SpecVersion != "" {
		fmt.Fprintf(&sb, "  Upstream Spec Version: %s\n", s.SpecVersion)
	}

	sb.WriteString("\n")

	if !s.HasChanges() {
		sb.WriteString("✔ All endpoints and models are already 100% in sync with specification.\n")
		return sb.String()
	}

	if len(s.AddedMethods) > 0 {
		fmt.Fprintf(&sb, "  [+] %d new endpoint(s) appended:\n", len(s.AddedMethods))

		for _, m := range s.AddedMethods {
			fmt.Fprintf(&sb, "      • %s\n", m)
		}
	}

	if len(s.UpdatedMethods) > 0 {
		fmt.Fprintf(&sb, "  [~] %d endpoint(s) updated (custom types & directives preserved):\n", len(s.UpdatedMethods))

		for _, m := range s.UpdatedMethods {
			fmt.Fprintf(&sb, "      • %s\n", m)
		}
	}

	if len(s.DeprecatedMethods) > 0 {
		fmt.Fprintf(&sb, "  [!] %d endpoint(s) missing upstream (marked @deprecated):\n", len(s.DeprecatedMethods))

		for _, m := range s.DeprecatedMethods {
			fmt.Fprintf(&sb, "      • %s\n", m)
		}
	}

	if len(s.PrunedMethods) > 0 {
		fmt.Fprintf(&sb, "  [-] %d endpoint(s) pruned from contract:\n", len(s.PrunedMethods))

		for _, m := range s.PrunedMethods {
			fmt.Fprintf(&sb, "      • %s\n", m)
		}
	}

	sb.WriteString("\n✔ Successfully reconciled Go contract AST.\n")

	return sb.String()
}

// MergeEngine performs 3-way semantic AST reconciliation between existing Go contracts and new OpenAPI specs.
type MergeEngine struct{}

// NewMergeEngine creates an initialized MergeEngine instance.
func NewMergeEngine() *MergeEngine {
	return &MergeEngine{}
}

type incomingOperation struct {
	httpMethod   string
	rawPath      string
	normPath     string
	operationID  string
	summary      string
	deprecated   bool
	pathItem     *openapi3.PathItem
	op           *openapi3.Operation
	sinceVersion string
}

// ReconcileService merges existing Go AST with incoming OpenAPI 3.x specifications.
func (e *MergeEngine) ReconcileService(
	existingAPISrc []byte,
	doc *openapi3.T,
	cfg MergeConfig,
) (mergedSrc []byte, summary *MergeSummary, err error) {
	summary = &MergeSummary{
		SpecSource:  cfg.SpecFile,
		Service:     cfg.ServiceName,
		SpecVersion: "",
	}

	if doc.Info != nil && doc.Info.Version != "" {
		summary.SpecVersion = doc.Info.Version
	}

	// 1. Parse existing Go contract AST
	p := parser.NewParser()

	var existingRoot *ir.RootIR
	if len(existingAPISrc) > 0 {
		existingRoot, _ = p.ParseSource("api.go", existingAPISrc)
	}

	var existingSvc *ir.ServiceIR
	if existingRoot != nil && len(existingRoot.Services) > 0 {
		if cfg.ServiceName != "" {
			for _, s := range existingRoot.Services {
				if strings.EqualFold(s.Name, cfg.ServiceName) {
					existingSvc = s
					break
				}
			}
		}

		if existingSvc == nil {
			existingSvc = existingRoot.Services[0]
		}
	}

	// 2. Index existing methods
	existingMethodsByName := make(map[string]*ir.MethodIR)
	existingMethodsByRoute := make(map[string]*ir.MethodIR)
	existingMethodsByOpID := make(map[string]*ir.MethodIR)

	if existingSvc != nil {
		for _, m := range existingSvc.Methods {
			existingMethodsByName[m.Name] = m
			if m.OperationID != "" {
				existingMethodsByOpID[m.OperationID] = m
			}

			if m.HTTPMethod != "" && m.Path != nil {
				routeKey := strings.ToUpper(m.HTTPMethod) + " " + normalizeRoutePath(m.Path.RawTemplate)
				existingMethodsByRoute[routeKey] = m
			}
		}
	}

	// 3. Index incoming OpenAPI operations
	incomingOps := make([]*incomingOperation, 0)
	if doc.Paths != nil {
		for pathStr, pathItem := range doc.Paths.Map() {
			if pathItem == nil || !isPathAllowed(pathStr, ImportConfig{
				IncludePaths: nil,
				ExcludePaths: nil,
			}) {
				continue
			}

			ops := map[string]*openapi3.Operation{
				"GET":     pathItem.Get,
				"POST":    pathItem.Post,
				"PUT":     pathItem.Put,
				"DELETE":  pathItem.Delete,
				"PATCH":   pathItem.Patch,
				"HEAD":    pathItem.Head,
				"OPTIONS": pathItem.Options,
			}

			for httpMethod, op := range ops {
				if op == nil {
					continue
				}

				if cfg.SkipDeprecated && op.Deprecated {
					continue
				}

				iop := &incomingOperation{
					httpMethod:   httpMethod,
					rawPath:      pathStr,
					normPath:     normalizeRoutePath(pathStr),
					operationID:  op.OperationID,
					summary:      op.Summary,
					deprecated:   op.Deprecated,
					pathItem:     pathItem,
					op:           op,
					sinceVersion: summary.SpecVersion,
				}

				incomingOps = append(incomingOps, iop)
			}
		}
	}

	// Sort incoming operations by route for deterministic output
	sort.Slice(incomingOps, func(i, j int) bool {
		if incomingOps[i].rawPath == incomingOps[j].rawPath {
			return incomingOps[i].httpMethod < incomingOps[j].httpMethod
		}

		return incomingOps[i].rawPath < incomingOps[j].rawPath
	})

	// 4. Reconcile methods
	type mergedMethodEntry struct {
		goName   string
		rendered string
	}

	var outputMethods []mergedMethodEntry

	matchedExisting := make(map[*ir.MethodIR]bool)
	usedNames := make(map[string]int)

	for _, iop := range incomingOps {
		// Match against existing method
		var matched *ir.MethodIR
		if iop.operationID != "" {
			if m, ok := existingMethodsByOpID[iop.operationID]; ok {
				matched = m
			}
		}

		if matched == nil {
			routeKey := iop.httpMethod + " " + iop.normPath
			if m, ok := existingMethodsByRoute[routeKey]; ok {
				matched = m
			}
		}

		if matched != nil {
			matchedExisting[matched] = true
			usedNames[matched.Name]++

			// Render merged method: preserve custom name, domain types, and directives
			rendered := e.renderMergedMethod(matched, iop, cfg)
			outputMethods = append(outputMethods, mergedMethodEntry{
				goName:   matched.Name,
				rendered: rendered,
			})
			summary.UpdatedMethods = append(
				summary.UpdatedMethods,
				fmt.Sprintf("%s (%s %s)", matched.Name, iop.httpMethod, iop.rawPath),
			)
		} else {
			// New endpoint discovered upstream
			methodName := buildMethodName(iop.rawPath, iop.httpMethod, iop.op, usedNames)
			rendered := e.renderNewMethod(methodName, iop, cfg)
			outputMethods = append(outputMethods, mergedMethodEntry{
				goName:   methodName,
				rendered: rendered,
			})
			summary.AddedMethods = append(
				summary.AddedMethods,
				fmt.Sprintf("%s (%s %s)", methodName, iop.httpMethod, iop.rawPath),
			)
		}
	}

	// 5. Handle remaining existing methods missing in incoming spec
	if existingSvc != nil {
		for _, m := range existingSvc.Methods {
			if matchedExisting[m] {
				continue
			}

			if cfg.Prune {
				summary.PrunedMethods = append(summary.PrunedMethods, m.Name)
				continue
			}

			// Soft Deprecation: keep method, mark @deprecated
			rendered := e.renderSoftDeprecatedMethod(m, summary.SpecVersion)
			outputMethods = append(outputMethods, mergedMethodEntry{
				goName:   m.Name,
				rendered: rendered,
			})
			summary.DeprecatedMethods = append(summary.DeprecatedMethods, m.Name)
		}
	}

	// 6. Assemble complete api.go source
	pkgName := cfg.PackageName
	if pkgName == "" && existingRoot != nil && existingRoot.PackageName != "" {
		pkgName = existingRoot.PackageName
	}

	if pkgName == "" {
		pkgName = "api"
	}

	serviceName := cfg.ServiceName
	if serviceName == "" && existingSvc != nil {
		serviceName = existingSvc.Name
	}

	if serviceName == "" {
		serviceName = "API"
	}

	casing := "snake_case"
	if existingSvc != nil && existingSvc.DefaultCasing != "" {
		casing = string(existingSvc.DefaultCasing)
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "package %s\n\n", pkgName)
	buf.WriteString("import (\n")
	buf.WriteString("\t\"context\"\n")

	// Collect custom imports
	var customImports []string
	if existingRoot != nil {
		for _, imp := range existingRoot.Imports {
			if imp.Path != "context" && imp.Path != "github.com/lemon4ksan/aoni" {
				if !slices.Contains(customImports, imp.Path) {
					customImports = append(customImports, imp.Path)
				}
			}
		}
	}

	for _, rawType := range cfg.TypeMap {
		if idx := strings.LastIndex(rawType, "/"); idx != -1 {
			dotIdx := strings.LastIndex(rawType, ".")
			if dotIdx > idx {
				pkgPath := rawType[:dotIdx]
				if !slices.Contains(customImports, pkgPath) {
					customImports = append(customImports, pkgPath)
				}
			}
		}
	}

	sort.Strings(customImports)

	for _, imp := range customImports {
		fmt.Fprintf(&buf, "\t%q\n", imp)
	}

	buf.WriteString("\n\t\"github.com/lemon4ksan/aoni\"\n")
	buf.WriteString(")\n\n")

	// BaseURL constant
	baseURL := resolveBaseURL(doc, ImportConfig{BaseURL: ""})
	if baseURL != "" {
		fmt.Fprintf(&buf, "// BaseURL is the default API base endpoint.\nconst BaseURL = %q\n\n", baseURL)
	}

	// Service interface
	fmt.Fprintf(&buf, "// @aoni:service casing=%s\n", casing)

	if summary.SpecVersion != "" {
		fmt.Fprintf(&buf, "// @version %q\n", summary.SpecVersion)
	}

	if cfg.SpecFile != "" {
		fmt.Fprintf(&buf, "// @source %q\n", cfg.SpecFile)
	}

	if existingSvc != nil {
		if existingSvc.Persona != "" {
			fmt.Fprintf(&buf, "// @persona %q\n", existingSvc.Persona)
		}

		if existingSvc.TLSSpec != "" {
			fmt.Fprintf(&buf, "// @tls_spec %q\n", existingSvc.TLSSpec)
		}

		if existingSvc.Engine != "" {
			fmt.Fprintf(&buf, "// @engine %s\n", string(existingSvc.Engine))
		}
	}

	if baseURL != "" {
		fmt.Fprintf(&buf, "// @base_url %q\n", baseURL)
	}

	fmt.Fprintf(&buf, "type %s interface {\n", serviceName)

	for _, m := range outputMethods {
		buf.WriteString(m.rendered)
		buf.WriteString("\n")
	}

	fmt.Fprintf(&buf, "}\n")

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, nil, fmt.Errorf("formatting reconciled contract: %w\nSource:\n%s", err, buf.String())
	}

	return formatted, summary, nil
}

func (e *MergeEngine) renderMergedMethod(
	existing *ir.MethodIR,
	iop *incomingOperation,
	cfg MergeConfig,
) string {
	var buf bytes.Buffer

	// 1. Doc comments: retain existing or update with summary
	if len(existing.Doc) > 0 {
		for _, l := range existing.Doc {
			if !strings.HasPrefix(l, "//") {
				l = "// " + l
			}

			fmt.Fprintf(&buf, "\t%s\n", l)
		}
	} else if iop.summary != "" {
		fmt.Fprintf(&buf, "\t// %s — %s\n\t//\n", existing.Name, strings.ReplaceAll(iop.summary, "\n", " "))
	}

	// 2. Route directive
	cleanPath := strings.TrimPrefix(iop.rawPath, "/")
	fmt.Fprintf(&buf, "\t// @%s %q\n", strings.ToLower(iop.httpMethod), cleanPath)

	// 3. Bind directive
	if iop.operationID != "" && iop.operationID != existing.Name {
		fmt.Fprintf(&buf, "\t// @bind %q\n", iop.operationID)
	}

	// 4. Since directive
	if existing.Since != "" {
		fmt.Fprintf(&buf, "\t// @since %q\n", existing.Since)
	} else if iop.sinceVersion != "" {
		fmt.Fprintf(&buf, "\t// @since %q\n", iop.sinceVersion)
	}

	// 5. Preserved human directives (Directive Union)
	if existing.UnwrapField != "" {
		fmt.Fprintf(&buf, "\t// @unwrap %q\n", existing.UnwrapField)
	}

	if existing.CallFunc != "" {
		fmt.Fprintf(&buf, "\t// @call %q\n", existing.CallFunc)
	}

	if existing.Idempotent {
		fmt.Fprintf(&buf, "\t// @idempotent\n")
	}

	if existing.Coalesce {
		fmt.Fprintf(&buf, "\t// @coalesce\n")
	}

	if existing.ETag {
		fmt.Fprintf(&buf, "\t// @etag\n")
	}

	if existing.LocalCacheTTL != "" {
		fmt.Fprintf(&buf, "\t// @cache %q\n", existing.LocalCacheTTL)
	}

	if existing.SignHMAC != nil {
		fmt.Fprintf(&buf, "\t// @sign header=%q secret=%q\n", existing.SignHMAC.HeaderName, existing.SignHMAC.SecretKey)
	}

	// 6. Parameter signature (Type Fidelity)
	paramSig := e.buildReconciledParams(existing, iop, cfg)

	// 7. Return signature
	var returnSig string
	if existing.Return != nil && !existing.Return.IsVoid && existing.Return.SuccessType.Name != "" {
		returnSig = fmt.Sprintf("(%s, error)", existing.Return.SuccessType.Name)
	} else {
		ret := determineReturnType(iop.op, ImportConfig{TypeMap: cfg.TypeMap})
		if ret == "" {
			returnSig = "error"
		} else {
			returnSig = fmt.Sprintf("(%s, error)", ret)
		}
	}

	if returnSig == "error" {
		fmt.Fprintf(&buf, "\t%s(ctx context.Context%s, mods ...aoni.RequestModifier) error\n", existing.Name, paramSig)
	} else {
		fmt.Fprintf(
			&buf,
			"\t%s(ctx context.Context%s, mods ...aoni.RequestModifier) %s\n",
			existing.Name,
			paramSig,
			returnSig,
		)
	}

	return buf.String()
}

func (e *MergeEngine) renderNewMethod(
	name string,
	iop *incomingOperation,
	cfg MergeConfig,
) string {
	var buf bytes.Buffer

	// Documentation
	if iop.summary != "" {
		fmt.Fprintf(&buf, "\t// %s — %s\n\t//\n", name, strings.ReplaceAll(iop.summary, "\n", " "))
	}

	// Route
	cleanPath := strings.TrimPrefix(iop.rawPath, "/")
	fmt.Fprintf(&buf, "\t// @%s %q\n", strings.ToLower(iop.httpMethod), cleanPath)

	// Bind
	if iop.operationID != "" && iop.operationID != name {
		fmt.Fprintf(&buf, "\t// @bind %q\n", iop.operationID)
	}

	// Since
	if iop.sinceVersion != "" {
		fmt.Fprintf(&buf, "\t// @since %q\n", iop.sinceVersion)
	}

	// Payload
	params := extractOperationParameters(iop.rawPath, iop.pathItem, iop.op)

	isForm := false
	if iop.op.RequestBody != nil && iop.op.RequestBody.Value != nil {
		content := iop.op.RequestBody.Value.Content
		if content.Get("application/x-www-form-urlencoded") != nil || content.Get("multipart/form-data") != nil {
			isForm = true

			fmt.Fprintf(&buf, "\t// @form casing=snake_case\n")
		} else if content.Get("application/json") != nil {
			fmt.Fprintf(&buf, "\t// @json\n")
		}
	}

	if iop.httpMethod != "GET" && !isForm && len(params.query) > 0 {
		fmt.Fprintf(&buf, "\t// @query casing=snake_case\n")
	}

	// Signatures
	paramList := make([]string, 0, len(params.path)+len(params.query))
	for _, p := range params.path {
		pType := mapMergeParamType(p, ImportConfig{TypeMap: cfg.TypeMap})
		paramList = append(paramList, fmt.Sprintf("%s %s", toCamelCase(p.Name), pType))
	}

	for _, p := range params.query {
		pType := mapMergeParamType(p, ImportConfig{TypeMap: cfg.TypeMap})
		paramList = append(paramList, fmt.Sprintf("%s %s", toCamelCase(p.Name), pType))
	}

	paramSig := ""
	if len(paramList) > 0 {
		paramSig = ", " + strings.Join(paramList, ", ")
	}

	ret := determineReturnType(iop.op, ImportConfig{TypeMap: cfg.TypeMap})
	if ret == "" {
		fmt.Fprintf(&buf, "\t%s(ctx context.Context%s, mods ...aoni.RequestModifier) error\n", name, paramSig)
	} else {
		fmt.Fprintf(
			&buf,
			"\t%s(ctx context.Context%s, mods ...aoni.RequestModifier) (%s, error)\n",
			name,
			paramSig,
			ret,
		)
	}

	return buf.String()
}

func (e *MergeEngine) renderSoftDeprecatedMethod(m *ir.MethodIR, currentVersion string) string {
	var buf bytes.Buffer

	reason := "Removed from upstream OpenAPI specification"
	if m.Deprecation != nil && m.Deprecation.Reason != "" {
		reason = m.Deprecation.Reason
	}

	fmt.Fprintf(&buf, "\t// @deprecated reason=%q since=%q\n", reason, currentVersion)

	if m.HTTPMethod != "" && m.Path != nil {
		fmt.Fprintf(&buf, "\t// @%s %q\n", strings.ToLower(m.HTTPMethod), strings.TrimPrefix(m.Path.RawTemplate, "/"))
	}

	if m.OperationID != "" {
		fmt.Fprintf(&buf, "\t// @bind %q\n", m.OperationID)
	}

	paramList := make([]string, 0)
	for _, p := range m.Params {
		if p.Location == ir.LocContext || p.Location == ir.LocModifiers {
			continue
		}

		paramList = append(paramList, fmt.Sprintf("%s %s", p.GoName, p.GoType.Name))
	}

	paramSig := ""
	if len(paramList) > 0 {
		paramSig = ", " + strings.Join(paramList, ", ")
	}

	returnSig := "(map[string]any, error)"
	if m.Return != nil && !m.Return.IsVoid && m.Return.SuccessType.Name != "" {
		returnSig = fmt.Sprintf("(%s, error)", m.Return.SuccessType.Name)
	}

	fmt.Fprintf(&buf, "\t%s(ctx context.Context%s, mods ...aoni.RequestModifier) %s\n", m.Name, paramSig, returnSig)

	return buf.String()
}

func (e *MergeEngine) buildReconciledParams(
	existing *ir.MethodIR,
	iop *incomingOperation,
	cfg MergeConfig,
) string {
	existingParamMap := make(map[string]*ir.ParamIR)
	for _, p := range existing.Params {
		if p.Location == ir.LocContext || p.Location == ir.LocModifiers {
			continue
		}

		existingParamMap[strings.ToLower(p.WireKey)] = p
		existingParamMap[strings.ToLower(p.GoName)] = p
	}

	params := extractOperationParameters(iop.rawPath, iop.pathItem, iop.op)
	paramList := make([]string, 0)

	for _, p := range append(params.path, params.query...) {
		wire := strings.ToLower(p.Name)
		goName := toCamelCase(p.Name)

		if existP, ok := existingParamMap[wire]; ok {
			// Rule A: Type Fidelity — preserve custom domain type
			paramList = append(paramList, fmt.Sprintf("%s %s", existP.GoName, existP.GoType.Name))
		} else {
			// New parameter
			pType := mapMergeParamType(p, ImportConfig{TypeMap: cfg.TypeMap})
			paramList = append(paramList, fmt.Sprintf("%s %s", goName, pType))
		}
	}

	if len(paramList) == 0 {
		return ""
	}

	return ", " + strings.Join(paramList, ", ")
}

func mapMergeParamType(p *openapi3.Parameter, cfg ImportConfig) string {
	pType := "string"

	if cfg.TypeMap != nil {
		if mapped, ok := cfg.TypeMap[p.Name]; ok {
			return shortTypeName(mapped)
		} else if mapped, ok := cfg.TypeMap[strings.ToLower(p.Name)]; ok {
			return shortTypeName(mapped)
		}
	}

	if p.Schema != nil && p.Schema.Value != nil {
		pType = mapSchemaType(p.Schema.Value, cfg)
	}

	return pType
}

func normalizeRoutePath(p string) string {
	clean := strings.Trim(p, "/")

	parts := strings.Split(clean, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			parts[i] = "{var}"
		}
	}

	return "/" + strings.Join(parts, "/")
}
