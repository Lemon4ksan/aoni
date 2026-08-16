// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package diff

import (
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

type remoteOp struct {
	httpMethod   string
	rawPath      string
	normPath     string
	operationID  string
	summary      string
	deprecated   bool
	pathParams   map[string]*openapi3.Parameter
	queryParams  map[string]*openapi3.Parameter
	headerParams map[string]*openapi3.Parameter
	requestBody  *openapi3.RequestBody
	responses    *openapi3.Responses
}

// Compare compares local RootIR against a loaded remote OpenAPI 3.x document.
func (e *DiffEngine) Compare(
	local *ir.RootIR,
	remoteDoc *openapi3.T,
	localTarget string,
	remoteTarget string,
) *DiffReport {
	report := &DiffReport{
		LocalTarget:  localTarget,
		RemoteTarget: remoteTarget,
	}

	if local == nil || remoteDoc == nil {
		return report
	}

	// 1. Index remote operations
	remoteOps := make([]*remoteOp, 0)
	remoteKeyMap := make(map[string]*remoteOp)
	remoteOpIDMap := make(map[string]*remoteOp)

	if remoteDoc.Paths != nil {
		for pathStr, pathItem := range remoteDoc.Paths.Map() {
			if pathItem == nil {
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

				rop := &remoteOp{
					httpMethod:   httpMethod,
					rawPath:      pathStr,
					normPath:     normalizePath(pathStr),
					operationID:  op.OperationID,
					summary:      op.Summary,
					deprecated:   op.Deprecated,
					pathParams:   make(map[string]*openapi3.Parameter),
					queryParams:  make(map[string]*openapi3.Parameter),
					headerParams: make(map[string]*openapi3.Parameter),
					requestBody:  nil,
					responses:    op.Responses,
				}

				if op.RequestBody != nil && op.RequestBody.Value != nil {
					rop.requestBody = op.RequestBody.Value
				}

				// Collect path item and operation parameters
				allParams := make([]*openapi3.ParameterRef, 0, len(pathItem.Parameters)+len(op.Parameters))
				allParams = append(allParams, pathItem.Parameters...)
				allParams = append(allParams, op.Parameters...)

				for _, pRef := range allParams {
					if pRef == nil || pRef.Value == nil {
						continue
					}

					p := pRef.Value
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
	}

	report.TotalEndpointsChecked = len(remoteOps)

	// 2. Track matched remote operations
	matchedRemote := make(map[*remoteOp]bool)

	// 3. Inspect each local service and method
	for _, svc := range local.Services {
		for _, m := range svc.Methods {
			if m.Operation != ir.OpHTTP && m.HTTPMethod == "" {
				continue
			}

			localHTTPMethod := strings.ToUpper(m.HTTPMethod)
			if localHTTPMethod == "" {
				localHTTPMethod = "GET"
			}

			localRawPath := "/"
			if m.Path != nil && m.Path.RawTemplate != "" {
				localRawPath = m.Path.RawTemplate
				if !strings.HasPrefix(localRawPath, "/") {
					localRawPath = "/" + localRawPath
				}
			}

			localNormPath := normalizePath(localRawPath)
			localKey := localHTTPMethod + " " + localNormPath

			// Match with remote operation
			var rop *remoteOp
			if matched, ok := remoteKeyMap[localKey]; ok {
				rop = matched
			} else if m.OperationID != "" {
				if matched, ok := remoteOpIDMap[m.OperationID]; ok {
					rop = matched
				}
			} else if matched, ok := remoteOpIDMap[m.Name]; ok {
				rop = matched
			}

			endpointDesc := fmt.Sprintf("%s %s (%s.%s)", localHTTPMethod, localRawPath, svc.Name, m.Name)

			if rop == nil {
				// Local endpoint missing in remote OpenAPI spec
				report.Drifts = append(report.Drifts, DriftItem{
					Severity: SeverityGhost,
					Kind:     DriftMissingEndpoint,
					Service:  svc.Name,
					Method:   m.Name,
					Endpoint: endpointDesc,
					Message: fmt.Sprintf(
						"Method %s exists in Go contract but is absent from remote OpenAPI specification",
						m.Name,
					),
					Suggestion: "Export local contract via `vortex oapi` or check if route URL changed",
				})

				continue
			}

			matchedRemote[rop] = true

			// Compare matched method
			e.compareMethod(report, svc, m, rop, endpointDesc)
		}
	}

	// 4. Check for remote operations not implemented in Go
	for _, rop := range remoteOps {
		if !matchedRemote[rop] {
			remoteDesc := fmt.Sprintf("%s %s", rop.httpMethod, rop.rawPath)
			if rop.operationID != "" {
				remoteDesc += fmt.Sprintf(" [%s]", rop.operationID)
			}

			report.Drifts = append(report.Drifts, DriftItem{
				Severity: SeverityGhost,
				Kind:     DriftMissingEndpoint,
				Endpoint: remoteDesc,
				Message: fmt.Sprintf(
					"Endpoint %s is declared in remote OpenAPI specification but not implemented in Go contracts",
					remoteDesc,
				),
				Suggestion: fmt.Sprintf("Add method to Go interface or run `vortex oapi --import=%s`", remoteTarget),
			})
		}
	}

	return report
}

func (e *DiffEngine) compareMethod(
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
		if qParam.Schema != nil && qParam.Schema.Value != nil {
			schemaType := ""
			if qParam.Schema.Value.Type != nil && len(*qParam.Schema.Value.Type) > 0 {
				schemaType = (*qParam.Schema.Value.Type)[0]
			}

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
			sb.WriteString(strings.ToUpper(p[:1]) + strings.ToLower(p[1:]))
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
