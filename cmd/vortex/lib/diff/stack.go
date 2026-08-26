// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package diff

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// StackFrame represents an immutable snapshot frame in the comparison stack.
type StackFrame struct {
	ID        string            `json:"id"`
	Index     int               `json:"index"`
	Label     string            `json:"label"`
	Tags      []string          `json:"tags,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	Files     map[string]string `json:"files"` // relPath -> full content
	Hashes    map[string]string `json:"hashes"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Summary   string            `json:"summary,omitempty"`
}

// FileDiff records unified line-level differences for a single file between frames.
type FileDiff struct {
	Path      string   `json:"path"`
	Status    string   `json:"status"` // "modified", "added", "deleted", "unchanged"
	Additions int      `json:"additions"`
	Deletions int      `json:"deletions"`
	Hunks     []string `json:"hunks,omitempty"`
}

// TupleRename records a detected field deobfuscation or rename by matching @aoni:tuple index tags.
type TupleRename struct {
	StructName string `json:"struct_name"`
	Tag        string `json:"tag"`
	OldField   string `json:"old_field"`
	NewField   string `json:"new_field"`
	GoType     string `json:"go_type"`
}

// MethodRename records a detected RPC / HTTP method rename by matching endpoint routes.
type MethodRename struct {
	Route     string `json:"route"`
	OldMethod string `json:"old_method"`
	NewMethod string `json:"new_method"`
	OldSig    string `json:"old_sig,omitempty"`
	NewSig    string `json:"new_sig,omitempty"`
}

// ASTEvolutionReport aggregates semantic Go contract transformations.
type ASTEvolutionReport struct {
	TupleRenames     []TupleRename  `json:"tuple_renames,omitempty"`
	MethodRenames    []MethodRename `json:"method_renames,omitempty"`
	AddedEndpoints   []string       `json:"added_endpoints,omitempty"`
	RemovedEndpoints []string       `json:"removed_endpoints,omitempty"`
	AddedTypes       []string       `json:"added_types,omitempty"`
	RemovedTypes     []string       `json:"removed_types,omitempty"`
}

// StackDiffResult represents the full comparison report between two stack frames.
type StackDiffResult struct {
	FromIndex    int                 `json:"from_index"`
	FromLabel    string              `json:"from_label"`
	ToIndex      int                 `json:"to_index"`
	ToLabel      string              `json:"to_label"`
	FileDiffs    []FileDiff          `json:"file_diffs"`
	ASTEvolution *ASTEvolutionReport `json:"ast_evolution,omitempty"`
	IsIdentical  bool                `json:"is_identical"`
}

// DiffStack represents the persistent snapshot stack stored in .vortex/cache/diff_stack.json.
type DiffStack struct {
	RootDir string       `json:"root_dir"`
	Frames  []StackFrame `json:"frames"`
	mu      sync.RWMutex
}

const stackFileName = "diff_stack.json"

func getStackFilePath(rootDir string) string {
	return filepath.Join(rootDir, ".vortex", "cache", stackFileName)
}

// LoadStack loads the diff stack from .vortex/cache/diff_stack.json.
func LoadStack(rootDir string) (*DiffStack, error) {
	s := &DiffStack{
		RootDir: rootDir,
		Frames:  make([]StackFrame, 0),
	}

	path := getStackFilePath(rootDir)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}

		return nil, fmt.Errorf("reading stack file: %w", err)
	}

	if len(data) == 0 {
		return s, nil
	}

	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("decoding diff stack: %w", err)
	}

	s.RootDir = rootDir

	return s, nil
}

// Save persists the diff stack to .vortex/cache/diff_stack.json atomically.
func (s *DiffStack) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := getStackFilePath(s.RootDir)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating stack cache dir: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling diff stack: %w", err)
	}

	tmpFile := path + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o600); err != nil {
		return fmt.Errorf("writing temp stack file: %w", err)
	}

	if err := os.Rename(tmpFile, path); err != nil {
		// Windows fallback if rename over existing fails
		_ = os.Remove(path)
		if rErr := os.Rename(tmpFile, path); rErr != nil {
			return fmt.Errorf("committing stack file: %w", rErr)
		}
	}

	return nil
}

