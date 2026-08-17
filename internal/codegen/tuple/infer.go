// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tuple

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/internal/codegen/builder"
	"github.com/lemon4ksan/aoni/internal/codegen/history"
	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	"github.com/lemon4ksan/aoni/internal/codegen/jsbundle"
)

// InferredTuple represents a generated or refactored tuple struct definition.
type InferredTuple struct {
	StructName string
	Fields     []InferredField
	DocComment string
}

// InferredField describes an individual field in an inferred tuple struct.
type InferredField struct {
	Name    string
	GoType  string
	TagPath string
}

// DeobfuscateResult summarizes the changes performed by tuple refactoring.
type DeobfuscateResult struct {
	ModifiedFile    string
	TuplesGenerated []string
	MethodsUpdated  []string
}

// InferTupleFromJSON parses a raw JSON sample payload and deterministically infers
// Go tuple struct fields, types, and heuristic semantic names.
func InferTupleFromJSON(structName string, jsonPayload []byte) (*InferredTuple, error) {
	var raw any
	if err := json.Unmarshal(jsonPayload, &raw); err != nil {
		return nil, fmt.Errorf("unmarshaling json payload: %w", err)
	}

	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("payload root is not a json array")
	}

	// Unwrap single-element container arrays (e.g. [[[item, item]]])
	unwrapped := arr
	for len(unwrapped) == 1 {
		if sub, isSub := unwrapped[0].([]any); isSub && len(sub) > 0 {
			unwrapped = sub
		} else {
			break
		}
	}

	// If unwrapped contains array of items (e.g. [[id, name, vendor], [id, name, vendor]])
	if len(unwrapped) > 0 {
		if firstItem, isItem := unwrapped[0].([]any); isItem && len(firstItem) > 0 {
			return inferFromFlatArray(structName, firstItem), nil
		}
		// If elements are primitive values, unwrapped itself is the tuple
		return inferFromFlatArray(structName, unwrapped), nil
	}

	// Otherwise treat unwrapped as a heterogeneous / JSPB tuple
	return inferFromJSPBArray(structName, arr), nil
}

func inferFromFlatArray(structName string, items []any) *InferredTuple {
	tuple := &InferredTuple{
		StructName: structName,
		DocComment: "// @aoni:tuple",
		Fields:     make([]InferredField, 0, len(items)),
	}

	usedNames := make(map[string]bool)
	for i, val := range items {
		field := inferHeuristicField(val, i, "", structName, usedNames)
		tuple.Fields = append(tuple.Fields, field)
	}

	return tuple
}

func inferFromJSPBArray(structName string, arr []any) *InferredTuple {
	tuple := &InferredTuple{
		StructName: structName,
		DocComment: "// @aoni:tuple",
		Fields:     make([]InferredField, 0),
	}

	usedNames := make(map[string]bool)
	extractJSPBFields(arr, "", structName, usedNames, &tuple.Fields)

	if len(tuple.Fields) == 0 {
		// Fallback for empty/sparse array
		tuple.Fields = append(tuple.Fields, InferredField{
			Name:    "Value",
			GoType:  "any",
			TagPath: "0",
		})
	}

	return tuple
}

func extractJSPBFields(arr []any, prefix string, methodContext string, usedNames map[string]bool, fields *[]InferredField) {
	for i, item := range arr {
		curPath := strconv.Itoa(i)
		if prefix != "" {
			curPath = prefix + "." + strconv.Itoa(i)
		}

		if item == nil {
			continue
		}

		switch val := item.(type) {
		case []any:
			if len(val) > 0 {
				extractJSPBFields(val, curPath, methodContext, usedNames, fields)
			}
		default:
			field := inferHeuristicField(val, i, curPath, methodContext, usedNames)
			*fields = append(*fields, field)
		}
	}
}

