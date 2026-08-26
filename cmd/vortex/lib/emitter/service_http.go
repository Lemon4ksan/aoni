// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package emitter

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

func emitHTTPService(buf *bytes.Buffer, tracker *ImportTracker, root *ir.RootIR, svc *ir.ServiceIR) {
	if svc.Protocol == ir.ProtocolSocket {
		emitSocketService(buf, tracker, root, svc)
		return
	}

	clientStructName := lowerFirst(svc.Name) + "Client"

	if svc.Protocol == ir.ProtocolRPC || svc.Protocol == ir.ProtocolChannel {
		emitRPCService(buf, tracker, root, svc, clientStructName)
		return
	}

	isStrict := svc.Engine == ir.EngineRequired || svc.RequesterType != ""
	if isStrict {
		emitStrictService(buf, tracker, root, svc, clientStructName)
		return
	}

	emitStandardService(buf, tracker, root, svc, clientStructName)
}

func emitRPCService(
	buf *bytes.Buffer,
	tracker *ImportTracker,
	root *ir.RootIR,
	svc *ir.ServiceIR,
	clientStructName string,
) {
	reqArgType := "request.Transport"
	if svc.RequesterType != "" {
		reqArgType = svc.RequesterType
	}

	hasEvents := false

	hasExplicitClose := false
	for _, m := range svc.Methods {
		if m.IsEvent || m.Operation == ir.OpEvent {
			hasEvents = true
		}

		if m.Name == "Close" {
			hasExplicitClose = true
		}
	}

	fmt.Fprintf(buf, "type %s struct {\n", clientStructName)
	fmt.Fprintf(buf, "\ttransport %s\n", reqArgType)

	if hasEvents || hasExplicitClose {
		tracker.Add("sync")
		buf.WriteString("\tmu        sync.Mutex\n")
		buf.WriteString("\tunregs    []func()\n")
	}

	buf.WriteString("}\n\n")

	constructorName := "New" + svc.Name
	if svc.Name == "Client" || svc.Name == "API" {
		constructorName = "New"
	}

	mustConstructorName := "Must" + constructorName

	fmt.Fprintf(
		buf,
		"// %s creates a new %s client instance backed by a transport.\n",
		constructorName,
		svc.Name,
	)
	fmt.Fprintf(
		buf,
		"func %s(transport %s) %s {\n",
		constructorName,
		reqArgType,
		svc.Name,
	)
	fmt.Fprintf(buf, "\treturn &%s{\n\t\ttransport: transport,\n\t}\n}\n\n", clientStructName)

	fmt.Fprintf(
		buf,
		"// %s initializes %s.\n",
		mustConstructorName,
		svc.Name,
	)
	fmt.Fprintf(
		buf,
		"func %s(transport %s) %s {\n",
		mustConstructorName,
		reqArgType,
		svc.Name,
	)
	fmt.Fprintf(buf, "\treturn %s(transport)\n}\n\n", constructorName)

	for _, m := range svc.Methods {
		if m.Name != "Close" {
			emitMethodRouter(buf, tracker, root, svc, clientStructName, m)
		}
	}

	if hasEvents || hasExplicitClose {
		fmt.Fprintf(buf, "// Close unsubscribes all active event listeners.\n")
		fmt.Fprintf(buf, "func (c *%s) Close() error {\n", clientStructName)
		buf.WriteString("\tc.mu.Lock()\n")
		buf.WriteString("\tfor _, unreg := range c.unregs {\n")
		buf.WriteString("\t\tunreg()\n")
		buf.WriteString("\t}\n")
		buf.WriteString("\tc.unregs = nil\n")
		buf.WriteString("\tc.mu.Unlock()\n")
		buf.WriteString("\treturn nil\n")
		buf.WriteString("}\n\n")
	}
}

