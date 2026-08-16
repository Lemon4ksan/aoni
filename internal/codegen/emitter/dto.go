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

func emitStructDTO(buf *bytes.Buffer, tracker *ImportTracker, s *ir.StructIR) {
	if !s.GenValueEncoder {
		return
	}

	tracker.Add("net/url")
	tracker.Add("strconv")

	fmt.Fprintf(
		buf,
		"// AppendFormData serializes %s into url-encoded form bytes on dst buffer (0 B/op).\n",
		s.Name,
	)
	fmt.Fprintf(buf, "func (r *%s) AppendFormData(dst []byte) []byte {\n", s.Name)
	buf.WriteString("\tif r == nil {\n\t\treturn dst\n\t}\n\n")

	for _, f := range s.Fields {
		emitFieldFormData(buf, tracker, f)
	}

	buf.WriteString("\n\treturn dst\n}\n\n")

	fmt.Fprintf(buf, "// AppendQuery serializes %s into query string bytes on dst buffer (0 B/op).\n", s.Name)
	fmt.Fprintf(buf, "func (r *%s) AppendQuery(dst []byte) []byte {\n", s.Name)
	buf.WriteString("\treturn r.AppendFormData(dst)\n}\n\n")

	// Also emit EncodeValues for url.Values interoperability
	fmt.Fprintf(buf, "// EncodeValues serializes %s into url.Values without reflection.\n", s.Name)
	fmt.Fprintf(buf, "func (r *%s) EncodeValues(vals url.Values) {\n", s.Name)
	buf.WriteString("\tif r == nil {\n\t\treturn\n\t}\n")

	for _, f := range s.Fields {
		emitFieldEncodeValues(buf, tracker, f)
	}

	buf.WriteString("}\n\n")
}

