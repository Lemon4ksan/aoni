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

func emitQueryBuffer(buf *bytes.Buffer, tracker *ImportTracker, m *ir.MethodIR, params []*ir.ParamIR, bufSize int) {
	if bufSize <= 0 {
		bufSize = 256
	}

	tracker.Add("net/url")
	fmt.Fprintf(buf, "\tvar qBuf [%d]byte\n", bufSize)
	buf.WriteString("\tqBytes := qBuf[:0]\n")

	for i, p := range params {
		if p.Location == ir.LocQueryStruct || p.Formatter == ir.FormatCompiledEncode {
			fmt.Fprintf(buf, "\tqBytes = %s.AppendQuery(qBytes)\n", p.GoName)
			continue
		}

		prefix := p.WireKey + "="
		if i > 0 {
			prefix = fmt.Sprintf("&%s=", p.WireKey)
		}

		fmt.Fprintf(buf, "\tqBytes = append(qBytes, %q...)\n", prefix)

		elemType := p.GoType.ElemType
		if elemType == "" {
			elemType = strings.TrimPrefix(p.GoType.Name, "[]")
		}

		if p.Pipeline != nil {
			tracker.Add("encoding/json")
			fmt.Fprintf(buf, "\t%sBytes, err := json.Marshal(%s)\n", p.GoName, p.GoName)

			errRet := "\tif err != nil {\n\t\treturn err\n\t}\n"
			if m != nil && m.Return != nil && !m.Return.IsVoid {
				errRet = "\tif err != nil {\n\t\treturn nil, err\n\t}\n"
			}

			buf.WriteString(errRet)
			fmt.Fprintf(buf, "\tqBytes = append(qBytes, url.QueryEscape(string(%sBytes))...)\n", p.GoName)

			continue
		}

		switch p.Formatter {
		case ir.FormatIntAppend:
			tracker.Add("strconv")
			fmt.Fprintf(buf, "\tqBytes = strconv.AppendInt(qBytes, int64(%s), 10)\n", p.GoName)
		case ir.FormatUintAppend:
			tracker.Add("strconv")
			fmt.Fprintf(buf, "\tqBytes = strconv.AppendUint(qBytes, uint64(%s), 10)\n", p.GoName)
		case ir.FormatBoolAppend:
			tracker.Add("strconv")
			fmt.Fprintf(buf, "\tqBytes = strconv.AppendBool(qBytes, %s)\n", p.GoName)
		case ir.FormatBoolInt:
			fmt.Fprintf(
				buf,
				"\tif %s {\n\t\tqBytes = append(qBytes, '1')\n\t} else {\n\t\tqBytes = append(qBytes, '0')\n\t}\n",
				p.GoName,
			)

		case ir.FormatTimeRFC3339:
			tracker.Add("time")
			fmt.Fprintf(buf, "\tqBytes = append(qBytes, url.QueryEscape(%s.Format(time.RFC3339))...)\n", p.GoName)
		case ir.FormatTimeUnixS:
			tracker.Add("strconv")
			fmt.Fprintf(buf, "\tqBytes = strconv.AppendInt(qBytes, %s.Unix(), 10)\n", p.GoName)
		case ir.FormatTimeUnixMS:
			tracker.Add("strconv")
			fmt.Fprintf(buf, "\tqBytes = strconv.AppendInt(qBytes, %s.UnixMilli(), 10)\n", p.GoName)
		case ir.FormatTimeLayout:
			layout := p.TimeLayout
			if layout == "" {
				layout = "2006-01-02"
			}

			fmt.Fprintf(buf, "\tqBytes = append(qBytes, url.QueryEscape(%s.Format(%q))...)\n", p.GoName, layout)

		case ir.FormatSliceComma:
			fmt.Fprintf(buf, "\tfor idx, v := range %s {\n", p.GoName)
			buf.WriteString("\t\tif idx > 0 {\n\t\t\tqBytes = append(qBytes, ',')\n\t\t}\n")
			emitSliceElementFormat(buf, tracker, elemType, "qBytes", "v")
			buf.WriteString("\t}\n")
		case ir.FormatSliceSpace:
			fmt.Fprintf(buf, "\tfor idx, v := range %s {\n", p.GoName)
			buf.WriteString("\t\tif idx > 0 {\n\t\t\tqBytes = append(qBytes, '+')\n\t\t}\n")
			emitSliceElementFormat(buf, tracker, elemType, "qBytes", "v")
			buf.WriteString("\t}\n")
		case ir.FormatSlicePipe:
			fmt.Fprintf(buf, "\tfor idx, v := range %s {\n", p.GoName)
			buf.WriteString("\t\tif idx > 0 {\n\t\t\tqBytes = append(qBytes, '|')\n\t\t}\n")
			emitSliceElementFormat(buf, tracker, elemType, "qBytes", "v")
			buf.WriteString("\t}\n")
		case ir.FormatBufferAppender:
			fmt.Fprintf(buf, "\tqBytes = %s.AppendBytes(qBytes)\n", p.GoName)
		case ir.FormatCustomStringer:
			fmt.Fprintf(buf, "\tqBytes = append(qBytes, url.QueryEscape(%s.String())...)\n", p.GoName)
		case ir.FormatQueryEscaped, ir.FormatDirectString:
			fallthrough
		default:
			if p.GoType.Name == "string" {
				fmt.Fprintf(buf, "\tqBytes = append(qBytes, url.QueryEscape(%s)...)\n", p.GoName)
			} else {
				tracker.Add("fmt")
				fmt.Fprintf(buf, "\tqBytes = append(qBytes, url.QueryEscape(fmt.Sprint(%s))...)\n", p.GoName)
			}
		}
	}

	buf.WriteString("\tallMods = append(allMods, mod.WithQuery(string(qBytes)))\n\n")
}

