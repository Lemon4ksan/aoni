// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package parser

import (
	"strconv"
	"strings"

	fheader "github.com/lemon4ksan/foundation/net/http/header"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/spec"
)

// IsKnownDirective reports whether a directive name is recognized by aoni-gen.
func IsKnownDirective(name string) bool {
	return spec.IsKnownDirective(name)
}

// ApplyServiceDirective updates ServiceIR according to a parsed directive.
func ApplyServiceDirective(s *ir.ServiceIR, d *Directive) {
	if d == nil || s == nil {
		return
	}

	switch d.Name {
	case "aoni:service", "service":
		if c, ok := d.Args["casing"]; ok {
			s.DefaultCasing = parseCasingStrategy(c)
		}
	case "casing":
		if d.Value != "" {
			s.DefaultCasing = parseCasingStrategy(d.Value)
		} else if c, ok := d.Args["style"]; ok {
			s.DefaultCasing = parseCasingStrategy(c)
		}

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

	case "protocol":
		switch strings.ToLower(d.Value) {
		case "rpc":
			s.Protocol = ir.ProtocolRPC
		case "channel":
			s.Protocol = ir.ProtocolChannel
		case "grpc":
			s.Protocol = ir.ProtocolGRPC
		case "ws", "websocket":
			s.Protocol = ir.ProtocolWS
		case "ssh":
			s.Protocol = ir.ProtocolSSH
		default:
			s.Protocol = ir.ProtocolHTTP
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
			for st := range strings.SplitSeq(onStr, ",") {
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
	case "unwrap":
		s.DefaultUnwrapField = d.Value
	case "error_model":
		s.DefaultErrorModel = d.Value
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

	case "aoni:mirror", "mirror":
		mirror := &ir.MirrorIR{}

		val := d.Value
		if val == "" {
			if src, ok := d.Args["source"]; ok {
				val = src
			}
		}

		if val != "" {
			lastColon := strings.LastIndex(val, ":")
			if lastColon != -1 && !strings.Contains(val[lastColon:], "/") && !strings.Contains(val[lastColon:], `\`) {
				mirror.Source = val[:lastColon]
				mirror.TargetType = val[lastColon+1:]
			} else {
				mirror.Source = val
			}
		}

		if target, ok := d.Args["target"]; ok {
			mirror.TargetType = target
		}

		if targetType, ok := d.Args["type"]; ok && mirror.TargetType == "" {
			mirror.TargetType = targetType
		}

		if strictStr, ok := d.Args["strict"]; ok {
			mirror.Strict = (strictStr == "true" || strictStr == "1")
		}

		s.Mirror = mirror

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

	case "aoni:socket", "socket":
		s.Protocol = ir.ProtocolSocket
		if s.SocketConfig == nil {
			s.SocketConfig = &ir.SocketConfigIR{}
		}

	case "telemetry":
		s.Telemetry = d.Value
		if s.Telemetry == "" {
			s.Telemetry = "all"
		}

	case "packet":
		if s.SocketConfig == nil {
			s.SocketConfig = &ir.SocketConfigIR{}
		}

		s.SocketConfig.PacketType = d.Value

	case "opcode":
		if s.SocketConfig == nil {
			s.SocketConfig = &ir.SocketConfigIR{}
		}

		s.SocketConfig.OpCodeType = d.Value

	case "job_id":
		if s.SocketConfig == nil {
			s.SocketConfig = &ir.SocketConfigIR{}
		}

		s.SocketConfig.JobIDType = d.Value

	case "endpoint":
		if s.SocketConfig == nil {
			s.SocketConfig = &ir.SocketConfigIR{}
		}

		s.SocketConfig.EndpointType = d.Value

	case "heartbeat":
		if s.SocketConfig == nil {
			s.SocketConfig = &ir.SocketConfigIR{}
		}

		hb := &ir.HeartbeatIR{
			Interval: d.Args["interval"],
			OpCode:   d.Args["op"],
			MsgType:  d.Args["msg"],
		}
		if hb.Interval == "" {
			hb.Interval = d.Value
		}

		s.SocketConfig.Heartbeat = hb

	case "deprecated":
		s.Deprecation = parseDeprecationDirective(d)
	case "summary":
		s.Summary = d.Value
	case "description":
		s.Description = d.Value
	case "tag", "tags":
		if d.Value != "" {
			s.Tags = append(s.Tags, d.Value)
		}
	case "version":
		s.Version = d.Value
	case "source":
		s.Source = d.Value
	}
}

// ApplyMethodDirective updates MethodIR according to a parsed directive.
func ApplyMethodDirective(m *ir.MethodIR, d *Directive) {
	if d == nil || m == nil {
		return
	}

	switch d.Name {
	case "op":
		m.OpID = d.Value

		m.OpIDIsQuoted = d.IsQuoted
		if m.IsNotify {
			m.Operation = ir.OpNotify
		} else {
			m.Operation = ir.OpRPC
		}

	case "notify":
		m.IsNotify = true
		m.Operation = ir.OpNotify
	case "event":
		m.OpID = d.Value
		m.OpIDIsQuoted = d.IsQuoted
		m.IsEvent = true
		m.Operation = ir.OpEvent
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
	case "return":
		if d.Pipeline != nil {
			m.ReturnPipeline = d.Pipeline
		} else if d.Value != "" {
			m.ReturnPipeline = ParsePipeline(d.Value)
		}

	case "body":
		if d.Pipeline != nil {
			m.BodyPipeline = d.Pipeline
		} else if d.Value != "" {
			m.BodyPipeline = ParsePipeline(d.Value)
		}

		if m.BodyPipeline != nil && len(m.BodyPipeline.Stages) > 0 {
			switch m.BodyPipeline.Stages[0].Type {
			case ir.StageJSON:
				m.PayloadKind = ir.PayloadJSON
			case ir.StageForm:
				m.PayloadKind = ir.PayloadForm
			case ir.StageProto:
				m.PayloadKind = ir.PayloadProto
			default:
				m.PayloadKind = ir.PayloadRaw
			}
		}

	case "extract":
		if d.Value != "" {
			m.ReturnPipeline = ParsePipeline("body | " + d.Value)
		} else if rx, ok := d.Args["regex"]; ok {
			m.ReturnPipeline = ParsePipeline("body | regex(" + strconv.Quote(rx) + ") | json")
		} else if bet, ok := d.Args["between"]; ok {
			suff := d.Args["and"]
			if suff == "" {
				suff = d.Args["suffix"]
			}

			m.ReturnPipeline = ParsePipeline(
				"body | between(prefix=" + strconv.Quote(bet) + ", suffix=" + strconv.Quote(suff) + ") | json",
			)
		} else if css, ok := d.Args["css"]; ok {
			attr := d.Args["attr"]
			m.ReturnPipeline = ParsePipeline(
				"body | attr(css=" + strconv.Quote(css) + ", name=" + strconv.Quote(attr) + ") | html_unescape | json",
			)
		} else if custom, ok := d.Args["custom"]; ok {
			m.ReturnPipeline = ParsePipeline("body | " + custom)
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

	case "idempotent", "idempotency_key":
		m.Idempotent = true
	case "coalesce":
		m.Coalesce = true
	case "etag":
		m.ETag = true
	case "telemetry":
		m.Telemetry = d.Value
		if m.Telemetry == "" {
			m.Telemetry = "all"
		}
	case "label":
		m.Label = d.Value
	case "sign":
		m.SignHMAC = parseSignDirective(d)
	case "multipart":
		m.PayloadKind = ir.PayloadMultipart
	case "query":
		if c, ok := d.Args["casing"]; ok {
			m.QueryCasing = parseCasingStrategy(c)
		}
	case "form":
		m.PayloadKind = ir.PayloadForm
		if c, ok := d.Args["casing"]; ok {
			m.FormCasing = parseCasingStrategy(c)
		}
	case "casing":
		strategy := parseCasingStrategy(d.Value)
		if strategy == "" {
			if c, ok := d.Args["style"]; ok {
				strategy = parseCasingStrategy(c)
			}
		}

		m.FormCasing = strategy
		m.QueryCasing = strategy

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
				ir.HeaderIR{Key: fheader.Accept, StaticValue: "application/json, text/javascript, */*; q=0.01"},
			)

		case "cors":
			m.Headers = append(m.Headers, ir.HeaderIR{Key: fheader.SecFetchDest, StaticValue: "empty"})
			m.Headers = append(m.Headers, ir.HeaderIR{Key: fheader.SecFetchMode, StaticValue: "cors"})
			m.Headers = append(m.Headers, ir.HeaderIR{Key: fheader.SecFetchSite, StaticValue: "same-origin"})
		case "navigate":
			m.Headers = append(m.Headers, ir.HeaderIR{Key: fheader.SecFetchDest, StaticValue: "document"})
			m.Headers = append(m.Headers, ir.HeaderIR{Key: fheader.SecFetchMode, StaticValue: "navigate"})
			m.Headers = append(m.Headers, ir.HeaderIR{Key: fheader.SecFetchSite, StaticValue: "none"})
			m.Headers = append(m.Headers, ir.HeaderIR{Key: fheader.SecFetchUser, StaticValue: "?1"})
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
		for st := range strings.SplitSeq(d.Value, ",") {
			for _, part := range strings.Fields(st) {
				if code, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
					m.ExpectStatus = append(m.ExpectStatus, code)
				}
			}
		}

	case "status":
		m.Return = ensureReturnIR(m)
		if m.Return.StatusMap == nil {
			m.Return.StatusMap = make(map[int]ir.GoTypeIR)
		}

		parts := strings.Split(d.Value, "=>")
		if len(parts) == 2 {
			codesStr := strings.TrimSpace(parts[0])

			targetType := strings.TrimSpace(parts[1])
			for cStr := range strings.SplitSeq(codesStr, ",") {
				for _, part := range strings.Fields(cStr) {
					if code, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
						m.Return.StatusMap[code] = ir.GoTypeIR{Name: targetType, IsCustomType: true}
					}
				}
			}
		}

	case "error_model":
		m.Return = ensureReturnIR(m)
		m.Return.ErrorModelType = d.Value

	case "deprecated":
		m.Deprecation = parseDeprecationDirective(d)
	case "bind", "op_id", "operation_id":
		m.OperationID = d.Value
	case "summary":
		m.Summary = d.Value
	case "description":
		m.Description = d.Value
	case "tag", "tags":
		if d.Value != "" {
			m.Tags = append(m.Tags, d.Value)
		}
	case "version":
		m.Version = d.Value
	case "since":
		m.Since = d.Value
	case "bench":
		if wStr, ok := d.Args["weight"]; ok {
			if w, err := strconv.Atoi(wStr); err == nil {
				m.BenchWeight = w
			}
		} else if d.Value != "" {
			if w, err := strconv.Atoi(d.Value); err == nil {
				m.BenchWeight = w
			}
		}

	case "mock:fixture", "fixture":
		fixture := &ir.MockFixtureIR{
			StatusCode:  200,
			ContentType: "application/json",
			Headers:     make(map[string]string),
		}
		if stStr, ok := d.Args["status"]; ok {
			if st, err := strconv.Atoi(stStr); err == nil {
				fixture.StatusCode = st
			}
		}

		if ct, ok := d.Args["type"]; ok {
			fixture.ContentType = ct
		}

		if body, ok := d.Args["body"]; ok {
			fixture.Body = body
		} else if d.Value != "" {
			fixture.Body = d.Value
		}

		m.MockFixture = fixture

	case "budget":
		if allocStr, ok := d.Args["client_allocs"]; ok {
			if allocs, err := strconv.Atoi(allocStr); err == nil {
				m.BudgetClientAllocs = &allocs
			}
		}

		if maxTime, ok := d.Args["max_client_time"]; ok {
			m.BudgetMaxClientTime = strings.Trim(maxTime, "\"")
		}
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
		HeaderName:  fheader.Authorization,
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

// ParsePipeline parses a Wire-Transform pipeline expression like:
//
//	"body | regex(`...`) | json"
//	"body | attr(css=\"#id\", name=\"data-attr\") | html_unescape | json"
//	"json | gzip | base64_url"
//	"proto | FrameVT01 | EncryptSteam(c.sessionKey)"
func ParsePipeline(raw string) *ir.PipelineIR {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	segments := splitPipelineSegments(raw)
	if len(segments) == 0 {
		return nil
	}

	pipeline := &ir.PipelineIR{}

	startIdx := 0
	first := strings.TrimSpace(segments[0])

	if first == "body" {
		pipeline.Source = "body"
		startIdx = 1
	} else if strings.HasPrefix(first, "header(") && strings.HasSuffix(first, ")") {
		pipeline.Source = "header"
		pipeline.SourceArg = strings.Trim(first[7:len(first)-1], "\"'` ")
		startIdx = 1
	}

	for i := startIdx; i < len(segments); i++ {
		seg := strings.TrimSpace(segments[i])
		if seg == "" {
			continue
		}

		stage := parsePipelineStage(seg)
		pipeline.Stages = append(pipeline.Stages, stage)
	}

	if len(pipeline.Stages) == 0 && pipeline.Source != "" {
		pipeline.Stages = append(pipeline.Stages, ir.PipelineStageIR{
			Type:    ir.StageJSON,
			RawName: "json",
		})
	}

	return pipeline
}

func splitPipelineSegments(s string) []string {
	var (
		segments  []string
		cur       strings.Builder
		inQuote   bool
		quoteChar byte
		parenNest int
	)

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

		if b == '(' {
			parenNest++

			cur.WriteByte(b)

			continue
		}

		if b == ')' {
			if parenNest > 0 {
				parenNest--
			}

			cur.WriteByte(b)

			continue
		}

		if b == '|' && parenNest == 0 {
			segments = append(segments, cur.String())
			cur.Reset()
			continue
		}

		cur.WriteByte(b)
	}

	if cur.Len() > 0 {
		segments = append(segments, cur.String())
	}

	return segments
}

func parsePipelineStage(seg string) ir.PipelineStageIR {
	seg = strings.TrimSpace(seg)
	stage := ir.PipelineStageIR{
		RawName:   seg,
		NamedArgs: make(map[string]string),
	}

	if strings.HasPrefix(seg, "custom=") || strings.HasPrefix(seg, "fn=") {
		parts := strings.SplitN(seg, "=", 2)
		stage.Type = ir.StageCustom
		stage.FuncExpr = unquote(strings.TrimSpace(parts[1]))
		stage.RawName = stage.FuncExpr

		return stage
	}

	idx := strings.IndexByte(seg, '(')
	if idx == -1 {
		stage.RawName = seg
		stage.FuncExpr = seg
		stage.Type = matchPipelineStageType(seg)

		return stage
	}

	name := strings.TrimSpace(seg[:idx])
	stage.RawName = name
	stage.FuncExpr = name
	stage.Type = matchPipelineStageType(name)

	argsStr := strings.TrimSuffix(strings.TrimSpace(seg[idx+1:]), ")")
	args := tokenizeArgs(argsStr)

	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}

		if !strings.HasPrefix(arg, "\"") && !strings.HasPrefix(arg, "'") && !strings.HasPrefix(arg, "`") &&
			strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			k := strings.ToLower(strings.TrimSpace(parts[0]))
			v := unquote(strings.TrimSpace(parts[1]))
			stage.NamedArgs[k] = v
		} else {
			stage.Args = append(stage.Args, unquote(arg))
		}
	}

	return stage
}

