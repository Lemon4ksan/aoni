// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openapi

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/format"
	goparser "go/parser"
	"go/token"
	"path"
	"slices"
	"strings"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/parser"
)

// MergeConfig defines parameters and operational modes for semantic contract reconciliation.
type MergeConfig struct {
	SpecFile       string            // Path or identifier of upstream OpenAPI specification
	PackageName    string            // Target Go package name
	ServiceName    string            // Target service interface name
	Prune          bool              // If true, delete missing endpoints instead of soft deprecating
	Additive       bool              // If true, preserve existing endpoints as active without marking @deprecated
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
	pathItem     *PathItem
	op           *Operation
	sinceVersion string
}

// ReconcileService merges existing Go AST with incoming OpenAPI specifications.
func (e *MergeEngine) ReconcileService(
	existingAPISrc []byte,
	doc *Document,
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
			existingSvc, _ = generic.Find(existingRoot.Services, func(s *ir.ServiceIR) bool {
				return strings.EqualFold(s.Name, cfg.ServiceName)
			})
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
		for pathStr, pathItem := range doc.Paths {
			if pathItem == nil || !isPathAllowed(pathStr, ImportConfig{
				IncludePaths: nil,
				ExcludePaths: nil,
			}) {
				continue
			}

			ops := pathItem.OperationsMap()
			for httpMethod, op := range ops {
				if op == nil {
					continue
				}

				if cfg.SkipDeprecated && op.Deprecated {
					continue
				}

				norm := normalizeRoutePath(pathStr)
				iop := &incomingOperation{
					httpMethod:  httpMethod,
					rawPath:     pathStr,
					normPath:    norm,
					operationID: op.OperationID,
					summary:     op.Summary,
					deprecated:  op.Deprecated,
					pathItem:    pathItem,
					op:          op,
				}

				if op.Summary == "" {
					iop.summary = op.Description
				}

				if op.Extensions != nil {
					if since, ok := op.Extensions["x-vortex-since"]; ok {
						if sStr, ok := since.(string); ok {
							iop.sinceVersion = sStr
						}
					}
				}

				incomingOps = append(incomingOps, iop)
			}
		}
	}

	// Stable deterministic ordering
	slices.SortFunc(incomingOps, func(a, b *incomingOperation) int {
		if c := cmp.Compare(a.rawPath, b.rawPath); c != 0 {
			return c
		}

		return cmp.Compare(a.httpMethod, b.httpMethod)
	})

	matchedIncoming := make(map[*incomingOperation]*ir.MethodIR)
	matchedExisting := make(map[*ir.MethodIR]*incomingOperation)

	// Pass 1: Match by OperationID (@bind or method name)
	for _, iop := range incomingOps {
		if iop.operationID == "" {
			continue
		}

		if existM, ok := existingMethodsByOpID[iop.operationID]; ok {
			matchedIncoming[iop] = existM
			matchedExisting[existM] = iop
			continue
		}

		pascalOpID := toPascalCase(iop.operationID)
		if existM, ok := existingMethodsByName[pascalOpID]; ok {
			if matchedExisting[existM] == nil {
				matchedIncoming[iop] = existM
				matchedExisting[existM] = iop
			}
		}
	}

	// Pass 2: Match by exact Route (METHOD /path/{var})
	for _, iop := range incomingOps {
		if matchedIncoming[iop] != nil {
			continue
		}

		routeKey := strings.ToUpper(iop.httpMethod) + " " + iop.normPath
		if existM, ok := existingMethodsByRoute[routeKey]; ok {
			if matchedExisting[existM] == nil {
				matchedIncoming[iop] = existM
				matchedExisting[existM] = iop
			}
		}
	}

	// Pass 3: Match by normalized Method Name
	usedMethodNames := make(map[string]int)
	for _, iop := range incomingOps {
		if matchedIncoming[iop] != nil {
			continue
		}

		candidateName := buildMethodName(iop.rawPath, iop.httpMethod, iop.op, usedMethodNames)
		if existM, ok := existingMethodsByName[candidateName]; ok {
			if matchedExisting[existM] == nil {
				matchedIncoming[iop] = existM
				matchedExisting[existM] = iop
			}
		}
	}

	// Rebuild Method Code Buffer
	var methodsBuf bytes.Buffer

	usedOutputNames := make(map[string]int)

	// Step A: Reconcile matched and new incoming methods
	for _, iop := range incomingOps {
		existM := matchedIncoming[iop]
		if existM != nil {
			// [~] Existing Method Reconciliation
			summary.UpdatedMethods = append(summary.UpdatedMethods, existM.Name)

			methodCode := e.reconcileMethodNode(existM, iop, cfg, usedOutputNames)
			methodsBuf.WriteString(methodCode)
			methodsBuf.WriteString("\n")
		} else {
			// [+] New Method Appended
			var singleBuf bytes.Buffer
			writeOperationMethod(
				&singleBuf,
				doc,
				iop.rawPath,
				iop.httpMethod,
				iop.pathItem,
				iop.op,
				ImportConfig{
					ServiceName: cfg.ServiceName,
					PackageName: cfg.PackageName,
					TypeMap:     cfg.TypeMap,
				},
				usedOutputNames,
			)

			methodName := buildMethodName(iop.rawPath, iop.httpMethod, iop.op, nil)
			summary.AddedMethods = append(summary.AddedMethods, methodName)

			methodsBuf.WriteString(singleBuf.String())
		}
	}

	// Step B: Handle Missing Existing Endpoints
	if existingSvc != nil {
		for _, m := range existingSvc.Methods {
			if matchedExisting[m] == nil {
				switch {
				case cfg.Prune:
					// [-] Pruned
					summary.PrunedMethods = append(summary.PrunedMethods, m.Name)
				case cfg.Additive:
					// Preserved Active
					methodCode := renderExistingMethodVerbatim(m)
					methodsBuf.WriteString(methodCode)
					methodsBuf.WriteString("\n")
				default:
					// [!] Soft Deprecation
					summary.DeprecatedMethods = append(summary.DeprecatedMethods, m.Name)
					methodCode := renderExistingMethodWithDeprecated(m, summary.SpecVersion)
					methodsBuf.WriteString(methodCode)
					methodsBuf.WriteString("\n")
				}
			}
		}
	}

	// Step C: Assemble Complete Go Source File
	var fullBuf bytes.Buffer

	pkgName := cfg.PackageName
	if pkgName == "" {
		if existingRoot != nil && existingRoot.PackageName != "" {
			pkgName = existingRoot.PackageName
		} else {
			pkgName = "api"
		}
	}

	fmt.Fprintf(&fullBuf, "package %s\n\n", pkgName)

	fullBuf.WriteString("import (\n")
	fullBuf.WriteString("\t\"context\"\n")

	if bytes.Contains(methodsBuf.Bytes(), []byte("time.Time")) {
		fullBuf.WriteString("\t\"time\"\n")
	}

	var customImports []string
	for _, rawType := range cfg.TypeMap {
		if idx := strings.LastIndex(rawType, "/"); idx != -1 {
			dotIdx := strings.LastIndex(rawType, ".")
			if dotIdx > idx {
				pkgPath := rawType[:dotIdx]

				short := path.Base(pkgPath) + "." + rawType[dotIdx+1:]
				if bytes.Contains(methodsBuf.Bytes(), []byte(short)) {
					if !slices.Contains(customImports, pkgPath) {
						customImports = append(customImports, pkgPath)
					}
				}
			}
		}
	}

	slices.Sort(customImports)

	for _, imp := range customImports {
		fmt.Fprintf(&fullBuf, "\t%q\n", imp)
	}

	fullBuf.WriteString("\n\t\"github.com/lemon4ksan/aoni\"\n")
	fullBuf.WriteString(")\n\n")

	serviceName := cfg.ServiceName
	if serviceName == "" {
		if existingSvc != nil && existingSvc.Name != "" {
			serviceName = existingSvc.Name
		} else {
			serviceName = "API"
		}
	}

	baseURL := resolveBaseURL(doc, ImportConfig{})
	if baseURL != "" {
		constName := "BaseURL"
		if serviceName != "" && serviceName != "API" {
			constName = serviceName + "BaseURL"
		}

		fmt.Fprintf(
			&fullBuf,
			"// %s is the default API base endpoint.\nconst %s = %q\n\n",
			constName,
			constName,
			baseURL,
		)
	}

	casing := "snake_case"
	if existingSvc != nil && existingSvc.DefaultCasing != "" {
		casing = string(existingSvc.DefaultCasing)
	}

	fmt.Fprintf(&fullBuf, "// %s provides the API client contract.\n//\n", serviceName)
	fmt.Fprintf(&fullBuf, "// @aoni:service casing=%s\n", casing)

	if doc.Info != nil && doc.Info.Version != "" {
		fmt.Fprintf(&fullBuf, "// @version %q\n", doc.Info.Version)
	}

	if cfg.SpecFile != "" {
		fmt.Fprintf(&fullBuf, "// @source %q\n", cfg.SpecFile)
	}

	fmt.Fprintf(&fullBuf, "type %s interface {\n", serviceName)
	fullBuf.Write(methodsBuf.Bytes())
	fullBuf.WriteString("}\n\n")

	// Step D: Append and preserve Schemas / Models
	if doc.Components != nil && len(doc.Components.Schemas) > 0 {
		var schemasBuf bytes.Buffer
		writeSchemas(&schemasBuf, doc.Components.Schemas, ImportConfig{TypeMap: cfg.TypeMap})
		fullBuf.Write(schemasBuf.Bytes())
	}

	// Step E: Preserve any custom hand-written types from existing file
	if doc.Components != nil {
		preserveExistingTypes(&fullBuf, existingAPISrc, doc.Components.Schemas)
	}

	formatted, err := format.Source(fullBuf.Bytes())
	if err != nil {
		return fullBuf.Bytes(), summary, fmt.Errorf(
			"formatting reconciled Go contract: %w\nSource:\n%s",
			err,
			fullBuf.String(),
		)
	}

	return formatted, summary, nil
}

