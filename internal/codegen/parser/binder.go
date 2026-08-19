// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package parser

import (
	"bytes"
	"fmt"
	"go/ast"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

func (p *Parser) parseInterface(
	root *ir.RootIR,
	fileComments []*ast.CommentGroup,
	name string,
	docLines []string,
	iface *ast.InterfaceType,
) *ir.ServiceIR {
	directives := p.extractDirectives(root, name, docLines)
	if !hasServiceDirective(directives) {
		return nil
	}

	svc := &ir.ServiceIR{
		Name:     name,
		Doc:      docLines,
		Protocol: ir.ProtocolHTTP,
		Engine:   ir.EngineFast, // Default to ultra-fast engine
		Methods:  make([]*ir.MethodIR, 0, len(iface.Methods.List)),
	}

	for _, d := range directives {
		ApplyServiceDirective(svc, d)
	}

	for _, field := range iface.Methods.List {
		funcType, ok := field.Type.(*ast.FuncType)
		if !ok || len(field.Names) == 0 {
			continue
		}

		methodName := field.Names[0].Name
		methodDoc := extractDocLines(field.Doc, field.Comment)
		methodDirectives := p.extractDirectives(root, name+"."+methodName, methodDoc)

		op := ir.OpHTTP
		if methodName == "Close" {
			op = ir.OpClose
		}

		m := &ir.MethodIR{
			Name:            methodName,
			Doc:             methodDoc,
			Operation:       op,
			StreamDirection: ir.StreamNone,
			PayloadKind:     ir.PayloadNone,
			StreamKind:      ir.StreamKindNone,
			Headers:         make([]ir.HeaderIR, 0),
			Params:          make([]*ir.ParamIR, 0),
			Checks:          make([]ir.CheckIR, 0),
		}

		for _, d := range methodDirectives {
			ApplyMethodDirective(m, d)
		}

		if m.UnwrapField == "" && svc.DefaultUnwrapField != "" {
			m.UnwrapField = svc.DefaultUnwrapField
		} else if m.UnwrapField == "none" || m.UnwrapField == `""` {
			m.UnwrapField = ""
		}

		// Parse Method Parameters
		p.parseMethodParams(root, fileComments, svc, m, methodDirectives, funcType.Params)

		// Parse Method Return Values
		p.parseMethodReturns(m, funcType.Results)

		svc.Methods = append(svc.Methods, m)
	}

	return svc
}

func (p *Parser) parseMethodParams(
	root *ir.RootIR,
	fileComments []*ast.CommentGroup,
	svc *ir.ServiceIR,
	m *ir.MethodIR,
	methodDirectives []*Directive,
	fields *ast.FieldList,
) {
	if fields == nil {
		return
	}

	pathVars := make(map[string]string)
	if m.Path != nil {
		for _, seg := range m.Path.Segments {
			if seg.IsVariable {
				actual := seg.VarName
				pathVars[actual] = actual
				pathVars[strings.ToLower(actual)] = actual
				pathVars[toCasing(actual, ir.CasingSnakeCase)] = actual
				pathVars[toCasing(actual, ir.CasingFlatCase)] = actual
				pathVars[cleanIdentifier(actual)] = actual
			}
		}
	}

	prevLine := p.fset.Position(fields.Opening).Line
	for _, field := range fields.List {
		paramDoc := extractDocLines(field.Doc, field.Comment)
		if len(paramDoc) == 0 && len(fileComments) > 0 {
			paramDoc = p.findCommentsForParam(fileComments, prevLine, field)
		}

		prevLine = p.fset.Position(field.End()).Line

		paramDirectives := p.extractDirectives(root, svc.Name+"."+m.Name+"(param)", paramDoc)
		goType := p.extractGoType(field.Type)

		names := field.Names
		if len(names) == 0 {
			names = []*ast.Ident{{Name: "_"}}
		}

		for _, ident := range names {
			formatter := selectFormatStrategy(goType)
			if svc != nil && svc.TypeMaps != nil {
				if mapped, ok := svc.TypeMaps[goType.Name]; ok {
					formatter = mapped
				}
			}

			loc := ir.LocQuery
			switch {
			case m.IsEvent || m.Operation == ir.OpEvent:
				loc = ir.LocHandler
			case m.Operation == ir.OpRPC || m.Operation == ir.OpNotify:
				loc = ir.LocBody
			default:
				switch m.PayloadKind {
				case ir.PayloadForm:
					loc = ir.LocFormFields
				case ir.PayloadMultipart:
					loc = ir.LocMultipartField
				}
			}

			paramName := ident.Name
			param := &ir.ParamIR{
				GoName:    paramName,
				GoType:    goType,
				Location:  loc,
				WireKey:   paramName,
				Formatter: formatter,
			}

			// Check explicit directives
			for _, d := range paramDirectives {
				switch d.Name {
				case "body":
					param.Location = ir.LocBody

					if d.Value != "" {
						m.PayloadKind = ir.PayloadKind(d.Value)
					}

				case "path", "var":
					param.Location = ir.LocPath
					if d.Value != "" {
						param.WireKey = d.Value
					}
				case "param":
					if d.Value != "" {
						param.WireKey = d.Value
					} else if val, ok := d.Args["name"]; ok {
						param.WireKey = val
					}

				case "format":
					if layout, ok := d.Args["layout"]; ok {
						param.Formatter = ir.FormatTimeLayout
						param.TimeLayout = layout
					} else {
						switch strings.ToLower(d.Value) {
						case "json_string", "json":
							param.Formatter = ir.FormatJSONString
						case "bool_int", "01", "10", "int":
							param.Formatter = ir.FormatBoolInt
						case "flag", "bool_flag":
							param.Formatter = ir.FormatBoolFlag
						case "rfc3339", "time":
							param.Formatter = ir.FormatTimeRFC3339
						case "unix_s", "unix":
							param.Formatter = ir.FormatTimeUnixS
						case "unix_ms", "unix_milli":
							param.Formatter = ir.FormatTimeUnixMS
						case "comma":
							param.Formatter = ir.FormatSliceComma
						case "space":
							param.Formatter = ir.FormatSliceSpace
						case "pipe":
							param.Formatter = ir.FormatSlicePipe
						case "bracket":
							param.Formatter = ir.FormatSliceBracket
						}
					}

				case "field":
					if d.Value != "" {
						param.WireKey = d.Value
					}

					if d.Pipeline != nil {
						param.Pipeline = d.Pipeline
					}

					if m.PayloadKind == ir.PayloadMultipart {
						param.Location = ir.LocMultipartField
					} else {
						param.Location = ir.LocFormFields
					}

				case "part":
					param.Location = ir.LocMultipartField
					if d.Value != "" {
						param.WireKey = d.Value
					}

					if d.Pipeline != nil {
						param.Pipeline = d.Pipeline
					}

				case "file":
					param.Location = ir.LocMultipartFile
					if name, ok := d.Args["name"]; ok {
						param.WireKey = name
					} else if d.Value != "" {
						param.WireKey = d.Value
					}

					if fn, ok := d.Args["filename"]; ok {
						param.FileName = fn
					}

					if ct, ok := d.Args["content_type"]; ok {
						param.ContentType = ct
					}

				case "query":
					param.Location = ir.LocQuery
					if d.Value != "" {
						param.WireKey = d.Value
					}

					if d.Pipeline != nil {
						param.Pipeline = d.Pipeline
					}

				case "query_struct":
					param.Location = ir.LocQueryStruct
				case "header":
					param.Location = ir.LocHeader
					if d.Value != "" {
						param.WireKey = d.Value
					}

					if d.Pipeline != nil {
						param.Pipeline = d.Pipeline
					}

				case "cookie":
					param.Location = ir.LocCookie
					if d.Value != "" {
						param.WireKey = d.Value
					}

				case "deprecated":
					param.Deprecation = parseDeprecationDirective(d)

				case "description":
					param.Description = d.Value

				case "since":
					param.Since = d.Value
				}
			}

			// Mandatory types take precedence
			switch {
			case goType.Name == "context.Context" || goType.Name == "Context":
				param.Location = ir.LocContext
			case goType.IsVariadic && strings.Contains(goType.Name, "RequestModifier"):
				param.Location = ir.LocModifiers
			default:
				matchedMethodDirective := false
				// Check if matching directive was declared on method level
				if len(paramDirectives) == 0 {
					snakeParam := toCasing(paramName, ir.CasingSnakeCase)
					flatParam := toCasing(paramName, ir.CasingFlatCase)

					for _, md := range methodDirectives {
						if (md.Name == "field" || md.Name == "query" || md.Name == "param" || md.Name == "header") &&
							(strings.EqualFold(md.Value, paramName) || strings.EqualFold(md.Value, snakeParam) ||
								strings.EqualFold(md.Value, flatParam) || md.Args["param"] == paramName ||
								strings.EqualFold(md.Args["param"], snakeParam)) {
							matchedMethodDirective = true

							if md.Pipeline != nil {
								param.Pipeline = md.Pipeline
							}

							if md.Value != "" {
								param.WireKey = md.Value
							}

							switch md.Name {
							case "field":
								if m.PayloadKind == ir.PayloadMultipart {
									param.Location = ir.LocMultipartField
								} else {
									param.Location = ir.LocFormFields
								}

							case "query":
								param.Location = ir.LocQuery
							case "header":
								param.Location = ir.LocHeader
							}

							break
						}
					}
				}

				// Implicit inference if location not explicitly set by directive
				if len(paramDirectives) == 0 && param.Pipeline == nil && !matchedMethodDirective {
					effectiveCasing := m.FormCasing
					if effectiveCasing == "" || effectiveCasing == ir.CasingNone {
						effectiveCasing = svc.DefaultCasing
					}

					snakeName := toCasing(paramName, ir.CasingSnakeCase)
					flatName := toCasing(paramName, ir.CasingFlatCase)

					var matchedVar string
					if v, ok := pathVars[paramName]; ok {
						matchedVar = v
					} else if v, ok := pathVars[snakeName]; ok {
						matchedVar = v
					} else if v, ok := pathVars[flatName]; ok {
						matchedVar = v
					} else if v, ok := pathVars[strings.ToLower(paramName)]; ok {
						matchedVar = v
					} else if v, ok := pathVars[cleanIdentifier(paramName)]; ok {
						matchedVar = v
					}

					switch {
					case matchedVar != "":
						// Automatically binds to path template / dynamic header variable!
						param.Location = ir.LocPath
						param.WireKey = matchedVar

					case (m.HTTPMethod == "GET" || m.HTTPMethod == "DELETE" || m.HTTPMethod == "HEAD") && isDTOQueryStruct(goType.Name):
						param.Location = ir.LocQueryStruct
						param.Formatter = ir.FormatCompiledEncode
					case m.PayloadKind == ir.PayloadForm && isDTOQueryStruct(goType.Name):
						param.Location = ir.LocFormFields
						param.Formatter = ir.FormatCompiledEncode
					case m.HTTPMethod == "POST" || m.HTTPMethod == "PUT" || m.HTTPMethod == "PATCH":
						isBodyCandidate := goType.IsCustomType || goType.IsPointer || goType.IsMap || goType.IsSlice ||
							strings.HasSuffix(goType.Name, "Request") ||
							paramName == "req" || paramName == "request" || paramName == "body"

						switch {
						case isBodyCandidate && m.PayloadKind != ir.PayloadForm && m.PayloadKind != ir.PayloadMultipart:
							if m.PayloadKind == ir.PayloadNone {
								m.PayloadKind = ir.PayloadJSON
							}

							param.Location = ir.LocBody

						case m.QueryCasing != "":
							param.Location = ir.LocQuery
							if effectiveCasing != "" && effectiveCasing != ir.CasingNone {
								param.WireKey = toCasing(paramName, effectiveCasing)
							} else {
								param.WireKey = toCasing(paramName, ir.CasingSnakeCase)
							}

						case m.PayloadKind == ir.PayloadForm:
							param.Location = ir.LocFormFields
							if effectiveCasing != "" && effectiveCasing != ir.CasingNone {
								param.WireKey = toCasing(paramName, effectiveCasing)
							} else {
								param.WireKey = toCasing(paramName, ir.CasingSnakeCase)
							}

						case m.PayloadKind == ir.PayloadMultipart:
							param.Location = ir.LocMultipartField
							if effectiveCasing != "" && effectiveCasing != ir.CasingNone {
								param.WireKey = toCasing(paramName, effectiveCasing)
							} else {
								param.WireKey = toCasing(paramName, ir.CasingSnakeCase)
							}

						case !isBodyCandidate:
							param.Location = ir.LocQuery
							if effectiveCasing != "" && effectiveCasing != ir.CasingNone {
								param.WireKey = toCasing(paramName, effectiveCasing)
							} else {
								param.WireKey = toCasing(paramName, ir.CasingSnakeCase)
							}

						case param.Location != ir.LocContext && param.Location != ir.LocModifiers:
							if m.PayloadKind == ir.PayloadNone {
								m.PayloadKind = ir.PayloadJSON
							}

							param.Location = ir.LocBody
						}
					}
				}

				if (param.Location == ir.LocQuery || param.Location == ir.LocFormFields) &&
					param.Formatter != ir.FormatCompiledEncode && len(paramDirectives) == 0 {
					var effectiveCasing ir.CasingStrategy
					if param.Location == ir.LocQuery {
						effectiveCasing = m.QueryCasing
					} else {
						effectiveCasing = m.FormCasing
					}

					if effectiveCasing == "" || effectiveCasing == ir.CasingNone {
						effectiveCasing = svc.DefaultCasing
					}

					if effectiveCasing != "" && effectiveCasing != ir.CasingNone {
						param.WireKey = toCasing(paramName, effectiveCasing)
					}
				}
			}

			m.Params = append(m.Params, param)
		}
	}
}

func (p *Parser) parseMethodReturns(m *ir.MethodIR, fields *ast.FieldList) {
	ret := m.Return
	if ret == nil {
		ret = &ir.ReturnIR{}
	}

	if fields == nil || len(fields.List) == 0 {
		ret.IsVoid = true
		m.Return = ret
		return
	}

	results := fields.List

	switch len(results) {
	case 1:
		t := p.extractGoType(results[0].Type)
		switch {
		case m.IsEvent || m.Operation == ir.OpEvent || strings.HasPrefix(t.Name, "func("):
			ret.SuccessType = t
			ret.IsVoid = false
		case t.Name == "error":
			ret.SuccessType = t
			ret.IsVoid = true
		default:
			ret.SuccessType = t
			ret.IsVoid = false
		}

	case 2:
		t := p.extractGoType(results[0].Type)
		if t.IsChannel {
			ret.IsStreamChan = true
		}

		if t.Name == "[]byte" {
			ret.IsDirectBytes = true
		}

		ret.SuccessType = t

	case 3:
		t0 := p.extractGoType(results[0].Type)
		t1 := p.extractGoType(results[1].Type)

		if t0.IsChannel && t1.IsChannel {
			ret.IsStreamChan = true
			ret.SuccessType = t0
		} else {
			ret.SuccessType = t0
			if t1.Name == "*http.Response" || t1.Name == "http.Response" {
				ret.HasRawResponse = true
			}
		}
	}

	m.Return = ret
}

func (p *Parser) parseStruct(root *ir.RootIR, name string, docLines []string, strct *ast.StructType) *ir.StructIR {
	directives := p.extractDirectives(root, name, docLines)
	isDTO := false
	isTuple := false
	isBitpack := false
	isUnion := false
	casing := ir.CasingSnakeCase
	omitEmpty := true

	var (
		sDep     *ir.DeprecationIR
		sDesc    string
		sVersion string
		sSince   string
	)

	for _, d := range directives {
		if d.Name == "aoni:tuple" || d.Name == "tuple" {
			isTuple = true
		}

		if d.Name == "aoni:bitpack" || d.Name == "bitpack" {
			isBitpack = true
		}

		if d.Name == "aoni:union" || d.Name == "union" {
			isUnion = true
		}

		if d.Name == "deprecated" {
			sDep = parseDeprecationDirective(d)
		}

		if d.Name == "description" {
			sDesc = d.Value
		}

		if d.Name == "version" {
			sVersion = d.Value
		}

		if d.Name == "since" {
			sSince = d.Value
		}

		if d.Name == "aoni:dto" || d.Name == "dto" {
			isDTO = true

			if c, ok := d.Args["casing"]; ok {
				switch strings.ToLower(c) {
				case "camel_case", "camelcase":
					casing = ir.CasingCamelCase
				case "pascal_case", "pascalcase":
					casing = ir.CasingPascalCase
				case "kebab_case", "kebabcase":
					casing = ir.CasingKebabCase
				default:
					casing = ir.CasingSnakeCase
				}
			}

			if oe, ok := d.Args["omitempty"]; ok {
				omitEmpty = (oe == "true")
			}
		}
	}

	if isTuple || isBitpack || isUnion {
		return nil
	}

	s := &ir.StructIR{
		Name:            name,
		Doc:             docLines,
		Casing:          casing,
		OmitEmpty:       omitEmpty,
		Fields:          make([]*ir.FieldIR, 0, len(strct.Fields.List)),
		GenValueEncoder: isDTO,
		Deprecation:     sDep,
		Description:     sDesc,
		Version:         sVersion,
		Since:           sSince,
	}

	for _, field := range strct.Fields.List {
		fieldDoc := extractDocLines(field.Doc, field.Comment)
		fieldDirectives := p.extractDirectives(root, name+"(field)", fieldDoc)
		goType := p.extractGoType(field.Type)

		for _, ident := range field.Names {
			fieldName := ident.Name
			wireName := toCasing(fieldName, casing)
			customTag := ""
			formatter := selectFormatStrategy(goType)

			var (
				fieldDep   *ir.DeprecationIR
				fieldDesc  string
				fieldSince string
			)

			if field.Tag != nil {
				customTag = strings.Trim(field.Tag.Value, "`")

				st := reflect.StructTag(customTag)
				if u := st.Get("url"); u != "" && u != "-" {
					wireName = strings.Split(u, ",")[0]
				} else if j := st.Get("json"); j != "" && j != "-" {
					wireName = strings.Split(j, ",")[0]
				}
			}

			for _, d := range fieldDirectives {
				switch d.Name {
				case "field":
					if d.Value != "" {
						wireName = d.Value
					}
				case "deprecated":
					fieldDep = parseDeprecationDirective(d)
				case "description":
					fieldDesc = d.Value
				case "since":
					fieldSince = d.Value
				case "format":
					if layout, ok := d.Args["layout"]; ok {
						formatter = ir.FormatTimeLayout
						_ = layout
					} else {
						switch strings.ToLower(d.Value) {
						case "bool_int", "01", "10", "int":
							formatter = ir.FormatBoolInt
						case "flag", "bool_flag":
							formatter = ir.FormatBoolFlag
						case "rfc3339", "time":
							formatter = ir.FormatTimeRFC3339
						case "unix_s", "unix":
							formatter = ir.FormatTimeUnixS
						case "unix_ms", "unix_milli":
							formatter = ir.FormatTimeUnixMS
						case "comma":
							formatter = ir.FormatSliceComma
						case "space":
							formatter = ir.FormatSliceSpace
						case "pipe":
							formatter = ir.FormatSlicePipe
						case "bracket":
							formatter = ir.FormatSliceBracket
						}
					}
				}
			}

			s.Fields = append(s.Fields, &ir.FieldIR{
				GoName:      fieldName,
				WireName:    wireName,
				Type:        goType,
				IsOmitEmpty: omitEmpty,
				CustomTag:   customTag,
				Formatter:   formatter,
				Deprecation: fieldDep,
				Description: fieldDesc,
				Since:       fieldSince,
			})
		}
	}

	return s
}

func (p *Parser) parseTuple(root *ir.RootIR, name string, docLines []string, strct *ast.StructType) *ir.TupleIR {
	directives := p.extractDirectives(root, name, docLines)
	isTuple := false

	for _, d := range directives {
		if d.Name == "aoni:tuple" || d.Name == "tuple" {
			isTuple = true
			break
		}
	}

	if !isTuple {
		return nil
	}

	tuple := &ir.TupleIR{
		Name:   name,
		Fields: make([]ir.TupleFieldIR, 0, len(strct.Fields.List)),
	}

	idx := 0
	for _, field := range strct.Fields.List {
		goType := p.extractGoType(field.Type)

		tagPathStr := ""
		if field.Tag != nil {
			tagVal := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
			if v := tagVal.Get("aoni"); v != "" {
				tagPathStr = v
			} else if v := tagVal.Get("tuple"); v != "" {
				tagPathStr = v
			}
		}

		for _, ident := range field.Names {
			fieldPathStr := tagPathStr

			var indexPath []int

			fieldIdx := idx

			if fieldPathStr != "" {
				parts := strings.Split(fieldPathStr, ".")

				valid := true
				for _, part := range parts {
					val, err := strconv.Atoi(strings.TrimSpace(part))
					if err != nil {
						valid = false
						break
					}

					indexPath = append(indexPath, val)
				}

				if valid && len(indexPath) > 0 {
					fieldIdx = indexPath[0]
				} else {
					indexPath = []int{idx}
					fieldPathStr = strconv.Itoa(idx)
				}
			} else {
				fieldPathStr = strconv.Itoa(idx)
				indexPath = []int{idx}
			}

			tuple.Fields = append(tuple.Fields, ir.TupleFieldIR{
				Index:     fieldIdx,
				IndexPath: indexPath,
				PathStr:   fieldPathStr,
				IsNested:  len(indexPath) > 1,
				GoName:    ident.Name,
				Type:      goType,
			})
			idx++
		}
	}

	return tuple
}

func (p *Parser) parseBitpack(root *ir.RootIR, name string, docLines []string, strct *ast.StructType) *ir.BitpackIR {
	directives := p.extractDirectives(root, name, docLines)
	isBitpack := false
	endianness := ir.EndianLittle

	for _, d := range directives {
		if d.Name == "aoni:bitpack" || d.Name == "bitpack" {
			isBitpack = true

			if end, ok := d.Args["endian"]; ok {
				if strings.ToLower(end) == "big" {
					endianness = ir.EndianBig
				}
			}
		}
	}

	if !isBitpack {
		return nil
	}

	bp := &ir.BitpackIR{
		Name:       name,
		Doc:        docLines,
		Endianness: endianness,
		Fields:     make([]*ir.BitpackFieldIR, 0, len(strct.Fields.List)),
	}

	currentOffset := 0

	for _, field := range strct.Fields.List {
		goType := p.extractGoType(field.Type)
		bitWidth := 0

		if field.Tag != nil {
			tagVal := strings.Trim(field.Tag.Value, "`")

			st := reflect.StructTag(tagVal)
			if bitsStr := st.Get("bits"); bitsStr != "" {
				if bw, err := strconv.Atoi(bitsStr); err == nil && bw > 0 {
					bitWidth = bw
				}
			}
		}

		if bitWidth == 0 {
			bitWidth = defaultTypeBitWidth(goType.Name)
		}

		isBool := (goType.Name == "bool")
		if isBool && bitWidth == 0 {
			bitWidth = 1
		}

		isSigned := isSignedIntType(goType.Name)

		var mask uint64
		if bitWidth >= 64 {
			mask = ^uint64(0)
		} else if bitWidth > 0 {
			mask = (uint64(1) << bitWidth) - 1
		}

		for _, ident := range field.Names {
			bp.Fields = append(bp.Fields, &ir.BitpackFieldIR{
				GoName:    ident.Name,
				Type:      goType,
				BitWidth:  bitWidth,
				BitOffset: currentOffset,
				Mask:      mask,
				IsSigned:  isSigned,
				IsBool:    isBool,
			})
			currentOffset += bitWidth
		}
	}

	bp.TotalBits = currentOffset
	bp.TotalBytes = (currentOffset + 7) / 8

	return bp
}

func (p *Parser) parseUnion(root *ir.RootIR, name string, docLines []string, strct *ast.StructType) *ir.UnionIR {
	directives := p.extractDirectives(root, name, docLines)
	isUnion := false

	for _, d := range directives {
		if d.Name == "aoni:union" || d.Name == "union" {
			isUnion = true
			break
		}
	}

	if !isUnion {
		return nil
	}

	union := &ir.UnionIR{
		Name:     name,
		Doc:      docLines,
		Fields:   make([]*ir.UnionFieldIR, 0, len(strct.Fields.List)),
		Variants: make(map[int]ir.GoTypeIR),
	}

	for _, field := range strct.Fields.List {
		goType := p.extractGoType(field.Type)

		var statusCodes []int

		if field.Tag != nil {
			tagVal := strings.Trim(field.Tag.Value, "`")

			st := reflect.StructTag(tagVal)
			if statusStr := st.Get("status"); statusStr != "" {
				for _, sc := range strings.Split(statusStr, ",") {
					for _, part := range strings.Fields(sc) {
						if code, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
							statusCodes = append(statusCodes, code)
						}
					}
				}
			}
		}

		for _, ident := range field.Names {
			fieldName := ident.Name
			if strings.EqualFold(fieldName, "StatusCode") || strings.EqualFold(fieldName, "Status") {
				continue
			}

			union.Fields = append(union.Fields, &ir.UnionFieldIR{
				GoName:      fieldName,
				Type:        goType,
				StatusCodes: statusCodes,
			})

			for _, code := range statusCodes {
				union.Variants[code] = goType
			}
		}
	}

	return union
}

