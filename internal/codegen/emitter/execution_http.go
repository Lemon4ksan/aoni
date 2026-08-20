// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package emitter

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

func emitExecution(
	buf *bytes.Buffer,
	tracker *ImportTracker,
	m *ir.MethodIR,
	targetReq, rawPath string,
	bodyParam *ir.ParamIR,
) {
	if m.ReturnPipeline != nil {
		emitReturnPipeline(buf, tracker, m, targetReq, rawPath)
		return
	}

	methodVerb := m.HTTPMethod
	if methodVerb == "" {
		methodVerb = "GET"
	}

	genericType := "request.NoResponse"
	if m.Return != nil && !m.Return.IsVoid && m.Return.SuccessType.Name != "" {
		genericType = strings.TrimPrefix(m.Return.SuccessType.Name, "*")
	}

	// Handle Envelope unwrapping (@unwrap data)
	if m.UnwrapField != "" {
		emitEnvelopeUnwrap(buf, tracker, m, targetReq, rawPath, bodyParam)
		return
	}

	// Status-aware wire dispatch (Multi-status union, explicit @status mapping, or @error_model)
	if m.Return != nil && (len(m.Return.StatusMap) > 0 || m.Return.UnionType != nil || m.Return.ErrorModelType != "") {
		emitStatusRoutingExecution(buf, tracker, m, targetReq, rawPath, bodyParam)
		return
	}

	bodyArg := "nil"
	if bodyParam != nil {
		bodyArg = bodyParam.GoName
	}

	// Custom call helper (@call custom.Func)
	if m.CallFunc != "" {
		emitCustomCall(buf, tracker, m, targetReq, rawPath, genericType, bodyArg, bodyParam)
		return
	}

	emitStandardHTTPMethodCall(buf, tracker, m, targetReq, rawPath, methodVerb, genericType, bodyArg)
}

func emitCustomCall(
	buf *bytes.Buffer,
	tracker *ImportTracker,
	m *ir.MethodIR,
	targetReq, rawPath, genericType, bodyArg string,
	bodyParam *ir.ParamIR,
) {
	callArgs := fmt.Sprintf("ctx, %s, %q", targetReq, rawPath)
	if bodyParam != nil {
		callArgs = fmt.Sprintf("ctx, %s, %q, %s", targetReq, rawPath, bodyArg)
	}

	isPointer := strings.HasPrefix(m.Return.SuccessType.Name, "*")

	switch {
	case m.Return.IsVoid:
		fmt.Fprintf(buf, "\t_, err := %s[%s](%s, allMods...)\n", m.CallFunc, genericType, callArgs)
		buf.WriteString("\treturn err\n")

	case m.Return.SuccessType.Name == "io.ReadCloser" || m.Return.SuccessType.Name == "io.Reader":
		tracker.Add("io")
		fmt.Fprintf(buf, "\treturn %s(%s, allMods...)\n", m.CallFunc, callArgs)

	default:
		fmt.Fprintf(buf, "\tresp, err := %s[%s](%s, allMods...)\n", m.CallFunc, genericType, callArgs)
		buf.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
		emitChecks(buf, tracker, m)

		if isPointer {
			buf.WriteString("\treturn resp, nil\n")
		} else {
			buf.WriteString("\treturn *resp, nil\n")
		}
	}
}

func emitStandardHTTPMethodCall(
	buf *bytes.Buffer,
	tracker *ImportTracker,
	m *ir.MethodIR,
	targetReq, rawPath, methodVerb, genericType, bodyArg string,
) {
	tracker.Add("github.com/lemon4ksan/aoni/request")

	isPointer := strings.HasPrefix(m.Return.SuccessType.Name, "*")
	zeroVal := zeroValueOf(m.Return)

	switch methodVerb {
	case "GET":
		emitGetExecution(buf, tracker, m, targetReq, rawPath, genericType, zeroVal, isPointer)
	case "POST":
		emitBodyVerbExecution(buf, tracker, m, targetReq, rawPath, "PostTo", bodyArg, genericType, zeroVal, isPointer)
	case "PUT":
		emitBodyVerbExecution(buf, tracker, m, targetReq, rawPath, "PutTo", bodyArg, genericType, zeroVal, isPointer)
	case "DELETE":
		emitBodyVerbExecution(buf, tracker, m, targetReq, rawPath, "DeleteTo", bodyArg, genericType, zeroVal, isPointer)
	case "PATCH":
		emitBodyVerbExecution(buf, tracker, m, targetReq, rawPath, "PatchTo", bodyArg, genericType, zeroVal, isPointer)
	}
}

