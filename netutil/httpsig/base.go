// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package httpsig

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

var stringBuilderPool = sync.Pool{
	New: func() any {
		return &strings.Builder{}
	},
}

func getStringBuilder() *strings.Builder {
	sb := stringBuilderPool.Get().(*strings.Builder)
	sb.Reset()
	return sb
}

func putStringBuilder(sb *strings.Builder) {
	stringBuilderPool.Put(sb)
}

// ParsedComponent represents a normalized component identifier and its parameters (RFC 9421 §2).
type ParsedComponent struct {
	Raw        string
	Name       string
	Key        string
	NameParam  string
	SF         bool
	BS         bool
	Req        bool
	TR         bool
	Serialized string // Formatted as `"name";param=value`
}

// ParseComponentIdentifier parses a raw component identifier string (RFC 9421 §2).
// Examples:
//   - "@method" -> Name: "@method", Serialized: `"@method"`
//   - `"@query-param";name="foo"` -> Name: "@query-param", NameParam: "foo", Serialized: `"@query-param";name="foo"`
//   - `"example-dict";key="a"` -> Name: "example-dict", Key: "a", Serialized: `"example-dict";key="a"`
func ParseComponentIdentifier(raw string) (ParsedComponent, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ParsedComponent{}, fmt.Errorf("%w: empty component identifier", ErrMissingComponent)
	}

	comp := ParsedComponent{Raw: raw}

	// Extract base component name (may be double-quoted or unquoted)
	var (
		namePart  string
		remainder string
	)

	if strings.HasPrefix(s, "\"") {
		endIdx := strings.Index(s[1:], "\"")
		if endIdx == -1 {
			return comp, fmt.Errorf("%w: unclosed quote in component identifier %q", ErrInvalidStructuredField, raw)
		}

		namePart = s[1 : endIdx+1]
		remainder = strings.TrimSpace(s[endIdx+2:])
	} else {
		// Unquoted name up to first semicolon or end
		semicolon := strings.IndexByte(s, ';')
		if semicolon != -1 {
			namePart = strings.TrimSpace(s[:semicolon])
			remainder = strings.TrimSpace(s[semicolon:])
		} else {
			namePart = s
			remainder = ""
		}
	}

	// Component names and derived components are lowercased
	comp.Name = strings.ToLower(namePart)

	// Parse parameters: ;param1="val" ;param2 ;key="k"
	sb := getStringBuilder()
	defer putStringBuilder(sb)

	sb.WriteByte('"')
	sb.WriteString(comp.Name)
	sb.WriteByte('"')

	if remainder != "" {
		parts := strings.Split(remainder, ";")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}

			eqIdx := strings.IndexByte(p, '=')
			if eqIdx == -1 {
				// Boolean flag
				paramName := strings.ToLower(strings.TrimSpace(p))
				switch paramName {
				case "sf":
					comp.SF = true

					sb.WriteString(";sf")
				case "bs":
					comp.BS = true

					sb.WriteString(";bs")
				case "req":
					comp.Req = true

					sb.WriteString(";req")
				case "tr":
					comp.TR = true

					sb.WriteString(";tr")
				default:
					sb.WriteByte(';')
					sb.WriteString(paramName)
				}
			} else {
				paramName := strings.ToLower(strings.TrimSpace(p[:eqIdx]))
				paramVal := strings.TrimSpace(p[eqIdx+1:])
				// Strip quotes if present
				if len(paramVal) >= 2 && strings.HasPrefix(paramVal, "\"") && strings.HasSuffix(paramVal, "\"") {
					paramVal = paramVal[1 : len(paramVal)-1]
				}

				switch paramName {
				case "key":
					comp.Key = paramVal

					sb.WriteString(";key=\"")
					sb.WriteString(paramVal)
					sb.WriteByte('"')

				case "name":
					comp.NameParam = paramVal

					sb.WriteString(";name=\"")
					sb.WriteString(paramVal)
					sb.WriteByte('"')

				default:
					sb.WriteByte(';')
					sb.WriteString(paramName)
					sb.WriteString("=\"")
					sb.WriteString(paramVal)
					sb.WriteByte('"')
				}
			}
		}
	}

	comp.Serialized = sb.String()

	// Validate parameter combinations per RFC 9421 §2.1
	if comp.BS && (comp.SF || comp.Key != "") {
		return comp, fmt.Errorf("%w: bs parameter cannot be combined with sf or key", ErrInvalidStructuredField)
	}

	return comp, nil
}