func defaultTypeBitWidth(typeName string) int {
	switch typeName {
	case "bool":
		return 1
	case "uint8", "byte", "int8":
		return 8
	case "uint16", "int16":
		return 16
	case "uint32", "int32":
		return 32
	case "uint64", "int64", "uint", "int", "uintptr":
		return 64
	default:
		return 8
	}
}

func isSignedIntType(typeName string) bool {
	switch typeName {
	case "int8", "int16", "int32", "int64", "int":
		return true
	default:
		return false
	}
}

func (p *Parser) extractGoType(expr ast.Expr) ir.GoTypeIR {
	var (
		buf    bytes.Buffer
		goType ir.GoTypeIR
	)

	switch t := expr.(type) {
	case *ast.Ident:
		goType.Name = t.Name
		if isPrimitive(t.Name) {
			goType.Underlying = t.Name
		} else {
			goType.IsCustomType = true
		}

	case *ast.StarExpr:
		elem := p.extractGoType(t.X)
		goType = elem
		goType.IsPointer = true
		goType.Name = "*" + elem.Name
	case *ast.ArrayType:
		elem := p.extractGoType(t.Elt)
		goType = elem
		goType.IsSlice = true
		goType.ElemType = elem.Name
		goType.Name = "[]" + elem.Name

	case *ast.MapType:
		k := p.extractGoType(t.Key)
		v := p.extractGoType(t.Value)
		goType.IsMap = true
		goType.KeyType = k.Name
		goType.ElemType = v.Name
		goType.Name = fmt.Sprintf("map[%s]%s", k.Name, v.Name)

	case *ast.SelectorExpr:
		pkg := p.extractGoType(t.X)
		goType.Package = pkg.Name
		goType.Name = fmt.Sprintf("%s.%s", pkg.Name, t.Sel.Name)
		goType.IsCustomType = true
	case *ast.ChanType:
		elem := p.extractGoType(t.Value)
		goType = elem
		goType.IsChannel = true
		goType.Name = "<-chan " + elem.Name
	case *ast.Ellipsis:
		elem := p.extractGoType(t.Elt)
		goType = elem
		goType.IsVariadic = true
		goType.Name = "..." + elem.Name
	case *ast.FuncType:
		var params []string
		if t.Params != nil {
			for _, f := range t.Params.List {
				pt := p.extractGoType(f.Type)
				if goType.ElemType == "" {
					goType.ElemType = pt.Name
				}

				if len(f.Names) > 0 {
					for _, n := range f.Names {
						params = append(params, fmt.Sprintf("%s %s", n.Name, pt.Name))
					}
				} else {
					params = append(params, pt.Name)
				}
			}
		}

		var results []string
		if t.Results != nil {
			for _, f := range t.Results.List {
				rt := p.extractGoType(f.Type)
				results = append(results, rt.Name)
			}
		}

		resStr := ""
		if len(results) == 1 {
			resStr = " " + results[0]
		} else if len(results) > 1 {
			resStr = " (" + strings.Join(results, ", ") + ")"
		}

		goType.Name = fmt.Sprintf("func(%s)%s", strings.Join(params, ", "), resStr)

	default:
		_ = buf
		goType.Name = "any"
	}

	return goType
}

