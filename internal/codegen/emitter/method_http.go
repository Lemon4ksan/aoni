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

func emitHTTPMethod(
	buf *bytes.Buffer,
	tracker *ImportTracker,
	root *ir.RootIR,
	svc *ir.ServiceIR,
	clientStructName string,
	m *ir.MethodIR,
) {
	paramsStr := formatMethodParams(m.Params)
	returnsStr := formatMethodReturns(m.Return)

	if m.Deprecation != nil {
		depMsg := m.Deprecation.Reason
		if depMsg == "" {
			depMsg = "This method is deprecated."
		}

		if m.Deprecation.Replacement != "" {
			depMsg = fmt.Sprintf("%s Use %s instead.", depMsg, m.Deprecation.Replacement)
		}

		fmt.Fprintf(buf, "// Deprecated: %s\n", depMsg)
	}

	fmt.Fprintf(buf, "func (c *%s) %s(%s) %s {\n", clientStructName, m.Name, paramsStr, returnsStr)

	// Stack modifiers buffer
	stackSize := max(m.StackModsSize, 4)

	tracker.Add("github.com/lemon4ksan/aoni")
	fmt.Fprintf(buf, "\tvar stackMods [%d]aoni.RequestModifier\n", stackSize)
	buf.WriteString("\tallMods := stackMods[:0]\n\n")

	// Telemetry & Distributed Tracing
	if svc.Telemetry != "" || m.Telemetry != "" || m.Label != "" {
		labelVal := m.Label
		if labelVal == "" {
			labelVal = svc.Name + "." + m.Name
		}

		tracker.Add("github.com/lemon4ksan/aoni/mod")
		fmt.Fprintf(buf, "\tallMods = append(allMods, mod.WithCorrelationID(\"\"), mod.WithLabel(%q))\n\n", labelVal)
	}

	// Build dynamic headers (e.g. Referer)
	for _, h := range m.Headers {
		switch {
		case h.DynamicTemplate != nil:
			emitDynamicHeader(buf, tracker, svc, m, &h)
		case strings.HasPrefix(h.StaticValue, ":"):
			emitKeywordHeader(buf, tracker, svc, m, &h)
		case h.StaticValue != "":
			tracker.Add("github.com/lemon4ksan/aoni/mod")
			fmt.Fprintf(buf, "\tallMods = append(allMods, mod.WithHeader(%q, %q))\n", h.Key, h.StaticValue)
		}
	}

	var (
		queryParams     []*ir.ParamIR
		pathParams      []*ir.ParamIR
		formParams      []*ir.ParamIR
		multipartParams []*ir.ParamIR
		bodyParam       *ir.ParamIR
		userModsParam   *ir.ParamIR
	)

	consumedFileVars := make(map[string]bool)
	for _, p := range m.Params {
		if p.Location == ir.LocMultipartFile {
			if strings.HasPrefix(p.FileName, "{") && strings.HasSuffix(p.FileName, "}") {
				v := strings.Trim(p.FileName, "{}")
				consumedFileVars[v] = true
				consumedFileVars[strings.ToLower(v)] = true
			}

			if strings.HasPrefix(p.ContentType, "{") && strings.HasSuffix(p.ContentType, "}") {
				v := strings.Trim(p.ContentType, "{}")
				consumedFileVars[v] = true
				consumedFileVars[strings.ToLower(v)] = true
			}
		}
	}

	for _, p := range m.Params {
		if consumedFileVars[p.GoName] || consumedFileVars[strings.ToLower(p.GoName)] {
			continue
		}

		switch p.Location {
		case ir.LocQuery, ir.LocQueryStruct:
			queryParams = append(queryParams, p)
		case ir.LocPath:
			pathParams = append(pathParams, p)
		case ir.LocFormFields:
			formParams = append(formParams, p)
		case ir.LocMultipartField, ir.LocMultipartFile:
			multipartParams = append(multipartParams, p)
		case ir.LocBody:
			bodyParam = p
		case ir.LocModifiers:
			userModsParam = p
		}
	}

	// Query buffer serialization
	var (
		stackQueryParams  []*ir.ParamIR
		structQueryParams []*ir.ParamIR
	)

	for _, p := range queryParams {
		if (p.Location == ir.LocQueryStruct || p.Formatter == ir.FormatCompiledEncode) &&
			!isCompiledDTO(root, p.GoType.Name) {
			structQueryParams = append(structQueryParams, p)
		} else {
			stackQueryParams = append(stackQueryParams, p)
		}
	}

	if len(stackQueryParams) > 0 {
		emitQueryBuffer(buf, tracker, m, stackQueryParams, m.StackBufSize)
	}

	for _, p := range structQueryParams {
		tracker.Add("github.com/lemon4ksan/aoni/mod")
		fmt.Fprintf(buf, "\tallMods = append(allMods, mod.WithQuery(%s))\n\n", p.GoName)
	}

	// Form body serialization
	hasFieldInject := false
	for _, inj := range m.Injects {
		if inj.Target == ir.InjectField {
			hasFieldInject = true
			break
		}
	}

	if m.PayloadKind == ir.PayloadForm && (len(formParams) > 0 || hasFieldInject) {
		emitFormBuffer(buf, tracker, m, formParams, m.StackBufSize)
	}

	// Multipart body serialization
	if m.PayloadKind == ir.PayloadMultipart && len(multipartParams) > 0 {
		emitMultipartBuffer(buf, tracker, multipartParams)
	}

	// Path variables
	for _, p := range pathParams {
		tracker.Add("github.com/lemon4ksan/aoni/mod")
		fmt.Fprintf(buf, "\tallMods = append(allMods, mod.WithVar(%q, %s))\n", p.WireKey, p.GoName)
	}

	// Append user variadic modifiers if provided
	if userModsParam != nil {
		fmt.Fprintf(buf, "\tif len(%s) > 0 {\n", userModsParam.GoName)
		fmt.Fprintf(buf, "\t\tallMods = append(allMods, %s...)\n", userModsParam.GoName)
		buf.WriteString("\t}\n")
	}

	buf.WriteString("\n")

	// Custom Decoder / Encoder modifiers
	if m.Decoder != "" {
		switch strings.ToLower(m.Decoder) {
		case "json", "xml", "proto", "grpc-web":
			// Handled natively by aoni codecs
		default:
			tracker.Add("github.com/lemon4ksan/aoni/mod")
			fmt.Fprintf(buf, "\tallMods = append(allMods, mod.WithDecoder(%s))\n", m.Decoder)
		}
	}

	if m.Encoder != "" {
		tracker.Add("github.com/lemon4ksan/aoni/mod")
		fmt.Fprintf(buf, "\tallMods = append(allMods, mod.WithEncoder(%s))\n", m.Encoder)
	}

	if m.Idempotent {
		tracker.Add("github.com/lemon4ksan/aoni/mod")
		buf.WriteString("\tallMods = append(allMods, mod.WithIdempotencyKey())\n")
	}

	if m.Coalesce {
		tracker.Add("github.com/lemon4ksan/aoni/mod")
		buf.WriteString("\tallMods = append(allMods, mod.WithCoalesce())\n")
	}

	if m.ETag {
		tracker.Add("github.com/lemon4ksan/aoni/mod")
		buf.WriteString("\tallMods = append(allMods, mod.WithETag())\n")
	}

	if m.SignHMAC != nil {
		tracker.Add("github.com/lemon4ksan/aoni/mod")

		if m.SignHMAC.KeyEnv != "" {
			tracker.Add("os")
			fmt.Fprintf(buf, "\tallMods = append(allMods, mod.WithSignHMAC(os.Getenv(%q)))\n", m.SignHMAC.KeyEnv)
		} else {
			fmt.Fprintf(buf, "\tallMods = append(allMods, mod.WithSignHMAC(%q))\n", m.SignHMAC.SecretKey)
		}
	}

	// Target requester
	targetReq := m.TargetRequester
	if targetReq == "" {
		targetReq = "c.r"
	}

	rawPath := ""
	if m.Path != nil {
		rawPath = m.Path.RawTemplate
	}

	// Execute call
	emitExecution(buf, tracker, m, targetReq, rawPath, bodyParam)

	buf.WriteString("}\n\n")
}