func inferHeuristicField(val any, index int, path string, methodContext string, usedNames map[string]bool) InferredField {
	name, goType := inferRawHeuristicName(val, index, path, methodContext, usedNames)

	// Deduplicate name
	finalName := name
	counter := 2
	for usedNames[finalName] {
		finalName = fmt.Sprintf("%s_%d", name, counter)
		counter++
	}
	usedNames[finalName] = true

	tagPath := strconv.Itoa(index)
	if path != "" {
		tagPath = path
	}

	return InferredField{
		Name:    finalName,
		GoType:  goType,
		TagPath: tagPath,
	}
}

func inferRawHeuristicName(val any, index int, path string, methodContext string, usedNames map[string]bool) (string, string) {
	goType := inferGoType(val)
	if val == nil {
		if path != "" {
			return pathToFieldName(path, "Val"), "any"
		}
		return fmt.Sprintf("Field%d", index), "any"
	}

	switch v := val.(type) {
	case bool:
		if path != "" {
			return pathToFieldName(path, "IsFlag"), "bool"
		}
		if !usedNames["IsEnabled"] {
			return "IsEnabled", "bool"
		}
		if !usedNames["IsActive"] {
			return "IsActive", "bool"
		}
		if !usedNames["IsArchived"] {
			return "IsArchived", "bool"
		}
		return fmt.Sprintf("IsFlag_%d", index), "bool"

	case string:
		lower := strings.ToLower(v)

		// 1. RFC 3986 — URLs
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			if strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".jpg") ||
				strings.HasSuffix(lower, ".jpeg") || strings.HasSuffix(lower, ".svg") ||
				strings.HasSuffix(lower, ".webp") || strings.HasSuffix(lower, ".gif") {
				if !usedNames["ImageURL"] {
					return "ImageURL", "string"
				}
			}
			if !usedNames["URL"] {
				return "URL", "string"
			}
			if !usedNames["EndpointURL"] {
				return "EndpointURL", "string"
			}
			return "URL", "string"
		}

		// 2. RFC 4122 — UUIDs (36 characters, 4 hyphens)
		if len(v) == 36 && strings.Count(v, "-") == 4 {
			if !usedNames["UUID"] {
				return "UUID", "string"
			}
			return "ID", "string"
		}

		// 3. RFC 6750 / RFC 7519 — Standard Bearer Tokens & JWT
		if strings.HasPrefix(v, "Bearer ") || (strings.HasPrefix(v, "eyJ") && strings.Count(v, ".") == 2) {
			if !usedNames["Token"] {
				return "Token", "string"
			}
			return "AuthToken", "string"
		}

		// 4. RFC 3339 / ISO 8601 — Standard Date-Time
		if _, err := time.Parse(time.RFC3339, v); err == nil {
			if !usedNames["CreatedAt"] {
				return "CreatedAt", "string"
			}
			if !usedNames["UpdatedAt"] {
				return "UpdatedAt", "string"
			}
			return "Timestamp", "string"
		}

		// 5. RFC 5322 — Standard Email Addresses
		if strings.Contains(v, "@") && strings.Contains(v, ".") && !strings.Contains(v, " ") {
			if !usedNames["Email"] {
				return "Email", "string"
			}
		}

		// 6. RFC 2045 / RFC 6838 — Media Types (MIME)
		if strings.HasPrefix(lower, "application/") || strings.HasPrefix(lower, "text/") ||
			strings.HasPrefix(lower, "image/") || strings.HasPrefix(lower, "audio/") ||
			strings.HasPrefix(lower, "video/") || strings.HasPrefix(lower, "multipart/") {
			if !usedNames["ContentType"] {
				return "ContentType", "string"
			}
		}

		// 7. RFC 5646 / BCP 47 — Standard Language Tags & Locales
		if len(v) == 2 || (len(v) == 5 && v[2] == '-') || (len(v) == 5 && v[2] == '_') {
			if isCommonLocale(v) {
				if !usedNames["Locale"] {
					return "Locale", "string"
				}
			}
		}

		// 8. Positional defaults
		if path != "" {
			return pathToFieldName(path, "Str"), "string"
		}
		switch index {
		case 0:
			return "ID", "string"
		case 1:
			return "Name", "string"
		case 2:
			return "Description", "string"
		}

	case float64:
		// Exact integer checks
		if v == float64(int64(v)) {
			// Unix Timestamp in ms (2001–2065)
			if v >= 1.0e12 && v <= 3.0e12 {
				if !usedNames["TimestampMs"] {
					return "TimestampMs", "int64"
				}
				return "CreatedAtMs", "int64"
			}
			// Unix Timestamp in sec (2001–2065)
			if v >= 1.0e9 && v <= 3.0e9 {
				if !usedNames["TimestampSec"] {
					return "TimestampSec", "int64"
				}
				return "CreatedAtSec", "int64"
			}
			// RFC 9110 HTTP status codes
			if isHTTPStatusCode(int(v)) {
				if !usedNames["StatusCode"] {
					return "StatusCode", "int64"
				}
			}
			// IANA standard network ports
			if isCommonPort(int(v)) {
				if !usedNames["Port"] {
					return "Port", "int64"
				}
			}
			if path != "" {
				return pathToFieldName(path, "Int"), "int64"
			}
			if index == 0 {
				return "Value", "int64"
			}
			return fmt.Sprintf("Field%d", index), "int64"
		}

		// Fractional float
		if v > 0.0 && v <= 1.0 {
			if !usedNames["Ratio"] {
				return "Ratio", "float64"
			}
			return "Score", "float64"
		}

	case []any:
		if path != "" {
			return pathToFieldName(path, "List"), goType
		}
		if len(v) > 0 {
			if firstStr, ok := v[0].(string); ok {
				if strings.HasPrefix(strings.ToLower(firstStr), "http") {
					if !usedNames["URLs"] {
						return "URLs", "[]string"
					}
				}
			}
		}
		switch index {
		case 0:
			return "Items", goType
		default:
			return fmt.Sprintf("List%d", index), goType
		}
	}

	if path != "" {
		return pathToFieldName(path, "Val"), goType
	}
	return fmt.Sprintf("Field%d", index), goType
}