func emitGetExecution(
	buf *bytes.Buffer,
	tracker *ImportTracker,
	m *ir.MethodIR,
	targetReq, rawPath, genericType, zeroVal string,
	isPointer bool,
) {
	switch {
	case m.Return.IsVoid:
		fmt.Fprintf(
			buf,
			"\t_, err := request.GetTo[%s](ctx, %s, %q, allMods...)\n",
			genericType,
			targetReq,
			rawPath,
		)
		buf.WriteString("\treturn err\n")

	case m.Return.IsDirectBytes:
		tracker.Add("io")
		tracker.Add("net/http")
		tracker.Add("github.com/lemon4ksan/aoni")
		fmt.Fprintf(buf, "\tresp, err := %s.Request(ctx, http.MethodGet, %q, allMods...)\n", targetReq, rawPath)
		buf.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
		buf.WriteString("\tdefer aoni.CloseResponse(resp)\n")
		buf.WriteString("\treturn io.ReadAll(resp.Body)\n")

	case m.Return.SuccessType.Name == "io.ReadCloser" || m.Return.SuccessType.Name == "io.Reader":
		tracker.Add("net/http")
		fmt.Fprintf(buf, "\tresp, err := %s.Request(ctx, http.MethodGet, %q, allMods...)\n", targetReq, rawPath)
		buf.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
		buf.WriteString("\treturn resp.Body, nil\n")

	default:
		fmt.Fprintf(
			buf,
			"\tresp, err := request.GetTo[%s](ctx, %s, %q, allMods...)\n",
			genericType,
			targetReq,
			rawPath,
		)
		fmt.Fprintf(buf, "\tif err != nil {\n\t\treturn %s, err\n\t}\n", zeroVal)
		emitChecks(buf, tracker, m)

		if isPointer {
			buf.WriteString("\treturn resp, nil\n")
		} else {
			buf.WriteString("\treturn *resp, nil\n")
		}
	}
}

func emitBodyVerbExecution(
	buf *bytes.Buffer,
	tracker *ImportTracker,
	m *ir.MethodIR,
	targetReq, rawPath, helperFn, bodyArg, genericType, zeroVal string,
	isPointer bool,
) {
	if m.Return.IsVoid {
		fmt.Fprintf(
			buf,
			"\t_, err := request.%s[%s](ctx, %s, %q, %s, allMods...)\n",
			helperFn,
			genericType,
			targetReq,
			rawPath,
			bodyArg,
		)
		buf.WriteString("\treturn err\n")

		return
	}

	fmt.Fprintf(
		buf,
		"\tresp, err := request.%s[%s](ctx, %s, %q, %s, allMods...)\n",
		helperFn,
		genericType,
		targetReq,
		rawPath,
		bodyArg,
	)
	fmt.Fprintf(buf, "\tif err != nil {\n\t\treturn %s, err\n\t}\n", zeroVal)
	emitChecks(buf, tracker, m)

	if isPointer {
		buf.WriteString("\treturn resp, nil\n")
	} else {
		buf.WriteString("\treturn *resp, nil\n")
	}
}