// RequestContext abstracts standard HTTP requests and responses for signature base derivation.
type RequestContext struct {
	Method     string
	URL        *url.URL
	Header     http.Header
	StatusCode int
	Trailers   http.Header
	IsResponse bool
	ReqContext *RequestContext // Related request for response signing (req flag)
}

// BuildSignatureBase constructs the RFC 9421 §2.5 Signature Base byte string.
func BuildSignatureBase(ctx *RequestContext, params *SignatureParams) ([]byte, error) {
	if params == nil {
		return nil, fmt.Errorf("%w: nil signature parameters", ErrMissingComponent)
	}

	seen := make(map[string]struct{}, len(params.Components))

	sb := getStringBuilder()
	defer putStringBuilder(sb)

	// Step 2: For each message component item in the covered components set
	for _, compRaw := range params.Components {
		parsed, err := ParseComponentIdentifier(compRaw)
		if err != nil {
			return nil, err
		}

		if _, exists := seen[parsed.Serialized]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateComponent, parsed.Serialized)
		}

		seen[parsed.Serialized] = struct{}{}

		// Determine target context (if req flag is set on response)
		targetCtx := ctx
		if parsed.Req {
			if !ctx.IsResponse {
				return nil, fmt.Errorf("%w: req parameter cannot be used on request message", ErrMissingComponent)
			}

			if ctx.ReqContext == nil {
				return nil, fmt.Errorf("%w: related request context missing for req parameter", ErrMissingComponent)
			}

			targetCtx = ctx.ReqContext
		}

		val, err := deriveComponentValue(targetCtx, parsed)
		if err != nil {
			return nil, err
		}

		// Append `<component-identifier>: <value>\n`
		sb.WriteString(parsed.Serialized)
		sb.WriteString(": ")
		sb.WriteString(val)
		sb.WriteByte('\n')
	}

	// Step 3: Append signature parameters line
	sigParamsVal, err := SerializeSignatureParams(params)
	if err != nil {
		return nil, err
	}

	sb.WriteString("\"@signature-params\": ")
	sb.WriteString(sigParamsVal)

	baseStr := sb.String()

	// Step 4: Verify ASCII-only
	for i := 0; i < len(baseStr); i++ {
		if baseStr[i] > unicode.MaxASCII {
			return nil, fmt.Errorf("%w: signature base contains non-ASCII characters", ErrInvalidStructuredField)
		}
	}

	return []byte(baseStr), nil
}

// SerializeSignatureParams serializes the SignatureParams into an RFC 8941 Inner List (RFC 9421 §2.3).
// Example: `("@method" "@authority" "@path");created=1618884473;keyid="test-key";alg="rsa-pss-sha512"`
func SerializeSignatureParams(params *SignatureParams) (string, error) {
	sb := getStringBuilder()
	defer putStringBuilder(sb)

	sb.WriteByte('(')

	for i, compRaw := range params.Components {
		parsed, err := ParseComponentIdentifier(compRaw)
		if err != nil {
			return "", err
		}

		if i > 0 {
			sb.WriteByte(' ')
		}

		sb.WriteString(parsed.Serialized)
	}

	sb.WriteByte(')')

	if params.Created > 0 {
		sb.WriteString(";created=")
		sb.WriteString(strconv.FormatInt(params.Created, 10))
	}

	if params.Expires > 0 {
		sb.WriteString(";expires=")
		sb.WriteString(strconv.FormatInt(params.Expires, 10))
	}

	if params.KeyID != "" {
		sb.WriteString(";keyid=\"")
		sb.WriteString(params.KeyID)
		sb.WriteByte('"')
	}

	if params.Alg != "" {
		sb.WriteString(";alg=\"")
		sb.WriteString(string(params.Alg))
		sb.WriteByte('"')
	}

	if params.Nonce != "" {
		sb.WriteString(";nonce=\"")
		sb.WriteString(params.Nonce)
		sb.WriteByte('"')
	}

	if params.Tag != "" {
		sb.WriteString(";tag=\"")
		sb.WriteString(params.Tag)
		sb.WriteByte('"')
	}

	return sb.String(), nil
}

