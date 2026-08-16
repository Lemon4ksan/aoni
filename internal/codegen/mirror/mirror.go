// Copyright 2026 The Aoni Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package mirror

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

// DriftKind categorizes the nature of synchronization drift with a root source.
type DriftKind string

const (
	DriftSourceNotFound DriftKind = "mirror-source-not-found"
	DriftTargetNotFound DriftKind = "mirror-target-not-found"
	DriftMethodMissing  DriftKind = "mirror-method-missing"
	DriftParamMismatch  DriftKind = "mirror-param-mismatch"
	DriftReturnMismatch DriftKind = "mirror-return-mismatch"
	DriftFieldMismatch  DriftKind = "mirror-field-mismatch"
	DriftMissingField   DriftKind = "mirror-missing-field"
	DriftGhostMethod    DriftKind = "mirror-ghost-method"
)

// DriftDiagnostic describes a single detected structural divergence.
type DriftDiagnostic struct {
	Kind     DriftKind `json:"kind"`
	Severity string    `json:"severity"` // "error", "warning", "info"
	Message  string    `json:"message"`
	File     string    `json:"file"`
	Line     int       `json:"line"`
	Service  string    `json:"service"`
	Method   string    `json:"method,omitempty"`
}

// CheckService inspects the service's @mirror directive and compares its AST against the root Go source.
func CheckService(
	rootDir, contractFilePath string,
	svc *ir.ServiceIR,
	structs []*ir.StructIR,
) ([]DriftDiagnostic, error) {
	if svc == nil || svc.Mirror == nil || svc.Mirror.Source == "" {
		return nil, nil
	}

	mirrorSrc := svc.Mirror.Source

	if rootDir == "" && contractFilePath != "" {
		curr := filepath.Dir(contractFilePath)
		for {
			if _, err := os.Stat(filepath.Join(curr, "go.mod")); err == nil {
				rootDir = curr
				break
			}

			if _, err := os.Stat(filepath.Join(curr, ".vortex.yml")); err == nil {
				rootDir = curr
				break
			}

			parent := filepath.Dir(curr)
			if parent == curr {
				// Fallback to top-level contract dir parent
				rootDir = filepath.Dir(filepath.Dir(filepath.Dir(contractFilePath)))
				break
			}

			curr = parent
		}
	}

	var absMirrorPath string
	if filepath.IsAbs(mirrorSrc) {
		absMirrorPath = mirrorSrc
	} else {
		// Try relative to rootDir
		cand1 := filepath.Join(rootDir, filepath.FromSlash(mirrorSrc))
		if _, err := os.Stat(cand1); err == nil {
			absMirrorPath = cand1
		} else {
			// Try relative to contract file directory
			cand2 := filepath.Join(filepath.Dir(contractFilePath), filepath.FromSlash(mirrorSrc))
			if _, err := os.Stat(cand2); err == nil {
				absMirrorPath = cand2
			} else {
				absMirrorPath = cand1
			}
		}
	}

	srcBytes, err := os.ReadFile(absMirrorPath)
	if err != nil {
		return nil, fmt.Errorf("mirror root source %q not found (%s): %w", mirrorSrc, absMirrorPath, err)
	}

	fset := token.NewFileSet()

	fileAst, err := parser.ParseFile(fset, absMirrorPath, srcBytes, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse mirror root source %s: %w", absMirrorPath, err)
	}

	var diagnostics []DriftDiagnostic

	// 1. Extract interfaces and structs from root source
	rootInterfaces := make(map[string]*ast.InterfaceType)
	rootStructs := make(map[string]*ast.StructType)

	ast.Inspect(fileAst, func(n ast.Node) bool {
		genDecl, ok := n.(*ast.GenDecl)
		if ok && genDecl.Tok == token.TYPE {
			for _, spec := range genDecl.Specs {
				if typeSpec, isType := spec.(*ast.TypeSpec); isType {
					if iface, isIface := typeSpec.Type.(*ast.InterfaceType); isIface {
						rootInterfaces[typeSpec.Name.Name] = iface
					} else if st, isStruct := typeSpec.Type.(*ast.StructType); isStruct {
						rootStructs[typeSpec.Name.Name] = st
					}
				}
			}
		}

		return true
	})

	var (
		matchedIface     *ast.InterfaceType
		matchedIfaceName string
	)

	if svc.Mirror.TargetType != "" {
		for name, iface := range rootInterfaces {
			if strings.EqualFold(name, svc.Mirror.TargetType) {
				matchedIface = iface
				matchedIfaceName = name
				break
			}
		}
	} else {
		// If omitted, look for identical name or first interface
		for name, iface := range rootInterfaces {
			if strings.EqualFold(name, svc.Name) {
				matchedIface = iface
				matchedIfaceName = name
				break
			}
		}

		if matchedIface == nil {
			for name, iface := range rootInterfaces {
				matchedIface = iface
				matchedIfaceName = name
				break
			}
		}
	}

	if matchedIface == nil {
		diagnostics = append(diagnostics, DriftDiagnostic{
			Kind:     DriftTargetNotFound,
			Severity: "error",
			Message: fmt.Sprintf("Target interface %q not found in root source %s",
				svc.Mirror.TargetType, filepath.ToSlash(mirrorSrc)),
			File:    contractFilePath,
			Line:    1,
			Service: svc.Name,
		})

		return diagnostics, nil
	}

	// 2. Compare methods between Root Interface and Aoni Service
	rootMethods := make(map[string]*ast.FuncType)
	if matchedIface.Methods != nil {
		for _, m := range matchedIface.Methods.List {
			if len(m.Names) > 0 {
				if fnType, ok := m.Type.(*ast.FuncType); ok {
					rootMethods[m.Names[0].Name] = fnType
				}
			}
		}
	}

	// Check methods in Aoni service
	for _, m := range svc.Methods {
		rootFn, exists := rootMethods[m.Name]
		if !exists {
			// Method in wrapper not present in root source
			if svc.Mirror.Strict {
				diagnostics = append(diagnostics, DriftDiagnostic{
					Kind:     DriftMethodMissing,
					Severity: "warning",
					Message: fmt.Sprintf("Method %s.%s is not defined in root source %s (%s)",
						svc.Name, m.Name, matchedIfaceName, filepath.ToSlash(mirrorSrc)),
					File:    contractFilePath,
					Line:    1,
					Service: svc.Name,
					Method:  m.Name,
				})
			}

			continue
		}

		// Method exists in both: compare parameter signatures
		rootParams := extractParamTypes(rootFn.Params)
		wrapperParams := extractWrapperParams(m)

		if errDesc := compareParams(rootParams, wrapperParams); errDesc != "" {
			diagnostics = append(diagnostics, DriftDiagnostic{
				Kind:     DriftParamMismatch,
				Severity: "error",
				Message: fmt.Sprintf("Signature drift in %s.%s: %s (Root source: %s, Wrapper: %s)",
					svc.Name, m.Name, errDesc, formatTypeList(rootParams), formatTypeList(wrapperParams)),
				File:    contractFilePath,
				Line:    1,
				Service: svc.Name,
				Method:  m.Name,
			})
		}
	}

	// 2b. Check for ghost methods in root source not yet exposed in wrapper
	wrapperMethodNames := make(map[string]bool)
	for _, m := range svc.Methods {
		wrapperMethodNames[m.Name] = true
	}

	for rootMethodName := range rootMethods {
		if !wrapperMethodNames[rootMethodName] {
			diagnostics = append(diagnostics, DriftDiagnostic{
				Kind:     DriftGhostMethod,
				Severity: "warning",
				Message: fmt.Sprintf(
					"Public method %s is defined in root source %s (%s), but not exposed in wrapper %s",
					rootMethodName,
					matchedIfaceName,
					filepath.ToSlash(mirrorSrc),
					svc.Name,
				),
				File:    contractFilePath,
				Line:    1,
				Service: svc.Name,
				Method:  rootMethodName,
			})
		}
	}

	// 3. Compare DTO structs referenced by return types
	wrapperStructMap := make(map[string]*ir.StructIR)
	for _, st := range structs {
		wrapperStructMap[st.Name] = st
	}

	for rootStructName, rootSt := range rootStructs {
		wrapperSt, found := findMatchingWrapperStruct(rootStructName, wrapperStructMap)
		if !found {
			continue
		}

		rootFields := extractStructFields(rootSt)

		wrapperFields := make(map[string]string)
		for _, f := range wrapperSt.Fields {
			wrapperFields[f.GoName] = normalizeType(goTypeIRToString(f.Type))
		}

		for rfName, rfType := range rootFields {
			wfType, wfExists := wrapperFields[rfName]
			if !wfExists {
				if svc.Mirror.Strict {
					diagnostics = append(diagnostics, DriftDiagnostic{
						Kind:     DriftMissingField,
						Severity: "info",
						Message: fmt.Sprintf(
							"Struct %s: field %q (%s) exists in root source, but omitted in wrapper %s",
							rootStructName,
							rfName,
							rfType,
							wrapperSt.Name,
						),
						File:    contractFilePath,
						Line:    1,
						Service: svc.Name,
					})
				}

				continue
			}

			if !typesCompatible(rfType, wfType) {
				diagnostics = append(diagnostics, DriftDiagnostic{
					Kind:     DriftFieldMismatch,
					Severity: "error",
					Message: fmt.Sprintf("Struct drift in %s.%s: root source has type %s, but wrapper has %s",
						wrapperSt.Name, rfName, rfType, wfType),
					File:    contractFilePath,
					Line:    1,
					Service: svc.Name,
				})
			}
		}
	}

	return diagnostics, nil
}

