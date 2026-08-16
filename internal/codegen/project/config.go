// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package project implements workspace configuration discovery, .vortex.yml parsing, and contract status analysis.
package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

// ConfigFileName is the primary project configuration filename.
const ConfigFileName = ".vortex.yml"

// Config represents the complete .vortex.yml configuration schema.
type Config struct {
	Version   int              `yaml:"version"`
	Defaults  DefaultsConfig   `yaml:"defaults,omitempty"`
	Contracts []ContractConfig `yaml:"contracts"`
	Lint      LintConfig       `yaml:"lint,omitempty"`
	Export    ExportConfig     `yaml:"export,omitempty"`
	Coverage  CoverageConfig   `yaml:"coverage,omitempty"`

	// Runtime metadata
	RootDir    string `yaml:"-"`
	ConfigPath string `yaml:"-"`
}

// CoverageConfig controls test coverage profile analysis rules in vortex cover.
type CoverageConfig struct {
	File    string   `yaml:"file,omitempty"`
	Min     float64  `yaml:"min,omitempty"`
	Sort    string   `yaml:"sort,omitempty"` // "percent" or "name"
	Exclude []string `yaml:"exclude,omitempty"`
}

// DefaultsConfig defines global defaults for code generation.
type DefaultsConfig struct {
	Casing  string `yaml:"casing,omitempty"`
	Engine  string `yaml:"engine,omitempty"`
	Persona string `yaml:"persona,omitempty"`
	TLSSpec string `yaml:"tlsspec,omitempty"`
}

// ContractConfig describes a single service contract definition within the workspace.
type ContractConfig struct {
	Name     string          `yaml:"name"`
	Package  string          `yaml:"package,omitempty"`
	File     string          `yaml:"file"`
	Gen      string          `yaml:"gen,omitempty"`
	Models   string          `yaml:"models,omitempty"`
	Harness  string          `yaml:"harness,omitempty"`
	Upstream *UpstreamConfig `yaml:"upstream,omitempty"`
	Plugins  []PluginConfig  `yaml:"plugins,omitempty"`
}

// UpstreamConfig links a Go contract to an external OpenAPI/Swagger schema source.
type UpstreamConfig struct {
	Source  string `yaml:"source"`
	Format  string `yaml:"format,omitempty"` // "openapi", "swagger", "har"
	Refresh string `yaml:"refresh,omitempty"`
}

// PluginConfig specifies code generation targets for polyglot platforms (e.g. TypeScript, C, Swift).
type PluginConfig struct {
	Name string `yaml:"name"`
	Out  string `yaml:"out"`
}

// LintConfig controls static analysis rules and severity thresholds.
type LintConfig struct {
	Strict  bool                `yaml:"strict,omitempty"`
	Disable []string            `yaml:"disable,omitempty"`
	Enable  []string            `yaml:"enable,omitempty"`
	Rules   map[string]RuleOpts `yaml:"rules,omitempty"`
}

// RuleOpts defines per-rule configuration overrides.
type RuleOpts struct {
	MaxBytes int `yaml:"max_bytes,omitempty"`
}

// ExportConfig defines defaults for external OpenAPI schema exports.
type ExportConfig struct {
	OpenAPI struct {
		Out     string `yaml:"out,omitempty"`
		Version string `yaml:"version,omitempty"`
	} `yaml:"openapi,omitempty"`
}

// FindRoot traverses upward from startDir to discover the repository root.
// Search precedence: .vortex.yml -> vortex.yaml -> .git -> go.mod.
func FindRoot(startDir string) (rootDir, configPath string, err error) {
	curr, err := filepath.Abs(startDir)
	if err != nil {
		curr = startDir
	}

	var fallbackRoot string

	for {
		// 1. Check for explicit .vortex.yml or vortex.yaml
		for _, name := range []string{".vortex.yml", "vortex.yaml", ".vortex.yaml"} {
			cfgFile := filepath.Join(curr, name)
			if info, err := os.Stat(cfgFile); err == nil && !info.IsDir() {
				return curr, cfgFile, nil
			}
		}

		// 2. Remember go.mod or .git as workspace boundary
		if fallbackRoot == "" {
			for _, boundary := range []string{"go.mod", ".git"} {
				bPath := filepath.Join(curr, boundary)
				if _, err := os.Stat(bPath); err == nil {
					fallbackRoot = curr
					break
				}
			}
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}

		curr = parent
	}

	if fallbackRoot != "" {
		return fallbackRoot, "", nil
	}

	absStart, _ := filepath.Abs(startDir)

	return absStart, "", nil
}