func selectFormatStrategy(t ir.GoTypeIR) ir.FormatStrategy {
	name := strings.TrimPrefix(t.Name, "*")
	switch name {
	case "string":
		return ir.FormatDirectString
	case "time.Time", "Time":
		return ir.FormatTimeRFC3339
	case "int", "int8", "int16", "int32", "int64":
		return ir.FormatIntAppend
	case "uint", "uint8", "uint16", "uint32", "uint64", "uintptr":
		return ir.FormatUintAppend
	case "bool":
		return ir.FormatBoolAppend
	default:
		if strings.HasSuffix(name, "ID") || strings.HasSuffix(name, "Code") || strings.HasSuffix(name, "Type") ||
			strings.HasSuffix(name, "State") || strings.HasSuffix(name, "Status") || strings.HasSuffix(name, "Kind") ||
			strings.HasSuffix(name, "Mode") || strings.HasSuffix(name, "Flag") || strings.HasSuffix(name, "Flags") {
			return ir.FormatDirectString
		}

		if t.IsCustomType {
			return ir.FormatCustomStringer
		}

		return ir.FormatDirectString
	}
}

func isPrimitive(s string) bool {
	switch s {
	case "string", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"float32", "float64", "bool", "byte", "rune":
		return true
	}

	return false
}

