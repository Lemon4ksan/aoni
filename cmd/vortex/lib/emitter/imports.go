// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package emitter

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

// ImportEntry captures an imported package path and optional local alias.
type ImportEntry struct {
	Path  string
	Alias string
}

// ImportTracker tracks required package imports and renders formatted Go import blocks.
type ImportTracker struct {
	stdImports      map[string]string
	thirdImports    map[string]string
	internalImports map[string]string
}

// NewImportTracker creates an empty import tracker.
func NewImportTracker() *ImportTracker {
	return &ImportTracker{
		stdImports:      make(map[string]string),
		thirdImports:    make(map[string]string),
		internalImports: make(map[string]string),
	}
}

// Add registers a package import without an alias.
func (t *ImportTracker) Add(path string) {
	t.AddNamed("", path)
}

// AddNamed registers a package import with an optional alias.
func (t *ImportTracker) AddNamed(alias, path string) {
	if path == "" {
		return
	}

	if isStdPkg(path) {
		t.stdImports[path] = alias
		return
	}

	if strings.HasPrefix(path, "github.com/lemon4ksan/aoni") {
		t.internalImports[path] = alias
		return
	}

	t.thirdImports[path] = alias
}

// Render formats all registered imports into standard grouped Go syntax.
func (t *ImportTracker) Render(buf *bytes.Buffer) {
	if len(t.stdImports) == 0 && len(t.thirdImports) == 0 && len(t.internalImports) == 0 {
		return
	}

	buf.WriteString("import (\n")

	// Group 1: Standard Library
	if len(t.stdImports) > 0 {
		renderGroup(buf, t.stdImports)
	}

	// Group 2: Third-party
	if len(t.thirdImports) > 0 {
		if len(t.stdImports) > 0 {
			buf.WriteString("\n")
		}

		renderGroup(buf, t.thirdImports)
	}

	// Group 3: Internal aoni packages
	if len(t.internalImports) > 0 {
		if len(t.stdImports) > 0 || len(t.thirdImports) > 0 {
			buf.WriteString("\n")
		}

		renderGroup(buf, t.internalImports)
	}

	buf.WriteString(")\n\n")
}

func renderGroup(buf *bytes.Buffer, group map[string]string) {
	keys := generic.Keys(group)
	slices.Sort(keys)

	for _, k := range keys {
		alias := group[k]
		if alias != "" {
			fmt.Fprintf(buf, "\t%s %q\n", alias, k)
		} else {
			fmt.Fprintf(buf, "\t%q\n", k)
		}
	}
}

func isStdPkg(path string) bool {
	return !strings.Contains(path, ".")
}

// RegisterRootImports scans root IR for standard imports and user custom imports.
func (t *ImportTracker) RegisterRootImports(root *ir.RootIR, bodyCode string) {
	if root == nil {
		return
	}

	hasHTTP := false
	hasFastClient := false
	hasSocket := false
	hasProto := false
	hasDecode := false
	hasCodec := false
	hasRequest := false

	for _, svc := range root.Services {
		if svc.Protocol == ir.ProtocolSocket {
			hasSocket = true
		} else if svc.Protocol != ir.ProtocolRPC && svc.Protocol != ir.ProtocolChannel {
			hasHTTP = true

			isStrict := svc.Engine == ir.EngineRequired || svc.RequesterType != ""
			if !isStrict && svc.Engine != ir.EngineNetHTTP && svc.Engine != ir.EngineCustom {
				hasFastClient = true
			}

			hasRequest = true
		}

		for _, m := range svc.Methods {
			if m.Decoder != "" ||
				(m.Return != nil && (len(m.Return.StatusMap) > 0 || m.Return.UnionType != nil || m.Return.ErrorModelType != "")) {
				hasDecode = true
			}

			if m.Codec != "" {
				hasCodec = true
			}

			if m.ReturnPipeline != nil {
				for _, st := range m.ReturnPipeline.Stages {
					if st.Type == ir.StageJSON || st.Type == ir.StageAttr || st.Type == ir.StageBetween ||
						st.Type == ir.StageHTMLUnescape {
						hasDecode = true
					}

					if st.Type == ir.StageProto {
						hasProto = true
					}
				}
			}

			if m.BodyPipeline != nil {
				for _, st := range m.BodyPipeline.Stages {
					if st.Type == ir.StageProto {
						hasProto = true
					}
				}
			}

			for _, p := range m.Params {
				if isProtoType(p.GoType.Name) || isProtoType(p.GoType.ElemType) {
					hasProto = true
				}
			}

			if m.Return != nil && isProtoType(m.Return.SuccessType.Name) {
				hasProto = true
			}
		}
	}

	if hasHTTP {
		t.Add("context")

		if strings.Contains(bodyCode, "http.") {
			t.Add("net/http")
		}

		t.Add("github.com/lemon4ksan/aoni")

		if strings.Contains(bodyCode, "mod.") {
			t.Add("github.com/lemon4ksan/aoni/mod")
		}

		if strings.Contains(bodyCode, "option.") {
			t.Add("github.com/lemon4ksan/aoni/option")
		}

		if hasFastClient {
			t.Add("github.com/lemon4ksan/aoni/fast")
		}
	}

	if hasRequest {
		t.Add("github.com/lemon4ksan/aoni/request")
	}

	if hasDecode {
		t.Add("github.com/lemon4ksan/aoni/codec/decode")
	}

	if hasCodec {
		t.Add("github.com/lemon4ksan/aoni/codec")
	}

	if hasProto {
		t.Add("google.golang.org/protobuf/proto")
	}

	if hasSocket {
		t.Add("context")
		t.Add("github.com/lemon4ksan/aoni/realtime/socket")
		t.Add("github.com/lemon4ksan/aoni/realtime/socket/connector")
		t.Add("github.com/lemon4ksan/aoni/realtime/socket/dispatcher")
		t.Add("github.com/lemon4ksan/aoni/realtime/socket/processor")
	}

	// Standard library package detection based on body references
	if strings.Contains(bodyCode, "json.") {
		t.Add("encoding/json")
	}

	if strings.Contains(bodyCode, "io.") {
		t.Add("io")
	}

	if strings.Contains(bodyCode, "bytes.") {
		t.Add("bytes")
	}

	if strings.Contains(bodyCode, "strings.") {
		t.Add("strings")
	}

	if strings.Contains(bodyCode, "strconv.") {
		t.Add("strconv")
	}

	if strings.Contains(bodyCode, "url.") {
		t.Add("net/url")
	}

	if strings.Contains(bodyCode, "sync.") {
		t.Add("sync")
	}

	if strings.Contains(bodyCode, "atomic.") {
		t.Add("sync/atomic")
	}

	if strings.Contains(bodyCode, "time.") {
		t.Add("time")
	}

	if strings.Contains(bodyCode, "errors.") {
		t.Add("errors")
	}

	if strings.Contains(bodyCode, "fmt.") {
		t.Add("fmt")
	}

	// Register user custom imports if present in root.Imports
	for _, imp := range root.Imports {
		if isStdPkg(imp.Path) || strings.Contains(imp.Path, "github.com/lemon4ksan/aoni") {
			continue
		}

		ref := imp.Alias
		if ref == "" {
			parts := strings.Split(imp.Path, "/")
			ref = parts[len(parts)-1]
		}

		if strings.Contains(bodyCode, ref+".") {
			t.AddNamed(imp.Alias, imp.Path)
		}
	}
}

func isProtoType(name string) bool {
	return strings.Contains(name, "pb.") || strings.Contains(name, "CMsg") || strings.Contains(name, "proto.")
}
