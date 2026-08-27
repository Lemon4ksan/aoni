// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package diff

import (
	"fmt"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/openapi"
)

type remoteOp struct {
	httpMethod   string
	rawPath      string
	normPath     string
	operationID  string
	summary      string
	deprecated   bool
	pathParams   map[string]*openapi.Parameter
	queryParams  map[string]*openapi.Parameter
	headerParams map[string]*openapi.Parameter
	requestBody  *openapi.RequestBody
	responses    map[string]*openapi.Response
}

// Compare compares local RootIR against a loaded remote OpenAPI document with optional custom options.
func Compare(
	local *ir.RootIR,
	remoteDoc *openapi.Document,
	localTarget string,
	remoteTarget string,
	opts ...DiffOptions,
) *DiffReport {
	var opt DiffOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	return CompareWithOptions(local, remoteDoc, localTarget, remoteTarget, opt)
}

// CompareWithOptions compares local RootIR against a loaded remote OpenAPI document with custom options.
func CompareWithOptions(
	local *ir.RootIR,
	remoteDoc *openapi.Document,
	localTarget string,
	remoteTarget string,
	opts DiffOptions,
) *DiffReport {
	report := &DiffReport{
		LocalTarget:  localTarget,
		RemoteTarget: remoteTarget,
		Additive:     opts.Additive,
	}

	if local == nil || remoteDoc == nil {
		return report
	}

	remoteOps, remoteKeyMap, remoteOpIDMap := indexRemoteOps(remoteDoc)
	report.TotalEndpointsChecked = len(remoteOps)

	matchedRemote := make(map[*remoteOp]bool)

	for _, svc := range local.Services {
		for _, m := range svc.Methods {
			if m.Operation != ir.OpHTTP && m.HTTPMethod == "" {
				continue
			}

			var rop *remoteOp
			if m.OperationID != "" {
				rop = remoteOpIDMap[m.OperationID]
			}

			if rop == nil && m.Path != nil {
				routeKey := strings.ToUpper(m.HTTPMethod) + " " + normalizePath(m.Path.RawTemplate)
				rop = remoteKeyMap[routeKey]
			}

			endpointDesc := fmt.Sprintf("%s %s", m.HTTPMethod, m.Path.RawTemplate)

			if rop == nil {
				if !opts.Additive {
					report.Drifts = append(report.Drifts, DriftItem{
						Severity:   SeverityNonBreaking,
						Kind:       DriftMissingEndpoint,
						Service:    svc.Name,
						Method:     m.Name,
						Endpoint:   endpointDesc,
						Message:    "Endpoint exists in Go contract but is absent in remote OpenAPI specification",
						Suggestion: "Verify if route was deprecated or removed upstream",
					})
				}

				continue
			}

			matchedRemote[rop] = true
			compareMethod(report, svc, m, rop, endpointDesc)
		}
	}

	for _, rop := range remoteOps {
		if !matchedRemote[rop] {
			severity := SeverityGhost
			msg := fmt.Sprintf(
				"Remote specification defines endpoint %s %s not implemented in Go contract",
				rop.httpMethod,
				rop.rawPath,
			)
			sugg := fmt.Sprintf("Run `vortex spec import` to generate method for `%s`", rop.rawPath)

			remoteDesc := fmt.Sprintf("%s %s", rop.httpMethod, rop.rawPath)
			if rop.deprecated {
				severity = SeverityNonBreaking
				msg = fmt.Sprintf(
					"Remote specification contains deprecated endpoint %s %s",
					rop.httpMethod,
					rop.rawPath,
				)
				sugg = "Ignore or import as deprecated"
			}

			report.Drifts = append(report.Drifts, DriftItem{
				Severity:   severity,
				Kind:       DriftMissingEndpoint,
				Endpoint:   remoteDesc,
				Message:    msg,
				Suggestion: sugg,
			})
		}
	}

	return report
}

