// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package base

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/builder"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/project"
)

// ContractTarget represents a resolved API contract within the workspace.
type ContractTarget struct {
	Name        string                  // Canonical contract name (e.g. "MakerSuiteAPI")
	FilePath    string                  // Absolute or relative file path to the Go contract
	AbsPath     string                  // Absolute file path
	Package     string                  // Go package name (e.g. "agy")
	Service     string                  // Target service interface name (e.g. "MakerSuiteAPI")
	ConfigEntry *project.ContractConfig // Config entry from .vortex.yml if matched
}

// Runtime encapsulates workspace root discovery, configuration loading, and target resolution.
type Runtime struct {
	RootDir string
	Config  *project.Config
}

// NewRuntime initializes a workspace runtime. If targetDir is empty, it discovers the root from cwd.
func NewRuntime(targetDir string) (*Runtime, error) {
	if targetDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}

		rootDir, _, _ := project.FindRoot(cwd)
		targetDir = rootDir
	}

	cfg, _ := project.Load(targetDir)

	return &Runtime{
		RootDir: targetDir,
		Config:  cfg,
	}, nil
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}

	for i, r := range s {
		if i == 0 && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '_' {
			return false
		}

		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}

	return true
}

// ResolveContract resolves a symbolic contract name (e.g. "MakerSuiteAPI") or a file path
// (e.g. "pkg/agy/makersuite.go") into a canonical ContractTarget.
func (r *Runtime) ResolveContract(target string) (*ContractTarget, error) {
	target = strings.TrimSpace(target)

	// Case 1: Target matches an entry in .vortex.yml
	if r.Config != nil {
		if ct := r.Config.FindContract(target); ct != nil {
			absPath := ct.File
			if !filepath.IsAbs(absPath) && r.RootDir != "" {
				absPath = filepath.Join(r.RootDir, ct.File)
			}

			return &ContractTarget{
				Name:        ct.Name,
				FilePath:    ct.File,
				AbsPath:     absPath,
				Package:     ct.Package,
				Service:     ct.Name,
				ConfigEntry: ct,
			}, nil
		}
	}

	// Case 2: Empty target with a single registered contract in .vortex.yml
	if target == "" && r.Config != nil && len(r.Config.Contracts) == 1 {
		ct := &r.Config.Contracts[0]

		absPath := ct.File
		if !filepath.IsAbs(absPath) && r.RootDir != "" {
			absPath = filepath.Join(r.RootDir, ct.File)
		}

		return &ContractTarget{
			Name:        ct.Name,
			FilePath:    ct.File,
			AbsPath:     absPath,
			Package:     ct.Package,
			Service:     ct.Name,
			ConfigEntry: ct,
		}, nil
	}

	// Case 3: Target is a file path or partial path
	if target != "" {
		resolvedPath := target
		if r.Config != nil && r.RootDir != "" && !filepath.IsAbs(resolvedPath) {
			candidate := filepath.Join(r.RootDir, resolvedPath)
			if _, err := os.Stat(candidate); err == nil {
				resolvedPath = candidate
			}
		}

		if ext := filepath.Ext(resolvedPath); ext == "" && resolvedPath != "-" {
			resolvedPath += ".go"
		}

		absPath := resolvedPath
		if !filepath.IsAbs(absPath) && r.RootDir != "" {
			absPath = filepath.Join(r.RootDir, resolvedPath)
		}

		pkgName := "api"

		dir := filepath.Dir(absPath)
		if dir != "" && dir != "." {
			baseDir := filepath.Base(dir)
			if baseDir != "" && baseDir != "." && baseDir != "pkg" && baseDir != "internal" {
				if isIdent(baseDir) {
					pkgName = baseDir
				}
			}
		}

		serviceName := "API"
		baseName := filepath.Base(resolvedPath)

		baseName = strings.TrimSuffix(baseName, filepath.Ext(baseName))
		if baseName != "" && baseName != "api" {
			serviceName = strings.ToUpper(baseName[:1]) + baseName[1:]
			if !strings.HasSuffix(serviceName, "API") {
				serviceName += "API"
			}
		}

		return &ContractTarget{
			Name:     serviceName,
			FilePath: resolvedPath,
			AbsPath:  absPath,
			Package:  pkgName,
			Service:  serviceName,
		}, nil
	}

	return nil, errors.New("contract target is required (e.g. specify contract name or Go file)")
}

// CollectFiles gathers Go contract files from paths or workspace patterns.
func (r *Runtime) CollectFiles(patterns []string) []string {
	if len(patterns) == 0 {
		if r.Config != nil && len(r.Config.Contracts) > 0 {
			files := make([]string, 0, len(r.Config.Contracts))
			for _, ct := range r.Config.Contracts {
				p := ct.File
				if !filepath.IsAbs(p) && r.RootDir != "" {
					p = filepath.Join(r.RootDir, p)
				}

				files = append(files, p)
			}

			return files
		}

		patterns = []string{"./..."}
	}

	return builder.CollectInputFiles("", patterns, builder.CollectOptions{})
}
