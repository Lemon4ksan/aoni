// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package parser

import (
	"strconv"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

// ApplyServiceDirective updates ServiceIR according to a parsed directive.
func ApplyServiceDirective(s *ir.ServiceIR, d *Directive) {
	if d == nil || s == nil {
		return
	}

	switch d.Name {
	case "aoni:service", "service":
		// Marker directive
	case "base_url":
		if d.Value != "" {
			s.BaseURL = d.Value
		}
	case "engine":
		switch strings.ToLower(d.Value) {
		case "fast":
			s.Engine = ir.EngineFast
		case "net/http", "std":
			s.Engine = ir.EngineNetHTTP
		case "required":
			s.Engine = ir.EngineRequired
		case "custom":
			s.Engine = ir.EngineCustom
			if c, ok := d.Args["type"]; ok {
				s.CustomEngine = c
				s.RequesterType = c
			}

			if _, ok := d.Args["required"]; ok {
				s.Engine = ir.EngineRequired
			}

		default:
			s.Engine = ir.EngineCustom
			s.CustomEngine = d.Value
			s.RequesterType = d.Value
		}

	case "requester":
		s.RequesterType = d.Value
		if typ, ok := d.Args["type"]; ok {
			s.RequesterType = typ
		}

		if _, ok := d.Args["required"]; ok {
			s.Engine = ir.EngineRequired
		}

	case "persona":
		s.Persona = d.Value
	case "tls_spec":
		s.TLSSpec = d.Value
	case "p0f":
		s.P0fOS = d.Value
	case "timeout":
		s.Timeout = d.Value
	case "header":
		s.Headers = append(s.Headers, ParseHeaderDirective(d.Value))
	case "retry":
		retry := &ir.RetryIR{}
		if attemptsStr, ok := d.Args["attempts"]; ok {
			if n, err := strconv.Atoi(attemptsStr); err == nil {
				retry.Attempts = n
			}
		}

		if backoff, ok := d.Args["backoff"]; ok {
			retry.Backoff = backoff
		}

		if jitter, ok := d.Args["jitter"]; ok {
			retry.Jitter = jitter
		}

		if onStr, ok := d.Args["on"]; ok {
			for _, st := range strings.Split(onStr, ",") {
				if code, err := strconv.Atoi(strings.TrimSpace(st)); err == nil {
					retry.OnStatus = append(retry.OnStatus, code)
				}
			}
		}

		s.Retry = retry

	case "circuit":
		cb := &ir.CircuitBreakerIR{}
		if th, ok := d.Args["threshold"]; ok {
			if n, err := strconv.Atoi(th); err == nil {
				cb.Threshold = n
			}
		}

		if cd, ok := d.Args["cooldown"]; ok {
			cb.Cooldown = cd
		}

		s.Circuit = cb

	case "envelope":
		s.Envelope = parseEnvelopeDirective(d)
	case "auth":
		s.AuthStrategy = parseAuthDirective(d)
	case "ssh":
		s.SSHConfig = parseSSHDirective(d)
		s.Protocol = ir.ProtocolSSH
	case "ws":
		if d.Value != "" {
			s.BaseURL = d.Value
		}

		s.Protocol = ir.ProtocolWS

	case "type_map":
		if s.TypeMaps == nil {
			s.TypeMaps = make(map[string]ir.FormatStrategy)
		}

		parseTypeMapDirective(s.TypeMaps, d)

	case "grpc":
		if d.Value != "" {
			s.BaseURL = d.Value
		}

		s.Protocol = ir.ProtocolGRPC
	}
}

// ApplyMethodDirective updates MethodIR according to a parsed directive.
func ApplyMethodDirective(m *ir.MethodIR, d *Directive) {
	if d == nil || m == nil {
		return
	}

	switch d.Name {
	case "call":
		m.CallFunc = d.Value
	case "get", "post", "put", "delete", "patch", "head", "options":
		m.HTTPMethod = strings.ToUpper(d.Name)

		m.Operation = ir.OpHTTP
		if d.Value != "" {
			m.Path = ParsePathTemplate(d.Value)
		}

	case "base_url":
		// Override for this specific method handled during Optimizer pass
		if d.Value != "" {
			m.TargetRequester = d.Value
		}
	case "codec":
		m.Codec = d.Value
		switch strings.ToLower(d.Value) {
		case "msgpack":
			m.PayloadKind = ir.PayloadKind("msgpack")
		case "xml":
			m.PayloadKind = ir.PayloadKind("xml")
		case "cbor":
			m.PayloadKind = ir.PayloadKind("cbor")
		case "yaml":
			m.PayloadKind = ir.PayloadKind("yaml")
		case "json":
			m.PayloadKind = ir.PayloadJSON
		case "form":
			m.PayloadKind = ir.PayloadForm
		case "proto":
			m.PayloadKind = ir.PayloadProto
		}

	case "decoder":
		if d.Value != "" {
			m.Decoder = d.Value
		} else if custom, ok := d.Args["custom"]; ok {
			m.Decoder = custom
		}

	case "encoder":
		if d.Value != "" {
			m.Encoder = d.Value
		} else if custom, ok := d.Args["custom"]; ok {
			m.Encoder = custom
		}

	case "extract":
		m.Extract = parseExtractDirective(d)
	case "idempotent", "idempotency_key":
		m.Idempotent = true
	case "coalesce":
		m.Coalesce = true
	case "etag":
		m.ETag = true
	case "sign":
		m.SignHMAC = parseSignDirective(d)
	case "multipart":
		m.PayloadKind = ir.PayloadMultipart
	case "form":
		m.PayloadKind = ir.PayloadForm
	case "json":
		m.PayloadKind = ir.PayloadJSON
	case "proto":
		m.PayloadKind = ir.PayloadProto
	case "grpc-web":
		m.PayloadKind = ir.PayloadGRPCWeb
	case "raw":
		m.PayloadKind = ir.PayloadRaw
	case "stream":
		switch strings.ToLower(d.Value) {
		case "sse":
			m.StreamKind = ir.StreamKindSSE
		case "ndjson":
			m.StreamKind = ir.StreamKindNDJSON
		case "raw", "bytes":
			m.StreamKind = ir.StreamKindRawBytes
		}

	case "ws:emit":
		m.Operation = ir.OpWSEmit
		m.EventName = d.Value
	case "ws:emit_ack":
		m.Operation = ir.OpWSEmitWithAck
		m.EventName = d.Value
	case "ws:on":
		m.Operation = ir.OpWSOn
		m.EventName = d.Value
	case "ssh:exec":
		m.Operation = ir.OpSSHExec
		m.SSHCommand = d.Value
	case "ssh:shell":
		m.Operation = ir.OpSSHShell
	case "header":
		m.Headers = append(m.Headers, ParseHeaderDirective(d.Value))
	case "referer":
		val := strings.TrimSpace(d.Value)
		if strings.HasPrefix(val, ":") {
			m.Headers = append(m.Headers, ir.HeaderIR{
				Key:         "Referer",
				StaticValue: val,
			})
		} else {
			m.Headers = append(m.Headers, ParseHeaderDirective("Referer: "+val))
		}

	case "preset":
		presetName := strings.ToLower(strings.TrimPrefix(d.Value, ":"))

		m.Presets = append(m.Presets, presetName)
		switch presetName {
		case "xhr", "ajax":
			m.Headers = append(m.Headers, ir.HeaderIR{Key: "X-Requested-With", StaticValue: "XMLHttpRequest"})
			m.Headers = append(
				m.Headers,
				ir.HeaderIR{Key: "Accept", StaticValue: "application/json, text/javascript, */*; q=0.01"},
			)

		case "cors":
			m.Headers = append(m.Headers, ir.HeaderIR{Key: "Sec-Fetch-Dest", StaticValue: "empty"})
			m.Headers = append(m.Headers, ir.HeaderIR{Key: "Sec-Fetch-Mode", StaticValue: "cors"})
			m.Headers = append(m.Headers, ir.HeaderIR{Key: "Sec-Fetch-Site", StaticValue: "same-origin"})
		case "navigate":
			m.Headers = append(m.Headers, ir.HeaderIR{Key: "Sec-Fetch-Dest", StaticValue: "document"})
			m.Headers = append(m.Headers, ir.HeaderIR{Key: "Sec-Fetch-Mode", StaticValue: "navigate"})
			m.Headers = append(m.Headers, ir.HeaderIR{Key: "Sec-Fetch-Site", StaticValue: "none"})
			m.Headers = append(m.Headers, ir.HeaderIR{Key: "Sec-Fetch-User", StaticValue: "?1"})
		}

	case "inject":
		inj := ir.InjectIR{
			Target: ir.InjectField,
		}

		if f, ok := d.Args["field"]; ok {
			inj.Target = ir.InjectField
			inj.WireKey = f
		} else if q, ok := d.Args["query"]; ok {
			inj.Target = ir.InjectQuery
			inj.WireKey = q
		} else if h, ok := d.Args["header"]; ok {
			inj.Target = ir.InjectHeader
			inj.WireKey = h
		} else if d.Value != "" {
			inj.WireKey = d.Value
		}

		if from, ok := d.Args["from"]; ok {
			inj.ProviderFn = from
		} else if inj.WireKey != "" {
			inj.ProviderFn = toPascalCase(inj.WireKey)
		}

		if inj.WireKey != "" {
			m.Injects = append(m.Injects, inj)
		}

	case "check":
		if chk := ParseCheckDirective(d.Value); chk != nil {
			m.Checks = append(m.Checks, *chk)
		}
	case "unwrap":
		m.UnwrapField = d.Value
	case "timeout":
		m.LocalTimeout = d.Value
	case "cache":
		if ttl, ok := d.Args["ttl"]; ok {
			m.LocalCacheTTL = ttl
		}
	case "expect_status":
		for _, st := range strings.Split(d.Value, ",") {
			if code, err := strconv.Atoi(strings.TrimSpace(st)); err == nil {
				m.ExpectStatus = append(m.ExpectStatus, code)
			}
		}

	case "error_model":
		m.Return = ensureReturnIR(m)
		m.Return.ErrorModelType = d.Value
	}
}

func ensureReturnIR(m *ir.MethodIR) *ir.ReturnIR {
	if m.Return == nil {
		m.Return = &ir.ReturnIR{}
	}

	return m.Return
}

func parseEnvelopeDirective(d *Directive) *ir.EnvelopeIR {
	env := &ir.EnvelopeIR{
		SuccessField: "Success",
		DataField:    "Data",
		ErrorField:   "Error",
	}

	if s, ok := d.Args["success"]; ok {
		env.SuccessField = s
	}

	if dat, ok := d.Args["data"]; ok {
		env.DataField = dat
	}

	if errStr, ok := d.Args["error"]; ok {
		env.ErrorField = errStr
	}

	return env
}

func parseAuthDirective(d *Directive) *ir.AuthStrategyIR {
	auth := &ir.AuthStrategyIR{
		Kind:        ir.AuthBearer,
		HeaderName:  "Authorization",
		ValuePrefix: "Bearer ",
	}

	if k, ok := d.Args["kind"]; ok {
		switch strings.ToLower(k) {
		case "static":
			auth.Kind = ir.AuthStatic
		case "bearer":
			auth.Kind = ir.AuthBearer
		case "oauth2":
			auth.Kind = ir.AuthOAuth2
		case "custom", "custom_provider":
			auth.Kind = ir.AuthCustomProvider
		}
	}

	if h, ok := d.Args["header"]; ok {
		auth.HeaderName = h
	}

	if p, ok := d.Args["prefix"]; ok {
		auth.ValuePrefix = p
	}

	if prov, ok := d.Args["provider"]; ok {
		auth.ProviderType = prov
	}

	return auth
}

func parseSSHDirective(d *Directive) *ir.SSHConfigIR {
	ssh := &ir.SSHConfigIR{}
	if h, ok := d.Args["host"]; ok {
		ssh.Host = h
	}

	if u, ok := d.Args["user"]; ok {
		ssh.User = u
	}

	if k, ok := d.Args["key"]; ok {
		ssh.KeyPath = k
	}

	if _, ok := d.Args["agent"]; ok {
		ssh.AgentAuth = true
	}

	if p, ok := d.Args["pass_env"]; ok {
		ssh.PassEnvVar = p
	}

	return ssh
}

func parseTypeMapDirective(tm map[string]ir.FormatStrategy, d *Directive) {
	raw := strings.TrimSpace(d.Raw)
	if strings.HasPrefix(raw, d.Name) {
		raw = strings.TrimSpace(raw[len(d.Name):])
	}

	raw = strings.Trim(raw, "\"")

	var from, to string
	switch {
	case strings.Contains(raw, "->"):
		parts := strings.SplitN(raw, "->", 2)
		from = strings.TrimSpace(parts[0])
		to = strings.TrimSpace(parts[1])
	case strings.Contains(raw, "="):
		parts := strings.SplitN(raw, "=", 2)
		from = strings.TrimSpace(parts[0])
		to = strings.TrimSpace(parts[1])
	default:
		for k, v := range d.Args {
			from = k
			to = v
			break
		}
	}

	if from != "" && to != "" {
		switch strings.ToLower(to) {
		case "unix_s", "unix":
			tm[from] = ir.FormatTimeUnixS
		case "unix_ms", "unix_milli":
			tm[from] = ir.FormatTimeUnixMS
		case "rfc3339":
			tm[from] = ir.FormatTimeRFC3339
		case "comma":
			tm[from] = ir.FormatSliceComma
		case "space":
			tm[from] = ir.FormatSliceSpace
		case "pipe":
			tm[from] = ir.FormatSlicePipe
		case "bracket":
			tm[from] = ir.FormatSliceBracket
		case "bool_int", "01", "10", "int":
			tm[from] = ir.FormatBoolInt
		case "flag", "bool_flag":
			tm[from] = ir.FormatBoolFlag
		default:
			tm[from] = ir.FormatCustomStringer
		}
	}
}

func parseExtractDirective(d *Directive) *ir.ExtractIR {
	if d == nil {
		return nil
	}

	ext := &ir.ExtractIR{}
	if rx, ok := d.Args["regex"]; ok {
		ext.Kind = ir.ExtractRegex
		ext.RegexPattern = rx
		return ext
	}

	if bet, ok := d.Args["between"]; ok {
		ext.Kind = ir.ExtractBetween

		ext.Prefix = bet
		if and, ok := d.Args["and"]; ok {
			ext.Suffix = and
		}

		return ext
	}

	if pref, ok := d.Args["prefix"]; ok {
		ext.Kind = ir.ExtractBetween

		ext.Prefix = pref
		if suff, ok := d.Args["suffix"]; ok {
			ext.Suffix = suff
		}

		return ext
	}

	if tag, ok := d.Args["tag"]; ok {
		ext.Kind = ir.ExtractHTMLToken
		ext.Tag = tag
		ext.ID = d.Args["id"]
		ext.Attr = d.Args["attr"]

		return ext
	}

	if css, ok := d.Args["css"]; ok {
		ext.Kind = ir.ExtractHTMLToken
		if strings.HasPrefix(css, "#") {
			ext.ID = strings.TrimPrefix(css, "#")
			ext.Tag = "div"
		} else {
			ext.Tag = css
		}

		ext.Attr = d.Args["attr"]

		return ext
	}

	if custom, ok := d.Args["custom"]; ok {
		ext.Kind = ir.ExtractCustom
		ext.CustomFunc = custom
		return ext
	}

	if d.Value != "" {
		ext.Kind = ir.ExtractCustom
		ext.CustomFunc = d.Value
		return ext
	}

	return nil
}

func parseSignDirective(d *Directive) *ir.SignHMACIR {
	if d == nil {
		return nil
	}

	sig := &ir.SignHMACIR{
		Algorithm:  "hmac_sha256",
		HeaderName: "X-Signature",
	}

	if d.Value != "" {
		sig.Algorithm = d.Value
	}

	if algo, ok := d.Args["algo"]; ok {
		sig.Algorithm = algo
	} else if algo, ok := d.Args["algorithm"]; ok {
		sig.Algorithm = algo
	}

	if key, ok := d.Args["key"]; ok {
		sig.SecretKey = key
	} else if secret, ok := d.Args["secret"]; ok {
		sig.SecretKey = secret
	}

	if env, ok := d.Args["key_env"]; ok {
		sig.KeyEnv = env
	} else if env, ok := d.Args["env"]; ok {
		sig.KeyEnv = env
	}

	if hdr, ok := d.Args["header"]; ok {
		sig.HeaderName = hdr
	}

	return sig
}

func toPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	})

	var res strings.Builder
	for _, p := range parts {
		if len(p) > 0 {
			res.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}

	return res.String()
}
