// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultTimeout defines the execution deadline for in-memory git commands.
const DefaultTimeout = 5 * time.Second

// CommitInfo records commit metadata from git log.
type CommitInfo struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

// BranchProposal represents an active consumer feature branch proposing contract changes.
type BranchProposal struct {
	Name       string `json:"name"`
	Author     string `json:"author"`
	Date       string `json:"date"`
	IsRemote   bool   `json:"is_remote"`
	RemoteName string `json:"remote_name,omitempty"`
}

// ShowFile retrieves the exact byte content of a file at a specific git ref into memory.
// Zero temporary files are written to disk.
func ShowFile(ctx context.Context, rootDir, ref, relPath string) ([]byte, error) {
	if ctx == nil {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(context.Background(), DefaultTimeout)
		defer cancel()
	}

	// Normalize relative path with forward slashes for Git
	cleanRelPath := filepath.ToSlash(filepath.Clean(relPath))
	cleanRelPath = strings.TrimPrefix(cleanRelPath, "./")
	cleanRelPath = strings.TrimPrefix(cleanRelPath, "/")

	target := fmt.Sprintf("%s:%s", ref, cleanRelPath)

	cmd := exec.CommandContext(ctx, "git", "show", target)
	cmd.Dir = rootDir

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git show %s failed: %s (err: %w)", target, strings.TrimSpace(stderr.String()), err)
	}

	return stdout.Bytes(), nil
}

// MergeBase finds the best common ancestor commit between two git references.
func MergeBase(ctx context.Context, rootDir, refA, refB string) (string, error) {
	if ctx == nil {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(context.Background(), DefaultTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "git", "merge-base", refA, refB)
	cmd.Dir = rootDir

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf(
			"git merge-base %s %s failed: %s (err: %w)",
			refA,
			refB,
			strings.TrimSpace(stderr.String()),
			err,
		)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// ListProposalBranches queries local and remote branches matching consumer proposal patterns.
func ListProposalBranches(ctx context.Context, rootDir string, prefixes []string) ([]BranchProposal, error) {
	if ctx == nil {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(context.Background(), DefaultTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(
		ctx,
		"git",
		"branch",
		"-a",
		"--format=%(refname:short)|%(authordate:relative)|%(authorname)",
	)
	cmd.Dir = rootDir

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git branch failed: %s (err: %w)", strings.TrimSpace(stderr.String()), err)
	}

	if len(prefixes) == 0 {
		prefixes = []string{
			"feat/", "feature/", "ios/", "swift/", "web/", "ts/", "android/", "kotlin/",
			"origin/feat/", "origin/feature/", "origin/ios/", "origin/swift/", "origin/web/", "origin/ts/",
		}
	}

	var proposals []BranchProposal

	lines := strings.Split(stdout.String(), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "HEAD") {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}

		name := strings.TrimSpace(parts[0])
		date := strings.TrimSpace(parts[1])
		author := strings.TrimSpace(parts[2])

		matches := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(name, prefix) {
				matches = true
				break
			}
		}

		if !matches {
			continue
		}

		isRemote := strings.HasPrefix(name, "origin/") || strings.Contains(name, "/")

		remoteName := ""
		if isRemote {
			slashIdx := strings.Index(name, "/")
			if slashIdx != -1 {
				remoteName = name[:slashIdx]
			}
		}

		proposals = append(proposals, BranchProposal{
			Name:       name,
			Author:     author,
			Date:       date,
			IsRemote:   isRemote,
			RemoteName: remoteName,
		})
	}

	return proposals, nil
}

// LogCommits retrieves the recent commit history for a specific file from git log.
func LogCommits(ctx context.Context, rootDir, relPath string, limit int) ([]CommitInfo, error) {
	if ctx == nil {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(context.Background(), DefaultTimeout)
		defer cancel()
	}

	if limit <= 0 {
		limit = 10
	}

	cleanRelPath := filepath.ToSlash(filepath.Clean(relPath))
	cleanRelPath = strings.TrimPrefix(cleanRelPath, "./")
	cleanRelPath = strings.TrimPrefix(cleanRelPath, "/")

	args := []string{
		"log",
		"-n", strconv.Itoa(limit),
		"--format=%H|%an|%ad|%s",
		"--date=short",
		"--",
		cleanRelPath,
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = rootDir

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git log failed: %s (err: %w)", strings.TrimSpace(stderr.String()), err)
	}

	var commits []CommitInfo

	lines := strings.Split(stdout.String(), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}

		commits = append(commits, CommitInfo{
			Hash:    strings.TrimSpace(parts[0]),
			Author:  strings.TrimSpace(parts[1]),
			Date:    strings.TrimSpace(parts[2]),
			Subject: strings.TrimSpace(parts[3]),
		})
	}

	return commits, nil
}