// Push captures a new snapshot frame onto the stack.
func (s *DiffStack) Push(label string, filePaths, tags []string, metadata map[string]string) (*StackFrame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(filePaths) == 0 {
		discovered, err := findWorkspaceFiles(s.RootDir)
		if err != nil || len(discovered) == 0 {
			return nil, errors.New("no files specified and no candidate files found in workspace")
		}

		filePaths = discovered
	}

	files := make(map[string]string)
	hashes := make(map[string]string)

	for _, rawPath := range filePaths {
		absPath := rawPath
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(s.RootDir, absPath)
		}

		relPath, err := filepath.Rel(s.RootDir, absPath)
		if err != nil {
			relPath = absPath
		}

		relSlash := filepath.ToSlash(relPath)

		content, rErr := os.ReadFile(absPath)
		if rErr != nil {
			if os.IsNotExist(rErr) {
				files[relSlash] = ""
				hashes[relSlash] = "deleted"
				continue
			}

			return nil, fmt.Errorf("reading %s for stack frame: %w", rawPath, rErr)
		}

		h := sha256.Sum256(content)
		files[relSlash] = string(content)
		hashes[relSlash] = hex.EncodeToString(h[:])
	}

	frameID := generateFrameID()

	if label == "" {
		label = fmt.Sprintf("frame-%d", len(s.Frames))
	}

	frame := StackFrame{
		ID:        frameID,
		Index:     len(s.Frames),
		Label:     label,
		Tags:      tags,
		CreatedAt: time.Now(),
		Files:     files,
		Hashes:    hashes,
		Metadata:  metadata,
	}

	// Compute summary vs previous top frame if available
	if len(s.Frames) > 0 {
		prev := s.Frames[len(s.Frames)-1]
		delta := computeFrameDelta(&prev, &frame)
		frame.Summary = delta
	} else {
		frame.Summary = fmt.Sprintf("Initial base frame (%d files)", len(files))
	}

	s.Frames = append(s.Frames, frame)

	// Persist
	path := getStackFilePath(s.RootDir)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	data, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(path, data, 0o600)

	return &frame, nil
}

func findWorkspaceFiles(rootDir string) ([]string, error) {
	var files []string

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr
		}

		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" ||
				name == "bin" || name == "dist" {
				return filepath.SkipDir
			}

			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".go", ".json", ".yaml", ".yml", ".proto", ".har", ".js", ".ts", ".md":
			files = append(files, path)
		}

		return nil
	})

	return files, err
}

// Pop removes the top frame from the stack. If restore is true, it restores the previous state to disk.
func (s *DiffStack) Pop(restore bool) (*StackFrame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.Frames) == 0 {
		return nil, errors.New("stack is empty, nothing to pop")
	}

	popped := s.Frames[len(s.Frames)-1]
	s.Frames = s.Frames[:len(s.Frames)-1]

	if restore {
		if len(s.Frames) > 0 {
			// Restore to new top frame
			target := s.Frames[len(s.Frames)-1]
			for relPath, content := range target.Files {
				absPath := filepath.Join(s.RootDir, filepath.FromSlash(relPath))
				if content == "" {
					_ = os.Remove(absPath)
				} else {
					_ = os.MkdirAll(filepath.Dir(absPath), 0o755)
					if err := os.WriteFile(absPath, []byte(content), 0o600); err != nil {
						return &popped, fmt.Errorf("restoring %s: %w", relPath, err)
					}
				}
			}
		} else {
			// Stack became empty; restore popped files' deletion or clear
			for relPath := range popped.Files {
				absPath := filepath.Join(s.RootDir, filepath.FromSlash(relPath))
				_ = os.Remove(absPath)
			}
		}
	}

	// Persist
	path := getStackFilePath(s.RootDir)
	data, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(path, data, 0o600)

	return &popped, nil
}