// ParseSignatureParams parses a serialized Signature-Input parameter line (RFC 9421 §2.3).
func ParseSignatureParams(input string) (*SignatureParams, error) {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "(") {
		return nil, fmt.Errorf("%w: signature parameters must start with '('", ErrInvalidStructuredField)
	}

	closeParen := strings.IndexByte(input, ')')
	if closeParen == -1 {
		return nil, fmt.Errorf("%w: unclosed '(' in signature parameters", ErrInvalidStructuredField)
	}

	innerList := strings.TrimSpace(input[1:closeParen])
	paramsPart := strings.TrimSpace(input[closeParen+1:])

	params := &SignatureParams{
		Components: make([]string, 0, 8),
	}

	if innerList != "" {
		// Split components respecting double quotes
		comps := splitInnerList(innerList)
		params.Components = append(params.Components, comps...)
	}

	// Parse parameters (;created=123;keyid="abc")
	if paramsPart != "" {
		parts := strings.Split(paramsPart, ";")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}

			eqIdx := strings.IndexByte(p, '=')
			if eqIdx == -1 {
				continue
			}

			k := strings.ToLower(strings.TrimSpace(p[:eqIdx]))
			v := strings.TrimSpace(p[eqIdx+1:])

			if len(v) >= 2 && strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"") {
				v = v[1 : len(v)-1]
			}

			switch k {
			case "created":
				if c, err := strconv.ParseInt(v, 10, 64); err == nil {
					params.Created = c
				}
			case "expires":
				if e, err := strconv.ParseInt(v, 10, 64); err == nil {
					params.Expires = e
				}
			case "keyid":
				params.KeyID = v
			case "alg":
				params.Alg = Algorithm(v)
			case "nonce":
				params.Nonce = v
			case "tag":
				params.Tag = v
			}
		}
	}

	return params, nil
}

