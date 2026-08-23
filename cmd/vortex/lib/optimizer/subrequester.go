// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package optimizer

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

var commonGenericHosts = []string{"www", "api", "app", "v1", "v2", "v3", "grpc", "ws", "rest"}

// clusterSubRequesters groups service methods by distinct base URLs into dedicated SubRequester instances.
func clusterSubRequesters(svc *ir.ServiceIR) {
	if svc == nil {
		return
	}

	urlToField := make(map[string]string)
	usedFieldNames := make(map[string]bool)

	var subRequesters []ir.SubRequesterIR

	// Default main BaseURL assigned to field 'r'
	mainURL := svc.BaseURL
	urlToField[mainURL] = "r"
	usedFieldNames["r"] = true

	subRequesters = append(subRequesters, ir.SubRequesterIR{
		FieldName: "r",
		BaseURL:   mainURL,
	})

	for _, m := range svc.Methods {
		if m == nil {
			continue
		}

		methodURL := m.TargetRequester
		if methodURL == "" || methodURL == mainURL || methodURL == "c.r" {
			m.TargetRequester = "c.r"
			continue
		}

		if field, exists := urlToField[methodURL]; exists {
			m.TargetRequester = "c." + field
			continue
		}

		fieldName := inferUniqueFieldName(methodURL, usedFieldNames, len(subRequesters))
		urlToField[methodURL] = fieldName
		usedFieldNames[fieldName] = true

		subRequesters = append(subRequesters, ir.SubRequesterIR{
			FieldName: fieldName,
			BaseURL:   methodURL,
		})
		m.TargetRequester = "c." + fieldName
	}

	svc.SubRequesters = subRequesters
}

func inferUniqueFieldName(rawURL string, used map[string]bool, fallbackIdx int) string {
	candidate := inferSubRequesterName(rawURL)
	if candidate == "" || candidate == "r" {
		candidate = fmt.Sprintf("r%d", fallbackIdx)
	}

	if !used[candidate] {
		return candidate
	}

	for i := 2; ; i++ {
		indexed := fmt.Sprintf("%s%d", candidate, i)
		if !used[indexed] {
			return indexed
		}
	}
}

func inferSubRequesterName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}

	hostParts := strings.Split(u.Hostname(), ".")
	if len(hostParts) > 2 {
		sub := hostParts[0]
		if !slices.Contains(commonGenericHosts, strings.ToLower(sub)) {
			return cleanGoIdentifier(sub)
		}

		// Check second segment if first is generic (e.g. api.pricing.domain.com -> pricing)
		second := hostParts[1]
		if len(hostParts) > 3 && !slices.Contains(commonGenericHosts, strings.ToLower(second)) {
			return cleanGoIdentifier(second)
		}
	}

	// Try extracting from path prefix if host is generic (e.g. https://api.com/billing/v1 -> billing)
	cleanPath := strings.Trim(u.Path, "/")
	if cleanPath != "" {
		pathParts := strings.Split(cleanPath, "/")
		for _, p := range pathParts {
			if p != "" && !slices.Contains(commonGenericHosts, strings.ToLower(p)) {
				return cleanGoIdentifier(p)
			}
		}
	}

	return ""
}

func cleanGoIdentifier(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}

	res := strings.ToLower(b.String())

	return generic.Coalesce(res, "sub")
}