func isDTOQueryStruct(name string) bool {
	if isPrimitive(name) {
		return false
	}

	switch name {
	case "time.Time", "Time", "time.Duration", "Duration",
		"values.Int64String", "values.Uint64String", "values.Float64String", "values.BoolInt",
		"uuid.UUID", "UUID", "decimal.Decimal", "Decimal", "netip.Addr", "Addr":
		return false
	}

	if strings.HasPrefix(name, "[]") || strings.HasPrefix(name, "map[") {
		return false
	}

	if strings.HasSuffix(name, "ID") || strings.HasSuffix(name, "Code") || strings.HasSuffix(name, "Type") ||
		strings.HasSuffix(name, "State") || strings.HasSuffix(name, "Status") || strings.HasSuffix(name, "Kind") ||
		strings.HasSuffix(name, "Mode") || strings.HasSuffix(name, "Flag") || strings.HasSuffix(name, "Flags") ||
		strings.HasSuffix(name, "Action") || strings.HasSuffix(name, "Level") || strings.HasSuffix(name, "Platform") {
		return false
	}

	return true
}

// ToCasing transforms an identifier according to the provided casing strategy.
func ToCasing(s string, strategy ir.CasingStrategy) string {
	if s == "" {
		return ""
	}

	switch strategy {
	case ir.CasingPascalCase:
		return s
	case ir.CasingCamelCase:
		return strings.ToLower(s[:1]) + s[1:]
	case ir.CasingKebabCase:
		return toDelimited(s, '-')
	case ir.CasingFlatCase:
		return strings.ToLower(s)
	case ir.CasingNone:
		return s
	case ir.CasingSnakeCase:
		fallthrough
	default:
		return toDelimited(s, '_')
	}
}