func emitFormBuffer(buf *bytes.Buffer, tracker *ImportTracker, m *ir.MethodIR, params []*ir.ParamIR, bufSize int) {
	if bufSize <= 0 {
		bufSize = 256
	}

	tracker.Add("net/url")

	for _, inj := range m.Injects {
		if inj.Target == ir.InjectField {
			tracker.Add("github.com/lemon4ksan/aoni")
			break
		}
	}

	buf.WriteString(
		"\tallMods = append(allMods, mod.WithHeader(\"Content-Type\", \"application/x-www-form-urlencoded\"))\n",
	)

	// If single struct argument with compiled encoder without explicit field pipeline
	if len(params) == 1 && params[0].Formatter == ir.FormatCompiledEncode && params[0].Pipeline == nil {
		p := params[0]

		fmt.Fprintf(buf, "\tvar formBuf [%d]byte\n", bufSize)
		fmt.Fprintf(buf, "\tformBytes := %s.AppendFormData(formBuf[:0])\n", p.GoName)

		emitInjects(buf, m)
		buf.WriteString("\tallMods = append(allMods, mod.WithBodyBytes(formBytes))\n\n")

		return
	}

	fmt.Fprintf(buf, "\tvar formBuf [%d]byte\n", bufSize)
	buf.WriteString("\tformBytes := formBuf[:0]\n")

	for i, p := range params {
		if p.Formatter == ir.FormatCompiledEncode && p.Pipeline == nil {
			fmt.Fprintf(buf, "\tformBytes = %s.AppendFormData(formBytes)\n", p.GoName)
			continue
		}

		prefix := p.WireKey + "="
		if i > 0 {
			prefix = fmt.Sprintf("&%s=", p.WireKey)
		}

		fmt.Fprintf(buf, "\tformBytes = append(formBytes, %q...)\n", prefix)

		if p.Pipeline != nil {
			tracker.Add("encoding/json")
			fmt.Fprintf(buf, "\t%sBytes, err := json.Marshal(%s)\n", p.GoName, p.GoName)

			errRet := "\tif err != nil {\n\t\treturn err\n\t}\n"
			if m != nil && m.Return != nil && !m.Return.IsVoid {
				errRet = "\tif err != nil {\n\t\treturn nil, err\n\t}\n"
			}

			buf.WriteString(errRet)
			fmt.Fprintf(buf, "\tformBytes = append(formBytes, url.QueryEscape(string(%sBytes))...)\n", p.GoName)

			continue
		}

		switch p.Formatter {
		case ir.FormatJSONString:
			tracker.Add("encoding/json")
			fmt.Fprintf(buf, "\t%sJSON, err := json.Marshal(%s)\n", p.GoName, p.GoName)

			errRet := "\tif err != nil {\n\t\treturn err\n\t}\n"
			if m != nil && m.Return != nil && !m.Return.IsVoid {
				errRet = "\tif err != nil {\n\t\treturn nil, err\n\t}\n"
			}

			buf.WriteString(errRet)
			fmt.Fprintf(buf, "\tformBytes = append(formBytes, url.QueryEscape(string(%sJSON))...)\n", p.GoName)

		case ir.FormatIntAppend:
			tracker.Add("strconv")
			fmt.Fprintf(buf, "\tformBytes = strconv.AppendInt(formBytes, int64(%s), 10)\n", p.GoName)
		case ir.FormatUintAppend:
			tracker.Add("strconv")
			fmt.Fprintf(buf, "\tformBytes = strconv.AppendUint(formBytes, uint64(%s), 10)\n", p.GoName)
		case ir.FormatBoolAppend:
			tracker.Add("strconv")
			fmt.Fprintf(buf, "\tformBytes = strconv.AppendBool(formBytes, %s)\n", p.GoName)
		default:
			if p.GoType.Name == "string" {
				fmt.Fprintf(buf, "\tformBytes = append(formBytes, url.QueryEscape(%s)...)\n", p.GoName)
			} else {
				tracker.Add("fmt")
				fmt.Fprintf(buf, "\tformBytes = append(formBytes, url.QueryEscape(fmt.Sprint(%s))...)\n", p.GoName)
			}
		}
	}

	emitInjects(buf, m)
	buf.WriteString("\tallMods = append(allMods, mod.WithBodyBytes(formBytes))\n\n")
}