func splitInnerList(inner string) []string {
	var (
		results []string
		current strings.Builder
	)

	inQuote := false

	for i := 0; i < len(inner); i++ {
		ch := inner[i]
		switch {
		case ch == '"':
			inQuote = !inQuote

			current.WriteByte(ch)
		case ch == ' ' && !inQuote:
			if current.Len() > 0 {
				results = append(results, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}

	if current.Len() > 0 {
		results = append(results, current.String())
	}

	return results
}

func deriveComponentValue(ctx *RequestContext, comp ParsedComponent) (string, error) {
	if strings.HasPrefix(comp.Name, "@") {
		return deriveDerivedComponent(ctx, comp)
	}

	return deriveHTTPField(ctx, comp)
}

func deriveDerivedComponent(ctx *RequestContext, comp ParsedComponent) (string, error) {
	switch comp.Name {
	case CompMethod:
		if ctx.Method == "" {
			return "", fmt.Errorf("%w: @method unavailable", ErrMissingComponent)
		}

		return ctx.Method, nil

	case CompTargetURI:
		if ctx.URL == nil {
			return "", fmt.Errorf("%w: @target-uri unavailable", ErrMissingComponent)
		}

		return ctx.URL.String(), nil

	case CompAuthority:
		if ctx.URL == nil || ctx.URL.Host == "" {
			return "", fmt.Errorf("%w: @authority unavailable", ErrMissingComponent)
		}

		return normalizeAuthority(ctx.URL), nil

	case CompScheme:
		if ctx.URL == nil || ctx.URL.Scheme == "" {
			return "", fmt.Errorf("%w: @scheme unavailable", ErrMissingComponent)
		}

		return strings.ToLower(ctx.URL.Scheme), nil

	case CompRequestTarget:
		if ctx.URL == nil {
			return "", fmt.Errorf("%w: @request-target unavailable", ErrMissingComponent)
		}

		path := ctx.URL.Path
		if path == "" {
			path = "/"
		}

		if ctx.URL.RawQuery != "" {
			return path + "?" + ctx.URL.RawQuery, nil
		}

		return path, nil

	case CompPath:
		if ctx.URL == nil {
			return "", fmt.Errorf("%w: @path unavailable", ErrMissingComponent)
		}

		p := ctx.URL.Path
		if p == "" {
			p = "/"
		}

		return p, nil

	case CompQuery:
		if ctx.URL == nil {
			return "", fmt.Errorf("%w: @query unavailable", ErrMissingComponent)
		}

		if ctx.URL.RawQuery != "" {
			return "?" + ctx.URL.RawQuery, nil
		}

		return "?", nil

	case CompQueryParam:
		if ctx.URL == nil || comp.NameParam == "" {
			return "", fmt.Errorf("%w: @query-param requires 'name' parameter", ErrMissingComponent)
		}

		return deriveQueryParam(ctx.URL.RawQuery, comp.NameParam)

	case CompStatus:
		if !ctx.IsResponse {
			return "", fmt.Errorf("%w: @status only valid for response messages", ErrMissingComponent)
		}

		if ctx.StatusCode == 0 {
			return "", fmt.Errorf("%w: @status unavailable", ErrMissingComponent)
		}

		return strconv.Itoa(ctx.StatusCode), nil

	default:
		return "", fmt.Errorf("%w: unknown derived component %q", ErrMissingComponent, comp.Name)
	}
}

func normalizeAuthority(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	port := u.Port()

	if port == "" {
		return host
	}

	// Omit default ports (RFC 9421 §2.2.3 & RFC 9110 §4.2.3)
	if (strings.EqualFold(u.Scheme, "http") && port == "80") ||
		(strings.EqualFold(u.Scheme, "https") && port == "443") {
		return host
	}

	return host + ":" + port
}

func deriveQueryParam(rawQuery, paramName string) (string, error) {
	if rawQuery == "" {
		return "", fmt.Errorf("%w: query parameter %q not found", ErrMissingComponent, paramName)
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", fmt.Errorf("%w: failed to parse query parameters: %w", ErrMissingComponent, err)
	}

	vals, exists := values[paramName]
	if !exists || len(vals) == 0 {
		return "", fmt.Errorf("%w: query parameter %q not found in %q", ErrMissingComponent, paramName, rawQuery)
	}

	if len(vals) > 1 {
		return "", fmt.Errorf(
			"%w: query parameter %q occurs multiple times (RFC 9421 §2.2.8 requires @query instead)",
			ErrMissingComponent,
			paramName,
		)
	}

	// Form-encode value per RFC 9421 §2.2.8
	return url.QueryEscape(vals[0]), nil
}

func deriveHTTPField(ctx *RequestContext, comp ParsedComponent) (string, error) {
	headers := ctx.Header
	if comp.TR {
		headers = ctx.Trailers
	}

	if headers == nil {
		return "", fmt.Errorf("%w: header %q not found", ErrMissingComponent, comp.Name)
	}

	// Canonical header lookup
	var matchValues []string
	for k, v := range headers {
		if strings.EqualFold(k, comp.Name) {
			matchValues = append(matchValues, v...)
		}
	}

	if len(matchValues) == 0 {
		return "", fmt.Errorf("%w: header %q not found in message", ErrMissingComponent, comp.Name)
	}

	// Handle Byte-Sequence wrapping (;bs)
	if comp.BS {
		var bsItems []string
		for _, v := range matchValues {
			cleaned := cleanHeaderValue(v)
			enc := base64.StdEncoding.EncodeToString([]byte(cleaned))
			bsItems = append(bsItems, ":"+enc+":")
		}

		return strings.Join(bsItems, ", "), nil
	}

	// Handle Dictionary key selection (;key="k")
	if comp.Key != "" {
		combined := strings.Join(matchValues, ", ")

		dictVal, err := extractDictionaryKey(combined, comp.Key)
		if err != nil {
			return "", err
		}

		return dictVal, nil
	}

	// Standard combining: comma + space (RFC 9421 §2.1)
	var cleanedValues []string
	for _, v := range matchValues {
		cleanedValues = append(cleanedValues, cleanHeaderValue(v))
	}

	return strings.Join(cleanedValues, ", "), nil
}

func cleanHeaderValue(v string) string {
	// Strip leading/trailing whitespace and replace line folding with single space
	v = strings.TrimSpace(v)
	v = strings.ReplaceAll(v, "\r\n", " ")
	v = strings.ReplaceAll(v, "\n", " ")
	v = strings.ReplaceAll(v, "\r", " ")

	return v
}

func extractDictionaryKey(dictHeader, key string) (string, error) {
	// RFC 8941 Dictionary member extraction: key=val or key=(inner list) or key
	items := strings.Split(dictHeader, ",")
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}

		eqIdx := strings.IndexByte(item, '=')
		if eqIdx == -1 {
			// Boolean true item (e.g. "d" -> "?1")
			if strings.EqualFold(item, key) {
				return "?1", nil
			}

			continue
		}

		k := strings.TrimSpace(item[:eqIdx])
		val := strings.TrimSpace(item[eqIdx+1:])

		if strings.EqualFold(k, key) {
			return val, nil
		}
	}

	return "", fmt.Errorf("%w: dictionary key %q not found in header", ErrMissingComponent, key)
}
