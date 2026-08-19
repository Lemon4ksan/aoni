// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package emitter

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

func emitStatusRoutingExecution(
	buf *bytes.Buffer,
	tracker *ImportTracker,
	m *ir.MethodIR,
	targetReq, rawPath string,
	bodyParam *ir.ParamIR,
) {
	tracker.Add("fmt")
	tracker.Add("net/http")
	tracker.Add("github.com/lemon4ksan/aoni")
	tracker.Add("github.com/lemon4ksan/aoni/codec/decode")

	methodVerb := m.HTTPMethod
	if methodVerb == "" {
		methodVerb = "GET"
	}

	zeroVal := zeroValueOf(m.Return)
	union := m.Return.UnionType

	if bodyParam != nil {
		tracker.Add("github.com/lemon4ksan/aoni/mod")
		fmt.Fprintf(buf, "\tallMods = append(allMods, mod.WithBodyJSON(%s))\n", bodyParam.GoName)
	}

	fmt.Fprintf(
		buf,
		"\tresp, err := %s.Request(ctx, http.Method%s, %q, allMods...)\n",
		targetReq,
		toPascalCase(strings.ToLower(methodVerb)),
		rawPath,
	)
	fmt.Fprintf(buf, "\tif err != nil {\n\t\treturn %s, err\n\t}\n", zeroVal)
	buf.WriteString("\tdefer aoni.CloseResponse(resp)\n\n")

	buf.WriteString("\tswitch resp.StatusCode {\n")

	if union != nil {
		emitUnionStatusCases(buf, union, zeroVal)
	} else if len(m.Return.StatusMap) > 0 {
		emitStatusMapCases(buf, m, zeroVal)
	}

	buf.WriteString("\tdefault:\n")

	if m.Return.ErrorModelType != "" {
		cleanErrType := strings.TrimPrefix(m.Return.ErrorModelType, "*")
		fmt.Fprintf(buf, "\t\tapiErr, err := decode.JSON[%s](resp.Body)\n", cleanErrType)
		buf.WriteString("\t\tif err == nil {\n")
		fmt.Fprintf(buf, "\t\t\treturn %s, &apiErr\n", zeroVal)
		buf.WriteString("\t\t}\n")
	}

	if m.Return.IsVoid {
		buf.WriteString("\t\treturn fmt.Errorf(\"unexpected response status code: %d\", resp.StatusCode)\n")
	} else {
		fmt.Fprintf(
			buf,
			"\t\treturn %s, fmt.Errorf(\"unexpected response status code: %%d\", resp.StatusCode)\n",
			zeroVal,
		)
	}

	buf.WriteString("\t}\n")
}

func emitUnionStatusCases(buf *bytes.Buffer, union *ir.UnionIR, zeroVal string) {
	for _, f := range union.Fields {
		if len(f.StatusCodes) == 0 {
			continue
		}

		codeStrs := make([]string, 0, len(f.StatusCodes))
		for _, sc := range f.StatusCodes {
			codeStrs = append(codeStrs, strconv.Itoa(sc))
		}

		fmt.Fprintf(buf, "\tcase %s:\n", strings.Join(codeStrs, ", "))

		cleanType := strings.TrimPrefix(f.Type.Name, "*")
		fmt.Fprintf(buf, "\t\tres, err := decode.JSON[%s](resp.Body)\n", cleanType)
		fmt.Fprintf(buf, "\t\tif err != nil {\n")
		fmt.Fprintf(
			buf,
			"\t\t\treturn %s, fmt.Errorf(\"decode status %%d response: %%w\", resp.StatusCode, err)\n",
			zeroVal,
		)
		fmt.Fprintf(buf, "\t\t}\n")

		if strings.HasPrefix(f.Type.Name, "*") {
			fmt.Fprintf(
				buf,
				"\t\treturn &%s{StatusCode: resp.StatusCode, %s: &res}, nil\n\n",
				union.Name,
				f.GoName,
			)
		} else {
			fmt.Fprintf(
				buf,
				"\t\treturn &%s{StatusCode: resp.StatusCode, %s: res}, nil\n\n",
				union.Name,
				f.GoName,
			)
		}
	}
}

func emitStatusMapCases(buf *bytes.Buffer, m *ir.MethodIR, zeroVal string) {
	typeGroups := make(map[string][]int)
	for code, t := range m.Return.StatusMap {
		typeGroups[t.Name] = append(typeGroups[t.Name], code)
	}

	sortedTypeNames := make([]string, 0, len(typeGroups))
	for typeName := range typeGroups {
		sortedTypeNames = append(sortedTypeNames, typeName)
	}

	sort.Strings(sortedTypeNames)

	for _, typeName := range sortedTypeNames {
		codes := typeGroups[typeName]
		sort.Ints(codes)

		codeStrs := make([]string, 0, len(codes))
		for _, sc := range codes {
			codeStrs = append(codeStrs, strconv.Itoa(sc))
		}

		fmt.Fprintf(buf, "\tcase %s:\n", strings.Join(codeStrs, ", "))

		cleanType := strings.TrimPrefix(typeName, "*")

		is2xx := false
		for _, sc := range codes {
			if sc >= 200 && sc <= 299 {
				is2xx = true
				break
			}
		}

		if is2xx {
			fmt.Fprintf(buf, "\t\tres, err := decode.JSON[%s](resp.Body)\n", cleanType)
			fmt.Fprintf(buf, "\t\tif err != nil {\n")
			fmt.Fprintf(
				buf,
				"\t\t\treturn %s, fmt.Errorf(\"decode status %%d response: %%w\", resp.StatusCode, err)\n",
				zeroVal,
			)
			fmt.Fprintf(buf, "\t\t}\n")

			if strings.HasPrefix(m.Return.SuccessType.Name, "*") {
				buf.WriteString("\t\treturn &res, nil\n\n")
			} else {
				buf.WriteString("\t\treturn res, nil\n\n")
			}
		} else {
			fmt.Fprintf(buf, "\t\terrModel, err := decode.JSON[%s](resp.Body)\n", cleanType)
			fmt.Fprintf(buf, "\t\tif err != nil {\n")
			fmt.Fprintf(
				buf,
				"\t\t\treturn %s, fmt.Errorf(\"decode status %%d error: %%w\", resp.StatusCode, err)\n",
				zeroVal,
			)
			fmt.Fprintf(buf, "\t\t}\n")
			fmt.Fprintf(buf, "\t\treturn %s, &errModel\n\n", zeroVal)
		}
	}
}

func zeroValueOf(ret *ir.ReturnIR) string {
	if ret == nil || ret.IsVoid {
		return ""
	}

	t := ret.SuccessType.Name
	switch {
	case strings.HasPrefix(t, "*") || strings.HasPrefix(t, "[]") || strings.HasPrefix(t, "map[") || t == "any" || t == "interface{}":
		return "nil"
	case t == "string":
		return `""`
	case t == "bool":
		return "false"
	case t == "int", t == "int8", t == "int16", t == "int32", t == "int64",
		t == "uint", t == "uint8", t == "uint16", t == "uint32", t == "uint64", t == "uintptr",
		t == "float32", t == "float64", t == "byte", t == "rune":
		return "0"
	default:
		return t + "{}"
	}
}

func isCompiledDTO(root *ir.RootIR, typeName string) bool {
	if root == nil {
		return false
	}

	typeName = strings.TrimPrefix(typeName, "*")
	for _, s := range root.Structs {
		if s.Name == typeName {
			return true
		}
	}

	return false
}
