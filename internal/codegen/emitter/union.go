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

// emitUnion generates discriminator helpers and Match callbacks for @aoni:union structs.
func emitUnion(buf *bytes.Buffer, u *ir.UnionIR) {
	if u == nil || len(u.Fields) == 0 {
		return
	}

	unionName := u.Name

	// 1. IsSuccess helper
	fmt.Fprintf(buf, "// IsSuccess reports whether %s holds a 2xx success status code.\n", unionName)
	fmt.Fprintf(buf, "func (u *%s) IsSuccess() bool {\n", unionName)
	buf.WriteString("\tif u == nil {\n\t\treturn false\n\t}\n")
	buf.WriteString("\treturn u.StatusCode >= 200 && u.StatusCode <= 299\n")
	buf.WriteString("}\n\n")

	// 2. Status helper
	fmt.Fprintf(buf, "// Status returns the HTTP status code of the wire response.\n")
	fmt.Fprintf(buf, "func (u *%s) Status() int {\n", unionName)
	buf.WriteString("\tif u == nil {\n\t\treturn 0\n\t}\n")
	buf.WriteString("\treturn u.StatusCode\n")
	buf.WriteString("}\n\n")

	// 3. Typed Match method
	var matchParams []string
	for _, f := range u.Fields {
		paramName := "on" + f.GoName

		cleanType := f.Type.Name
		if !strings.HasPrefix(cleanType, "*") && f.Type.IsCustomType {
			cleanType = "*" + cleanType
		}

		matchParams = append(matchParams, fmt.Sprintf("%s func(%s)", paramName, cleanType))
	}

	fmt.Fprintf(buf, "// Match invokes the closure corresponding to the active status variant.\n")
	fmt.Fprintf(buf, "func (u *%s) Match(\n", unionName)

	for _, mp := range matchParams {
		fmt.Fprintf(buf, "\t%s,\n", mp)
	}

	buf.WriteString(") {\n")
	buf.WriteString("\tif u == nil {\n\t\treturn\n\t}\n")

	for _, f := range u.Fields {
		paramName := "on" + f.GoName
		fmt.Fprintf(buf, "\tif u.%s != nil && %s != nil {\n", f.GoName, paramName)
		fmt.Fprintf(buf, "\t\t%s(u.%s)\n", paramName, f.GoName)
		buf.WriteString("\t}\n")
	}

	buf.WriteString("}\n\n")
}
