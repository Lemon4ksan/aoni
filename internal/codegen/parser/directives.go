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
		case "custom":
			s.Engine = ir.EngineCustom
			if c, ok := d.Args["type"]; ok {
				s.CustomEngine = c
			}
		default:
			s.Engine = ir.EngineCustom
			s.CustomEngine = d.Value
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