// CurrentBranch returns the name of the currently checked-out branch.
func CurrentBranch(ctx context.Context, rootDir string) (string, error) {
	if ctx == nil {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(context.Background(), DefaultTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = rootDir

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed: %s (err: %w)", strings.TrimSpace(stderr.String()), err)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// IsClean checks whether the working tree has no uncommitted changes for a specific path (or entire tree if empty).
func IsClean(ctx context.Context, rootDir, relPath string) (bool, error) {
	if ctx == nil {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(context.Background(), DefaultTimeout)
		defer cancel()
	}

	args := []string{"status", "--porcelain"}
	if relPath != "" {
		cleanRelPath := filepath.ToSlash(filepath.Clean(relPath))
		args = append(args, "--", cleanRelPath)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = rootDir

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("git status failed: %s (err: %w)", strings.TrimSpace(stderr.String()), err)
	}

	return strings.TrimSpace(stdout.String()) == "", nil
}

// RootDir resolves the top-level directory of the current Git repository.
func RootDir(ctx context.Context, startDir string) (string, error) {
	if ctx == nil {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(context.Background(), DefaultTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = startDir

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", errors.New("not a git repository (or any parent directory)")
	}

	return filepath.Clean(strings.TrimSpace(stdout.String())), nil
}

// BlameLine holds blame information for a single source line.
type BlameLine struct {
	LineNumber int    `json:"line_number"`
	Commit     string `json:"commit"`
	Author     string `json:"author"`
	AuthorMail string `json:"author_mail"`
	Date       string `json:"date"`
	Summary    string `json:"summary"`
}

// BlameFile runs git blame in porcelain mode and returns line-by-line provenance.
func BlameFile(ctx context.Context, rootDir, relPath string) (map[int]BlameLine, error) {
	if ctx == nil {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(context.Background(), DefaultTimeout)
		defer cancel()
	}

	cleanRelPath := filepath.ToSlash(filepath.Clean(relPath))
	cmd := exec.CommandContext(ctx, "git", "blame", "--line-porcelain", cleanRelPath)
	cmd.Dir = rootDir

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Not tracked in git or working tree dirty
		return map[int]BlameLine{}, nil
	}

	result := make(map[int]BlameLine)
	lines := strings.Split(stdout.String(), "\n")

	var (
		currentHash, author, mail, dateStr, summary string
		finalLine                                   int
	)

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		if strings.HasPrefix(line, "\t") {
			// Tab prefix indicates the actual source line content.
			// This concludes the current porcelain entry.
			if finalLine > 0 {
				shortHash := currentHash
				if len(shortHash) > 7 {
					shortHash = shortHash[:7]
				}

				result[finalLine] = BlameLine{
					LineNumber: finalLine,
					Commit:     shortHash,
					Author:     author,
					AuthorMail: mail,
					Date:       dateStr,
					Summary:    summary,
				}
			}

			finalLine = 0

			continue
		}

		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 0 {
			continue
		}

		// First line of header is: <40-char-hash> <origLine> <finalLine> [<groupLines>]
		if len(parts[0]) == 40 && len(parts) >= 2 {
			currentHash = parts[0]

			fields := strings.Fields(parts[1])
			if len(fields) >= 2 {
				if l, err := strconv.Atoi(fields[1]); err == nil {
					finalLine = l
				}
			}

			continue
		}

		switch parts[0] {
		case "author":
			if len(parts) > 1 {
				author = parts[1]
			}
		case "author-mail":
			if len(parts) > 1 {
				mail = strings.Trim(parts[1], "<>")
			}
		case "author-time":
			if len(parts) > 1 {
				if sec, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
					dateStr = time.Unix(sec, 0).Format("2006-01-02")
				}
			}

		case "summary":
			if len(parts) > 1 {
				summary = parts[1]
			}
		}
	}

	return result, nil
}
