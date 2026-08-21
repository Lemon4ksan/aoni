// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package optimizer

import (
	"strings"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

const (
	// CacheLineSize defines the standard CPU L1 cache line width in bytes.
	CacheLineSize = 64

	// MinStackModsSize defines the minimum stack-allocated modifier array capacity.
	MinStackModsSize = 4

	// MaxStackModsSize defines the maximum stack-allocated modifier array capacity.
	MaxStackModsSize = 32

	// MinStackBufSize defines the minimum stack buffer capacity for query/form builders.
	MinStackBufSize = 64

	// MaxStackBufSize defines the maximum stack buffer capacity for query/form builders.
	MaxStackBufSize = 1024
)

// estimateStackAllocations sizes the stack array buffers for all methods in the service.
func estimateStackAllocations(svc *ir.ServiceIR) {
	if svc == nil {
		return
	}

	for _, m := range svc.Methods {
		if m == nil {
			continue
		}
		optimizeMethodStack(m, svc)
	}
}

func optimizeMethodStack(m *ir.MethodIR, svc *ir.ServiceIR) {
	modsCount := calculateModifierSlots(m, svc)
	bufBytes := calculateBufferBytes(m)

	m.StackModsSize = alignStackMods(modsCount)
	m.StackBufSize = alignStackBuf(bufBytes)
}

func calculateModifierSlots(m *ir.MethodIR, svc *ir.ServiceIR) int {
	// Base headroom for telemetry and default user modifiers
	slots := 2

	if svc.Telemetry != "" || m.Telemetry != "" || m.Label != "" {
		slots++
	}

	// Service-level header overrides
	hasServiceContentType := generic.Any(svc.Headers, func(h ir.HeaderIR) bool {
		return strings.EqualFold(h.Key, "content-type") && h.StaticValue != ""
	})
	if hasServiceContentType {
		slots++
	}

	// Dynamic and static headers
	slots += len(m.Headers)

	// Path variables
	pathParamsCount := len(generic.Filter(m.Params, func(p *ir.ParamIR) bool {
		return p != nil && p.Location == ir.LocPath
	}))
	slots += pathParamsCount

	// Query params (1 slot for combined mod.WithRawQuery)
	hasQueryParams := generic.Any(m.Params, func(p *ir.ParamIR) bool {
		return p != nil && p.Location == ir.LocQuery
	})
	if hasQueryParams {
		slots++
	}

	// Struct query params (1 slot per struct query parameter)
	structQueryCount := len(generic.Filter(m.Params, func(p *ir.ParamIR) bool {
		return p != nil && p.Location == ir.LocQueryStruct
	}))
	slots += structQueryCount

	// Form body encoding
	if m.PayloadKind == ir.PayloadForm {
		slots += 2 // Content-Type header + BodyBytes
	}

	// Custom codecs
	if m.Decoder != "" && !isStandardCodec(m.Decoder) {
		slots++
	}
	if m.Encoder != "" {
		slots++
	}

	return slots
}

func isStandardCodec(decoder string) bool {
	switch strings.ToLower(decoder) {
	case "json", "xml", "proto", "grpc-web":
		return true
	default:
		return false
	}
}

func calculateBufferBytes(m *ir.MethodIR) int {
	bufBytes := 0

	for _, p := range m.Params {
		if p == nil {
			continue
		}

		switch p.Location {
		case ir.LocQuery:
			// Estimated wire key + '=' + avg 16 byte value + '&'
			keyLen := generic.Coalesce(len(p.WireKey), len(p.GoName))
			bufBytes += keyLen + 18

		case ir.LocFormFields:
			bufBytes += len(p.WireKey) + 32

		case ir.LocMultipartFile:
			bufBytes += 64
		}
	}

	for _, h := range m.Headers {
		if h.DynamicTemplate != nil {
			bufBytes += 64
		}
	}

	return bufBytes
}

func alignStackMods(n int) int {
	if n <= 4 {
		return MinStackModsSize
	}
	if n <= 8 {
		return 8
	}
	if n <= 16 {
		return 16
	}
	return MaxStackModsSize
}

func alignStackBuf(n int) int {
	if n <= 64 {
		return MinStackBufSize
	}
	if n <= 128 {
		return 128
	}
	if n <= 256 {
		return 256
	}
	if n <= 512 {
		return 512
	}
	return MaxStackBufSize
}