func isHTTPStatusCode(code int) bool {
	switch code {
	case 200, 201, 202, 204, 301, 302, 304, 307, 308,
		400, 401, 402, 403, 404, 405, 408, 409, 410, 422, 429,
		500, 501, 502, 503, 504:
		return true
	default:
		return false
	}
}

func isCommonPort(port int) bool {
	switch port {
	case 80, 443, 8080, 8443, 3000, 5000, 9090:
		return true
	default:
		return false
	}
}

func isCommonLocale(loc string) bool {
	switch strings.ToLower(loc) {
	case "en", "ru", "fr", "de", "es", "zh", "ja", "ko", "it", "pt",
		"en-us", "en-gb", "ru-ru", "zh-cn", "zh-tw", "ja-jp", "ko-kr",
		"fr-fr", "de-de", "es-es", "pt-br":
		return true
	default:
		return false
	}
}

func pathToFieldName(path, suffix string) string {
	parts := strings.Split(path, ".")
	var b strings.Builder
	b.WriteString("Val")
	for _, p := range parts {
		b.WriteString("_")
		b.WriteString(p)
	}
	return b.String()
}

func inferGoType(val any) string {
	if val == nil {
		return "any"
	}
	switch v := val.(type) {
	case bool:
		return "bool"
	case float64:
		if v == float64(int64(v)) {
			return "int64"
		}
		return "float64"
	case string:
		return "string"
	case []any:
		if len(v) > 0 {
			return "[]" + inferGoType(v[0])
		}
		return "[]any"
	default:
		return "any"
	}
}