func matchPipelineStageType(name string) ir.PipelineStageType {
	switch strings.ToLower(name) {
	case "json":
		return ir.StageJSON
	case "proto":
		return ir.StageProto
	case "form":
		return ir.StageForm
	case "xml":
		return ir.StageXML
	case "cbor":
		return ir.StageCBOR
	case "msgpack":
		return ir.StageMsgPack
	case "tuple":
		return ir.StageTuple
	case "gzip":
		return ir.StageGzip
	case "gunzip":
		return ir.StageGunzip
	case "zstd":
		return ir.StageZstd
	case "zstd_decompress":
		return ir.StageZstdDecompress
	case "deflate", "flate":
		return ir.StageDeflate
	case "inflate":
		return ir.StageInflate
	case "snappy":
		return ir.StageSnappy
	case "base64":
		return ir.StageBase64
	case "base64_decode":
		return ir.StageBase64Decode
	case "base64_url":
		return ir.StageBase64URL
	case "base64_url_decode":
		return ir.StageBase64URLDecode
	case "hex":
		return ir.StageHex
	case "hex_decode":
		return ir.StageHexDecode
	case "url_escape", "escape":
		return ir.StageURLEscape
	case "url_unescape", "unescape":
		return ir.StageURLUnescape
	case "html_escape":
		return ir.StageHTMLEscape
	case "html_unescape":
		return ir.StageHTMLUnescape
	case "regex":
		return ir.StageRegex
	case "between":
		return ir.StageBetween
	case "attr":
		return ir.StageAttr
	case "hmac_sha256":
		return ir.StageHMACSHA256
	default:
		return ir.StageCustom
	}
}

func parseCasingStrategy(c string) ir.CasingStrategy {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "camel_case", "camelcase":
		return ir.CasingCamelCase
	case "pascal_case", "pascalcase":
		return ir.CasingPascalCase
	case "kebab_case", "kebabcase", "kebab":
		return ir.CasingKebabCase
	case "flat_case", "flatcase", "lower", "lowercase":
		return ir.CasingFlatCase
	case "none", "raw":
		return ir.CasingNone
	case "snake_case", "snakecase", "snake":
		return ir.CasingSnakeCase
	default:
		return ir.CasingSnakeCase
	}
}

func parseDeprecationDirective(d *Directive) *ir.DeprecationIR {
	if d == nil {
		return nil
	}

	dep := &ir.DeprecationIR{
		Reason:      d.Value,
		Replacement: d.Args["replace"],
		Since:       d.Args["since"],
		Deadline:    d.Args["deadline"],
	}
	if r, ok := d.Args["reason"]; ok && r != "" {
		dep.Reason = r
	}

	if dep.Replacement == "" {
		dep.Replacement = d.Args["with"]
	}

	return dep
}
