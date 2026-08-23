// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analysis

import (
	"net/http"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

// RuleServiceNameNotEmpty ensures every service interface has a declared name.
func RuleServiceNameNotEmpty(ctx *Context, svc *ir.ServiceIR) {
	if svc.Name == "" {
		ctx.Error("service/empty-name", "service", "service name cannot be empty")
	}
}

// RuleServiceMethodsDeclared warns if a service interface declares no methods.
func RuleServiceMethodsDeclared(ctx *Context, svc *ir.ServiceIR) {
	if len(svc.Methods) == 0 {
		ctx.Warn("service/no-methods", svc.Name, "service interface has no declared methods")
	}
}

// RuleServiceUniqueMethodNames guards against duplicate method names within the same service.
func RuleServiceUniqueMethodNames(ctx *Context, svc *ir.ServiceIR) {
	seen := make(map[string]bool, len(svc.Methods))
	for _, m := range svc.Methods {
		if seen[m.Name] {
			ctx.Errorf(
				"service/duplicate-method",
				ctx.Target(svc.Name, m.Name),
				"duplicate method name %q in service %s",
				m.Name,
				svc.Name,
			)
		}

		seen[m.Name] = true
	}
}

// RuleServiceDurations validates timeout, backoff, jitter, and cooldown duration format strings.
func RuleServiceDurations(ctx *Context, svc *ir.ServiceIR) {
	if svc.Timeout != "" {
		if err := validateDuration(svc.Timeout); err != nil {
			ctx.Errorf("service/invalid-timeout", svc.Name, "invalid service timeout %q: %v", svc.Timeout, err)
		}
	}

	if svc.Circuit != nil && svc.Circuit.Cooldown != "" {
		if err := validateDuration(svc.Circuit.Cooldown); err != nil {
			ctx.Errorf(
				"service/invalid-cooldown",
				svc.Name,
				"invalid circuit breaker cooldown %q: %v",
				svc.Circuit.Cooldown,
				err,
			)
		}
	}

	if svc.Retry != nil {
		if svc.Retry.Backoff != "" {
			if err := validateDuration(svc.Retry.Backoff); err != nil {
				ctx.Errorf("service/invalid-backoff", svc.Name, "invalid retry backoff %q: %v", svc.Retry.Backoff, err)
			}
		}

		if svc.Retry.Jitter != "" {
			if err := validateDuration(svc.Retry.Jitter); err != nil {
				ctx.Errorf("service/invalid-jitter", svc.Name, "invalid retry jitter %q: %v", svc.Retry.Jitter, err)
			}
		}
	}
}

// RuleServiceRetryStatus validates that retry onStatus codes fall within standard HTTP bounds (100-599).
func RuleServiceRetryStatus(ctx *Context, svc *ir.ServiceIR) {
	if svc.Retry == nil {
		return
	}

	for _, sc := range svc.Retry.OnStatus {
		if sc < 100 || sc > 599 {
			ctx.Errorf(
				"service/invalid-retry-status",
				svc.Name,
				"invalid retry onStatus code %d (must be between 100 and 599)",
				sc,
			)
		}
	}
}

// RuleMethodHTTPDirective verifies that HTTP operations specify a valid verb (@get, @post, etc.).
func RuleMethodHTTPDirective(ctx *Context, svc *ir.ServiceIR, m *ir.MethodIR) {
	if m.Operation == ir.OpHTTP && m.HTTPMethod == "" {
		ctx.Error(
			"method/missing-http-verb",
			ctx.Target(svc.Name, m.Name),
			"missing HTTP method directive (e.g. @get, @post, @put, @delete)",
		)
	}
}

// RuleMethodContextParameter verifies that context.Context is positioned as the first parameter.
func RuleMethodContextParameter(ctx *Context, svc *ir.ServiceIR, m *ir.MethodIR) {
	if m.Operation == ir.OpWSOn || m.Operation == ir.OpEvent || m.Operation == ir.OpClose || m.IsEvent {
		return
	}

	if len(m.Params) == 0 || m.Params[0].Location != ir.LocContext {
		ctx.Error(
			"method/missing-context",
			ctx.Target(svc.Name, m.Name),
			"first method parameter must be context.Context",
		)
	}
}

// RuleMethodUniqueParamNames guards against duplicate parameter names in a generated method signature.
func RuleMethodUniqueParamNames(ctx *Context, svc *ir.ServiceIR, m *ir.MethodIR) {
	seen := make(map[string]bool, len(m.Params))

	for _, p := range m.Params {
		if p.GoName == "" {
			continue
		}

		if seen[p.GoName] {
			ctx.Errorf(
				"method/duplicate-param",
				ctx.Target(svc.Name, m.Name),
				"duplicate parameter name %q in method signature",
				p.GoName,
			)
		}

		seen[p.GoName] = true
	}
}

// RuleMethodWireKeys ensures Query, Header, and Cookie parameters specify non-empty wire serialization keys.
func RuleMethodWireKeys(ctx *Context, svc *ir.ServiceIR, m *ir.MethodIR) {
	for _, p := range m.Params {
		switch p.Location {
		case ir.LocQuery, ir.LocHeader, ir.LocCookie:
			if p.WireKey == "" {
				ctx.Errorf(
					"method/empty-wire-key",
					ctx.Target(svc.Name, m.Name),
					"parameter %q at location %s requires a non-empty wire key",
					p.GoName,
					p.Location,
				)
			}
		}
	}
}

// RuleMethodPathVariables ensures every {var} placeholder in URL path template matches a method parameter.
func RuleMethodPathVariables(ctx *Context, svc *ir.ServiceIR, m *ir.MethodIR) {
	if m.Path == nil {
		return
	}

	paramNames := collectParamNames(m.Params)

	for _, seg := range m.Path.Segments {
		cleanSeg := cleanIdentifier(seg.VarName)
		if seg.IsVariable && !paramNames[seg.VarName] && !paramNames[strings.ToLower(seg.VarName)] &&
			!paramNames[cleanSeg] {
			ctx.Errorf(
				"method/path-param-mismatch",
				ctx.Target(svc.Name, m.Name),
				"path variable {%s} does not match any method parameter",
				seg.VarName,
			)
		}
	}
}

// RuleMethodDynamicHeaders ensures every {var} placeholder in header templates matches a method parameter.
func RuleMethodDynamicHeaders(ctx *Context, svc *ir.ServiceIR, m *ir.MethodIR) {
	paramNames := collectParamNames(m.Params)

	for _, h := range m.Headers {
		if h.DynamicTemplate == nil {
			continue
		}

		for _, seg := range h.DynamicTemplate.Segments {
			if !seg.IsVariable {
				continue
			}

			rootVar := strings.Split(seg.VarName, ".")[0]
			if paramNames[seg.VarName] || paramNames[strings.ToLower(seg.VarName)] ||
				paramNames[rootVar] || paramNames[strings.ToLower(rootVar)] {
				continue
			}

			ctx.Errorf(
				"method/dynamic-header-mismatch",
				ctx.Target(svc.Name, m.Name),
				"dynamic header {%s} variable does not match any method parameter",
				seg.VarName,
			)
		}
	}
}

// RuleMethodReturnSignature verifies that a method defines a non-empty return contract.
func RuleMethodReturnSignature(ctx *Context, svc *ir.ServiceIR, m *ir.MethodIR) {
	if m.Return == nil {
		ctx.Error(
			"method/empty-return",
			ctx.Target(svc.Name, m.Name),
			"method return signature cannot be empty (must return at least error)",
		)
	}
}

// RuleMethodBodyPayloadLimit enforces that a method declares at most one request body parameter.
func RuleMethodBodyPayloadLimit(ctx *Context, svc *ir.ServiceIR, m *ir.MethodIR) {
	bodyCount := 0
	for _, p := range m.Params {
		if p.Location == ir.LocBody {
			bodyCount++
		}
	}

	if bodyCount > 1 {
		ctx.Errorf(
			"method/multiple-bodies",
			ctx.Target(svc.Name, m.Name),
			"method has %d request body parameters; at most 1 is allowed",
			bodyCount,
		)
	}
}

// RuleMethodHTTPPayloadSemantics warns if request body is attached to GET, HEAD, or DELETE requests (RFC 9110 §9.3.1).
func RuleMethodHTTPPayloadSemantics(ctx *Context, svc *ir.ServiceIR, m *ir.MethodIR) {
	httpMethod := strings.ToUpper(m.HTTPMethod)

	if httpMethod == http.MethodGet || httpMethod == http.MethodHead || httpMethod == http.MethodDelete {
		for _, p := range m.Params {
			if p.Location == ir.LocBody {
				ctx.Warnf(
					"method/rfc9110-body-discouraged",
					ctx.Target(svc.Name, m.Name),
					"HTTP %s with request body is discouraged (RFC 9110 §9.3.1); intermediate proxies and CDNs may drop the payload",
					httpMethod,
				)

				break
			}
		}
	}
}

// RuleMethodStatusAndDurations validates expected HTTP status codes and local timeouts/TTLs.
func RuleMethodStatusAndDurations(ctx *Context, svc *ir.ServiceIR, m *ir.MethodIR) {
	for _, sc := range m.ExpectStatus {
		if sc < 100 || sc > 599 {
			ctx.Errorf(
				"method/invalid-expected-status",
				ctx.Target(svc.Name, m.Name),
				"invalid expected status code %d (must be between 100 and 599)",
				sc,
			)
		}
	}

	if m.LocalTimeout != "" {
		if err := validateDuration(m.LocalTimeout); err != nil {
			ctx.Errorf(
				"method/invalid-timeout",
				ctx.Target(svc.Name, m.Name),
				"invalid local timeout %q: %v",
				m.LocalTimeout,
				err,
			)
		}
	}

	if m.LocalCacheTTL != "" {
		if err := validateDuration(m.LocalCacheTTL); err != nil {
			ctx.Errorf(
				"method/invalid-cache-ttl",
				ctx.Target(svc.Name, m.Name),
				"invalid local cache TTL %q: %v",
				m.LocalCacheTTL,
				err,
			)
		}
	}
}

func validateDuration(d string) error {
	_, err := time.ParseDuration(d)
	return err
}

func collectParamNames(params []*ir.ParamIR) map[string]bool {
	paramNames := make(map[string]bool, len(params)*4)
	for _, p := range params {
		paramNames[p.GoName] = true
		paramNames[strings.ToLower(p.GoName)] = true
		paramNames[cleanIdentifier(p.GoName)] = true

		if p.WireKey != "" {
			paramNames[p.WireKey] = true
			paramNames[strings.ToLower(p.WireKey)] = true
			paramNames[cleanIdentifier(p.WireKey)] = true
		}
	}

	return paramNames
}

func cleanIdentifier(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}

	return strings.ToLower(b.String())
}