func compareMethod(
	report *DiffReport,
	svc *ir.ServiceIR,
	m *ir.MethodIR,
	rop *remoteOp,
	endpointDesc string,
) {
	// 1. Deprecation check
	localDeprecated := (m.Deprecation != nil)
	if rop.deprecated && !localDeprecated {
		report.Drifts = append(report.Drifts, DriftItem{
			Severity:   SeverityNonBreaking,
			Kind:       DriftDeprecationMismatch,
			Service:    svc.Name,
			Method:     m.Name,
			Endpoint:   endpointDesc,
			Message:    "Endpoint is marked as deprecated in remote OpenAPI specification",
			Suggestion: "Add `// @deprecated` directive to method",
		})
	}

	// 2. Path parameters check
	localPathVars := make(map[string]bool)
	if m.Path != nil {
		for _, seg := range m.Path.Segments {
			if seg.IsVariable {
				localPathVars[strings.ToLower(seg.VarName)] = true
			}
		}
	}

	for pName := range rop.pathParams {
		if !localPathVars[pName] {
			report.Drifts = append(report.Drifts, DriftItem{
				Severity: SeverityBreaking,
				Kind:     DriftPathMismatch,
				Service:  svc.Name,
				Method:   m.Name,
				Endpoint: endpointDesc,
				Param:    pName,
				Expected: "path parameter {" + pName + "}",
				Actual:   "missing in Go path template",
				Message: fmt.Sprintf(
					"Path template is missing variable {%s} required by remote specification",
					pName,
				),
				Suggestion: fmt.Sprintf(
					"Update `@%s` route template to include {%s}",
					strings.ToLower(m.HTTPMethod),
					pName,
				),
			})
		}
	}

	// 3. Query parameters check
	localQueryParams := make(map[string]*ir.ParamIR)
	for _, p := range m.Params {
		if p.Location == ir.LocContext || p.Location == ir.LocModifiers {
			continue
		}

		if p.Location == ir.LocQuery || p.Location == "" {
			wire := strings.ToLower(p.WireKey)
			if wire == "" {
				wire = strings.ToLower(p.GoName)
			}

			localQueryParams[wire] = p
			localQueryParams[strings.ToLower(p.GoName)] = p
		}
	}

	for qName, qParam := range rop.queryParams {
		localParam, exists := localQueryParams[qName]
		if !exists {
			if qParam.Required {
				report.Drifts = append(report.Drifts, DriftItem{
					Severity:   SeverityBreaking,
					Kind:       DriftMissingParam,
					Service:    svc.Name,
					Method:     m.Name,
					Endpoint:   endpointDesc,
					Param:      qParam.Name,
					Expected:   fmt.Sprintf("required query parameter %q", qParam.Name),
					Actual:     "absent in Go method signature",
					Message:    fmt.Sprintf("Required query parameter %q is missing in Go method", qParam.Name),
					Suggestion: fmt.Sprintf("Add parameter `%s` to method `%s`", toParamGoName(qParam.Name), m.Name),
				})
			} else {
				report.Drifts = append(report.Drifts, DriftItem{
					Severity: SeverityNonBreaking,
					Kind:     DriftMissingParam,
					Service:  svc.Name,
					Method:   m.Name,
					Endpoint: endpointDesc,
					Param:    qParam.Name,
					Expected: fmt.Sprintf("optional query parameter %q", qParam.Name),
					Actual:   "not mapped in Go method signature",
					Message: fmt.Sprintf(
						"Optional query parameter %q is available in remote specification",
						qParam.Name,
					),
					Suggestion: fmt.Sprintf(
						"Add parameter `%s` or DTO field to method `%s`",
						toParamGoName(qParam.Name),
						m.Name,
					),
				})
			}

			continue
		}

		// Type compatibility check
		if qParam.Schema != nil && len(qParam.Schema.Type) > 0 {
			schemaType := qParam.Schema.Type.Primary()

			if schemaType != "" && !isTypeCompatible(localParam.GoType.Name, schemaType) {
				report.Drifts = append(report.Drifts, DriftItem{
					Severity: SeverityBreaking,
					Kind:     DriftTypeMismatch,
					Service:  svc.Name,
					Method:   m.Name,
					Endpoint: endpointDesc,
					Param:    qParam.Name,
					Expected: schemaType,
					Actual:   localParam.GoType.Name,
					Message: fmt.Sprintf(
						"Parameter %q type mismatch: remote expects %s, Go contract uses %s",
						qParam.Name,
						schemaType,
						localParam.GoType.Name,
					),
					Suggestion: fmt.Sprintf(
						"Change parameter `%s` type to match OpenAPI `%s`",
						localParam.GoName,
						schemaType,
					),
				})
			}
		}
	}
}

func normalizePath(p string) string {
	clean := strings.Trim(p, "/")

	parts := strings.Split(clean, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			parts[i] = "{var}"
		}
	}

	return "/" + strings.Join(parts, "/")
}

func toParamGoName(wire string) string {
	parts := strings.FieldsFunc(wire, func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	})
	if len(parts) == 0 {
		return wire
	}

	var sb strings.Builder
	sb.WriteString(strings.ToLower(parts[0]))

	for _, p := range parts[1:] {
		if len(p) > 0 {
			sb.WriteString(strings.ToUpper(p[:1]))
			sb.WriteString(strings.ToLower(p[1:]))
		}
	}

	return sb.String()
}

