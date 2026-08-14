// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package parser

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

// Directive represents a single parsed doc comment directive starting with '@'.
type Directive struct {
	Name  string            // e.g. "aoni:service", "get", "retry", "header", "check", "field"
	Value string            // First positional argument if present (unquoted)
	Args  map[string]string // Key-value arguments (e.g. attempts="3", casing="snake_case")
	Raw   string            // Raw directive text without leading "@"
}

// ParseDirective extracts a structured [Directive] from a single comment line.
// If the line does not start with an '@' directive, it returns nil.
func ParseDirective(line string) *Directive {
	trimmed := strings.TrimSpace(line)
	// Strip leading slashes from comments
	trimmed = strings.TrimLeft(trimmed, "/ ")
	if !strings.HasPrefix(trimmed, "@") {
		return nil
	}

	content := strings.TrimSpace(trimmed[1:])
	if content == "" {
		return nil
	}

	d := &Directive{
		Args: make(map[string]string),
		Raw:  content,
	}

	// Tokenize directive name
	idx := strings.IndexFunc(content, func(r rune) bool {
		return unicode.IsSpace(r) || r == '('
	})

	if idx == -1 {
		d.Name = strings.ToLower(content)
		return d
	}

	d.Name = strings.ToLower(content[:idx])
	rest := strings.TrimSpace(content[idx:])

	parseDirectiveArgs(rest, d)

	return d
}

func parseDirectiveArgs(rest string, d *Directive) {
	if rest == "" {
		return
	}

	tokens := tokenizeArgs(rest)
	if len(tokens) == 0 {
		return
	}

	first := true
	for _, tok := range tokens {
		trimmed := strings.TrimSpace(tok)
		if trimmed == "" {
			continue
		}

		// A token is a key=value pair if it does NOT start with a quote and contains '='
		switch {
		case !strings.HasPrefix(trimmed, "\"") && !strings.HasPrefix(trimmed, "'") && !strings.HasPrefix(trimmed, "`") && strings.Contains(trimmed, "="):
			parts := strings.SplitN(trimmed, "=", 2)
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			val := unquote(strings.TrimSpace(parts[1]))
			d.Args[key] = val
		case first:
			d.Value = unquote(trimmed)
			first = false
		default:
			// Positional flag or value
			d.Args[strings.ToLower(unquote(trimmed))] = "true"
		}
	}
}

func tokenizeArgs(s string) []string {
	var (
		tokens []string
		cur    strings.Builder
	)

	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(s); i++ {
		b := s[i]

		if inQuote {
			cur.WriteByte(b)

			if b == quoteChar {
				inQuote = false
			} else if b == '\\' && i+1 < len(s) {
				cur.WriteByte(s[i+1])
				i++
			}

			continue
		}

		if b == '"' || b == '\'' || b == '`' {
			inQuote = true
			quoteChar = b
			cur.WriteByte(b)

			continue
		}

		if unicode.IsSpace(rune(b)) || b == ',' {
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}

			continue
		}

		cur.WriteByte(b)
	}

	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}

	return tokens
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '`' && s[len(s)-1] == '`') {
			if unq, err := strconv.Unquote(s); err == nil {
				return unq
			}

			return strings.ReplaceAll(s[1:len(s)-1], `\"`, `"`)
		}

		if s[0] == '\'' && s[len(s)-1] == '\'' {
			return s[1 : len(s)-1]
		}
	}

	return s
}

// ParsePathTemplate decomposes a URL template string like "/users/{id}/orders/{order_id:path_escape}"
// into structured literal and variable segments.
func ParsePathTemplate(tmpl string) *ir.PathIR {
	pathIR := &ir.PathIR{
		RawTemplate: tmpl,
		Segments:    make([]ir.PathSegmentIR, 0, 4),
	}

	var curLit strings.Builder

	i := 0
	n := len(tmpl)

	for i < n {
		if tmpl[i] == '{' {
			closeIdx := strings.IndexByte(tmpl[i:], '}')
			if closeIdx != -1 {
				if curLit.Len() > 0 {
					pathIR.Segments = append(pathIR.Segments, ir.PathSegmentIR{
						IsVariable: false,
						Literal:    curLit.String(),
					})
					curLit.Reset()
				}

				varContent := tmpl[i+1 : i+closeIdx]
				varName := varContent
				transform := ir.TransformNone

				if colonIdx := strings.IndexByte(varContent, ':'); colonIdx != -1 {
					varName = varContent[:colonIdx]

					tf := strings.ToLower(strings.TrimSpace(varContent[colonIdx+1:]))
					switch tf {
					case "path_escape", "escape":
						transform = ir.TransformPathEscape
					case "query_escape":
						transform = ir.TransformQueryEscape
					}
				}

				pathIR.Segments = append(pathIR.Segments, ir.PathSegmentIR{
					IsVariable: true,
					VarName:    strings.TrimSpace(varName),
					Transform:  transform,
				})

				i += closeIdx + 1

				continue
			}
		}

		curLit.WriteByte(tmpl[i])
		i++
	}

	if curLit.Len() > 0 {
		pathIR.Segments = append(pathIR.Segments, ir.PathSegmentIR{
			IsVariable: false,
			Literal:    curLit.String(),
		})
	}

	return pathIR
}

// ParseHeaderDirective parses a "@header" directive value which may be static or templated.
func ParseHeaderDirective(val string) ir.HeaderIR {
	colonIdx := strings.IndexByte(val, ':')
	if colonIdx == -1 {
		return ir.HeaderIR{
			Key:         strings.TrimSpace(val),
			StaticValue: "",
		}
	}

	key := strings.TrimSpace(val[:colonIdx])
	valueStr := strings.TrimSpace(val[colonIdx+1:])

	if strings.Contains(valueStr, "{") && strings.Contains(valueStr, "}") {
		return ir.HeaderIR{
			Key:             key,
			DynamicTemplate: ParsePathTemplate(valueStr),
		}
	}

	return ir.HeaderIR{
		Key:         key,
		StaticValue: valueStr,
	}
}

// ParseCheckDirective parses a "@check" expression like "success == true" or "purchase_eresult == 1".
func ParseCheckDirective(val string) *ir.CheckIR {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil
	}

	var (
		op    ir.CheckOperator
		parts []string
	)

	switch {
	case strings.Contains(val, "=="):
		op = ir.OpEqual
		parts = strings.SplitN(val, "==", 2)
	case strings.Contains(val, "!="):
		op = ir.OpNotEqual
		parts = strings.SplitN(val, "!=", 2)
	default:
		return nil
	}

	field := strings.TrimSpace(parts[0])
	expected := strings.TrimSpace(parts[1])

	return &ir.CheckIR{
		Field:       field,
		Operator:    op,
		ExpectedVal: expected,
		ErrorMsg:    "api: check failed (" + field + " " + string(op) + " " + expected + ")",
	}
}
