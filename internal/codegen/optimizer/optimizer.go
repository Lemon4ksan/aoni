// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package optimizer

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

// Optimizer optimizes and normalizes the parsed IR.
type Optimizer struct{}

// NewOptimizer creates a new Optimizer instance.
func NewOptimizer() *Optimizer {
	return &Optimizer{}
}

// Optimize runs all optimization passes over root IR in-place.
func (opt *Optimizer) Optimize(root *ir.RootIR) {
	if root == nil {
		return
	}

	for _, svc := range root.Services {
		opt.optimizeService(svc)
	}
}

func (opt *Optimizer) optimizeService(svc *ir.ServiceIR) {
	// Pass 1: SubRequester Clustering
	opt.clusterSubRequesters(svc)

	// Pass 2: Method-level buffer and modifier sizing
	for _, m := range svc.Methods {
		opt.optimizeMethod(m)
	}
}

func (opt *Optimizer) clusterSubRequesters(svc *ir.ServiceIR) {
	urlToField := make(map[string]string)

	var subRequesters []ir.SubRequesterIR

	// Default main BaseURL
	mainURL := svc.BaseURL
	urlToField[mainURL] = "r"
	subRequesters = append(subRequesters, ir.SubRequesterIR{
		FieldName: "r",
		BaseURL:   mainURL,
	})

	for _, m := range svc.Methods {
		methodURL := m.TargetRequester
		if methodURL == "" || methodURL == mainURL || methodURL == "c.r" {
			m.TargetRequester = "c.r"
			continue
		}

		if field, exists := urlToField[methodURL]; exists {
			m.TargetRequester = "c." + field
			continue
		}

		// Generate a clean field name from URL subdomain or path
		fieldName := inferSubRequesterName(methodURL, len(subRequesters))
		urlToField[methodURL] = fieldName
		subRequesters = append(subRequesters, ir.SubRequesterIR{
			FieldName: fieldName,
			BaseURL:   methodURL,
		})
		m.TargetRequester = "c." + fieldName
	}

	svc.SubRequesters = subRequesters
}

func inferSubRequesterName(rawURL string, idx int) string {
	u, err := url.Parse(rawURL)
	if err == nil && u.Hostname() != "" {
		hostParts := strings.Split(u.Hostname(), ".")
		if len(hostParts) > 2 {
			sub := hostParts[0]
			if sub != "www" && sub != "api" {
				return cleanIdentifier(sub)
			}
		}
	}

	return fmt.Sprintf("r%d", idx)
}

func cleanIdentifier(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}

	res := strings.ToLower(b.String())
	if res == "" {
		return "sub"
	}

	return res
}

func (opt *Optimizer) optimizeMethod(m *ir.MethodIR) {
	modsCount := 0
	bufBytes := 0

	// Dynamic headers
	for _, h := range m.Headers {
		if h.DynamicTemplate != nil {
			modsCount++
			bufBytes += 64
		} else {
			modsCount++
		}
	}

	// Query parameters
	queryCount := 0
	for _, p := range m.Params {
		switch p.Location {
		case ir.LocQuery:
			queryCount++
			bufBytes += len(p.WireKey) + 24
		case ir.LocPath:
			modsCount++
		case ir.LocFormFields:
			bufBytes += 64
		}
	}

	if queryCount > 0 {
		modsCount++ // mod.WithRawQuery
	}

	if m.PayloadKind == ir.PayloadForm {
		modsCount += 2 // Content-Type + BodyBytes
	}

	// Size power-of-two or standard stack brackets
	m.StackModsSize = alignModsSize(modsCount + 4)
	m.StackBufSize = alignBufSize(bufBytes)
}

func alignModsSize(n int) int {
	if n <= 4 {
		return 4
	}

	if n <= 8 {
		return 8
	}

	if n <= 16 {
		return 16
	}

	return 32
}

func alignBufSize(n int) int {
	if n <= 64 {
		return 64
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

	return 1024
}