func isTypeCompatible(goType, openAPIType string) bool {
	clean := strings.TrimPrefix(goType, "*")
	switch openAPIType {
	case "string":
		return clean == "string" || clean == "ID" || strings.HasSuffix(clean, ".ID") ||
			clean == "time.Time" || clean == "Time" || clean == "[]byte" || clean == "any"
	case "integer":
		return clean == "int" || clean == "int64" || clean == "int32" || clean == "int16" || clean == "int8" ||
			clean == "uint" || clean == "uint64" || clean == "uint32" || clean == "uint16" || clean == "uint8" ||
			clean == "ID" || strings.HasSuffix(clean, ".ID") || clean == "any"
	case "number":
		return clean == "float64" || clean == "float32" || clean == "int" || clean == "int64" || clean == "any"
	case "boolean":
		return clean == "bool" || clean == "any"
	case "array":
		return strings.HasPrefix(clean, "[]") || clean == "any"
	case "object":
		return strings.HasPrefix(clean, "map[") || clean == "any" || !isPrimitiveType(clean)
	default:
		return true
	}
}

func isPrimitiveType(t string) bool {
	switch t {
	case "string", "int", "int64", "int32", "uint", "uint64", "uint32", "float64", "float32", "bool", "any":
		return true
	default:
		return false
	}
}

func indexRemoteOps(remoteDoc *openapi.Document) ([]*remoteOp, map[string]*remoteOp, map[string]*remoteOp) {
	remoteOps := make([]*remoteOp, 0)
	remoteKeyMap := make(map[string]*remoteOp)
	remoteOpIDMap := make(map[string]*remoteOp)

	if remoteDoc == nil || remoteDoc.Paths == nil {
		return remoteOps, remoteKeyMap, remoteOpIDMap
	}

	for pathStr, pathItem := range remoteDoc.Paths {
		if pathItem == nil {
			continue
		}

		for httpMethod, op := range pathItem.OperationsMap() {
			if op == nil {
				continue
			}

			rop := &remoteOp{
				httpMethod:   httpMethod,
				rawPath:      pathStr,
				normPath:     normalizePath(pathStr),
				operationID:  op.OperationID,
				summary:      op.Summary,
				deprecated:   op.Deprecated,
				pathParams:   make(map[string]*openapi.Parameter),
				queryParams:  make(map[string]*openapi.Parameter),
				headerParams: make(map[string]*openapi.Parameter),
				requestBody:  op.RequestBody,
				responses:    op.Responses,
			}

			allParams := make([]*openapi.Parameter, 0, len(pathItem.Parameters)+len(op.Parameters))
			allParams = append(allParams, pathItem.Parameters...)
			allParams = append(allParams, op.Parameters...)

			for _, p := range allParams {
				if p == nil {
					continue
				}

				switch p.In {
				case "path":
					rop.pathParams[strings.ToLower(p.Name)] = p
				case "query":
					rop.queryParams[strings.ToLower(p.Name)] = p
				case "header":
					rop.headerParams[strings.ToLower(p.Name)] = p
				}
			}

			routeKey := httpMethod + " " + rop.normPath
			remoteOps = append(remoteOps, rop)

			remoteKeyMap[routeKey] = rop
			if rop.operationID != "" {
				remoteOpIDMap[rop.operationID] = rop
			}
		}
	}

	return remoteOps, remoteKeyMap, remoteOpIDMap
}

// CompareSpecs compares two OpenAPI/HAR/Swagger specification documents directly without requiring Go contract files.
func CompareSpecs(
	baseDoc *openapi.Document,
	headDoc *openapi.Document,
	baseTarget string,
	headTarget string,
) *DiffReport {
	report := &DiffReport{
		LocalTarget:  baseTarget,
		RemoteTarget: headTarget,
	}

	if baseDoc == nil || headDoc == nil {
		return report
	}

	baseOps, baseKeyMap, _ := indexRemoteOps(baseDoc)
	headOps, headKeyMap, _ := indexRemoteOps(headDoc)

	report.TotalEndpointsChecked = len(baseOps)

	// 1. Detect removed endpoints in Head (BREAKING)
	for _, bOp := range baseOps {
		routeKey := bOp.httpMethod + " " + bOp.normPath
		hOp := headKeyMap[routeKey]

		if hOp == nil {
			report.Drifts = append(report.Drifts, DriftItem{
				Severity:   SeverityBreaking,
				Kind:       DriftMissingEndpoint,
				Endpoint:   bOp.httpMethod + " " + bOp.rawPath,
				Expected:   "endpoint present in base specification",
				Actual:     "endpoint removed in target specification",
				Message:    fmt.Sprintf("Endpoint %s %s was removed in %s", bOp.httpMethod, bOp.rawPath, headTarget),
				Suggestion: "Ensure dependent consumer clients are migrated before dropping route",
			})

			continue
		}

		// Compare parameters across matching operations
		compareSpecOperations(bOp, hOp, headTarget, report)
	}

	// 2. Detect added endpoints in Head (GHOST / New routes)
	for _, hOp := range headOps {
		routeKey := hOp.httpMethod + " " + hOp.normPath
		if baseKeyMap[routeKey] == nil {
			report.TotalEndpointsChecked++
			report.Drifts = append(report.Drifts, DriftItem{
				Severity:   SeverityGhost,
				Kind:       DriftMissingEndpoint,
				Endpoint:   hOp.httpMethod + " " + hOp.rawPath,
				Expected:   "endpoint present in target specification",
				Actual:     "not present in base specification",
				Message:    fmt.Sprintf("Endpoint %s %s was added in %s", hOp.httpMethod, hOp.rawPath, headTarget),
				Suggestion: "New endpoint available in upstream specification",
			})
		}
	}

	return report
}