// Peek returns the top frame of the stack without removing it.
func (s *DiffStack) Peek() *StackFrame {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.Frames) == 0 {
		return nil
	}

	f := s.Frames[len(s.Frames)-1]

	return &f
}

// Base returns the bottom (Frame 0) frame of the stack.
func (s *DiffStack) Base() *StackFrame {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.Frames) == 0 {
		return nil
	}

	f := s.Frames[0]

	return &f
}

// List returns all frames currently in the stack (from base 0 to top N-1).
func (s *DiffStack) List() []StackFrame {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]StackFrame, len(s.Frames))
	copy(res, s.Frames)

	return res
}

// Clear resets and wipes the diff stack.
func (s *DiffStack) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Frames = make([]StackFrame, 0)
	path := getStackFilePath(s.RootDir)
	_ = os.Remove(path)

	return nil
}

// Find finds a frame by index string ("0", "1", "top", "base") or by label/tag.
func (s *DiffStack) Find(query string) (*StackFrame, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.Frames) == 0 {
		return nil, -1, errors.New("stack is empty")
	}

	q := strings.TrimSpace(query)
	if q == "" || strings.EqualFold(q, "top") || strings.EqualFold(q, "head") {
		idx := len(s.Frames) - 1
		return &s.Frames[idx], idx, nil
	}

	if strings.EqualFold(q, "base") || strings.EqualFold(q, "root") {
		return &s.Frames[0], 0, nil
	}

	// Try numeric index
	if idx, err := strconv.Atoi(q); err == nil {
		if idx >= 0 && idx < len(s.Frames) {
			return &s.Frames[idx], idx, nil
		}

		return nil, -1, fmt.Errorf("stack index %d out of bounds (stack size: %d)", idx, len(s.Frames))
	}

	// Match by Label or Tag
	for i, f := range s.Frames {
		if strings.EqualFold(f.Label, q) {
			return &s.Frames[i], i, nil
		}

		for _, tag := range f.Tags {
			if strings.EqualFold(tag, q) {
				return &s.Frames[i], i, nil
			}
		}
	}

	return nil, -1, fmt.Errorf("frame matching label or tag %q not found in stack", query)
}

// DiffFrames compares two frames identified by label or index.
func (s *DiffStack) DiffFrames(fromQuery, toQuery string) (*StackDiffResult, error) {
	fromFrame, fromIdx, err := s.Find(fromQuery)
	if err != nil {
		return nil, fmt.Errorf("resolving 'from' frame: %w", err)
	}

	toFrame, toIdx, err := s.Find(toQuery)
	if err != nil {
		return nil, fmt.Errorf("resolving 'to' frame: %w", err)
	}

	return compareTwoFrames(fromFrame, fromIdx, toFrame, toIdx), nil
}

// DiffAdjacent compares the top frame against the immediately preceding frame (Top vs Top-1).
func (s *DiffStack) DiffAdjacent() (*StackDiffResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.Frames) < 2 {
		return nil, errors.New("at least 2 frames required in stack for adjacent diff")
	}

	topIdx := len(s.Frames) - 1
	prevIdx := topIdx - 1

	return compareTwoFrames(&s.Frames[prevIdx], prevIdx, &s.Frames[topIdx], topIdx), nil
}

// DiffCumulative generates an end-to-end cumulative comparison between Frame 0 (Base) and Frame Top.
func (s *DiffStack) DiffCumulative() (*StackDiffResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.Frames) < 2 {
		return nil, errors.New("at least 2 frames required in stack for cumulative diff")
	}

	baseIdx := 0
	topIdx := len(s.Frames) - 1

	return compareTwoFrames(&s.Frames[baseIdx], baseIdx, &s.Frames[topIdx], topIdx), nil
}