func (e *MergeEngine) reconcileMethodNode(
	existing *ir.MethodIR,
	iop *incomingOperation,
	cfg MergeConfig,
	usedNames map[string]int,
) string {
	var buf bytes.Buffer

	methodName := existing.Name
	usedNames[methodName] = 1

	// 1. Doc comments & custom directives
	if len(existing.Doc) > 0 {
		for _, l := range existing.Doc {
			if isManagedDirective(l) {
				continue
			}

			if !strings.HasPrefix(l, "//") {
				l = "// " + l
			}

			fmt.Fprintf(&buf, "\t%s\n", l)
		}
	} else if iop.summary != "" {
		fmt.Fprintf(&buf, "\t// %s — %s\n\t//\n", methodName, strings.ReplaceAll(iop.summary, "\n", " "))
	}

	// 2. Upstream Route directive
	cleanPath := strings.TrimPrefix(iop.rawPath, "/")
	fmt.Fprintf(&buf, "\t// @%s %q\n", strings.ToLower(iop.httpMethod), cleanPath)

	// 3. Upstream OperationID binding
	if iop.operationID != "" && iop.operationID != methodName {
		fmt.Fprintf(&buf, "\t// @bind %q\n", iop.operationID)
	}

	// 4. Preserved Directives
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

	if existing.Since != "" {
		fmt.Fprintf(&buf, "\t// @since %q\n", existing.Since)
	} else if iop.sinceVersion != "" {
		fmt.Fprintf(&buf, "\t// @since %q\n", iop.sinceVersion)
	}

	if iop.deprecated {
		fmt.Fprintf(&buf, "\t// @deprecated\n")
	}

	// Headers preservation
	seenHeaders := make(map[string]bool)
	for _, h := range existing.Headers {
		if h.Key != "" && h.StaticValue != "" {
			seenHeaders[strings.ToLower(h.Key)] = true
			fmt.Fprintf(&buf, "\t// @header %q %q\n", h.Key, h.StaticValue)
		}
	}

	// Payload directive
	isForm := false
	if iop.op.RequestBody != nil && iop.op.RequestBody.Content != nil {
		if iop.op.RequestBody.Content["application/x-www-form-urlencoded"] != nil ||
			iop.op.RequestBody.Content["multipart/form-data"] != nil {
			isForm = true

			fmt.Fprintf(&buf, "\t// @form casing=snake_case\n")
		}
	}

	// Build reconciled parameter signature
	paramList := e.buildReconciledParams(existing, iop, cfg)

	if iop.httpMethod != "GET" && !isForm {
		hasQuery := false
		for _, p := range paramList {
			if strings.Contains(p, "@query") {
				hasQuery = true
				break
			}
		}

		if hasQuery {
			fmt.Fprintf(&buf, "\t// @query casing=snake_case\n")
		}
	}

	// Return type
	returnType := determineReturnType(iop.op, ImportConfig{TypeMap: cfg.TypeMap})
	if returnType == "" {
		if existing.Return != nil && !existing.Return.IsVoid && existing.Return.SuccessType.Name != "" {
			returnType = existing.Return.SuccessType.Name
		}
	}

	returnSig := "error"
	if returnType != "" {
		returnSig = fmt.Sprintf("(%s, error)", returnType)
	}

	renderMethodSignature(&buf, methodName, paramList, returnSig)

	return buf.String()
}