func goTypeIRToString(gt ir.GoTypeIR) string {
	prefix := ""
	if gt.IsSlice {
		prefix = "[]"
	} else if gt.IsPointer {
		prefix = "*"
	}

	if gt.Package != "" {
		return prefix + gt.Package + "." + gt.Name
	}

	return prefix + gt.Name
}

func extractParamTypes(fields *ast.FieldList) []string {
	if fields == nil || fields.List == nil {
		return nil
	}

	var types []string
	for _, f := range fields.List {
		tStr := exprToString(f.Type)
		// Skip standard context.Context
		if tStr == "context.Context" || tStr == "Context" {
			continue
		}

		// Skip RequestModifier varargs
		if strings.Contains(tStr, "RequestModifier") {
			continue
		}

		namesCount := len(f.Names)
		if namesCount == 0 {
			namesCount = 1
		}

		for i := 0; i < namesCount; i++ {
			types = append(types, normalizeType(tStr))
		}
	}

	return types
}

func extractWrapperParams(m *ir.MethodIR) []string {
	var types []string
	for _, p := range m.Params {
		tStr := goTypeIRToString(p.GoType)
		if tStr == "context.Context" || tStr == "Context" || strings.Contains(tStr, "RequestModifier") {
			continue
		}

		types = append(types, normalizeType(tStr))
	}

	return types
}