func emitStrictService(
	buf *bytes.Buffer,
	tracker *ImportTracker,
	root *ir.RootIR,
	svc *ir.ServiceIR,
	clientStructName string,
) {
	tracker.Add("errors")
	tracker.Add("github.com/lemon4ksan/aoni")
	tracker.Add("github.com/lemon4ksan/aoni/option")

	// 1. Client Struct
	fmt.Fprintf(buf, "type %s struct {\n", clientStructName)

	for _, sub := range svc.SubRequesters {
		fmt.Fprintf(buf, "\t%s *aoni.Client\n", sub.FieldName)
	}

	buf.WriteString("}\n\n")

	// 2. Factory Constructor
	constructorName := "New" + svc.Name
	mustConstructorName := "Must" + constructorName

	reqArgType := "aoni.RequestDoer"
	if svc.RequesterType != "" {
		reqArgType = svc.RequesterType
	}

	fmt.Fprintf(
		buf,
		"// %s creates a new %s client instance backed by an authenticated %s.\n",
		constructorName,
		svc.Name,
		reqArgType,
	)
	fmt.Fprintf(
		buf,
		"func %s(client %s, opts ...aoni.ClientOption) (%s, error) {\n",
		constructorName,
		reqArgType,
		svc.Name,
	)
	fmt.Fprintf(
		buf,
		"\tif client == nil {\n\t\treturn nil, errors.New(\"aoni: client (%s) is required to initialize %s\")\n\t}\n\n",
		reqArgType,
		svc.Name,
	)

	emitBaseOpts(buf, svc)

	// Target requester resolution
	fmt.Fprintf(
		buf,
		"\ttargetReq := aoni.NewClient(client, append([]aoni.ClientOption{option.WithBaseURL(%q)}, baseOpts...)...)\n\n",
		svc.BaseURL,
	)

	// Instantiate SubRequesters
	fmt.Fprintf(buf, "\treturn &%s{\n", clientStructName)

	for _, sub := range svc.SubRequesters {
		fmt.Fprintf(buf, "\t\t%s: targetReq,\n", sub.FieldName)
	}

	buf.WriteString("\t}, nil\n}\n\n")

	// MustNew helper
	fmt.Fprintf(
		buf,
		"// %s initializes %s and panics if an error occurs.\n",
		mustConstructorName,
		svc.Name,
	)
	fmt.Fprintf(
		buf,
		"func %s(client %s, opts ...aoni.ClientOption) %s {\n",
		mustConstructorName,
		reqArgType,
		svc.Name,
	)
	fmt.Fprintf(buf, "\tapi, err := %s(client, opts...)\n", constructorName)
	buf.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	buf.WriteString("\treturn api\n}\n\n")

	// Underlying requester getter
	fmt.Fprintf(buf, "// R returns the underlying *aoni.Client used by the client.\n")
	fmt.Fprintf(buf, "func (c *%s) R() *aoni.Client {\n\treturn c.r\n}\n\n", clientStructName)

	// Methods
	for _, m := range svc.Methods {
		emitMethodRouter(buf, tracker, root, svc, clientStructName, m)
	}
}

