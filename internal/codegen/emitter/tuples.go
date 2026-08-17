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

func emitTuple(buf *bytes.Buffer, tracker *ImportTracker, t *ir.TupleIR) {
	tracker.Add("encoding/json")

	fmt.Fprintf(
		buf,
		"// UnmarshalJSON decodes a heterogeneous JSON array tuple or standard JSON object into %s.\n",
		t.Name,
	)
	fmt.Fprintf(buf, "func (t *%s) UnmarshalJSON(data []byte) error {\n", t.Name)
	buf.WriteString("\tif len(data) > 0 && data[0] == '{' {\n")
	fmt.Fprintf(buf, "\t\ttype alias %s\n", t.Name)
	buf.WriteString("\t\treturn json.Unmarshal(data, (*alias)(t))\n")
	buf.WriteString("\t}\n\n")
	buf.WriteString("\tvar raw []json.RawMessage\n")
	buf.WriteString("\tif err := json.Unmarshal(data, &raw); err != nil {\n")

	if len(t.Fields) > 0 {
		fmt.Fprintf(buf, "\t\treturn json.Unmarshal(data, &t.%s)\n", t.Fields[0].GoName)
	} else {
		buf.WriteString("\t\treturn err\n")
	}

	buf.WriteString("\t}\n\n")

	for _, f := range t.Fields {
		if f.IsNested && len(f.IndexPath) > 1 {
			emitNestedTupleFieldUnmarshal(buf, f)
		} else {
			idx := f.Index
			fmt.Fprintf(buf, "\tif len(raw) > %d && len(raw[%d]) > 0 && string(raw[%d]) != \"null\" {\n", idx, idx, idx)
			fmt.Fprintf(buf, "\t\t_ = json.Unmarshal(raw[%d], &t.%s)\n", idx, f.GoName)
			buf.WriteString("\t}\n")
		}
	}

	buf.WriteString("\treturn nil\n")
	buf.WriteString("}\n\n")
}

func emitNestedTupleFieldUnmarshal(buf *bytes.Buffer, f ir.TupleFieldIR) {
	path := f.IndexPath
	indent := "\t"

	fmt.Fprintf(
		buf,
		"%sif len(raw) > %d && len(raw[%d]) > 0 && string(raw[%d]) != \"null\" {\n",
		indent,
		path[0],
		path[0],
		path[0],
	)

	curVar := fmt.Sprintf("raw[%d]", path[0])
	for i := 1; i < len(path); i++ {
		indent += "\t"
		subVar := fmt.Sprintf("sub%d_%s", i, f.GoName)
		fmt.Fprintf(buf, "%svar %s []json.RawMessage\n", indent, subVar)
		fmt.Fprintf(
			buf,
			"%sif err := json.Unmarshal(%s, &%s); err == nil && len(%s) > %d && len(%s[%d]) > 0 && string(%s[%d]) != \"null\" {\n",
			indent,
			curVar,
			subVar,
			subVar,
			path[i],
			subVar,
			path[i],
			subVar,
			path[i],
		)
		curVar = fmt.Sprintf("%s[%d]", subVar, path[i])
	}

	indent += "\t"
	fmt.Fprintf(buf, "%s_ = json.Unmarshal(%s, &t.%s)\n", indent, curVar, f.GoName)

	for i := len(path) - 1; i >= 0; i-- {
		indent = strings.Repeat("\t", i+1)
		fmt.Fprintf(buf, "%s}\n", indent)
	}
}
