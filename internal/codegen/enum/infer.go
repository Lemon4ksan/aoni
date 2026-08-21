// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package enum

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/lemon4ksan/foundation/generic"
)

// EnumValue represents a single named enum variant.
type EnumValue struct {
	ConstName string `json:"const_name"`
	RawValue  any    `json:"raw_value"`
}

// EnumSpec represents a Go type alias and associated const block.
type EnumSpec struct {
	Name             string      `json:"name"`
	BaseType         string      `json:"base_type"`
	Values           []EnumValue `json:"values"`
	ReferencingField string      `json:"referencing_field"`
	Doc              string      `json:"doc"`
}

// ExtractEnumsFromHAR analyzes HAR samples and extracts candidate enums for struct/tuple fields.
func ExtractEnumsFromHAR(harBytes []byte, structName string) []EnumSpec {
	var har struct {
		Log struct {
			Entries []struct {
				Request struct {
					URL string `json:"url"`
				} `json:"request"`
				Response struct {
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"response"`
			} `json:"entries"`
		} `json:"log"`
	}

	if err := json.Unmarshal(harBytes, &har); err != nil {
		return nil
	}

	// 1. Collect all tuple arrays
	var allTuples [][]any
	for _, entry := range har.Log.Entries {
		text := strings.TrimPrefix(entry.Response.Content.Text, ")]}'\n")

		var parsed any
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			continue
		}

		if rootArr, ok := parsed.([]any); ok {
			for _, item := range rootArr {
				if subArr, isSub := item.([]any); isSub && len(subArr) > 0 {
					for _, elem := range subArr {
						if tupleElem, isTuple := elem.([]any); isTuple {
							allTuples = append(allTuples, tupleElem)
						}
					}
				}
			}
		}
	}

	if len(allTuples) == 0 {
		return nil
	}

	// Analyze each index for distinct string/int values
	var specs []EnumSpec

	maxLen := 0
	for _, t := range allTuples {
		if len(t) > maxLen {
			maxLen = len(t)
		}
	}

	for idx := 0; idx < maxLen; idx++ {
		stringVals := make(map[string]int)
		for _, t := range allTuples {
			if idx < len(t) && t[idx] != nil {
				if strList, isList := t[idx].([]any); isList {
					for _, item := range strList {
						if s, isStr := item.(string); isStr && s != "" {
							stringVals[s]++
						}
					}
				}
			}
		}

		// Candidate for []Enum if unique string values are between 2 and 30
		if len(stringVals) >= 2 && len(stringVals) <= 30 {
			keys := generic.Keys(stringVals)
			slices.Sort(keys)

			enumName := "GenerationMethod"
			if idx != 7 {
				enumName = fmt.Sprintf("Field%dEnum", idx)
			}

			values := generic.Map(keys, func(k string) EnumValue {
				return EnumValue{
					ConstName: enumName + toPascalCase(k),
					RawValue:  k,
				}
			})

			specs = append(specs, EnumSpec{
				Name:             enumName,
				BaseType:         "string",
				Values:           values,
				ReferencingField: strconv.Itoa(idx),
				Doc:              enumName + " represents supported capabilities or modes.",
			})
		}
	}

	return specs
}

// InjectEnums injects enum type and const declarations into the contract source file and updates the struct.
func InjectEnums(filePath, targetStructName string, specs []EnumSpec) error {
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	fset := token.NewFileSet()

	fileAst, err := parser.ParseFile(fset, filePath, contentBytes, parser.ParseComments)
	if err != nil {
		return err
	}

	var codeToAdd strings.Builder
	for _, spec := range specs {
		// Check if type already exists in file
		alreadyExists := false
		ast.Inspect(fileAst, func(n ast.Node) bool {
			if ts, ok := n.(*ast.TypeSpec); ok && ts.Name.Name == spec.Name {
				alreadyExists = true
				return false
			}

			return true
		})

		if alreadyExists {
			continue
		}

		fmt.Fprintf(&codeToAdd, "\n// %s\n", spec.Doc)
		fmt.Fprintf(&codeToAdd, "type %s %s\n\n", spec.Name, spec.BaseType)
		codeToAdd.WriteString("const (\n")

		for _, v := range spec.Values {
			if spec.BaseType == "string" {
				fmt.Fprintf(&codeToAdd, "\t%s %s = %q\n", v.ConstName, spec.Name, v.RawValue)
			} else {
				fmt.Fprintf(&codeToAdd, "\t%s %s = %v\n", v.ConstName, spec.Name, v.RawValue)
			}
		}

		codeToAdd.WriteString(")\n")
	}

	// Update field types only in target struct
	ast.Inspect(fileAst, func(n ast.Node) bool {
		if ts, ok := n.(*ast.TypeSpec); ok &&
			(targetStructName == "" || strings.EqualFold(ts.Name.Name, targetStructName)) {
			if st, ok := ts.Type.(*ast.StructType); ok && st.Fields != nil {
				for _, spec := range specs {
					for _, f := range st.Fields.List {
						if f.Tag != nil {
							tagVal := strings.Trim(f.Tag.Value, "`")
							if strings.Contains(tagVal, fmt.Sprintf(`aoni:"%s"`, spec.ReferencingField)) {
								if _, isArray := f.Type.(*ast.ArrayType); isArray {
									f.Type = &ast.ArrayType{
										Elt: ast.NewIdent(spec.Name),
									}
								} else {
									f.Type = ast.NewIdent(spec.Name)
								}
							}
						}
					}
				}
			}

			return false
		}

		return true
	})

	var buf strings.Builder
	if err := format.Node(&buf, fset, fileAst); err != nil {
		return fmt.Errorf("formatting AST: %w", err)
	}

	updated := buf.String()
	if codeToAdd.Len() > 0 {
		if idx := strings.Index(updated, "type ListModelsRequest"); idx != -1 {
			updated = updated[:idx] + codeToAdd.String() + "\n" + updated[idx:]
		} else if idx := strings.Index(updated, "type ListModelsTuple"); idx != -1 {
			updated = updated[:idx] + codeToAdd.String() + "\n" + updated[idx:]
		} else {
			updated += "\n" + codeToAdd.String()
		}
	}

	return os.WriteFile(filePath, []byte(updated), 0o600)
}

func toPascalCase(s string) string {
	var sb strings.Builder

	capitalize := true

	for _, r := range s {
		if r == '_' || r == '-' || r == '.' || r == ' ' {
			capitalize = true
			continue
		}

		if capitalize {
			sb.WriteRune(unicode.ToUpper(r))

			capitalize = false
		} else {
			sb.WriteRune(r)
		}
	}

	return sb.String()
}