// InferTupleFromJSONWithJS parses a raw JSON sample payload and enriches the inferred fields
// with exact names and nested types discovered from JavaScript bundles.
func InferTupleFromJSONWithJS(structName string, jsonPayload []byte, jsScan *jsbundle.ScanResult) (*InferredTuple, error) {
	inf, err := InferTupleFromJSON(structName, jsonPayload)
	if err != nil {
		return nil, err
	}

	if jsScan != nil && inf != nil {
		for i, f := range inf.Fields {
			idx, pErr := strconv.Atoi(f.TagPath)
			if pErr == nil {
				if desc, ok := jsScan.FindFieldDescriptor(structName, idx); ok {
					if desc.Name != "" && !strings.HasPrefix(desc.Name, "Field") {
						inf.Fields[i].Name = desc.Name
					}
					if desc.IsNested && desc.SubMsgType != "" {
						inf.Fields[i].GoType = desc.SubMsgType + "Tuple"
					}
				}
			}
		}
	}

	return inf, nil
}

// DeobfuscateFile scans contract file methods with nested slice signatures, loads recorded
// fixtures from @source, infers @aoni:tuple struct definitions, and refactors the AST.
func DeobfuscateFile(rootDir, contractFile string, dryRun bool) (*DeobfuscateResult, error) {
	return DeobfuscateFileWithJS(rootDir, contractFile, nil, dryRun)
}