func renderExistingMethodVerbatim(m *ir.MethodIR) string {
	var buf bytes.Buffer

	if len(m.Doc) > 0 {
		for _, l := range m.Doc {
			if isManagedDirective(l) {
				continue
			}

			if !strings.HasPrefix(l, "//") {
				l = "// " + l
			}

			fmt.Fprintf(&buf, "\t%s\n", l)
		}
	} else if m.Summary != "" {
		fmt.Fprintf(&buf, "\t// %s — %s\n\t//\n", m.Name, m.Summary)
	}

	if m.HTTPMethod != "" && m.Path != nil {
		fmt.Fprintf(&buf, "\t// @%s %q\n", strings.ToLower(m.HTTPMethod), strings.TrimPrefix(m.Path.RawTemplate, "/"))
	}

	if m.OperationID != "" && m.OperationID != m.Name {
		fmt.Fprintf(&buf, "\t// @bind %q\n", m.OperationID)
	}

	paramList := make([]string, 0)
	for _, p := range m.Params {
		if p.Location == ir.LocContext || p.Location == ir.LocModifiers {
			continue
		}

		paramList = append(paramList, fmt.Sprintf("%s %s", p.GoName, p.GoType.Name))
	}

	returnSig := "(map[string]any, error)"
	if m.Return != nil && !m.Return.IsVoid && m.Return.SuccessType.Name != "" {
		returnSig = fmt.Sprintf("(%s, error)", m.Return.SuccessType.Name)
	}

	renderMethodSignature(&buf, m.Name, paramList, returnSig)

	return buf.String()
}

