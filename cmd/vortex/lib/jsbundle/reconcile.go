// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsbundle

import (
	"strconv"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

// ReconcileServiceIR enriches a ServiceIR using descriptors discovered in JavaScript bundles.
func ReconcileServiceIR(svc *ir.ServiceIR, scan *ScanResult) {
	if svc == nil || scan == nil {
		return
	}

	// 1. Match Endpoints by Path
	endpointMap := make(map[string]Endpoint)
	for _, ep := range scan.Endpoints {
		endpointMap[strings.ToLower(ep.Path)] = ep
	}

	for _, m := range svc.Methods {
		if m.Path == nil {
			continue
		}

		p := strings.ToLower(m.Path.RawTemplate)
		if ep, ok := endpointMap[p]; ok {
			if m.HTTPMethod == "" && ep.HTTPMethod != "" {
				m.HTTPMethod = ep.HTTPMethod
			}
		}
	}
}

// ReconcileTupleFields replaces generic field names in a tuple with discovered JS descriptor names.
func ReconcileTupleFields(tupleName string, fields []ir.TupleFieldIR, scan *ScanResult) []ir.TupleFieldIR {
	if scan == nil || len(fields) == 0 {
		return fields
	}

	// Look up message descriptor matching tupleName or substring
	var matchedMsg *MessageDescriptor
	for id, msg := range scan.Messages {
		if strings.EqualFold(id, tupleName) ||
			strings.Contains(strings.ToLower(tupleName), strings.ToLower(id)) ||
			strings.Contains(strings.ToLower(id), strings.ToLower(tupleName)) {
			matchedMsg = msg
			break
		}
	}

	if matchedMsg == nil {
		return fields
	}

	for i, f := range fields {
		if desc, ok := matchedMsg.Fields[f.Index]; ok {
			if desc.Name != "" && !strings.HasPrefix(desc.Name, "Field") {
				fields[i].GoName = desc.Name
			}

			if desc.IsNested && desc.SubMsgType != "" {
				fields[i].IsNested = true
			}
		}
	}

	return fields
}

// FindFieldDescriptor returns a discovered field descriptor by index if available.
func (r *ScanResult) FindFieldDescriptor(structOrMethodName string, index int) (FieldDescriptor, bool) {
	lowerTarget := strings.ToLower(structOrMethodName)

	for id, msg := range r.Messages {
		if strings.Contains(strings.ToLower(id), lowerTarget) || strings.Contains(lowerTarget, strings.ToLower(id)) {
			if f, ok := msg.Fields[index]; ok {
				return f, true
			}
		}
	}

	// Fallback to searching all messages if specific index is known
	for _, msg := range r.Messages {
		if f, ok := msg.Fields[index]; ok && f.Name != "" && f.Name != "Field"+strconv.Itoa(index) {
			return f, true
		}
	}

	return FieldDescriptor{}, false
}