// Load loads and parses .vortex.yml from the workspace root or performs auto-discovery.
func Load(startDir string) (*Config, error) {
	rootDir, configPath, err := FindRoot(startDir)
	if err != nil {
		return nil, fmt.Errorf("discovering workspace root: %w", err)
	}

	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", configPath, err)
		}

		var cfg Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", configPath, err)
		}

		cfg.RootDir = rootDir

		cfg.ConfigPath = configPath
		if cfg.Version == 0 {
			cfg.Version = 1
		}

		return &cfg, nil
	}

	// Zero-Config Auto-Discovery
	return AutoDiscover(rootDir)
}

// AutoDiscover scans the workspace directory tree and builds a virtual Config.
func AutoDiscover(rootDir string) (*Config, error) {
	cfg := &Config{
		Version:   1,
		RootDir:   rootDir,
		Contracts: make([]ContractConfig, 0),
		Defaults: DefaultsConfig{
			Casing: "snake_case",
			Engine: "fast",
		},
	}

	p := parser.NewParser()

	_ = filepath.WalkDir(rootDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil {
			return filepath.SkipDir
		}

		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" || name == ".system_generated" ||
				name == "testdata" {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), ".gen.go") ||
			strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		root, _ := p.ParseFile(path)
		if root == nil || len(root.Services) == 0 {
			return nil
		}

		relFile, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			relFile = path
		}

		dir := filepath.Dir(relFile)
		baseName := filepath.Base(relFile)
		genName := strings.TrimSuffix(baseName, ".go") + ".gen.go"
		genPath := filepath.Join(dir, genName)

		modelsPath := ""
		if _, statErr := os.Stat(filepath.Join(rootDir, dir, "models.gen.go")); statErr == nil {
			modelsPath = filepath.Join(dir, "models.gen.go")
		}

		for _, s := range root.Services {
			var upstream *UpstreamConfig
			if s.Source != "" {
				upstream = &UpstreamConfig{
					Source: s.Source,
					Format: "openapi",
				}
			}

			cfg.Contracts = append(cfg.Contracts, ContractConfig{
				Name:     s.Name,
				Package:  root.PackageName,
				File:     filepath.ToSlash(relFile),
				Gen:      filepath.ToSlash(genPath),
				Models:   filepath.ToSlash(modelsPath),
				Upstream: upstream,
			})
		}

		return nil
	})

	return cfg, nil
}

// Init creates a fresh .vortex.yml file in rootDir based on auto-discovered workspace contracts.
func Init(rootDir string, force bool) (*Config, error) {
	configPath := filepath.Join(rootDir, ConfigFileName)
	if !force {
		if _, err := os.Stat(configPath); err == nil {
			return nil, errors.New(".vortex.yml already exists (use --force to overwrite)")
		}
	}

	cfg, err := AutoDiscover(rootDir)
	if err != nil {
		return nil, fmt.Errorf("auto-discovering services: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling configuration: %w", err)
	}

	header := []byte(
		"# .vortex.yml — Vortex API Guardian Workspace Configuration\n# Documentation: https://github.com/lemon4ksan/aoni\n\n",
	)
	header = append(header, data...)

	if err := os.WriteFile(configPath, header, 0o600); err != nil {
		return nil, fmt.Errorf("writing %s: %w", configPath, err)
	}

	cfg.ConfigPath = configPath

	return cfg, nil
}

// FilterContext filters contracts relevant to targetDir.
// If targetDir is the rootDir, returns all contracts unless specific target names are given.
func (c *Config) FilterContext(targetDir string, targetNames ...string) []ContractConfig {
	if len(targetNames) > 0 {
		var filtered []ContractConfig
		for _, name := range targetNames {
			for _, ct := range c.Contracts {
				if strings.EqualFold(ct.Name, name) || strings.EqualFold(ct.Package, name) {
					filtered = append(filtered, ct)
				}
			}
		}

		if len(filtered) > 0 {
			return filtered
		}
	}

	if targetDir == "" || targetDir == "." || filepath.Clean(targetDir) == filepath.Clean(c.RootDir) {
		return c.Contracts
	}

	relTarget, err := filepath.Rel(c.RootDir, targetDir)
	if err != nil {
		relTarget = targetDir
	}

	relTarget = filepath.ToSlash(relTarget)

	var matched []ContractConfig
	for _, ct := range c.Contracts {
		contractDir := filepath.ToSlash(filepath.Dir(ct.File))
		if contractDir == relTarget || strings.HasPrefix(contractDir, relTarget+"/") ||
			strings.HasPrefix(relTarget, contractDir+"/") {
			matched = append(matched, ct)
		}
	}

	if len(matched) == 0 {
		return c.Contracts
	}

	return matched
}
