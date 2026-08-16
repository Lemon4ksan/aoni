// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package emitter

import (
	"bytes"
	"fmt"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

func emitTuple(buf *bytes.Buffer, tracker *ImportTracker, t *ir.TupleIR) {
	tracker.Add("encoding/json")

	n := len(t.Fields)
	fmt.Fprintf(buf, "// UnmarshalJSON decodes a heterogeneous JSON array tuple into %s.\n", t.Name)
	fmt.Fprintf(buf, "func (t *%s) UnmarshalJSON(data []byte) error {\n", t.Name)
	fmt.Fprintf(buf, "\tvar raw [%d]json.RawMessage\n", n)
	buf.WriteString("\tif err := json.Unmarshal(data, &raw); err != nil {\n\t\treturn err\n\t}\n")

	for i, f := range t.Fields {
		fmt.Fprintf(buf, "\t_ = json.Unmarshal(raw[%d], &t.%s)\n", i, f.GoName)
	}

	buf.WriteString("\treturn nil\n")
	buf.WriteString("}\n\n")
}