func emitInjects(buf *bytes.Buffer, m *ir.MethodIR) {
	if m == nil {
		return
	}

	for _, inj := range m.Injects {
		if inj.Target != ir.InjectField {
			continue
		}

		fnName := inj.ProviderFn
		if fnName == "" {
			fnName = "SessionID"
		}

		fmt.Fprintf(buf, "\tif getter, ok := aoni.UnwrapAs[interface{ %s(string) string }](c.r); ok {\n", fnName)
		fmt.Fprintf(buf, "\t\tif val := getter.%s(\"\"); val != \"\" {\n", fnName)
		buf.WriteString("\t\t\tif len(formBytes) > 0 {\n")
		fmt.Fprintf(buf, "\t\t\t\tformBytes = append(formBytes, \"&%s=\"...)\n", inj.WireKey)
		buf.WriteString("\t\t\t} else {\n")
		fmt.Fprintf(buf, "\t\t\t\tformBytes = append(formBytes, \"%s=\"...)\n", inj.WireKey)
		buf.WriteString("\t\t\t}\n")
		buf.WriteString("\t\t\tformBytes = append(formBytes, url.QueryEscape(val)...)\n")
		buf.WriteString("\t\t}\n")
		fmt.Fprintf(buf, "\t} else if getter, ok := aoni.UnwrapAs[interface{ %s() string }](c.r); ok {\n", fnName)
		fmt.Fprintf(buf, "\t\tif val := getter.%s(); val != \"\" {\n", fnName)
		buf.WriteString("\t\t\tif len(formBytes) > 0 {\n")
		fmt.Fprintf(buf, "\t\t\t\tformBytes = append(formBytes, \"&%s=\"...)\n", inj.WireKey)
		buf.WriteString("\t\t\t} else {\n")
		fmt.Fprintf(buf, "\t\t\t\tformBytes = append(formBytes, \"%s=\"...)\n", inj.WireKey)
		buf.WriteString("\t\t\t}\n")
		buf.WriteString("\t\t\tformBytes = append(formBytes, url.QueryEscape(val)...)\n")
		buf.WriteString("\t\t}\n")
		buf.WriteString("\t}\n")
	}
}

