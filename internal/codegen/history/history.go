// Copyright 2026 The Aoni Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package history

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/internal/codegen/builder"
)

// OpEntry records a single modifying AST or configuration operation.
type OpEntry struct {
	ID        string            `json:"id"`
	Command   string            `json:"command"`
	CreatedAt time.Time         `json:"created_at"`
	Files     []string          `json:"files"`
	Snapshots map[string]string `json:"snapshots"` // relPath -> original file content
	Undone    bool              `json:"undone"`
}

// Journal represents the persistent operation history in .vortex/history/journal.json.
type Journal struct {
	Entries []OpEntry `json:"entries"`
}

const maxJournalEntries = 50

// Record takes a pre-flight snapshot of files before modifying them.
func Record(rootDir, command string, targetFiles []string) (*OpEntry, error) {
	if rootDir == "" {
		return nil, errors.New("empty rootDir for history recording")
	}

	snapshots := make(map[string]string)

	var relFiles []string

	for _, f := range targetFiles {
		absPath := f
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(rootDir, absPath)
		}

		relPath, relErr := filepath.Rel(rootDir, absPath)
		if relErr != nil {
			relPath = absPath
		}

		relFiles = append(relFiles, filepath.ToSlash(relPath))

		if data, err := os.ReadFile(absPath); err == nil {
			snapshots[filepath.ToSlash(relPath)] = string(data)
		} else {
			// If file didn't exist before, record empty marker
			snapshots[filepath.ToSlash(relPath)] = ""
		}
	}

	id := generateOpID()
	entry := OpEntry{
		ID:        id,
		Command:   command,
		CreatedAt: time.Now(),
		Files:     relFiles,
		Snapshots: snapshots,
		Undone:    false,
	}

	journal, _ := loadJournal(rootDir)
	journal.Entries = append(journal.Entries, entry)

	// Keep only latest maxJournalEntries
	if len(journal.Entries) > maxJournalEntries {
		journal.Entries = journal.Entries[len(journal.Entries)-maxJournalEntries:]
	}

	if err := saveJournal(rootDir, journal); err != nil {
		return nil, err
	}

	return &entry, nil
}

// List returns the full operation history sorted newest first.
func List(rootDir string) ([]OpEntry, error) {
	journal, err := loadJournal(rootDir)
	if err != nil {
		return nil, err
	}

	entries := slices.Clone(journal.Entries)
	slices.SortFunc(entries, func(a, b OpEntry) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})

	return entries, nil
}

// Undo restores files from the latest operation (or a specific opID) and regenerates clients.
func Undo(ctx context.Context, rootDir, opID string) (*OpEntry, error) {
	journal, err := loadJournal(rootDir)
	if err != nil {
		return nil, err
	}

	if len(journal.Entries) == 0 {
		return nil, errors.New("no history operations found to undo")
	}

	targetIdx := -1
	if opID == "" {
		// Find last active (non-undone) operation
		for i := len(journal.Entries) - 1; i >= 0; i-- {
			if !journal.Entries[i].Undone {
				targetIdx = i
				break
			}
		}
	} else {
		for i, e := range journal.Entries {
			if strings.EqualFold(e.ID, opID) {
				targetIdx = i
				break
			}
		}
	}

	if targetIdx == -1 {
		return nil, fmt.Errorf("no reversible operation found matching %q", opID)
	}

	entry := &journal.Entries[targetIdx]

	var restoredFiles []string
	for relPath, content := range entry.Snapshots {
		absPath := filepath.Join(rootDir, filepath.FromSlash(relPath))
		if content == "" {
			// File didn't exist before operation, remove it
			_ = os.Remove(absPath)
		} else {
			if err := os.MkdirAll(filepath.Dir(absPath), 0o750); err != nil {
				return nil, err
			}

			if err := os.WriteFile(absPath, []byte(content), 0o600); err != nil {
				return nil, fmt.Errorf("restoring %s: %w", relPath, err)
			}
		}

		if strings.HasSuffix(relPath, ".go") && !strings.HasSuffix(relPath, ".gen.go") {
			restoredFiles = append(restoredFiles, absPath)
		}
	}

	entry.Undone = true
	_ = saveJournal(rootDir, journal)

	// Re-generate API clients for restored contracts
	if len(restoredFiles) > 0 {
		b := builder.New(builder.Config{})
		_, _ = b.BuildFiles(ctx, restoredFiles)
	}

	return entry, nil
}

func loadJournal(rootDir string) (*Journal, error) {
	journalPath := filepath.Join(rootDir, ".vortex", "history", "journal.json")

	data, err := os.ReadFile(journalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Journal{}, nil
		}

		return nil, err
	}

	var j Journal
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("parsing journal: %w", err)
	}

	return &j, nil
}

func saveJournal(rootDir string, j *Journal) error {
	dir := filepath.Join(rootDir, ".vortex", "history")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, "journal.json"), data, 0o600)
}

func generateOpID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return "op-" + hex.EncodeToString(b)
}