func compareTwoFrames(from *StackFrame, fromIdx int, to *StackFrame, toIdx int) *StackDiffResult {
	res := &StackDiffResult{
		FromIndex:    fromIdx,
		FromLabel:    from.Label,
		ToIndex:      toIdx,
		ToLabel:      to.Label,
		FileDiffs:    make([]FileDiff, 0),
		ASTEvolution: &ASTEvolutionReport{},
		IsIdentical:  true,
	}

	allPaths := make(map[string]bool)
	for p := range from.Files {
		allPaths[p] = true
	}

	for p := range to.Files {
		allPaths[p] = true
	}

	for p := range allPaths {
		oldContent := from.Files[p]
		newContent := to.Files[p]

		if oldContent == newContent {
			res.FileDiffs = append(res.FileDiffs, FileDiff{
				Path:   p,
				Status: "unchanged",
			})

			continue
		}

		res.IsIdentical = false

		status := "modified"
		if oldContent == "" && newContent != "" {
			status = "added"
		} else if oldContent != "" && newContent == "" {
			status = "deleted"
		}

		adds, dels, hunks := computeTextDiff(oldContent, newContent)
		res.FileDiffs = append(res.FileDiffs, FileDiff{
			Path:      p,
			Status:    status,
			Additions: adds,
			Deletions: dels,
			Hunks:     hunks,
		})

		// If Go file, extract AST-aware tuple & method renamings
		if strings.HasSuffix(p, ".go") {
			extractASTEvolution(oldContent, newContent, res.ASTEvolution)
		}
	}

	return res
}

func computeFrameDelta(prev, next *StackFrame) string {
	modCount := 0
	addCount := 0
	delCount := 0

	for p, nextContent := range next.Files {
		prevContent, exists := prev.Files[p]
		if !exists || (prevContent == "" && nextContent != "") {
			addCount++
		} else if prevContent != nextContent {
			modCount++
		}
	}

	for p := range prev.Files {
		if _, exists := next.Files[p]; !exists {
			delCount++
		}
	}

	parts := make([]string, 0, 3)
	if modCount > 0 {
		parts = append(parts, fmt.Sprintf("%d modified", modCount))
	}

	if addCount > 0 {
		parts = append(parts, fmt.Sprintf("%d added", addCount))
	}

	if delCount > 0 {
		parts = append(parts, fmt.Sprintf("%d deleted", delCount))
	}

	if len(parts) == 0 {
		return "Identical to previous frame"
	}

	return strings.Join(parts, ", ")
}

func computeTextDiff(oldText, newText string) (int, int, []string) {
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")

	if oldText == "" {
		oldLines = []string{}
	}

	if newText == "" {
		newLines = []string{}
	}

	adds := 0
	dels := 0

	var hunks []string

	oldSet := make(map[string]bool)
	for _, l := range oldLines {
		if strings.TrimSpace(l) != "" {
			oldSet[l] = true
		}
	}

	newSet := make(map[string]bool)
	for _, l := range newLines {
		if strings.TrimSpace(l) != "" {
			newSet[l] = true
		}
	}

	for _, l := range newLines {
		if strings.TrimSpace(l) != "" && !oldSet[l] {
			adds++

			if len(hunks) < 15 {
				hunks = append(hunks, "+ "+l)
			}
		}
	}

	for _, l := range oldLines {
		if strings.TrimSpace(l) != "" && !newSet[l] {
			dels++

			if len(hunks) < 15 {
				hunks = append(hunks, "- "+l)
			}
		}
	}

	return adds, dels, hunks
}