func emitFieldFormData(buf *bytes.Buffer, tracker *ImportTracker, f *ir.FieldIR) {
	switch f.Type.Name {
	case "string":
		fmt.Fprintf(buf, "\tif r.%s != \"\" {\n", f.GoName)
		buf.WriteString("\t\tif len(dst) > 0 { dst = append(dst, '&') }\n")
		fmt.Fprintf(buf, "\t\tdst = append(dst, %q...)\n", f.WireName+"=")
		fmt.Fprintf(buf, "\t\tdst = append(dst, url.QueryEscape(r.%s)...)\n", f.GoName)
		buf.WriteString("\t}\n")

	case "int", "int64":
		fmt.Fprintf(buf, "\tif r.%s != 0 {\n", f.GoName)
		buf.WriteString("\t\tif len(dst) > 0 { dst = append(dst, '&') }\n")
		fmt.Fprintf(buf, "\t\tdst = append(dst, %q...)\n", f.WireName+"=")
		fmt.Fprintf(buf, "\t\tdst = strconv.AppendInt(dst, int64(r.%s), 10)\n", f.GoName)
		buf.WriteString("\t}\n")

	case "uint", "uint32", "uint64":
		fmt.Fprintf(buf, "\tif r.%s != 0 {\n", f.GoName)
		buf.WriteString("\t\tif len(dst) > 0 { dst = append(dst, '&') }\n")
		fmt.Fprintf(buf, "\t\tdst = append(dst, %q...)\n", f.WireName+"=")
		fmt.Fprintf(buf, "\t\tdst = strconv.AppendUint(dst, uint64(r.%s), 10)\n", f.GoName)
		buf.WriteString("\t}\n")

	case "time.Time":
		tracker.Add("time")
		fmt.Fprintf(buf, "\tif !r.%s.IsZero() {\n", f.GoName)
		buf.WriteString("\t\tif len(dst) > 0 { dst = append(dst, '&') }\n")
		fmt.Fprintf(buf, "\t\tdst = append(dst, %q...)\n", f.WireName+"=")
		fmt.Fprintf(
			buf,
			"\t\tdst = append(dst, url.QueryEscape(r.%s.Format(time.RFC3339))...)\n",
			f.GoName,
		)
		buf.WriteString("\t}\n")

	case "bool":
		fmt.Fprintf(buf, "\tif r.%s {\n", f.GoName)
		buf.WriteString("\t\tif len(dst) > 0 { dst = append(dst, '&') }\n")

		switch f.Formatter {
		case ir.FormatBoolInt:
			fmt.Fprintf(buf, "\t\tdst = append(dst, %q...)\n", f.WireName+"=1")
		case ir.FormatBoolFlag:
			fmt.Fprintf(buf, "\t\tdst = append(dst, %q...)\n", f.WireName)
		default:
			fmt.Fprintf(buf, "\t\tdst = append(dst, %q...)\n", f.WireName+"=true")
		}

		buf.WriteString("\t}\n")

	case "[]int", "[]int64":
		fmt.Fprintf(buf, "\tfor _, v := range r.%s {\n", f.GoName)
		buf.WriteString("\t\tif len(dst) > 0 { dst = append(dst, '&') }\n")
		fmt.Fprintf(buf, "\t\tdst = append(dst, %q...)\n", f.WireName+"=")
		fmt.Fprintf(buf, "\t\tdst = strconv.AppendInt(dst, int64(v), 10)\n")
		buf.WriteString("\t}\n")

	case "[]string":
		fmt.Fprintf(buf, "\tfor _, v := range r.%s {\n", f.GoName)
		buf.WriteString("\t\tif len(dst) > 0 { dst = append(dst, '&') }\n")
		fmt.Fprintf(buf, "\t\tdst = append(dst, %q...)\n", f.WireName+"=")
		fmt.Fprintf(buf, "\t\tdst = append(dst, url.QueryEscape(v)...)\n")
		buf.WriteString("\t}\n")

	case "any", "interface{}":
		tracker.Add("fmt")
		fmt.Fprintf(buf, "\tif r.%s != nil {\n", f.GoName)
		buf.WriteString("\t\tif len(dst) > 0 { dst = append(dst, '&') }\n")
		fmt.Fprintf(buf, "\t\tdst = append(dst, %q...)\n", f.WireName+"=")
		fmt.Fprintf(buf, "\t\tdst = append(dst, fmt.Sprint(r.%s)...)\n", f.GoName)
		buf.WriteString("\t}\n")

	case "float32", "float64":
		fmt.Fprintf(buf, "\tif r.%s != 0 {\n", f.GoName)
		buf.WriteString("\t\tif len(dst) > 0 { dst = append(dst, '&') }\n")
		fmt.Fprintf(buf, "\t\tdst = append(dst, %q...)\n", f.WireName+"=")
		fmt.Fprintf(buf, "\t\tdst = strconv.AppendFloat(dst, float64(r.%s), 'f', -1, 64)\n", f.GoName)
		buf.WriteString("\t}\n")

	case "values.Int64String", "values.Uint64String", "values.Float64String", "values.BoolInt":
		fmt.Fprintf(buf, "\tif r.%s != 0 {\n", f.GoName)
		buf.WriteString("\t\tif len(dst) > 0 { dst = append(dst, '&') }\n")
		fmt.Fprintf(buf, "\t\tdst = append(dst, %q...)\n", f.WireName+"=")
		fmt.Fprintf(buf, "\t\tdst = strconv.AppendInt(dst, int64(r.%s), 10)\n", f.GoName)
		buf.WriteString("\t}\n")

	default:
		if strings.HasPrefix(f.Type.Name, "[]") || strings.HasPrefix(f.Type.Name, "map[") {
			return
		}

		if f.Type.IsPointer || strings.HasPrefix(f.Type.Name, "*") {
			tracker.Add("fmt")
			fmt.Fprintf(buf, "\tif r.%s != nil {\n", f.GoName)
			buf.WriteString("\t\tif len(dst) > 0 { dst = append(dst, '&') }\n")
			fmt.Fprintf(buf, "\t\tdst = append(dst, %q...)\n", f.WireName+"=")
			fmt.Fprintf(buf, "\t\tdst = append(dst, fmt.Sprint(r.%s)...)\n", f.GoName)
			buf.WriteString("\t}\n")
		} else {
			tracker.Add("fmt")
			fmt.Fprintf(buf, "\tif strVal := fmt.Sprint(r.%s); strVal != \"\" && strVal != \"0\" {\n", f.GoName)
			buf.WriteString("\t\tif len(dst) > 0 { dst = append(dst, '&') }\n")
			fmt.Fprintf(buf, "\t\tdst = append(dst, %q...)\n", f.WireName+"=")
			fmt.Fprintf(buf, "\t\tdst = append(dst, url.QueryEscape(strVal)...)\n")
			buf.WriteString("\t}\n")
		}
	}
}