func emitMultipartBuffer(buf *bytes.Buffer, tracker *ImportTracker, params []*ir.ParamIR) {
	tracker.Add("bytes")
	tracker.Add("mime/multipart")
	tracker.Add("net/textproto")

	buf.WriteString("\tvar bodyBuf bytes.Buffer\n")
	buf.WriteString("\tmw := multipart.NewWriter(&bodyBuf)\n")

	for _, p := range params {
		switch p.Location {
		case ir.LocMultipartField:
			tracker.Add("fmt")
			fmt.Fprintf(buf, "\t_ = mw.WriteField(%q, fmt.Sprint(%s))\n", p.WireKey, p.GoName)
		case ir.LocMultipartFile:
			tracker.Add("fmt")

			fn := p.FileName
			switch {
			case fn == "" || fn == "{filename}":
				fn = "filename"
			case strings.HasPrefix(fn, "{") && strings.HasSuffix(fn, "}"):
				fn = strings.Trim(fn, "{}")
			case !strings.HasPrefix(fn, `"`):
				fn = fmt.Sprintf("%q", fn)
			}

			ct := p.ContentType
			switch {
			case ct == "" || ct == "{content_type}":
				ct = "contentType"
			case strings.HasPrefix(ct, "{") && strings.HasSuffix(ct, "}"):
				ct = strings.Trim(ct, "{}")
			case !strings.HasPrefix(ct, `"`):
				ct = fmt.Sprintf("%q", ct)
			}

			fmt.Fprintf(buf, "\thdr := make(textproto.MIMEHeader)\n")
			fmt.Fprintf(
				buf,
				"\thdr.Set(\"Content-Disposition\", fmt.Sprintf(`form-data; name=%%q; filename=%%q`, %q, %s))\n",
				p.WireKey,
				fn,
			)
			fmt.Fprintf(buf, "\thdr.Set(\"Content-Type\", %s)\n", ct)
			fmt.Fprintf(buf, "\tpw, err := mw.CreatePart(hdr)\n")
			fmt.Fprintf(buf, "\tif err == nil {\n\t\t_, _ = pw.Write(%s)\n\t}\n", p.GoName)
		}
	}

	buf.WriteString("\t_ = mw.Close()\n")
	buf.WriteString(
		"\tallMods = append(allMods, mod.WithHeader(\"Content-Type\", mw.FormDataContentType()), mod.WithBodyBytes(bodyBuf.Bytes()))\n\n",
	)
}

func emitSliceElementFormat(buf *bytes.Buffer, tracker *ImportTracker, elemType, targetBuf, valVar string) {
	elemType = strings.TrimPrefix(elemType, "*")
	switch elemType {
	case "string":
		tracker.Add("net/url")
		fmt.Fprintf(buf, "\t\t%s = append(%s, url.QueryEscape(%s)...)\n", targetBuf, targetBuf, valVar)
	case "int", "int8", "int16", "int32", "int64":
		tracker.Add("strconv")
		fmt.Fprintf(buf, "\t\t%s = strconv.AppendInt(%s, int64(%s), 10)\n", targetBuf, targetBuf, valVar)
	case "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte":
		tracker.Add("strconv")
		fmt.Fprintf(buf, "\t\t%s = strconv.AppendUint(%s, uint64(%s), 10)\n", targetBuf, targetBuf, valVar)
	case "bool":
		tracker.Add("strconv")
		fmt.Fprintf(buf, "\t\t%s = strconv.AppendBool(%s, %s)\n", targetBuf, targetBuf, valVar)
	default:
		tracker.Add("net/url")
		tracker.Add("fmt")
		fmt.Fprintf(buf, "\t\t%s = append(%s, url.QueryEscape(fmt.Sprint(%s))...)\n", targetBuf, targetBuf, valVar)
	}
}