// extractASTEvolution compares old and new Go files to find tuple field and method renames.
func extractASTEvolution(oldSrc, newSrc string, report *ASTEvolutionReport) {
	if oldSrc == "" || newSrc == "" {
		return
	}

	fset := token.NewFileSet()
	oldAst, err1 := goparser.ParseFile(fset, "old.go", oldSrc, goparser.ParseComments)

	newAst, err2 := goparser.ParseFile(fset, "new.go", newSrc, goparser.ParseComments)
	if err1 != nil || err2 != nil {
		return
	}

	// 1. Analyze Tuple Fields (Mapping by `aoni:"N"` tag)
	oldTuples := extractTupleFieldTags(oldAst)
	newTuples := extractTupleFieldTags(newAst)

	for structName, newTagMap := range newTuples {
		oldTagMap, ok := oldTuples[structName]
		if !ok {
			report.AddedTypes = append(report.AddedTypes, structName)
			continue
		}

		for tag, newField := range newTagMap {
			if oldField, ok := oldTagMap[tag]; ok {
				if oldField.Name != newField.Name {
					report.TupleRenames = append(report.TupleRenames, TupleRename{
						StructName: structName,
						Tag:        tag,
						OldField:   oldField.Name,
						NewField:   newField.Name,
						GoType:     newField.Type,
					})
				}
			}
		}
	}

	for structName := range oldTuples {
		if _, ok := newTuples[structName]; !ok {
			report.RemovedTypes = append(report.RemovedTypes, structName)
		}
	}

	// 2. Analyze Service Methods (Mapping by @post/@get route or method name)
	oldMethods := extractServiceMethods(fset, oldAst)
	newMethods := extractServiceMethods(fset, newAst)

	for route, newM := range newMethods {
		if oldM, ok := oldMethods[route]; ok {
			if oldM.Name != newM.Name {
				report.MethodRenames = append(report.MethodRenames, MethodRename{
					Route:     route,
					OldMethod: oldM.Name,
					NewMethod: newM.Name,
					OldSig:    oldM.Signature,
					NewSig:    newM.Signature,
				})
			}
		} else {
			report.AddedEndpoints = append(report.AddedEndpoints, fmt.Sprintf("%s (%s)", route, newM.Name))
		}
	}

	for route, oldM := range oldMethods {
		if _, ok := newMethods[route]; !ok {
			report.RemovedEndpoints = append(report.RemovedEndpoints, fmt.Sprintf("%s (%s)", route, oldM.Name))
		}
	}
}

type fieldInfo struct {
	Name string
	Type string
}

func extractTupleFieldTags(file *ast.File) map[string]map[string]fieldInfo {
	res := make(map[string]map[string]fieldInfo)

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			st, ok := typeSpec.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				continue
			}

			structName := typeSpec.Name.Name
			tagMap := make(map[string]fieldInfo)

			for _, f := range st.Fields.List {
				if len(f.Names) == 0 || f.Tag == nil {
					continue
				}

				rawTag := strings.Trim(f.Tag.Value, "`")

				aoniTag := reflect.StructTag(rawTag).Get("aoni")
				if aoniTag == "" {
					continue
				}

				typeName := formatTypeExpr(f.Type)
				tagMap[aoniTag] = fieldInfo{
					Name: f.Names[0].Name,
					Type: typeName,
				}
			}

			if len(tagMap) > 0 {
				res[structName] = tagMap
			}
		}
	}

	return res
}

type methodInfo struct {
	Name      string
	Signature string
}

func extractServiceMethods(fset *token.FileSet, file *ast.File) map[string]methodInfo {
	res := make(map[string]methodInfo)

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			iface, ok := typeSpec.Type.(*ast.InterfaceType)
			if !ok || iface.Methods == nil {
				continue
			}

			for _, m := range iface.Methods.List {
				if len(m.Names) == 0 {
					continue
				}

				methodName := m.Names[0].Name
				route := ""

				doc := m.Doc
				if doc == nil && fset != nil && len(m.Names) > 0 {
					doc = findPrecedingDoc(fset, file.Comments, m.Names[0].Pos())
				}

				// Extract route from doc comments (@post, @get)
				if doc != nil {
					for _, c := range doc.List {
						text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
						if strings.HasPrefix(text, "@post") || strings.HasPrefix(text, "@get") ||
							strings.HasPrefix(text, "@put") || strings.HasPrefix(text, "@delete") ||
							strings.HasPrefix(text, "@patch") {
							route = text
							break
						}
					}
				}

				if route == "" {
					route = methodName
				}

				res[route] = methodInfo{
					Name: methodName,
				}
			}
		}
	}

	return res
}