func renderExistingMethodWithDeprecated(m *ir.MethodIR, specVersion string) string {
	var buf bytes.Buffer

	if specVersion != "" {
		fmt.Fprintf(
			&buf,
			"\t// @deprecated reason=%q since=%q\n",
			"Removed from upstream OpenAPI specification",
			specVersion,
		)
	} else {
		fmt.Fprintf(&buf, "\t// @deprecated reason=%q\n", "Removed from upstream OpenAPI specification")
	}

	if len(m.Doc) > 0 {
		for _, l := range m.Doc {
			if isManagedDirective(l) {
				continue
			}

			if !strings.HasPrefix(l, "//") {
				l = "// " + l
			}

			fmt.Fprintf(&buf, "\t%s\n", l)
		}
	} else if m.Summary != "" {
		fmt.Fprintf(&buf, "\t// %s — %s\n\t//\n", m.Name, m.Summary)
	}

	if m.HTTPMethod != "" && m.Path != nil {
		fmt.Fprintf(&buf, "\t// @%s %q\n", strings.ToLower(m.HTTPMethod), strings.TrimPrefix(m.Path.RawTemplate, "/"))
	}

	if m.OperationID != "" && m.OperationID != m.Name {
		fmt.Fprintf(&buf, "\t// @bind %q\n", m.OperationID)
	}

	paramList := make([]string, 0)
	for _, p := range m.Params {
		if p.Location == ir.LocContext || p.Location == ir.LocModifiers {
			continue
		}

		paramList = append(paramList, fmt.Sprintf("%s %s", p.GoName, p.GoType.Name))
	}

	returnSig := "(map[string]any, error)"
	if m.Return != nil && !m.Return.IsVoid && m.Return.SuccessType.Name != "" {
		returnSig = fmt.Sprintf("(%s, error)", m.Return.SuccessType.Name)
	}

	renderMethodSignature(&buf, m.Name, paramList, returnSig)

	return buf.String()
}

