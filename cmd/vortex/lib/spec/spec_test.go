// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spec_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/spec"
)

func TestSpecRegistry(t *testing.T) {
	require.NotEmpty(t, spec.Registry)

	// Ensure all directives have non-empty Name, Scopes, Description, Example
	seen := make(map[string]bool)
	for _, d := range spec.Registry {
		require.NotEmpty(t, d.Name)
		require.NotEmpty(t, d.Scopes)
		require.NotEmpty(t, d.Description)
		require.NotEmpty(t, d.Example)

		require.False(t, seen[d.Name], "duplicate directive name: %s", d.Name)
		seen[d.Name] = true

		// Lookup by exact name and with leading @
		require.NotNil(t, spec.Lookup(d.Name))
		require.NotNil(t, spec.Lookup("@"+d.Name))
		require.True(t, spec.IsKnownDirective(d.Name))
		require.True(t, spec.IsKnownDirective("@"+d.Name))

		// Lookup by aliases
		for _, alias := range d.Aliases {
			require.NotNil(t, spec.Lookup(alias))
			require.NotNil(t, spec.Lookup("@"+alias))
			require.True(t, spec.IsKnownDirective(alias))
		}
	}

	// Verify scopes
	scopes := spec.Scopes()
	require.Len(t, scopes, 6)

	for _, s := range scopes {
		list := spec.ByScope(s)
		require.NotEmpty(t, list, "scope %s should not be empty", s)

		for _, d := range list {
			require.True(t, d.HasScope(s))
		}
	}

	// Verify markdown generation
	md := spec.GenerateMarkdownTable()
	require.NotEmpty(t, md)
	require.Contains(t, md, "### Service Scope Directives")
	require.Contains(t, md, "### Method Scope Directives")
	require.Contains(t, md, "@aoni:service")
	require.Contains(t, md, "@form")
	require.Contains(t, md, "@referer")
}

func TestExamples(t *testing.T) {
	require.NotEmpty(t, spec.Examples)
	require.NotEmpty(t, spec.PrintExampleHelp())

	kinds := []string{"http", "ws", "socket", "pipeline"}
	for _, k := range kinds {
		ex := spec.LookupExample(k)
		require.NotNil(t, ex, "example %s not found", k)
		require.NotEmpty(t, ex.Title)
		require.NotEmpty(t, ex.Description)
		require.NotEmpty(t, ex.SourceCode)

		for _, alias := range ex.Aliases {
			require.Equal(t, ex, spec.LookupExample(alias))
		}
	}
}