func emitKeywordHeader(buf *bytes.Buffer, tracker *ImportTracker, svc *ir.ServiceIR, m *ir.MethodIR, h *ir.HeaderIR) {
	tracker.Add("github.com/lemon4ksan/aoni/mod")

	kw := strings.ToLower(strings.TrimPrefix(h.StaticValue, ":"))

	baseURL := ""
	if svc != nil {
		baseURL = strings.TrimRight(svc.BaseURL, "/")
	}

	switch kw {
	case "origin", "base":
		if baseURL != "" {
			fmt.Fprintf(buf, "\tallMods = append(allMods, mod.WithHeader(%q, %q))\n", h.Key, baseURL+"/")
		} else {
			buf.WriteString("\tif buGetter, ok := any(c.r).(interface{ BaseURL() string }); ok {\n")
			fmt.Fprintf(buf, "\t\tallMods = append(allMods, mod.WithHeader(%q, buGetter.BaseURL()))\n", h.Key)
			buf.WriteString("\t}\n")
		}

	case "self":
		if m.Path != nil {
			targetURL := m.Path.RawTemplate
			if baseURL != "" && !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
				targetURL = baseURL + "/" + targetURL
			}

			fmt.Fprintf(buf, "\tallMods = append(allMods, mod.WithHeader(%q, %q))\n", h.Key, targetURL)
		}

	case "page":
		if m.Path != nil {
			pagePath := cleanPagePath(m.Path.RawTemplate)
			if baseURL != "" && !strings.HasPrefix(pagePath, "http://") && !strings.HasPrefix(pagePath, "https://") {
				pagePath = baseURL + "/" + pagePath
			}

			fmt.Fprintf(buf, "\tallMods = append(allMods, mod.WithHeader(%q, %q))\n", h.Key, pagePath)
		}

	case "parent":
		if m.Path != nil {
			parentPath := cleanParentPath(m.Path.RawTemplate)
			if baseURL != "" && !strings.HasPrefix(parentPath, "http://") &&
				!strings.HasPrefix(parentPath, "https://") {
				parentPath = baseURL + "/" + parentPath
			}

			fmt.Fprintf(buf, "\tallMods = append(allMods, mod.WithHeader(%q, %q))\n", h.Key, parentPath)
		}
	}
}