func emitReturnPipeline(buf *bytes.Buffer, tracker *ImportTracker, m *ir.MethodIR, targetReq, rawPath string) {
	tracker.Add("io")
	tracker.Add("net/http")
	tracker.Add("github.com/lemon4ksan/aoni/codec/decode")
	tracker.Add("github.com/lemon4ksan/aoni/codec/extract")

	methodVerb := m.HTTPMethod
	if methodVerb == "" {
		methodVerb = "GET"
	}

	resultType := "any"

	isPointer := true
	if m.Return != nil && !m.Return.IsVoid && m.Return.SuccessType.Name != "" {
		resultType = strings.TrimPrefix(m.Return.SuccessType.Name, "*")
		isPointer = m.Return.SuccessType.IsPointer
	}

	p := m.ReturnPipeline

	httpMethod := "http.MethodGet"
	switch strings.ToUpper(methodVerb) {
	case "POST":
		httpMethod = "http.MethodPost"
	case "PUT":
		httpMethod = "http.MethodPut"
	case "DELETE":
		httpMethod = "http.MethodDelete"
	case "PATCH":
		httpMethod = "http.MethodPatch"
	}

	fmt.Fprintf(buf, "\tresp, err := %s.Request(ctx, %s, %q, allMods...)\n", targetReq, httpMethod, rawPath)
	buf.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
	buf.WriteString("\tdefer resp.Body.Close()\n\n")

	if p.Source == "header" {
		fmt.Fprintf(buf, "\tstageIn := []byte(resp.Header.Get(%q))\n\n", p.SourceArg)
	} else {
		buf.WriteString("\tbodyBytes, err := io.ReadAll(resp.Body)\n")
		buf.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n\n")
		buf.WriteString("\tstageIn := bodyBytes\n")
	}

	for i, stage := range p.Stages {
		isLast := (i == len(p.Stages)-1)

		switch stage.Type {
		case ir.StageRegex:
			pattern := ""
			if len(stage.Args) > 0 {
				pattern = stage.Args[0]
			} else if pat, ok := stage.NamedArgs["pattern"]; ok {
				pattern = pat
			}

			fmt.Fprintf(buf, "\tstageOut%d, err := extract.Regex(stageIn, %q)\n", i, pattern)
			buf.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
			fmt.Fprintf(buf, "\tstageIn = stageOut%d\n\n", i)

		case ir.StageBetween:
			prefix := stage.NamedArgs["prefix"]

			suffix := stage.NamedArgs["suffix"]
			if suffix == "" {
				suffix = stage.NamedArgs["and"]
			}

			if len(stage.Args) >= 1 && prefix == "" {
				prefix = stage.Args[0]
			}

			if len(stage.Args) >= 2 && suffix == "" {
				suffix = stage.Args[1]
			}

			fmt.Fprintf(buf, "\tstageOut%d, err := extract.Between(stageIn, %q, %q)\n", i, prefix, suffix)
			buf.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
			fmt.Fprintf(buf, "\tstageIn = stageOut%d\n\n", i)

		case ir.StageAttr:
			css := stage.NamedArgs["css"]

			attr := stage.NamedArgs["name"]
			if attr == "" {
				attr = stage.NamedArgs["attr"]
			}

			if len(stage.Args) >= 1 && css == "" {
				css = stage.Args[0]
			}

			if len(stage.Args) >= 2 && attr == "" {
				attr = stage.Args[1]
			}

			fmt.Fprintf(buf, "\tstageOut%d, err := extract.Attr(stageIn, %q, %q)\n", i, css, attr)
			buf.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
			fmt.Fprintf(buf, "\tstageIn = stageOut%d\n\n", i)

		case ir.StageHTMLUnescape:
			fmt.Fprintf(buf, "\tstageIn = extract.HTMLUnescape(stageIn)\n\n")

		case ir.StageCustom:
			if isLast {
				fmt.Fprintf(buf, "\treturn %s(stageIn)\n", stage.FuncExpr)
				return
			}

			fmt.Fprintf(buf, "\tstageOut%d, err := %s(stageIn)\n", i, stage.FuncExpr)
			buf.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
			fmt.Fprintf(buf, "\tstageIn = stageOut%d\n\n", i)

		case ir.StageJSON:
			tracker.Add("fmt")
			fmt.Fprintf(buf, "\tvar result %s\n", resultType)
			buf.WriteString("\tif err := decode.UnmarshalJSON(stageIn, &result); err != nil {\n")
			buf.WriteString("\t\treturn nil, fmt.Errorf(\"pipeline json unmarshal: %w\", err)\n")
			buf.WriteString("\t}\n")
			emitChecks(buf, tracker, m)

			if isPointer {
				buf.WriteString("\treturn &result, nil\n")
			} else {
				buf.WriteString("\treturn result, nil\n")
			}

			return
		}
	}

	switch resultType {
	case "[]byte":
		buf.WriteString("\treturn stageIn, nil\n")
	case "string":
		buf.WriteString("\treturn string(stageIn), nil\n")
	default:
		fmt.Fprintf(buf, "\tvar result %s\n", resultType)
		buf.WriteString("\tif err := decode.UnmarshalJSON(stageIn, &result); err != nil {\n")
		buf.WriteString("\t\treturn nil, err\n")
		buf.WriteString("\t}\n")

		if isPointer {
			buf.WriteString("\treturn &result, nil\n")
		} else {
			buf.WriteString("\treturn result, nil\n")
		}
	}
}