func compareParams(rootParams, wrapperParams []string) string {
	if len(rootParams) != len(wrapperParams) {
		return fmt.Sprintf(
			"parameter count mismatch (%d in root vs %d in wrapper)",
			len(rootParams),
			len(wrapperParams),
		)
	}

	for i := range rootParams {
		if !typesCompatible(rootParams[i], wrapperParams[i]) {
			return fmt.Sprintf("param #%d type mismatch: expected %s, got %s", i+1, rootParams[i], wrapperParams[i])
		}
	}

	return ""
}

func extractStructFields(st *ast.StructType) map[string]string {
	fields := make(map[string]string)
	if st == nil || st.Fields == nil {
		return fields
	}

	for _, f := range st.Fields.List {
		tStr := normalizeType(exprToString(f.Type))
		for _, name := range f.Names {
			fields[name.Name] = tStr
		}
	}

	return fields
}

func findMatchingWrapperStruct(rootName string, wrapperMap map[string]*ir.StructIR) (*ir.StructIR, bool) {
	if st, ok := wrapperMap[rootName]; ok {
		return st, true
	}

	// Try stripping "Legacy" prefix if present
	trimmed := strings.TrimPrefix(rootName, "Legacy")
	if st, ok := wrapperMap[trimmed]; ok {
		return st, true
	}

	return nil, false
}

func exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	case *ast.ArrayType:
		return "[]" + exprToString(e.Elt)
	case *ast.MapType:
		return "map[" + exprToString(e.Key) + "]" + exprToString(e.Value)
	case *ast.SelectorExpr:
		return exprToString(e.X) + "." + e.Sel.Name
	case *ast.Ellipsis:
		return "..." + exprToString(e.Elt)
	case *ast.InterfaceType:
		return "any"
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func normalizeType(t string) string {
	t = strings.TrimSpace(t)
	t = strings.ReplaceAll(t, "interface{}", "any")
	return t
}

func typesCompatible(t1, t2 string) bool {
	t1 = normalizeType(t1)
	t2 = normalizeType(t2)

	if t1 == t2 {
		return true
	}

	// Remove package qualifiers for comparison (e.g. "time.Time" vs "Time", "*Item" vs "*legacy.Item")
	strip1 := stripPackageQualifier(t1)
	strip2 := stripPackageQualifier(t2)

	return strip1 == strip2
}

func stripPackageQualifier(t string) string {
	parts := strings.Split(t, ".")
	if len(parts) > 1 {
		prefix := ""
		if strings.HasPrefix(parts[0], "*") {
			prefix = "*"
		} else if strings.HasPrefix(parts[0], "[]") {
			prefix = "[]"
		}

		return prefix + parts[len(parts)-1]
	}

	return t
}

func formatTypeList(types []string) string {
	if len(types) == 0 {
		return "()"
	}

	return "(" + strings.Join(types, ", ") + ")"
}