func renderMethodSignature(buf *bytes.Buffer, name string, paramList []string, returnSig string) {
	allParams := make([]string, 0, len(paramList)+2)
	allParams = append(allParams, "ctx context.Context")
	allParams = append(allParams, paramList...)
	allParams = append(allParams, "mods ...aoni.RequestModifier")

	hasComments := false
	for _, p := range allParams {
		if strings.Contains(p, "//") {
			hasComments = true
			break
		}
	}

	if len(allParams) > 4 || hasComments {
		fmt.Fprintf(buf, "\t%s(\n", name)

		for _, p := range allParams {
			if idx := strings.Index(p, "//"); idx != -1 {
				codePart := strings.TrimSpace(p[:idx])
				commentPart := p[idx:]
				fmt.Fprintf(buf, "\t\t%s, %s\n", codePart, commentPart)
			} else {
				fmt.Fprintf(buf, "\t\t%s,\n", p)
			}
		}

		if returnSig == "error" || returnSig == "" {
			fmt.Fprintf(buf, "\t) error\n")
		} else {
			fmt.Fprintf(buf, "\t) %s\n", returnSig)
		}
	} else {
		if returnSig == "error" || returnSig == "" {
			fmt.Fprintf(buf, "\t%s(%s) error\n", name, strings.Join(allParams, ", "))
		} else {
			fmt.Fprintf(buf, "\t%s(%s) %s\n", name, strings.Join(allParams, ", "), returnSig)
		}
	}
}