func cleanPagePath(raw string) string {
	raw = strings.TrimSuffix(raw, "/render")
	raw = strings.TrimSuffix(raw, "/json")

	if idx := strings.Index(raw, "?"); idx != -1 {
		raw = raw[:idx]
	}

	return raw
}

func cleanParentPath(raw string) string {
	if idx := strings.LastIndex(raw, "/"); idx != -1 {
		return raw[:idx]
	}

	return raw
}

func emitDynamicHeader(buf *bytes.Buffer, tracker *ImportTracker, svc *ir.ServiceIR, m *ir.MethodIR, h *ir.HeaderIR) {
	buf.WriteString("\tvar refBuf [128]byte\n")
	buf.WriteString("\tref := refBuf[:0]\n")

	baseURL := ""
	if svc != nil {
		baseURL = strings.TrimRight(svc.BaseURL, "/")
	}

	for i, seg := range h.DynamicTemplate.Segments {
		if !seg.IsVariable {
			lit := seg.Literal
			if i == 0 && baseURL != "" && !strings.HasPrefix(lit, "http://") && !strings.HasPrefix(lit, "https://") {
				lit = baseURL + "/" + lit
			}

			fmt.Fprintf(buf, "\tref = append(ref, %q...)\n", lit)

			continue
		}

		// Find matching parameter
		var matchedParam *ir.ParamIR
		for _, p := range m.Params {
			if p.GoName == seg.VarName || strings.EqualFold(p.GoName, seg.VarName) {
				matchedParam = p
				break
			}
		}

		if matchedParam != nil {
			typeName := matchedParam.GoType.Name
			switch typeName {
			case "int", "int8", "int16", "int32", "int64":
				tracker.Add("strconv")
				fmt.Fprintf(buf, "\tref = strconv.AppendInt(ref, int64(%s), 10)\n", matchedParam.GoName)
			case "uint", "uint8", "uint16", "uint32", "uint64", "uintptr":
				tracker.Add("strconv")
				fmt.Fprintf(buf, "\tref = strconv.AppendUint(ref, uint64(%s), 10)\n", matchedParam.GoName)
			case "bool":
				tracker.Add("strconv")
				fmt.Fprintf(buf, "\tref = strconv.AppendBool(ref, %s)\n", matchedParam.GoName)
			case "string":
				switch seg.Transform {
				case ir.TransformQueryEscape:
					tracker.Add("net/url")
					fmt.Fprintf(buf, "\tref = append(ref, url.QueryEscape(%s)...)\n", matchedParam.GoName)
				case ir.TransformLower:
					tracker.Add("strings")
					fmt.Fprintf(buf, "\tref = append(ref, strings.ToLower(%s)...)\n", matchedParam.GoName)
				case ir.TransformUpper:
					tracker.Add("strings")
					fmt.Fprintf(buf, "\tref = append(ref, strings.ToUpper(%s)...)\n", matchedParam.GoName)
				default:
					tracker.Add("net/url")
					fmt.Fprintf(buf, "\tref = append(ref, url.PathEscape(%s)...)\n", matchedParam.GoName)
				}

			default:
				switch {
				case strings.HasSuffix(typeName, "ID") || strings.HasSuffix(typeName, "Code") || strings.Contains(typeName, "uint"):
					tracker.Add("strconv")
					fmt.Fprintf(buf, "\tref = strconv.AppendUint(ref, uint64(%s), 10)\n", matchedParam.GoName)
				case strings.Contains(typeName, "int"):
					tracker.Add("strconv")
					fmt.Fprintf(buf, "\tref = strconv.AppendInt(ref, int64(%s), 10)\n", matchedParam.GoName)
				default:
					tracker.Add("net/url")
					tracker.Add("fmt")
					fmt.Fprintf(buf, "\tref = append(ref, url.PathEscape(fmt.Sprint(%s))...)\n", matchedParam.GoName)
				}
			}
		} else {
			switch seg.Transform {
			case ir.TransformQueryEscape:
				tracker.Add("net/url")
				tracker.Add("fmt")
				fmt.Fprintf(buf, "\tref = append(ref, url.QueryEscape(fmt.Sprint(%s))...)\n", seg.VarName)
			case ir.TransformLower:
				tracker.Add("strings")
				tracker.Add("fmt")
				fmt.Fprintf(buf, "\tref = append(ref, strings.ToLower(fmt.Sprint(%s))...)\n", seg.VarName)
			case ir.TransformUpper:
				tracker.Add("strings")
				tracker.Add("fmt")
				fmt.Fprintf(buf, "\tref = append(ref, strings.ToUpper(fmt.Sprint(%s))...)\n", seg.VarName)
			default:
				tracker.Add("net/url")
				tracker.Add("fmt")
				fmt.Fprintf(buf, "\tref = append(ref, url.PathEscape(fmt.Sprint(%s))...)\n", seg.VarName)
			}
		}
	}

	fmt.Fprintf(buf, "\tallMods = append(allMods, mod.WithHeader(%q, string(ref)))\n\n", h.Key)
}