func emitEnvelopeUnwrap(
	buf *bytes.Buffer,
	tracker *ImportTracker,
	m *ir.MethodIR,
	targetReq, rawPath string,
	bodyParam *ir.ParamIR,
) {
	tracker.Add("errors")
	tracker.Add("github.com/lemon4ksan/aoni/request")

	successType := m.Return.SuccessType.Name
	fieldName := strings.ToUpper(m.UnwrapField[:1]) + m.UnwrapField[1:]

	buf.WriteString("\ttype envelope struct {\n")
	buf.WriteString("\t\tSuccess bool `json:\"success\"`\n")
	fmt.Fprintf(buf, "\t\t%s %s `json:\"%s\"`\n", fieldName, successType, m.UnwrapField)
	buf.WriteString("\t\tErrorMsg string `json:\"error,omitempty\"`\n")
	buf.WriteString("\t}\n\n")

	if m.HTTPMethod == "POST" {
		bodyArg := "nil"
		if bodyParam != nil {
			bodyArg = bodyParam.GoName
		}

		fmt.Fprintf(
			buf,
			"\tresp, err := request.PostTo[envelope](ctx, %s, %q, %s, allMods...)\n",
			targetReq,
			rawPath,
			bodyArg,
		)
	} else {
		fmt.Fprintf(buf, "\tresp, err := request.GetTo[envelope](ctx, %s, %q, allMods...)\n", targetReq, rawPath)
	}

	zeroVal := zeroValueOf(m.Return)
	fmt.Fprintf(buf, "\tif err != nil {\n\t\treturn %s, err\n\t}\n", zeroVal)
	fmt.Fprintf(
		buf,
		"\tif !resp.Success && resp.ErrorMsg != \"\" {\n\t\treturn %s, errors.New(resp.ErrorMsg)\n\t}\n",
		zeroVal,
	)
	fmt.Fprintf(buf, "\treturn resp.%s, nil\n", fieldName)
}

func emitChecks(buf *bytes.Buffer, tracker *ImportTracker, m *ir.MethodIR) {
	if len(m.Checks) == 0 {
		return
	}

	tracker.Add("errors")

	zeroVal := zeroValueOf(m.Return)

	for _, chk := range m.Checks {
		fieldGoName := toPascalCase(chk.Field)

		cond := "!resp." + fieldGoName
		if chk.ExpectedVal != "true" {
			switch chk.Operator {
			case ir.OpEqual:
				cond = fmt.Sprintf("resp.%s != %s", fieldGoName, chk.ExpectedVal)
			case ir.OpNotEqual:
				cond = fmt.Sprintf("resp.%s == %s", fieldGoName, chk.ExpectedVal)
			default:
				cond = fmt.Sprintf("!(resp.%s %s %s)", fieldGoName, chk.Operator, chk.ExpectedVal)
			}
		}

		fmt.Fprintf(buf, "\tif %s {\n", cond)
		fmt.Fprintf(buf, "\t\treturn %s, errors.New(%q)\n", zeroVal, chk.ErrorMsg)
		buf.WriteString("\t}\n")
	}
}