func emitStandardService(
	buf *bytes.Buffer,
	tracker *ImportTracker,
	root *ir.RootIR,
	svc *ir.ServiceIR,
	clientStructName string,
) {
	tracker.Add("github.com/lemon4ksan/aoni")
	tracker.Add("github.com/lemon4ksan/aoni/option")

	// 1. Client Struct
	fmt.Fprintf(buf, "type %s struct {\n", clientStructName)

	for _, sub := range svc.SubRequesters {
		fmt.Fprintf(buf, "\t%s *aoni.Client\n", sub.FieldName)
	}

	buf.WriteString("}\n\n")

	constructorName := "New" + svc.Name
	internalConstructorName := "new" + svc.Name

	fmt.Fprintf(buf, "func %s(doer any, opts ...aoni.ClientOption) *%s {\n", internalConstructorName, clientStructName)
	buf.WriteString("\tif doer == nil {\n")

	switch svc.Engine {
	case ir.EngineNetHTTP:
		tracker.Add("github.com/lemon4ksan/aoni")
		buf.WriteString("\t\tdoer = aoni.NewClient(nil)\n")
	case ir.EngineCustom:
		fmt.Fprintf(buf, "\t\tpanic(\"aoni: custom RequestDoer is required to initialize %s\")\n", svc.Name)
	default:
		tracker.Add("github.com/lemon4ksan/aoni/fast")
		buf.WriteString("\t\tdoer = fast.NewClient()\n")
	}

	buf.WriteString("\t}\n\n")

	emitBaseOpts(buf, svc)

	// Target requester resolution
	fmt.Fprintf(
		buf,
		"\ttargetReq := aoni.NewClient(doer, append([]aoni.ClientOption{option.WithBaseURL(%q)}, baseOpts...)...)\n\n",
		svc.BaseURL,
	)

	// Instantiate SubRequesters
	fmt.Fprintf(buf, "\treturn &%s{\n", clientStructName)

	for _, sub := range svc.SubRequesters {
		baseURL := sub.BaseURL
		if baseURL == "" {
			baseURL = svc.BaseURL
		}

		if sub.FieldName == "r" && baseURL == svc.BaseURL {
			fmt.Fprintf(buf, "\t\t%s: targetReq,\n", sub.FieldName)
		} else {
			tracker.Add("github.com/lemon4ksan/aoni/fast")
			fmt.Fprintf(
				buf,
				"\t\t%s: aoni.NewClient(fast.NewClient(), append([]aoni.ClientOption{option.WithBaseURL(%q)}, baseOpts...)...),\n",
				sub.FieldName,
				baseURL,
			)
		}
	}

	buf.WriteString("\t}\n}\n\n")

	// Public constructor
	fmt.Fprintf(
		buf,
		"// %s creates a new %s client instance with preconfigured execution pipelines.\n",
		constructorName,
		svc.Name,
	)
	fmt.Fprintf(buf, "func %s(doer any, opts ...aoni.ClientOption) %s {\n", constructorName, svc.Name)
	fmt.Fprintf(buf, "\treturn %s(doer, opts...)\n}\n\n", internalConstructorName)

	if svc.Name == "API" || svc.Name == "Client" {
		aliasName := "New"
		fmt.Fprintf(
			buf,
			"// %s creates a new %s client instance (alias for %s).\n",
			aliasName,
			svc.Name,
			constructorName,
		)
		fmt.Fprintf(buf, "func %s(doer any, opts ...aoni.ClientOption) %s {\n", aliasName, svc.Name)
		fmt.Fprintf(buf, "\treturn %s(doer, opts...)\n}\n\n", internalConstructorName)
	}

	// Underlying requester getter
	fmt.Fprintf(buf, "// R returns the underlying *aoni.Client used by the client.\n")
	fmt.Fprintf(buf, "func (c *%s) R() *aoni.Client {\n\treturn c.r\n}\n\n", clientStructName)

	// 3. Methods
	for _, m := range svc.Methods {
		emitMethodRouter(buf, tracker, root, svc, clientStructName, m)
	}
}

func emitBaseOpts(buf *bytes.Buffer, svc *ir.ServiceIR) {
	buf.WriteString("\tvar baseOpts []aoni.ClientOption\n")

	for _, h := range svc.Headers {
		if h.DynamicTemplate == nil && h.StaticValue != "" {
			if strings.EqualFold(h.Key, "User-Agent") {
				fmt.Fprintf(buf, "\tbaseOpts = append(baseOpts, option.WithUserAgent(%q))\n", h.StaticValue)
			} else {
				fmt.Fprintf(buf, "\tbaseOpts = append(baseOpts, option.WithHeader(%q, %q))\n", h.Key, h.StaticValue)
			}
		}
	}

	if svc.Persona != "" {
		fmt.Fprintf(buf, "\tbaseOpts = append(baseOpts, option.WithPersona(%q))\n", svc.Persona)
	}

	if svc.Timeout != "" {
		fmt.Fprintf(buf, "\tbaseOpts = append(baseOpts, option.WithTimeoutString(%q))\n", svc.Timeout)
	}

	buf.WriteString("\tbaseOpts = append(baseOpts, opts...)\n\n")
}

func emitMethodRouter(
	buf *bytes.Buffer,
	tracker *ImportTracker,
	root *ir.RootIR,
	svc *ir.ServiceIR,
	clientStructName string,
	m *ir.MethodIR,
) {
	if m.Operation == ir.OpEvent || m.IsEvent {
		emitEventMethod(buf, tracker, root, svc, clientStructName, m)
		return
	}

	if m.Operation == ir.OpRPC || m.Operation == ir.OpNotify || m.OpID != "" {
		emitRPCMethod(buf, tracker, root, svc, clientStructName, m)
		return
	}

	emitHTTPMethod(buf, tracker, root, svc, clientStructName, m)
}