// DeobfuscateFileWithJS performs tuple deobfuscation cross-referencing JavaScript bundles.
func DeobfuscateFileWithJS(rootDir, contractFile string, jsGlobs []string, dryRun bool) (*DeobfuscateResult, error) {
	absPath := contractFile
	if !filepath.IsAbs(absPath) && rootDir != "" {
		absPath = filepath.Join(rootDir, absPath)
	}

	fset := token.NewFileSet()
	fileAst, err := parser.ParseFile(fset, absPath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", absPath, err)
	}

	// Scan JavaScript bundles if provided or auto-discover in root directory
	var jsScan *jsbundle.ScanResult
	if len(jsGlobs) > 0 {
		var resolvedGlobs []string
		for _, g := range jsGlobs {
			if !filepath.IsAbs(g) && rootDir != "" {
				resolvedGlobs = append(resolvedGlobs, filepath.Join(rootDir, g))
			} else {
				resolvedGlobs = append(resolvedGlobs, g)
			}
		}
		jsScan, _ = jsbundle.ScanFiles(resolvedGlobs)
	} else if rootDir != "" {
		// Auto-discover local .js files
		if autoMatches, aErr := filepath.Glob(filepath.Join(rootDir, "*.js")); aErr == nil && len(autoMatches) > 0 {
			jsScan, _ = jsbundle.ScanFiles(autoMatches)
		}
	}

	// 1. Extract @source from service interface
	var (
		sourceSpec  string
		targetIface *ast.InterfaceType
	)

	ast.Inspect(fileAst, func(n ast.Node) bool {
		genDecl, ok := n.(*ast.GenDecl)
		if ok && genDecl.Tok == token.TYPE {
			for _, spec := range genDecl.Specs {
				if typeSpec, isType := spec.(*ast.TypeSpec); isType {
					if iface, isIface := typeSpec.Type.(*ast.InterfaceType); isIface {
						targetIface = iface
						if genDecl.Doc != nil {
							for _, comment := range genDecl.Doc.List {
								text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
								if strings.HasPrefix(text, "@source") {
									parts := strings.SplitN(text, " ", 2)
									if len(parts) == 2 {
										sourceSpec = strings.Trim(parts[1], `"`+"'")
									}
								}
							}
						}
					}
				}
			}
		}
		return true
	})

	if targetIface == nil {
		return nil, fmt.Errorf("no interface found in %s", contractFile)
	}

	fixtures := make(map[string]*ir.MockFixtureIR)
	if sourceSpec != "" {
		fixtures, _ = builder.LoadFixturesFromSource(rootDir, sourceSpec)
	}

	result := &DeobfuscateResult{
		ModifiedFile: contractFile,
	}

	var newDecls []ast.Decl

	for _, m := range targetIface.Methods.List {
		if len(m.Names) == 0 {
			continue
		}

		funcType, isFunc := m.Type.(*ast.FuncType)
		if !isFunc || funcType.Results == nil || len(funcType.Results.List) == 0 {
			continue
		}

		retField := funcType.Results.List[0]
		typeStr := typeToString(retField.Type)

		// Check if return type is a nested slice (e.g. [][][]string, [][]string, []any)
		if strings.HasPrefix(typeStr, "[][][]") || strings.HasPrefix(typeStr, "[][]") || typeStr == "[]any" || typeStr == "[]int64" {
			methodName := m.Names[0].Name
			tupleName := methodName + "Tuple"

			// Check for fixture payload
			var payload []byte
			if f, ok := fixtures[methodName]; ok && f.Body != "" {
				payload = []byte(f.Body)
			} else {
				for k, f := range fixtures {
					if (strings.Contains(strings.ToLower(k), strings.ToLower(methodName)) ||
						strings.Contains(strings.ToLower(methodName), strings.ToLower(k))) && f.Body != "" {
						payload = []byte(f.Body)
						break
					}
				}
			}

			var inf *InferredTuple
			if len(payload) > 0 {
				inf, _ = InferTupleFromJSONWithJS(tupleName, payload, jsScan)
			}

			if inf == nil {
				// Default triplet tuple
				inf = &InferredTuple{
					StructName: tupleName,
					DocComment: "// @aoni:tuple",
					Fields: []InferredField{
						{Name: "ID", GoType: "string", TagPath: "0"},
						{Name: "DisplayName", GoType: "string", TagPath: "1"},
						{Name: "Vendor", GoType: "string", TagPath: "2"},
					},
				}
			}

			// Generate struct AST
			structDecl := buildTupleStructDecl(inf)
			newDecls = append(newDecls, structDecl)

			// Replace return type in interface method
			if strings.HasPrefix(typeStr, "[]") {
				retField.Type = &ast.ArrayType{
					Elt: ast.NewIdent(tupleName),
				}
			} else {
				retField.Type = ast.NewIdent(tupleName)
			}

			result.TuplesGenerated = append(result.TuplesGenerated, tupleName)
			result.MethodsUpdated = append(result.MethodsUpdated, methodName)
		}
	}

	if len(result.TuplesGenerated) == 0 {
		return result, nil
	}

	fileAst.Decls = append(fileAst.Decls, newDecls...)

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, fileAst); err != nil {
		return nil, fmt.Errorf("formatting modified file: %w", err)
	}

	if !dryRun {
		_, _ = history.Record(
			rootDir,
			fmt.Sprintf("vortex ast tuple %s", contractFile),
			[]string{absPath},
		)

		if err := os.WriteFile(absPath, buf.Bytes(), 0o600); err != nil {
			return nil, fmt.Errorf("writing %s: %w", absPath, err)
		}
	}

	return result, nil
}

func buildTupleStructDecl(t *InferredTuple) *ast.GenDecl {
	var fieldList []*ast.Field
	for _, f := range t.Fields {
		fieldList = append(fieldList, &ast.Field{
			Names: []*ast.Ident{ast.NewIdent(f.Name)},
			Type:  ast.NewIdent(f.GoType),
			Tag: &ast.BasicLit{
				Kind:  token.STRING,
				Value: fmt.Sprintf("`aoni:%q`", f.TagPath),
			},
		})
	}

	return &ast.GenDecl{
		Tok: token.TYPE,
		Doc: &ast.CommentGroup{
			List: []*ast.Comment{
				{Text: fmt.Sprintf("// %s represents a type-safe @aoni:tuple mapping.", t.StructName)},
				{Text: "// @aoni:tuple"},
			},
		},
		Specs: []ast.Spec{
			&ast.TypeSpec{
				Name: ast.NewIdent(t.StructName),
				Type: &ast.StructType{
					Fields: &ast.FieldList{
						List: fieldList,
					},
				},
			},
		},
	}
}

func typeToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.ArrayType:
		return "[]" + typeToString(t.Elt)
	case *ast.StarExpr:
		return "*" + typeToString(t.X)
	default:
		return "any"
	}
}