func compareSpecOperations(bOp, hOp *remoteOp, headTarget string, report *DiffReport) {
	endpointDesc := bOp.httpMethod + " " + bOp.rawPath

	// 1. Check Query Parameters
	for qName, hParam := range hOp.queryParams {
		bParam := bOp.queryParams[qName]
		if bParam == nil {
			if hParam.Required {
				report.Drifts = append(report.Drifts, DriftItem{
					Severity: SeverityBreaking,
					Kind:     DriftMissingParam,
					Endpoint: endpointDesc,
					Param:    hParam.Name,
					Expected: fmt.Sprintf("required parameter %q in %s", hParam.Name, headTarget),
					Actual:   "missing in base specification",
					Message: fmt.Sprintf(
						"Target specification %s added required query parameter %q",
						headTarget,
						hParam.Name,
					),
					Suggestion: "Add parameter handling in consumer clients",
				})
			} else {
				report.Drifts = append(report.Drifts, DriftItem{
					Severity: SeverityNonBreaking,
					Kind:     DriftMissingParam,
					Endpoint: endpointDesc,
					Param:    hParam.Name,
					Expected: fmt.Sprintf("optional parameter %q in %s", hParam.Name, headTarget),
					Actual:   "not present in base specification",
					Message: fmt.Sprintf(
						"Target specification %s added optional query parameter %q",
						headTarget,
						hParam.Name,
					),
					Suggestion: "Optional parameter available in upstream",
				})
			}

			continue
		}

		bType := getParamType(bParam)
		hType := getParamType(hParam)

		if bType != "" && hType != "" && bType != hType {
			report.Drifts = append(report.Drifts, DriftItem{
				Severity: SeverityBreaking,
				Kind:     DriftTypeMismatch,
				Endpoint: endpointDesc,
				Param:    hParam.Name,
				Expected: bType,
				Actual:   hType,
				Message: fmt.Sprintf(
					"Parameter %q type changed from %s to %s in %s",
					hParam.Name,
					bType,
					hType,
					headTarget,
				),
				Suggestion: "Update consumer type mappings",
			})
		}
	}

	// 2. Check Dropped Query Parameters in Head
	for qName, bParam := range bOp.queryParams {
		if hOp.queryParams[qName] == nil {
			report.Drifts = append(report.Drifts, DriftItem{
				Severity:   SeverityNonBreaking,
				Kind:       DriftExtraParam,
				Endpoint:   endpointDesc,
				Param:      bParam.Name,
				Expected:   fmt.Sprintf("parameter %q in base specification", bParam.Name),
				Actual:     "removed in " + headTarget,
				Message:    fmt.Sprintf("Query parameter %q was dropped in %s", bParam.Name, headTarget),
				Suggestion: "Verify if parameter is obsolete",
			})
		}
	}

	// 3. Request Body requirement changes
	if hOp.requestBody != nil && hOp.requestBody.Required && (bOp.requestBody == nil || !bOp.requestBody.Required) {
		report.Drifts = append(report.Drifts, DriftItem{
			Severity:   SeverityBreaking,
			Kind:       DriftMissingParam,
			Endpoint:   endpointDesc,
			Expected:   "required request body in target specification",
			Actual:     "optional or missing in base specification",
			Message:    fmt.Sprintf("Endpoint %s now requires a request body in %s", endpointDesc, headTarget),
			Suggestion: "Ensure request payload is provided",
		})
	}

	// 4. Deprecations
	if hOp.deprecated && !bOp.deprecated {
		report.Drifts = append(report.Drifts, DriftItem{
			Severity:   SeverityNonBreaking,
			Kind:       DriftDeprecationMismatch,
			Endpoint:   endpointDesc,
			Expected:   "active route in base specification",
			Actual:     "deprecated in " + headTarget,
			Message:    fmt.Sprintf("Endpoint %s was marked as deprecated in %s", endpointDesc, headTarget),
			Suggestion: "Plan migration to newer endpoint alternative",
		})
	}
}

func getParamType(p *openapi.Parameter) string {
	if p == nil || p.Schema == nil || len(p.Schema.Type) == 0 {
		return ""
	}

	return p.Schema.Type.Primary()
}