func (e *MergeEngine) buildReconciledParams(
	existing *ir.MethodIR,
	iop *incomingOperation,
	cfg MergeConfig,
) []string {
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

	for _, p := range params.path {
		wire := strings.ToLower(p.Name)
		goName := toCamelCase(p.Name)

		if existP, ok := existingParamMap[wire]; ok {
			paramList = append(paramList, fmt.Sprintf("%s %s", existP.GoName, existP.GoType.Name))
		} else {
			pType := mapMergeParamType(p, ImportConfig{TypeMap: cfg.TypeMap})
			paramList = append(paramList, fmt.Sprintf("%s %s", goName, pType))
		}
	}

	for _, p := range params.query {
		wire := strings.ToLower(p.Name)
		goName := toCamelCase(p.Name)

		sig := ""
		if existP, ok := existingParamMap[wire]; ok {
			sig = fmt.Sprintf("%s %s", existP.GoName, existP.GoType.Name)
		} else {
			pType := mapMergeParamType(p, ImportConfig{TypeMap: cfg.TypeMap})
			sig = fmt.Sprintf("%s %s", goName, pType)
		}

		expectedSnake := toSnakeCase(goName)
		if p.Name != "" && (iop.httpMethod != "GET" || (p.Name != expectedSnake && p.Name != goName)) {
			sig += fmt.Sprintf(" // @query %q", p.Name)
		}

		paramList = append(paramList, sig)
	}

	// Request Body parameter if JSON or custom struct
	if iop.op != nil && iop.op.RequestBody != nil && iop.op.RequestBody.Content != nil {
		jsonContent := iop.op.RequestBody.Content["application/json"]
		if jsonContent != nil && jsonContent.Schema != nil {
			var bodyType string
			if jsonContent.Schema.Ref != "" {
				bodyType = toPascalCase(path.Base(jsonContent.Schema.Ref))
			} else {
				bodyType = mapSchemaType(jsonContent.Schema, ImportConfig{TypeMap: cfg.TypeMap})
			}

			if existReq, ok := existingParamMap["req"]; ok && existReq.GoType.Name != "" &&
				existReq.GoType.Name != "any" && existReq.GoType.Name != "map[string]any" {
				paramList = append(paramList, fmt.Sprintf("%s %s", existReq.GoName, existReq.GoType.Name))
			} else {
				paramList = append(paramList, "req "+bodyType)
			}
		}
	} else if existReq, ok := existingParamMap["req"]; ok && existReq.GoType.Name != "" &&
		existReq.GoType.Name != "any" {
		paramList = append(paramList, fmt.Sprintf("%s %s", existReq.GoName, existReq.GoType.Name))
	}

	return paramList
}

func preserveExistingTypes(buf *bytes.Buffer, existingAPISrc []byte, incomingSchemas map[string]*Schema) {
	if len(existingAPISrc) == 0 {
		return
	}

	fset := token.NewFileSet()

	fileAst, err := goparser.ParseFile(fset, "api.go", existingAPISrc, goparser.ParseComments)
	if err != nil {
		return
	}

	for _, decl := range fileAst.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			// Skip the service interface itself
			if _, isInterface := typeSpec.Type.(*ast.InterfaceType); isInterface {
				continue
			}

			typeName := typeSpec.Name.Name
			// If incoming OpenAPI schemas already defines this type, let writeSchemas handle it
			if incomingSchemas != nil {
				if _, exists := incomingSchemas[typeName]; exists {
					continue
				}

				if _, exists := incomingSchemas[strings.ToLower(typeName)]; exists {
					continue
				}
			}

			// Preserve existing struct/tuple/enum in full
			var declBuf bytes.Buffer
			if err := format.Node(&declBuf, fset, genDecl); err == nil {
				buf.WriteString(declBuf.String())
				buf.WriteString("\n\n")
			}

			break
		}
	}
}

func mapMergeParamType(p *Parameter, cfg ImportConfig) string {
	pType := "string"

	if cfg.TypeMap != nil {
		if mapped, ok := cfg.TypeMap[p.Name]; ok {
			return shortTypeName(mapped)
		} else if mapped, ok := cfg.TypeMap[strings.ToLower(p.Name)]; ok {
			return shortTypeName(mapped)
		}
	}

	if p.Schema != nil {
		pType = mapSchemaType(p.Schema, cfg)
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

var managedDirectives = []string{
	"@get", "@post", "@put", "@delete", "@patch", "@head", "@options", "@connect", "@trace",
	"@source", "@version", "@bind", "@unwrap", "@call", "@idempotent", "@coalesce", "@etag",
	"@cache", "@sign", "@json", "@since", "@form", "@query", "@deprecated", "@header",
}

func isManagedDirective(line string) bool {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "//"))
	if !strings.HasPrefix(trimmed, "@") {
		return false
	}

	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}

	return slices.Contains(managedDirectives, strings.ToLower(fields[0]))
}