func findPrecedingDoc(fset *token.FileSet, comments []*ast.CommentGroup, targetPos token.Pos) *ast.CommentGroup {
	targetLine := fset.Position(targetPos).Line
	for _, cg := range comments {
		endLine := fset.Position(cg.End()).Line
		if endLine == targetLine-1 || endLine == targetLine {
			return cg
		}
	}

	return nil
}

func formatTypeExpr(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + formatTypeExpr(t.X)
	case *ast.ArrayType:
		return "[]" + formatTypeExpr(t.Elt)
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", formatTypeExpr(t.Key), formatTypeExpr(t.Value))
	default:
		return "any"
	}
}

func generateFrameID() string {
	b := make([]byte, 4)
	_, _ = cryptoRandRead(b)
	return hex.EncodeToString(b)
}

func cryptoRandRead(b []byte) (int, error) {
	t := time.Now().UnixNano()
	for i := range b {
		b[i] = byte(t >> (i * 8))
	}

	return len(b), nil
}

// RenderText formats the StackDiffResult as a clear, human-readable terminal report.
func (r *StackDiffResult) RenderText() string {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "=== Vortex Diff Stack Report ===\n")
	fmt.Fprintf(&buf, "From: Frame #%d [%s]\n", r.FromIndex, r.FromLabel)
	fmt.Fprintf(&buf, "To:   Frame #%d [%s]\n\n", r.ToIndex, r.ToLabel)

	if r.IsIdentical {
		fmt.Fprintf(&buf, "✔ No changes detected between frames (Identical state)\n")
		return buf.String()
	}

	// 1. AST Evolution Summary
	if r.ASTEvolution != nil {
		if len(r.ASTEvolution.TupleRenames) > 0 {
			fmt.Fprintf(&buf, "📦 Tuple Field Renames (Deobfuscation Dictionary):\n")

			for _, tr := range r.ASTEvolution.TupleRenames {
				fmt.Fprintf(&buf, "  • %s [Tag #%s]: %s ➔ %s (%s)\n",
					tr.StructName, tr.Tag, tr.OldField, tr.NewField, tr.GoType)
			}

			fmt.Fprintf(&buf, "\n")
		}

		if len(r.ASTEvolution.MethodRenames) > 0 {
			fmt.Fprintf(&buf, "⚡ RPC / Method Renames:\n")

			for _, mr := range r.ASTEvolution.MethodRenames {
				fmt.Fprintf(&buf, "  • %s: %s ➔ %s\n", mr.Route, mr.OldMethod, mr.NewMethod)
			}

			fmt.Fprintf(&buf, "\n")
		}

		if len(r.ASTEvolution.AddedEndpoints) > 0 {
			fmt.Fprintf(&buf, "➕ Added Endpoints (%d):\n", len(r.ASTEvolution.AddedEndpoints))

			for _, ep := range r.ASTEvolution.AddedEndpoints {
				fmt.Fprintf(&buf, "  + %s\n", ep)
			}

			fmt.Fprintf(&buf, "\n")
		}

		if len(r.ASTEvolution.RemovedEndpoints) > 0 {
			fmt.Fprintf(&buf, "➖ Removed Endpoints (%d):\n", len(r.ASTEvolution.RemovedEndpoints))

			for _, ep := range r.ASTEvolution.RemovedEndpoints {
				fmt.Fprintf(&buf, "  - %s\n", ep)
			}

			fmt.Fprintf(&buf, "\n")
		}
	}

	// 2. File Level Changes
	fmt.Fprintf(&buf, "📄 File Modifications:\n")

	for _, fd := range r.FileDiffs {
		switch fd.Status {
		case "added":
			fmt.Fprintf(&buf, "  [+ADDED]    %s (+%d lines)\n", fd.Path, fd.Additions)
		case "deleted":
			fmt.Fprintf(&buf, "  [-DELETED]  %s (-%d lines)\n", fd.Path, fd.Deletions)
		case "modified":
			fmt.Fprintf(&buf, "  [MODIFIED]  %s (+%d / -%d lines)\n", fd.Path, fd.Additions, fd.Deletions)
		}
	}

	return buf.String()
}