func toCasing(s string, strategy ir.CasingStrategy) string {
	return ToCasing(s, strategy)
}

func toDelimited(s string, delimiter byte) string {
	if s == "" {
		return ""
	}

	var b strings.Builder

	runes := []rune(s)
	n := len(runes)

	for i := 0; i < n; i++ {
		r := runes[i]
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]

			var next rune
			if i+1 < n {
				next = runes[i+1]
			}

			if unicode.IsLower(prev) || unicode.IsDigit(prev) ||
				(unicode.IsUpper(prev) && next != 0 && unicode.IsLower(next)) {
				b.WriteByte(delimiter)
			}
		}

		b.WriteRune(unicode.ToLower(r))
	}

	return b.String()
}

func (p *Parser) findCommentsForParam(fileComments []*ast.CommentGroup, prevLine int, field *ast.Field) []string {
	if field == nil || len(fileComments) == 0 {
		return nil
	}

	fieldStart := p.fset.Position(field.Pos()).Line
	fieldEnd := p.fset.Position(field.End()).Line

	var lines []string
	for _, cg := range fileComments {
		for _, c := range cg.List {
			cPos := p.fset.Position(c.Pos())
			isTrailing := cPos.Line >= fieldStart && cPos.Line <= fieldEnd
			isPreceding := cPos.Line < fieldStart && cPos.Line > prevLine

			if isTrailing || isPreceding {
				lines = append(lines, c.Text)
			}
		}
	}

	return lines
}

func cleanIdentifier(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(unicode.ToLower(r))
		}
	}

	return b.String()
}