func emitFieldEncodeValues(buf *bytes.Buffer, tracker *ImportTracker, f *ir.FieldIR) {
	switch f.Type.Name {
	case "string":
		fmt.Fprintf(buf, "\tif r.%s != \"\" {\n", f.GoName)
		fmt.Fprintf(buf, "\t\tvals.Set(%q, r.%s)\n", f.WireName, f.GoName)
		buf.WriteString("\t}\n")
	case "int", "int64":
		fmt.Fprintf(buf, "\tif r.%s != 0 {\n", f.GoName)
		fmt.Fprintf(buf, "\t\tvals.Set(%q, strconv.FormatInt(int64(r.%s), 10))\n", f.WireName, f.GoName)
		buf.WriteString("\t}\n")
	case "uint", "uint32", "uint64":
		fmt.Fprintf(buf, "\tif r.%s != 0 {\n", f.GoName)
		fmt.Fprintf(buf, "\t\tvals.Set(%q, strconv.FormatUint(uint64(r.%s), 10))\n", f.WireName, f.GoName)
		buf.WriteString("\t}\n")
	case "float32", "float64":
		fmt.Fprintf(buf, "\tif r.%s != 0 {\n", f.GoName)
		fmt.Fprintf(
			buf,
			"\t\tvals.Set(%q, strconv.FormatFloat(float64(r.%s), 'f', -1, 64))\n",
			f.WireName,
			f.GoName,
		)
		buf.WriteString("\t}\n")

	case "bool":
		fmt.Fprintf(buf, "\tif r.%s {\n", f.GoName)
		fmt.Fprintf(buf, "\t\tvals.Set(%q, \"true\")\n", f.WireName)
		buf.WriteString("\t}\n")
	case "[]int", "[]int64":
		fmt.Fprintf(buf, "\tfor _, v := range r.%s {\n", f.GoName)
		fmt.Fprintf(buf, "\t\tvals.Add(%q, strconv.FormatInt(int64(v), 10))\n", f.WireName)
		buf.WriteString("\t}\n")
	case "[]string":
		fmt.Fprintf(buf, "\tfor _, v := range r.%s {\n", f.GoName)
		fmt.Fprintf(buf, "\t\tvals.Add(%q, v)\n", f.WireName)
		buf.WriteString("\t}\n")
	case "any", "interface{}":
		tracker.Add("fmt")
		fmt.Fprintf(buf, "\tif r.%s != nil {\n", f.GoName)
		fmt.Fprintf(buf, "\t\tvals.Set(%q, fmt.Sprint(r.%s))\n", f.WireName, f.GoName)
		buf.WriteString("\t}\n")
	case "values.Int64String", "values.Uint64String", "values.Float64String", "values.BoolInt":
		fmt.Fprintf(buf, "\tif r.%s != 0 {\n", f.GoName)
		fmt.Fprintf(buf, "\t\tvals.Set(%q, strconv.FormatInt(int64(r.%s), 10))\n", f.WireName, f.GoName)
		buf.WriteString("\t}\n")
	default:
		if strings.HasPrefix(f.Type.Name, "[]") || strings.HasPrefix(f.Type.Name, "map[") {
			return
		}

		if f.Type.IsPointer || strings.HasPrefix(f.Type.Name, "*") {
			tracker.Add("fmt")
			fmt.Fprintf(buf, "\tif r.%s != nil {\n", f.GoName)
			fmt.Fprintf(buf, "\t\tvals.Set(%q, fmt.Sprint(r.%s))\n", f.WireName, f.GoName)
			buf.WriteString("\t}\n")
		} else {
			tracker.Add("fmt")
			fmt.Fprintf(buf, "\tif strVal := fmt.Sprint(r.%s); strVal != \"\" && strVal != \"0\" {\n", f.GoName)
			fmt.Fprintf(buf, "\t\tvals.Set(%q, strVal)\n", f.WireName)
			buf.WriteString("\t}\n")
		}
	}
}