// PopTo pops frames down to a specific target frame index or label.
// It removes all frames above the target frame.
// If restore is true, disk state is restored to the target frame.
func (s *DiffStack) PopTo(targetQuery string, restore bool) ([]StackFrame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.Frames) == 0 {
		return nil, errors.New("stack is empty")
	}

	targetIdx := -1

	q := strings.TrimSpace(targetQuery)
	if strings.EqualFold(q, "base") || strings.EqualFold(q, "root") {
		targetIdx = 0
	} else if idx, err := strconv.Atoi(q); err == nil {
		if idx >= 0 && idx < len(s.Frames) {
			targetIdx = idx
		}
	} else {
		for i, f := range s.Frames {
			if strings.EqualFold(f.Label, q) {
				targetIdx = i
				break
			}

			for _, tag := range f.Tags {
				if strings.EqualFold(tag, q) {
					targetIdx = i
					break
				}
			}

			if targetIdx != -1 {
				break
			}
		}
	}

	if targetIdx == -1 {
		return nil, fmt.Errorf("target frame %q not found in stack", targetQuery)
	}

	if targetIdx >= len(s.Frames)-1 {
		return nil, fmt.Errorf("target frame %q is already the top frame", targetQuery)
	}

	poppedCount := len(s.Frames) - 1 - targetIdx
	popped := make([]StackFrame, poppedCount)
	copy(popped, s.Frames[targetIdx+1:])
	s.Frames = s.Frames[:targetIdx+1]

	if restore {
		target := s.Frames[targetIdx]
		for relPath, content := range target.Files {
			absPath := filepath.Join(s.RootDir, filepath.FromSlash(relPath))
			if content == "" {
				_ = os.Remove(absPath)
			} else {
				_ = os.MkdirAll(filepath.Dir(absPath), 0o755)
				if err := os.WriteFile(absPath, []byte(content), 0o600); err != nil {
					return popped, fmt.Errorf("restoring %s: %w", relPath, err)
				}
			}
		}
	}

	path := getStackFilePath(s.RootDir)
	data, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(path, data, 0o600)

	return popped, nil
}

// RenderStackDiagram renders an ASCII visual diagram of the snapshot stack.
func (s *DiffStack) RenderStackDiagram() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var buf bytes.Buffer

	if len(s.Frames) == 0 {
		return "Stack is empty. Use 'vortex stack push [files...]' to capture snapshots.\n"
	}

	fmt.Fprintf(&buf, "=== Vortex Diff Comparison Stack (%d frames) ===\n", len(s.Frames))
	fmt.Fprintf(&buf, "TOP\n")

	for i, f := range slices.Backward(s.Frames) {
		prefix := "├──"
		if i == 0 {
			prefix = "└──"
		}

		tagStr := ""
		if len(f.Tags) > 0 {
			tagStr = fmt.Sprintf(" [%s]", strings.Join(f.Tags, ", "))
		}

		baseSuffix := ""
		if i == 0 {
			baseSuffix = " [BASE]"
		} else if i == len(s.Frames)-1 {
			baseSuffix = " [CURRENT HEAD]"
		}

		timeStr := f.CreatedAt.Format("15:04:05")
		fmt.Fprintf(&buf, " %s [#%d] %s%s (%s) - %s%s\n",
			prefix, f.Index, f.Label, tagStr, timeStr, f.Summary, baseSuffix)
	}

	return buf.String()
}
